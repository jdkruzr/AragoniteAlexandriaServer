package library

import (
	"context"
	"database/sql"
	"errors"

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
