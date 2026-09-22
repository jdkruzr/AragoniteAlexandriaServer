package library

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
)

// SetMode is privileged management; runtime roles must have SELECT-only access
// to alexandria_library_runtime. Setting maintenance stops new admission; the
// migrator's exclusive lock waits for admitted work across all processes.
func SetMode(ctx context.Context, db *sql.DB, id, mode string) error {
	switch mode {
	case "active", "read_only":
		if err := Migrations().Check(ctx, db); err != nil {
			return err
		}
	case "maintenance", "suspended":
	default:
		return errors.New("invalid library mode")
	}
	result, err := db.ExecContext(ctx, `UPDATE alexandria_library_runtime SET mode=$2,updated_at=now() WHERE singleton AND library_id=$1`, id, mode)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("library binding missing")
	}
	return nil
}

func SetAdministrator(ctx context.Context, db *sql.DB, username, password string) error {
	return auth.NewStore(db).SetUser(ctx, username, password)
}
func CreateToken(ctx context.Context, db *sql.DB, label string) (string, error) {
	return auth.NewStore(db).CreateToken(ctx, label)
}

// GrantRuntime is deliberately explicit after every schema upgrade. Runtime
// credentials can manipulate content but never schemas or admission/ledger state.
func GrantRuntime(ctx context.Context, db *sql.DB, role string) error {
	var safe bool
	if err := db.QueryRowContext(ctx, `SELECT rolcanlogin AND NOT rolsuper AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolinherit AND NOT rolreplication AND NOT rolbypassrls AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid) FROM pg_roles r WHERE rolname=$1`, role).Scan(&safe); err != nil {
		return err
	}
	if !safe {
		return errors.New("runtime role has unsafe privileges")
	}
	var ownsSchema bool
	if err := db.QueryRowContext(ctx, `SELECT has_schema_privilege($1,'public','CREATE') OR EXISTS(SELECT 1 FROM pg_tables WHERE schemaname='public' AND tableowner=$1)`, role).Scan(&ownsSchema); err != nil {
		return err
	}
	if ownsSchema {
		return errors.New("runtime role must not own or create library schema objects")
	}
	quoted := pgx.Identifier{role}.Sanitize()
	_, err := db.ExecContext(ctx, `GRANT USAGE ON SCHEMA public TO `+quoted+`; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO `+quoted+`; GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA public TO `+quoted+`; REVOKE INSERT,UPDATE,DELETE ON alexandria_schema_migrations,alexandria_migration_attempts,alexandria_library_runtime FROM `+quoted)
	return err
}
