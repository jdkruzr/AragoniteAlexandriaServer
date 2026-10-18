package schema_test

import (
	"context"
	"database/sql"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"github.com/jdkruzr/AragoniteAlexandriaServer/migrations"
	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testenv.UnmigratedDatabase(t).DB
	db.SetMaxOpenConns(8)
	return db
}

func TestFreshIdentityAndConcurrentMigration(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	r := schema.Runner{Files: migrations.Files}
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.Apply(ctx, db, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if err := r.Check(ctx, db); err != nil {
		t.Fatal(err)
	}
	var current, old bool
	if err := db.QueryRow(`SELECT to_regclass('alexandria_tasks') IS NOT NULL,to_regclass('loom_tasks') IS NOT NULL`).Scan(&current, &old); err != nil {
		t.Fatal(err)
	}
	if !current || old {
		t.Fatal("identity migration not applied")
	}
	embedded, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM alexandria_schema_migrations`).Scan(&count); err != nil || count != len(embedded) {
		t.Fatalf("ledger: %d %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("leaked migration lock: %d %v", count, err)
	}
}

func TestFailureRollbackRetryAndChecksumFence(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	files := fstest.MapFS{"0001.sql": &fstest.MapFile{Data: []byte("CREATE TABLE example(id integer PRIMARY KEY)")}, "0002.sql": &fstest.MapFile{Data: []byte("CREATE TABLE rolled_back(id integer); SELECT 1/0;")}}
	r := schema.Runner{Files: files}
	if err := r.Apply(ctx, db, nil); err == nil {
		t.Fatal("expected failure")
	}
	var absent bool
	if err := db.QueryRow(`SELECT to_regclass('rolled_back') IS NULL`).Scan(&absent); err != nil || !absent {
		t.Fatal("partial migration persisted", err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM alexandria_migration_attempts WHERE succeeded=false`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failure not recorded", err)
	}
	files["0002.sql"].Data = []byte("CREATE TABLE recovered(id integer)")
	if err := r.Apply(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	files["0001.sql"].Data = []byte("CREATE TABLE example(id text)")
	if err := r.Check(ctx, db); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
	delete(files, "0002.sql")
	if err := r.Apply(ctx, db, nil); err == nil {
		t.Fatal("downgrade accepted")
	}
}

func TestLegacyRequiresAdoption(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`CREATE TABLE loom_schema_migrations(version text PRIMARY KEY,applied_at timestamptz DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	if err := (schema.Runner{Files: migrations.Files}).Apply(context.Background(), db, nil); err == nil {
		t.Fatal("silently adopted legacy schema")
	}
}
