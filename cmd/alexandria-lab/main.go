// alexandria-lab is a LOCAL headless interoperability fixture for the
// Alexandria client's Kotlin HTTP tests, not the server. It implements the
// command-line contract of UltraBridge's cmd/assetlab over the real library
// runtime: the first stdout line is the URL, EOF on stdin shuts it down, and
// it binds loopback only.
//
// --db is a marker, not a file: its absolute path names a disposable
// PostgreSQL database and S3 prefix, so a restart with the same path keeps
// state. The cluster comes from ALEXANDRIA_LAB_DATABASE_URL (an admin URL on a
// disposable cluster) and objects from ALEXANDRIA_LAB_S3_ENDPOINT. Never point
// either at a live library. Objects live under libraries/<derived id>/ in
// one shared bucket (default aragonite-alexandria-server, the test bucket):
// SeaweedFS gives each bucket its own volumes, so many buckets exhaust a
// small fixture.
//
// Fixture credentials are deliberately public and exist only here:
// readerlab Basic users reader-a/reader-b (without --reader-enrollment, mapped
// to enrolled device keys for fixed sites) and admin assetlab:assetlab.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/reader"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/library"
)

const (
	siteA = "0000000000000000000000000A"
	siteB = "0000000000000000000000000B"
)

func main() {
	path := flag.String("db", "", "disposable library marker path (required)")
	readerFlag := flag.Bool("reader", false, "reader rows with fixture credentials reader-a/reader-b")
	readerAssets := flag.Bool("reader-assets", false, "with --reader: enable assets-v1")
	enrollment := flag.Bool("reader-enrollment", false, "with --reader-assets: enrolled device keys; fixture admin assetlab:assetlab")
	restoreSync := flag.Bool("reader-restore-sync", false, "with --reader-enrollment: authoritative publication/adoption")
	checkpoint := flag.String("checkpoint", "", "gate a successful METHOD:path response before delivery")
	projection := flag.String("reader-project", "", "with --reader: drain, print annotation projection, exit")
	for _, unsupported := range []string{"reader-inspect", "reader-inventory", "metadata-only"} {
		flag.Bool(unsupported, false, "not supported by alexandria-lab")
	}
	for _, unsupported := range []string{"reader-backup", "reader-restore", "restore-id"} {
		flag.String(unsupported, "", "not supported by alexandria-lab")
	}
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "reader-inspect", "reader-inventory", "metadata-only", "reader-backup", "reader-restore", "restore-id":
			log.Fatalf("--%s reads a SQLite library file and is not supported by alexandria-lab", f.Name)
		}
	})
	if *path == "" {
		log.Fatal("--db is required")
	}
	if !*readerFlag {
		log.Fatal("alexandria-lab serves the shared-library protocol only; legacy writer-only sync needs --reader")
	}
	if (*readerAssets && !*readerFlag) || (*enrollment && !*readerAssets) || (*restoreSync && !*enrollment) {
		log.Fatal("reader-assets requires reader; enrollment requires reader-assets; restore-sync requires enrollment")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	runtime, dbURL, err := open(ctx, *path)
	if err != nil {
		log.Fatal(err)
	}
	defer runtime.Close()

	if *projection != "" {
		result, err := project(ctx, runtime, dbURL, *projection)
		if err != nil {
			log.Fatal(err)
		}
		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			log.Fatal(err)
		}
		return
	}

	var handler http.Handler = runtime
	if !*enrollment {
		handler, err = fixtureDevices(ctx, runtime, handler)
		if err != nil {
			log.Fatal(err)
		}
	}
	if !*readerAssets {
		handler = withoutAssets(handler)
	}
	if !*restoreSync {
		handler = without("/sync/restore/v1/", handler)
	}
	if *checkpoint != "" {
		handler = gateResponse(handler, *checkpoint)
	}

	// The background materializer stands in for the worker role.
	work, stopWork := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for work.Err() == nil {
			if _, err := runtime.MaterializeReader(work); err != nil && work.Err() == nil {
				log.Printf("reader worker: %v", err)
			}
			select {
			case <-work.Done():
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second}
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); _ = server.Close() }()
	go func() { <-ctx.Done(); _ = server.Close() }()
	fmt.Printf("http://%s\n", listener.Addr())
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	stopWork()
	workers.Wait()
}

// open maps the marker path to a disposable database and object prefix,
// creating and migrating them on first use.
func open(ctx context.Context, marker string) (*library.Runtime, string, error) {
	admin := os.Getenv("ALEXANDRIA_LAB_DATABASE_URL")
	endpoint := os.Getenv("ALEXANDRIA_LAB_S3_ENDPOINT")
	if admin == "" || endpoint == "" {
		return nil, "", errors.New("ALEXANDRIA_LAB_DATABASE_URL and ALEXANDRIA_LAB_S3_ENDPOINT are required (disposable fixtures only)")
	}
	abs, err := filepath.Abs(marker)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256([]byte(abs))
	name := "alexandria_lab_" + hex.EncodeToString(sum[:12])
	raw := sum[12:28]
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	id := uuid.UUID(raw).String()

	adminDB, err := sql.Open("pgx", admin)
	if err != nil {
		return nil, "", err
	}
	_, err = adminDB.ExecContext(ctx, `CREATE DATABASE "`+name+`"`)
	adminDB.Close()
	var pgErr *pgconn.PgError
	if err != nil && !(errors.As(err, &pgErr) && pgErr.Code == "42P04") {
		return nil, "", err
	}
	u, err := url.Parse(admin)
	if err != nil {
		return nil, "", err
	}
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		return nil, "", err
	}
	defer db.Close()
	if err := library.Migrations().Apply(ctx, db, nil); err != nil {
		return nil, "", err
	}
	if err := library.Initialize(ctx, db, id); err != nil {
		return nil, "", err
	}
	store, err := blob.NewS3(ctx, blob.S3Config{
		Endpoint: endpoint, Region: "us-east-1", Bucket: envOr("ALEXANDRIA_LAB_S3_BUCKET", "aragonite-alexandria-server"),
		AccessKey: envOr("ALEXANDRIA_LAB_S3_ACCESS_KEY", "alexandria-local"), SecretKey: envOr("ALEXANDRIA_LAB_S3_SECRET_KEY", "alexandria-local-secret"),
		PathStyle: true, DisableTLS: strings.HasPrefix(endpoint, "http://"),
	})
	if err != nil {
		return nil, "", err
	}
	if err := store.EnsureBucket(ctx); err != nil {
		return nil, "", err
	}
	runtime, err := library.Open(ctx, library.Config{ID: id, DatabaseURL: u.String(), Objects: store, MaxConnections: 16,
		AuthenticateAccount: func(_ context.Context, r *http.Request, _ string) error {
			if user, password, ok := r.BasicAuth(); ok && user == "assetlab" && password == "assetlab" {
				return nil
			}
			return errors.New("fixture admin required")
		}})
	return runtime, u.String(), err
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fixtureToken(site string) string {
	return identity.TokenPrefix + strings.Repeat(strings.ToLower(site[len(site)-1:]), 64)
}

// fixtureDevices enrolls the two fixed reader sites through the real admin
// route, then turns readerlab Basic credentials into their device keys, so
// every request still takes the production device-key path.
func fixtureDevices(ctx context.Context, runtime *library.Runtime, next http.Handler) (http.Handler, error) {
	for _, site := range []string{siteA, siteB} {
		sum := sha256.Sum256([]byte(fixtureToken(site)))
		body, _ := json.Marshal(map[string]string{"site_id": site, "token_hash": hex.EncodeToString(sum[:])})
		r := httptest.NewRequest("POST", "/sync/devices/v1/enroll", bytes.NewReader(body)).WithContext(ctx)
		r.Header.Set("Content-Type", "application/json")
		r.SetBasicAuth("assetlab", "assetlab")
		w := httptest.NewRecorder()
		runtime.ServeHTTP(w, r)
		if w.Code != 204 {
			return nil, fmt.Errorf("fixture enrollment of %s: %d %s", site, w.Code, strings.TrimSpace(w.Body.String()))
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		site, known := map[string]string{"reader-a": siteA, "reader-b": siteB}[user]
		if !ok || !known || password != "readerlab" {
			w.Header().Set("WWW-Authenticate", `Basic realm="Disposable Reader Lab"`)
			http.Error(w, "fixture credentials required", 401)
			return
		}
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+fixtureToken(site))
		next.ServeHTTP(w, r)
	}), nil
}

func without(prefix string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withoutAssets hides assets-v1 exactly as an assets-disabled host would:
// no routes, and capabilities that do not advertise it.
func withoutAssets(next http.Handler) http.Handler {
	next = without("/sync/assets/v1/", next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sync/capabilities" {
			next.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		body := rec.Body.Bytes()
		if rec.Code == 200 {
			var caps map[string]any
			if json.Unmarshal(body, &caps) == nil {
				if features, ok := caps["features"].([]any); ok {
					kept := []any{}
					for _, f := range features {
						if f != "assets-v1" {
							kept = append(kept, f)
						}
					}
					caps["features"] = kept
				}
				delete(caps, "assets")
				body, _ = json.Marshal(caps)
			}
		}
		for k, values := range rec.Header() {
			if k != "Content-Length" {
				w.Header()[k] = values
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(body)
	})
}

// gateResponse: a returned response proves the handler's transaction finished;
// blocking before delivery exposes the committed-but-unacknowledged gap. The
// parent kills this process after reading the checkpoint line.
func gateResponse(next http.Handler, target string) http.Handler {
	var once sync.Once
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method+":"+r.URL.Path != target {
			next.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		if rec.Code >= 200 && rec.Code < 300 {
			once.Do(func() { fmt.Println(`{"checkpoint":"server_response"}`); <-r.Context().Done() })
		}
		for k, values := range rec.Header() {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	})
}

// project drains the library, then reads the server's reducer.
func project(ctx context.Context, runtime *library.Runtime, dbURL, id string) (any, error) {
	for sweep := 0; sweep < 64; sweep++ {
		n, err := runtime.MaterializeReader(ctx)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			break
		}
	}
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return reader.Store{DB: db}.Projection(ctx, id, reader.DefaultLimits())
}
