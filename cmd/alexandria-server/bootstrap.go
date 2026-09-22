package main

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/config"
	"github.com/jdkruzr/AragoniteAlexandriaServer/library"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

// Bootstrap creates only a restricted runtime identity, never rotates existing
// credentials. Supply a random password via an injected secret or protected file.
func runBootstrapRuntime(logger *slog.Logger) error {
	password := os.Getenv("ALEXANDRIA_RUNTIME_PASSWORD")
	if file := os.Getenv("ALEXANDRIA_RUNTIME_PASSWORD_FILE"); file != "" {
		body, err := os.ReadFile(file)
		if err != nil {
			return errors.New("cannot read runtime password file")
		}
		password = strings.TrimSpace(string(body))
	}
	if len(password) < 24 {
		return errors.New("runtime password must contain at least 24 characters")
	}
	role := strings.TrimSpace(os.Getenv("ALEXANDRIA_RUNTIME_ROLE"))
	if role == "" {
		role = "alexandria_runtime"
	}
	if len(role) > 63 {
		return errors.New("runtime role name too long")
	}
	db, ctx, closeDB, err := openAdminDB(logger)
	if err != nil {
		return err
	}
	defer closeDB()
	quoted := pgx.Identifier{role}.Sanitize()
	var exists bool
	if err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, role).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err = db.ExecContext(ctx, `CREATE ROLE `+quoted+` LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD '`+strings.ReplaceAll(password, "'", "''")+`'`); err != nil {
			return errors.New("runtime role creation failed")
		}
	}
	var database string
	if err = db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&database); err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, `REVOKE CREATE ON SCHEMA public FROM PUBLIC; REVOKE CONNECT ON DATABASE `+pgx.Identifier{database}.Sanitize()+` FROM PUBLIC; GRANT CONNECT ON DATABASE `+pgx.Identifier{database}.Sanitize()+` TO `+quoted); err != nil {
		return err
	}
	if err = library.GrantRuntime(ctx, db, role); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	raw, err := url.Parse(cfg.DatabaseURL)
	if err != nil {
		return errors.New("invalid management database URL")
	}
	raw.User = url.UserPassword(role, password)
	return verifyRuntimePassword(ctx, raw.String())
}

func verifyRuntimePassword(ctx context.Context, raw string) error {
	db, err := sql.Open("pgx", raw)
	if err != nil {
		return errors.New("invalid runtime connection")
	}
	defer db.Close()
	if err = db.PingContext(ctx); err != nil {
		return errors.New("runtime credential verification failed; existing credentials were not changed")
	}
	return nil
}
