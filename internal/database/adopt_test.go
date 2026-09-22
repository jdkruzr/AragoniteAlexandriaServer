package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"
)

func legacyDB(t *testing.T) *sql.DB {
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
	admin, err := Open(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("alexandria_adopt_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := Open(context.Background(), u.String())
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
	if _, err = db.Exec(`CREATE TABLE loom_schema_migrations(version text PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	plan, err := Migrator().Plan()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range plan[:3] {
		if _, err = db.Exec(m.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO loom_schema_migrations(version) VALUES($1)`, m.Version); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestAdoptPreservesContentAndTokens(t *testing.T) {
	db := legacyDB(t)
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO loom_tasks(task_id,title,created_at,updated_at) VALUES('keep','Original note',1,1); INSERT INTO loom_api_tokens(token_hash,label) VALUES('old-hash','old token')`); err != nil {
		t.Fatal(err)
	}
	if err := AdoptLegacy(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	if err := Ready(ctx, db); err != nil {
		t.Fatal(err)
	}
	var title, hash string
	if err := db.QueryRow(`SELECT title FROM alexandria_tasks WHERE task_id='keep'`).Scan(&title); err != nil || title != "Original note" {
		t.Fatal("lost content", err)
	}
	if err := db.QueryRow(`SELECT token_hash FROM alexandria_api_tokens WHERE label='old token'`).Scan(&hash); err != nil || hash != "old-hash" {
		t.Fatal("lost token", err)
	}
}

func TestAdoptionRefusesDrift(t *testing.T) {
	db := legacyDB(t)
	if _, err := db.Exec(`ALTER TABLE loom_tasks ADD COLUMN unknown_data text`); err != nil {
		t.Fatal(err)
	}
	if err := AdoptLegacy(context.Background(), db); err == nil {
		t.Fatal("adopted unknown schema")
	}
	var untouched bool
	if err := db.QueryRow(`SELECT to_regclass('alexandria_schema_migrations') IS NULL AND to_regclass('alexandria_adoption_reference.loom_tasks') IS NULL`).Scan(&untouched); err != nil || !untouched {
		t.Fatal("failed adoption changed schema", err)
	}
}
