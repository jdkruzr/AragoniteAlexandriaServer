package main

import (
	"context"
	"database/sql"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestStandaloneRuntimeBootstrap(t *testing.T) {
	raw := os.Getenv("ALEXANDRIA_TEST_DATABASE_URL")
	if raw == "" {
		if os.Getenv("ALEXANDRIA_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("database required")
		}
		t.Skip("database not configured")
	}
	admin, err := sql.Open("pgx", raw)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	name, role := "bootstrap_"+suffix, "runtime_"+suffix
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := admin.Exec(`CREATE DATABASE ` + quoted); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP DATABASE ` + quoted + ` WITH (FORCE)`); err != nil {
			t.Error(err)
		}
		if _, err := admin.Exec(`DROP ROLE IF EXISTS ` + pgx.Identifier{role}.Sanitize()); err != nil {
			t.Error(err)
		}
	}()
	uri, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	uri.Path = "/" + name
	t.Setenv("ALEXANDRIA_DATABASE_URL", uri.String())
	t.Setenv("ALEXANDRIA_RUNTIME_ROLE", role)
	t.Setenv("ALEXANDRIA_RUNTIME_PASSWORD", "fixture-only-runtime-password-123")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := runMigrate(logger); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := runBootstrapRuntime(logger); err != nil {
			t.Fatal(err)
		}
	}
	uri.User = url.UserPassword(role, "fixture-only-runtime-password-123")
	runtime, err := sql.Open("pgx", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if err := runtime.PingContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{`CREATE TABLE forbidden(id int)`, `DELETE FROM alexandria_schema_migrations`, `UPDATE alexandria_library_runtime SET mode='active'`} {
		if _, err := runtime.Exec(query); err == nil {
			t.Error("runtime has management authority", query)
		}
	}
	t.Setenv("ALEXANDRIA_RUNTIME_PASSWORD", "wrong-but-long-enough-password")
	if err := runBootstrapRuntime(logger); err == nil {
		t.Fatal("existing role password silently replaced")
	}
}
