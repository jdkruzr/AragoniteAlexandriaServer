// Package schema owns forward-only, checksummed PostgreSQL migrations. It is
// shared by Server library schemas and Hosting's independent control schema.
package schema

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"
)

const LockID int64 = 0x414c45584d4947

type Runner struct{ Files fs.FS }
type Migration struct{ Version, Checksum, SQL string }

func (r Runner) Plan() ([]Migration, error) {
	entries, err := fs.ReadDir(r.Files, ".")
	if err != nil {
		return nil, err
	}
	var result []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		body, err := fs.ReadFile(r.Files, entry.Name())
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(body)
		result = append(result, Migration{entry.Name(), hex.EncodeToString(hash[:]), string(body)})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	if len(result) == 0 {
		return nil, errors.New("empty migration set")
	}
	return result, nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func verify(ctx context.Context, q queryer, plan []Migration, complete bool) error {
	rows, err := q.QueryContext(ctx, `SELECT version,checksum FROM alexandria_schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("schema not initialized; run the migration command: %w", err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var version, checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return err
		}
		if i >= len(plan) || plan[i].Version != version {
			return fmt.Errorf("unknown or noncontiguous migration %s; refusing schema", version)
		}
		if plan[i].Checksum != checksum {
			return fmt.Errorf("migration checksum mismatch: %s", version)
		}
		i++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if complete && i != len(plan) {
		return errors.New("schema upgrade required; run the migration command")
	}
	return nil
}

// Check is read-only. Runtime processes must not bootstrap or migrate schemas.
func (r Runner) Check(ctx context.Context, db *sql.DB) error {
	plan, err := r.Plan()
	if err != nil {
		return err
	}
	return verify(ctx, db, plan, true)
}

func (r Runner) CheckConnection(ctx context.Context, conn *sql.Conn) error {
	plan, err := r.Plan()
	if err != nil {
		return err
	}
	return verify(ctx, conn, plan, true)
}

// ReleaseLock never returns a session with an uncertain lock state to its pool.
func ReleaseLock(conn *sql.Conn, shared bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	statement := "SELECT pg_advisory_unlock($1)"
	if shared {
		statement = "SELECT pg_advisory_unlock_shared($1)"
	}
	var unlocked bool
	err := conn.QueryRowContext(ctx, statement, LockID).Scan(&unlocked)
	if err != nil || !unlocked {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	}
	_ = conn.Close()
}

func (r Runner) Apply(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	plan, err := r.Plan()
	if err != nil {
		return err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, LockID); err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		conn.Close()
		return err
	}
	defer ReleaseLock(conn, false)
	var legacy bool
	if err = conn.QueryRowContext(ctx, `SELECT to_regclass('loom_schema_migrations') IS NOT NULL AND to_regclass('alexandria_schema_migrations') IS NULL`).Scan(&legacy); err != nil {
		return err
	}
	if legacy {
		return errors.New("legacy schema requires explicit adoption before migration; do not restart the old binary after adoption")
	}
	if _, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS alexandria_schema_migrations (version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()); CREATE TABLE IF NOT EXISTS alexandria_migration_attempts (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, version text NOT NULL, started_at timestamptz NOT NULL DEFAULT now(), finished_at timestamptz, succeeded boolean, error_code text)`); err != nil {
		return err
	}
	if err = verify(ctx, conn, plan, false); err != nil {
		return err
	}
	for _, m := range plan {
		var applied bool
		if err = conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alexandria_schema_migrations WHERE version=$1)`, m.Version).Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		var attempt int64
		if err = conn.QueryRowContext(ctx, `INSERT INTO alexandria_migration_attempts(version) VALUES($1) RETURNING id`, m.Version).Scan(&attempt); err != nil {
			return err
		}
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, m.SQL); err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO alexandria_schema_migrations(version,checksum) VALUES($1,$2)`, m.Version, m.Checksum)
		}
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
		recordCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		code := ""
		if err != nil {
			code = "migration_failed"
		}
		_, recordErr := conn.ExecContext(recordCtx, `UPDATE alexandria_migration_attempts SET finished_at=now(),succeeded=$2,error_code=$3 WHERE id=$1`, attempt, err == nil, code)
		cancel()
		if err != nil {
			return fmt.Errorf("migration %s failed: %w", m.Version, err)
		}
		if recordErr != nil {
			return recordErr
		}
		if logger != nil {
			logger.Info("migration applied", "version", m.Version)
		}
	}
	return verify(ctx, conn, plan, true)
}
