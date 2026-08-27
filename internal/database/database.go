package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jdkruzr/aragonite-loom/migrations"
)

func Open(ctx context.Context, databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(12)
	db.SetMaxIdleConns(2)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return db, nil
}

// Migrate serializes migration runners using a PostgreSQL advisory lock. Each
// file is one transaction and is recorded only after it commits.
func Migrate(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	if _, err := db.ExecContext(ctx, `SELECT pg_advisory_lock(hashtext('aragonite-loom-migrations'))`); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer db.ExecContext(context.Background(), `SELECT pg_advisory_unlock(hashtext('aragonite-loom-migrations'))`)

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS loom_schema_migrations (
		version text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var applied bool
		if err := db.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM loom_schema_migrations WHERE version=$1)`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}
		body, err := migrations.Files.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO loom_schema_migrations(version) VALUES($1)`, name)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		logger.Info("database migration applied", "version", name)
	}
	return nil
}

func Ready(ctx context.Context, db *sql.DB) error {
	var one int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return err
	}
	if one != 1 {
		return fmt.Errorf("unexpected readiness result %d", one)
	}
	return nil
}
