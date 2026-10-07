// Package testenv provides disposable PostgreSQL databases and S3 prefixes for
// integration tests. It never touches a live library: every database and object
// prefix is created for one test and removed afterwards.
//
// Tests skip when the fixtures are not configured, unless
// ALEXANDRIA_REQUIRE_INTEGRATION=1, in which case they fail (the CI gate).
package testenv

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/database"
)

// Library is one migrated, initialized single-library database.
type Library struct {
	ID  string // library UUID
	URL string // connection string for the test database
	DB  *sql.DB
}

func required(t testing.TB, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		if os.Getenv("ALEXANDRIA_REQUIRE_INTEGRATION") == "1" {
			t.Fatalf("%s is required for the integration gate", name)
		}
		t.Skipf("%s not set", name)
	}
	return value
}

// Database creates a fresh database, applies every migration and initializes
// the library singleton. It is dropped (forcibly) when the test ends.
func Database(t testing.TB) Library {
	t.Helper()
	raw := required(t, "ALEXANDRIA_TEST_DATABASE_URL")
	base, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("alexandria_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	base.Path = "/" + name
	db, err := sql.Open("pgx", base.String())
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
	if err := database.Migrator().Apply(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO alexandria_library_runtime(singleton, library_id) VALUES(true, $1)
		ON CONFLICT (singleton) DO UPDATE SET library_id=EXCLUDED.library_id`, id); err != nil {
		t.Fatal(err)
	}
	return Library{ID: id, URL: base.String(), DB: db}
}

// Objects returns an S3 store scoped to a fresh library prefix in the shared
// test bucket. Objects are deleted on cleanup when the test tracked them.
func Objects(t testing.TB, libraryID string) *blob.Scoped {
	t.Helper()
	endpoint := required(t, "ALEXANDRIA_TEST_S3_ENDPOINT")
	ctx := context.Background()
	store, err := blob.NewS3(ctx, blob.S3Config{
		Endpoint: endpoint, Region: "us-east-1", Bucket: "aragonite-alexandria-server",
		AccessKey: envOr("ALEXANDRIA_TEST_S3_ACCESS_KEY", "alexandria-local"),
		SecretKey: envOr("ALEXANDRIA_TEST_S3_SECRET_KEY", "alexandria-local-secret"),
		PathStyle: true, DisableTLS: strings.HasPrefix(endpoint, "http://"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatal(err)
	}
	scoped, err := blob.ForLibrary(store, libraryID)
	if err != nil {
		t.Fatal(err)
	}
	return scoped
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
