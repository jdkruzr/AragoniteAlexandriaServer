package library

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
)

type emptyObjects struct{}

func (emptyObjects) Put(context.Context, string, string, io.Reader, int64) (BlobInfo, error) {
	return BlobInfo{}, nil
}
func (emptyObjects) Get(context.Context, string) (io.ReadCloser, BlobInfo, error) {
	return io.NopCloser(strings.NewReader("hello")), BlobInfo{}, nil
}
func (emptyObjects) Stat(context.Context, string) (BlobInfo, error) { return BlobInfo{}, nil }
func (emptyObjects) Delete(context.Context, string) error           { return nil }
func (emptyObjects) SignedGetURL(context.Context, string, time.Duration) (string, error) {
	return "", nil
}

func fixture(t *testing.T) (*Runtime, *sql.DB, string) {
	t.Helper()
	raw := os.Getenv("ALEXANDRIA_TEST_DATABASE_URL")
	if raw == "" {
		if os.Getenv("ALEXANDRIA_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("database required")
		}
		t.Skip("database not configured")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("alexandria_runtime_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		_, err := admin.Exec(`DROP DATABASE "` + name + `" WITH (FORCE)`)
		admin.Close()
		if err != nil {
			t.Error(err)
		}
	})
	ctx := context.Background()
	id := uuid.NewString()
	if err := Migrations().Apply(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, db, id); err != nil {
		t.Fatal(err)
	}
	if err := SetAdministrator(ctx, db, "author", "a-long-test-password"); err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, Config{ID: id, DatabaseURL: u.String(), Objects: emptyObjects{}, MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, db, id
}

func request(r *Runtime, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else {
		req.SetBasicAuth("author", "a-long-test-password")
	}
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	return out
}

func TestBoundRuntimeIsolationAndPolicy(t *testing.T) {
	a, dbA, idA := fixture(t)
	b, _, _ := fixture(t)
	ctx := context.Background()
	token, err := CreateToken(ctx, dbA, "A only")
	if err != nil {
		t.Fatal(err)
	}
	if got := request(b, "GET", "/api/v1/tasks", "", token); got.Code != 401 {
		t.Fatalf("foreign token: %d", got.Code)
	}
	if got := request(a, "POST", "/api/v1/tasks", `{"id":"same","title":"Only A"}`, token); got.Code != 201 {
		t.Fatalf("create: %d %s", got.Code, got.Body)
	}
	if got := request(b, "GET", "/api/v1/tasks", "", ""); strings.Contains(got.Body.String(), "Only A") {
		t.Fatal("cross-library data")
	}
	if err := SetMode(ctx, dbA, idA, "read_only"); err != nil {
		t.Fatal(err)
	}
	if got := request(a, "POST", "/api/v1/tasks", `{}`, token); got.Code != 403 {
		t.Fatal(got.Code)
	}
	if got := request(a, "GET", "/api/v1/tasks", "", token); got.Code != 200 {
		t.Fatal(got.Code)
	}
	if err := SetMode(ctx, dbA, idA, "maintenance"); err != nil {
		t.Fatal(err)
	}
	if got := request(a, "GET", "/api/v1/tasks", "", token); got.Code != 503 || got.Header().Get("Retry-After") == "" {
		t.Fatal(got.Code)
	}
	if got := request(b, "GET", "/api/v1/tasks", "", ""); got.Code != 200 {
		t.Fatal("other tenant unavailable")
	}
}

func TestDownloadHoldsCrossProcessBarrier(t *testing.T) {
	r, db, id := fixture(t)
	ctx := context.Background()
	stream, _, err := r.ReadObject(ctx, "book")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetMode(ctx, db, id, "maintenance"); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var acquired bool
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, schema.LockID).Scan(&acquired); err != nil || acquired {
		t.Fatal("upgrade bypassed active stream", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, schema.LockID).Scan(&acquired); err != nil || !acquired {
		t.Fatal("stream leaked admission lock", err)
	}
	schema.ReleaseLock(conn, false)
}
