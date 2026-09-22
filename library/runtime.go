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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/api"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
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
type Config struct {
	ID             string
	DatabaseURL    string
	Objects        BlobStore
	Authenticate   Authenticator
	Authorize      Policy
	Launcher       Launcher
	MaxConnections int
}

type Runtime struct {
	cfg     Config
	db      *sql.DB
	objects BlobStore
	mu      sync.Mutex
	closed  bool
	active  sync.WaitGroup
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
	failed = false
	return &Runtime{cfg: cfg, db: db, objects: objects}, nil
}

func (r *Runtime) Close() error {
	r.mu.Lock()
	r.closed = true
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
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		action = Read
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
	mux := http.NewServeMux()
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

func (r *admittedReader) Close() error {
	r.once.Do(func() { r.err = r.ReadCloser.Close(); r.done() })
	return r.err
}
