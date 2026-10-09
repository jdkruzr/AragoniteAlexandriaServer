// Package library exposes a single-library runtime. It has no knowledge of
// customer directories, payment providers, or multi-tenant host routing.
package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/host"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/mcptools"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/oauth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/reader"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/readersearch"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskhost"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/api"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/boox"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/database"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/jobs"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/tasks"
	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
)

type BlobStore = blob.Store
type BlobInfo = blob.Info
type S3Config = blob.S3Config
type Job = jobs.Job
type Launcher = jobs.Launcher
type JobHandler func(context.Context, Job, BlobStore) error
type Action string

const (
	Read    Action = "read"
	Write   Action = "write"
	Process Action = "process"
	Export  Action = "export"
)

var ErrUnavailable = errors.New("library unavailable")
var ErrReadOnly = errors.New("library is read-only")

// Authenticator must validate the request and bind it to the supplied library.
// The standalone default uses credentials stored in that library's database.
type Authenticator func(context.Context, *http.Request, string) error
type Policy func(context.Context, string, Action) error
type NativeBOOXConfig = boox.Config

type Config struct {
	NativeBOOX   *NativeBOOXConfig
	ID           string
	DatabaseURL  string
	Objects      BlobStore
	Authenticate Authenticator
	// AuthenticateAccount approves device enrollment with the library owner's
	// account. Without it the local account (HTTP Basic) is used, unless a
	// custom Authenticate is set, in which case enrollment fails closed.
	AuthenticateAccount Authenticator
	// Pages configures the notebook page pipeline (recognition, embeddings).
	// The zero value indexes text boxes and device text only.
	Pages notes.Pipeline
	// PublicURL is the externally reachable base URL ("" = relative links).
	PublicURL      string
	Authorize      Policy
	Launcher       Launcher
	MaxConnections int
}

type Runtime struct {
	cfg     Config
	db      *sql.DB
	objects BlobStore
	sync    *host.Host
	// attachSecret signs public task-attachment URLs; stable across restarts.
	attachSecret  string
	mu            sync.Mutex
	closed        bool
	nativeContext context.Context
	nativeCancel  context.CancelFunc
	active        sync.WaitGroup
}

func NewS3(ctx context.Context, cfg S3Config) (BlobStore, error) { return blob.NewS3(ctx, cfg) }
func Migrations() schema.Runner                                  { return database.Migrator() }

// Initialize is a management operation, not a runtime startup side effect.
func Initialize(ctx context.Context, db *sql.DB, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed == uuid.Nil || parsed.String() != id {
		return errors.New("invalid library ID")
	}
	if err := database.Ready(ctx, db); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO alexandria_library_runtime(singleton,library_id) VALUES(true,$1) ON CONFLICT(singleton) DO NOTHING`, id)
	if err != nil {
		return err
	}
	var actual string
	if err = db.QueryRowContext(ctx, `SELECT library_id::text FROM alexandria_library_runtime WHERE singleton`).Scan(&actual); err != nil {
		return err
	}
	if actual != id {
		return errors.New("database belongs to a different library")
	}
	return nil
}

func Open(ctx context.Context, cfg Config) (*Runtime, error) {
	if cfg.NativeBOOX != nil {
		if err := cfg.NativeBOOX.Validate(); err != nil {
			return nil, err
		}
	}
	objects, err := blob.ForLibrary(cfg.Objects, cfg.ID)
	if err != nil {
		return nil, err
	}
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			db.Close()
		}
	}()
	if cfg.MaxConnections == 0 {
		cfg.MaxConnections = 4
	}
	if cfg.MaxConnections < 1 || cfg.MaxConnections > 64 {
		return nil, errors.New("invalid pool limit")
	}
	db.SetMaxOpenConns(cfg.MaxConnections)
	db.SetMaxIdleConns(0)
	db.SetConnMaxLifetime(15 * time.Minute)
	if err := database.Ready(ctx, db); err != nil {
		return nil, err
	}
	var id string
	if err := db.QueryRowContext(ctx, `SELECT library_id::text FROM alexandria_library_runtime WHERE singleton`).Scan(&id); err != nil {
		return nil, err
	}
	if id != cfg.ID {
		return nil, errors.New("database belongs to a different library")
	}
	if cfg.Launcher == nil {
		cfg.Launcher = jobs.LocalLauncher{}
	}
	// The server's authoring site and first library generation are plain DML,
	// idempotent, and survive restarts.
	if err := identity.EnsureSite(ctx, db); err != nil {
		return nil, err
	}
	if err := generation.Ensure(ctx, db); err != nil {
		return nil, err
	}
	protocol, err := host.New()
	if err != nil {
		return nil, err
	}
	secret, err := settings.EnsureSecret(ctx, db, settings.TaskAttachSecret)
	if err != nil {
		return nil, err
	}
	nativeContext, nativeCancel := context.WithCancel(context.Background())
	failed = false
	return &Runtime{nativeContext: nativeContext, nativeCancel: nativeCancel, cfg: cfg, db: db, objects: objects, sync: protocol, attachSecret: secret}, nil
}

func (r *Runtime) Close() error {
	r.mu.Lock()
	r.closed = true
	if r.nativeCancel != nil {
		r.nativeCancel()
	}
	r.mu.Unlock()
	r.active.Wait()
	return r.db.Close()
}

func (r *Runtime) admit(ctx context.Context, action Action) (*sql.Conn, func(), error) {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil, nil, ErrUnavailable
	}
	r.active.Add(1)
	r.mu.Unlock()
	conn, err := r.db.Conn(ctx)
	if err != nil {
		r.active.Done()
		return nil, nil, err
	}
	var acquired bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock_shared($1)`, schema.LockID).Scan(&acquired); err != nil || !acquired {
		conn.Close()
		r.active.Done()
		return nil, nil, ErrUnavailable
	}
	done := func() { schema.ReleaseLock(conn, true); r.active.Done() }
	var mode string
	if err = conn.QueryRowContext(ctx, `SELECT mode FROM alexandria_library_runtime WHERE singleton AND library_id=$1`, r.cfg.ID).Scan(&mode); err != nil {
		done()
		return nil, nil, err
	}
	if mode == "maintenance" || mode == "suspended" {
		done()
		return nil, nil, ErrUnavailable
	}
	if mode == "read_only" && (action == Write || action == Process) {
		done()
		return nil, nil, ErrReadOnly
	}
	// Recheck schema AFTER admission, including a migration by another process.
	if err = Migrations().CheckConnection(ctx, conn); err != nil {
		done()
		return nil, nil, err
	}
	if r.cfg.Authorize != nil {
		if err = r.cfg.Authorize(ctx, r.cfg.ID, action); err != nil {
			done()
			return nil, nil, err
		}
	}
	return conn, done, nil
}

func (r *Runtime) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	action := Write
	switch req.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, "PROPFIND", "REPORT":
		action = Read
	}
	if r.cfg.NativeBOOX != nil && boox.RequiresWrite(req) {
		action = Write
	}
	conn, done, err := r.admit(req.Context(), action)
	if err != nil {
		code := http.StatusServiceUnavailable
		if errors.Is(err, ErrReadOnly) {
			code = http.StatusForbidden
		} else {
			w.Header().Set("Retry-After", "30")
		}
		http.Error(w, http.StatusText(code), code)
		return
	}
	defer done()
	if r.cfg.NativeBOOX != nil {
		native := boox.Service{Config: *r.cfg.NativeBOOX, DB: conn, Objects: r.objects, LibraryID: r.cfg.ID}
		if err := native.ResolveIdentity(req.Context()); err != nil {
			http.Error(w, "native identity unavailable", http.StatusServiceUnavailable)
			return
		}
		if native.Owns(req) {
			ctx, cancel := context.WithCancel(req.Context())
			stop := context.AfterFunc(r.nativeContext, cancel)
			defer func() { stop(); cancel() }()
			native.ServeHTTP(w, req.WithContext(ctx))
			return
		}
		if req.URL.Path == "/boox" || strings.HasPrefix(req.URL.Path, "/boox/") || strings.HasPrefix(req.URL.Path, "/api/v1/boox/admin/") {
			if err := r.account(conn)(req); err != nil {
				if req.URL.Path == "/boox" || strings.HasPrefix(req.URL.Path, "/boox/") {
					w.Header().Set("WWW-Authenticate", `Basic realm="Aragonite Alexandria Server"`)
				}
				http.Error(w, "unauthorized", 401)
				return
			}
			native.Admin(w, req)
			return
		}
	}
	if host.Owns(req.URL.Path) {
		r.sync.Serve(host.Library{DB: conn, Objects: r.objects, Account: r.account(conn)}, w, req)
		return
	}
	taskDeps := taskhost.Deps{DB: conn, Objects: r.objects, Secret: r.attachSecret, PublicURL: r.cfg.PublicURL}
	if oauth.Public(req.URL.Path) {
		oauth.PublicHandler(conn, r.cfg.PublicURL).ServeHTTP(w, req)
		return
	}
	if (req.URL.Path == "/mcp" || strings.HasPrefix(req.URL.Path, "/mcp/")) && req.Header.Get("Authorization") == "" {
		// Lets an MCP client discover how to obtain a token (OAuth).
		oauth.Challenge(r.cfg.PublicURL, w, req)
		return
	}
	if taskhost.Owns(req.URL.Path) {
		// Public by design: CalDAV clients fetch ATTACH URLs without
		// credentials; each URL carries its own signature.
		taskhost.Attachments(taskDeps).ServeHTTP(w, req)
		return
	}
	mux := http.NewServeMux()
	searcher := notes.Searcher{DB: conn, Embedder: r.cfg.Pages.Embedder}
	mux.Handle("GET /api/v1/search", searcher.Handler())
	mcp := mcptools.Handler(mcptools.Deps{DB: conn, Search: searcher, PublicURL: r.cfg.PublicURL,
		Tasks: tasksvc.NewTaskService(taskdb.NewStore(conn), nil)})
	mux.Handle("/mcp", mcp)
	mux.Handle("/mcp/", mcp)
	mux.Handle(taskhost.Prefix+"/", taskhost.CalDAV(taskDeps))
	mux.Handle("/.well-known/caldav", taskhost.WellKnown())
	mux.Handle("/.well-known/caldav/", taskhost.WellKnown())
	status := web.Status{NativeBOOX: r.cfg.NativeBOOX != nil}
	if r.cfg.Pages.OCR != nil {
		status.OCR = r.cfg.Pages.OCR.Model()
	}
	if r.cfg.Pages.Embedder != nil {
		status.Embedding = r.cfg.Pages.Embedder.Model()
	}
	mux.Handle("/", web.Handler(web.Deps{DB: conn, Objects: r.objects, Search: searcher, PublicURL: r.cfg.PublicURL, Status: status}))
	api.Tasks{Store: tasks.NewStore(conn)}.Register(mux)
	api.Jobs{Service: jobs.Service{Store: jobs.NewStore(conn), Launcher: r.cfg.Launcher}}.Register(mux)
	if r.cfg.Authenticate != nil {
		if err := r.cfg.Authenticate(req.Context(), req, r.cfg.ID); err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, req)
		return
	}
	auth.NewStore(conn).Middleware(mux).ServeHTTP(w, req)
}

// account approves enrollment only. Bearer tokens (operator API or device
// keys) are never enrollment authority.
func (r *Runtime) account(conn *sql.Conn) identity.AccountCheck {
	return func(req *http.Request) error {
		if r.cfg.AuthenticateAccount != nil {
			return r.cfg.AuthenticateAccount(req.Context(), req, r.cfg.ID)
		}
		if r.cfg.Authenticate != nil {
			return identity.ErrAccount
		}
		username, password, ok := req.BasicAuth()
		if !ok || username == "" {
			return identity.ErrAccount
		}
		if _, err := auth.NewStore(conn).Authenticate(req.Context(), username, password, ""); err != nil {
			return identity.ErrAccount
		}
		return nil
	}
}

func (r *Runtime) RunJob(ctx context.Context, id uuid.UUID, owner string) error {
	conn, done, err := r.admit(ctx, Process)
	if err != nil {
		return err
	}
	defer done()
	runner := jobs.Runner{Store: jobs.NewStore(conn), Owner: owner, Lease: 15 * time.Minute, Handlers: map[string]jobs.Handler{
		"blob.verify": func(ctx context.Context, job jobs.Job) error {
			var p struct {
				Key  string `json:"key"`
				Size int64  `json:"size"`
			}
			if err := json.Unmarshal(job.Payload, &p); err != nil {
				return errors.New("invalid verification payload")
			}
			info, err := r.objects.Stat(ctx, p.Key)
			if err != nil {
				return errors.New("object verification failed")
			}
			if info.Size != p.Size {
				return errors.New("object length mismatch")
			}
			return nil
		},
	}}
	return runner.RunOne(ctx, id)
}

// ReadObject is admitted for the full stream lifetime, so replacement/upgrade
// cannot race a download. Its caller authenticates the owner before calling it.
func (r *Runtime) ReadObject(ctx context.Context, key string) (io.ReadCloser, BlobInfo, error) {
	_, done, err := r.admit(ctx, Export)
	if err != nil {
		return nil, BlobInfo{}, err
	}
	reader, info, err := r.objects.Get(ctx, key)
	if err != nil {
		done()
		return nil, BlobInfo{}, err
	}
	return &admittedReader{ReadCloser: reader, done: done}, info, nil
}

type admittedReader struct {
	io.ReadCloser
	done func()
	once sync.Once
	err  error
}

// WorkOnce is bounded: shared hosting schedulers can rotate libraries after each
// call, while the standalone process can simply poll its one library.
func (r *Runtime) WorkOnce(ctx context.Context, owner string) (bool, error) {
	conn, done, err := r.admit(ctx, Process)
	if err != nil {
		return false, err
	}
	var id uuid.UUID
	err = conn.QueryRowContext(ctx, `SELECT id FROM alexandria_jobs WHERE status='pending' AND available_at<=now() AND attempts<max_attempts ORDER BY available_at,created_at LIMIT 1`).Scan(&id)
	done()
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, r.RunJob(ctx, id, owner)
}

func (r *Runtime) Reconcile(ctx context.Context) (int64, error) {
	conn, done, err := r.admit(ctx, Process)
	if err != nil {
		return 0, err
	}
	defer done()
	return jobs.NewStore(conn).RequeueExpired(ctx)
}

// MaterializeReader drains pending reader rows into the reader mirrors: one
// bounded sweep (it repeats only while rows keep resolving dependencies).
func (r *Runtime) MaterializeReader(ctx context.Context) (int, error) {
	conn, done, err := r.admit(ctx, Process)
	if err != nil {
		return 0, err
	}
	defer done()
	store := reader.Store{DB: conn}
	if pending, err := store.HasPending(ctx); err != nil || !pending {
		return 0, err
	}
	return store.Sweep(ctx, 32, 64)
}

// IndexReader hands reader journal changes to annotation search and indexes
// up to max jobs (each bounded to a page of annotations).
func (r *Runtime) IndexReader(ctx context.Context, max int) (int, error) {
	conn, done, err := r.admit(ctx, Process)
	if err != nil {
		return 0, err
	}
	defer done()
	search := readersearch.New(conn)
	if _, err := search.Pump(ctx); err != nil {
		return 0, err
	}
	n := 0
	for n < max && ctx.Err() == nil {
		more, err := search.Step(ctx, 32)
		if err != nil {
			// A failed job is durable and retried later; keep the step bounded.
			return n, err
		}
		if !more {
			break
		}
		n++
	}
	return n, nil
}

// ProcessPages runs the page pipeline for up to max due pages.
func (r *Runtime) ProcessPages(ctx context.Context, max int) (int, error) {
	conn, done, err := r.admit(ctx, Process)
	if err != nil {
		return 0, err
	}
	defer done()
	n := 0
	for n < max && ctx.Err() == nil {
		found, err := r.cfg.Pages.Step(ctx, conn)
		if err != nil || !found {
			return n, err
		}
		n++
	}
	return n, nil
}

// AssetGrace is how long an unreferenced chunk object survives. It must
// exceed any plausible gap between a chunk PUT and its row commit.
const AssetGrace = 7 * 24 * time.Hour

// CollectAssets removes up to limit orphaned asset chunk objects.
func (r *Runtime) CollectAssets(ctx context.Context, limit int) (int, error) {
	conn, done, err := r.admit(ctx, Process)
	if err != nil {
		return 0, err
	}
	defer done()
	return host.Assets(host.Library{DB: conn, Objects: r.objects}).CollectGarbage(ctx, AssetGrace, limit)
}

func (r *admittedReader) Close() error {
	r.once.Do(func() { r.err = r.ReadCloser.Close(); r.done() })
	return r.err
}

// ProcessNativeBOOX runs bounded journal ingestion and one publication step.
func (r *Runtime) ProcessNativeBOOX(ctx context.Context) (int, error) {
	if r.cfg.NativeBOOX == nil {
		return 0, nil
	}
	conn, done, err := r.admit(ctx, Process)
	if err != nil {
		return 0, err
	}
	defer done()
	native := boox.Service{Config: *r.cfg.NativeBOOX, DB: conn, Objects: r.objects, LibraryID: r.cfg.ID}
	n, err := native.Observe(ctx)
	if err == nil {
		_, err = native.PublishOne(ctx)
	}
	return n, err
}
