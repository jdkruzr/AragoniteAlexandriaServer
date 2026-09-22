package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
)

// AdoptLegacy verifies the exact original foundation's shape in an isolated
// transactional schema before assigning checksums. It does not adopt UltraBridge.
// Stop all legacy processes first. The old ledger is retained so their startup
// cannot mistake this database for an empty installation and replay old DDL.
func AdoptLegacy(ctx context.Context, db *sql.DB) error {
	plan, err := Migrator().Plan()
	if err != nil {
		return err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, schema.LockID); err != nil {
		conn.Close()
		return err
	}
	defer schema.ReleaseLock(conn, false)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT to_regclass('alexandria_schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errors.New("already initialized; use migrate instead of legacy adoption")
	}
	rows, err := tx.QueryContext(ctx, `SELECT version FROM loom_schema_migrations ORDER BY version`)
	if err != nil {
		return err
	}
	var versions []string
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		versions = append(versions, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(versions, []string{"0001_foundation.sql", "0002_tasks.sql", "0003_identity.sql"}) {
		return errors.New("unknown legacy migration history")
	}
	// A transaction-local reference schema gives us actual PostgreSQL's parsing
	// of the original migrations, rather than a hand-maintained column checklist.
	const reference = "alexandria_adoption_reference"
	if _, err = tx.ExecContext(ctx, `CREATE SCHEMA alexandria_adoption_reference; SET LOCAL search_path=alexandria_adoption_reference,public`); err != nil {
		return err
	}
	for _, m := range plan[:3] {
		if _, err = tx.ExecContext(ctx, m.SQL); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL search_path=public`); err != nil {
		return err
	}
	actual, err := legacyShape(ctx, tx, "public")
	if err != nil {
		return err
	}
	expected, err := legacyShape(ctx, tx, reference)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return errors.New("legacy schema differs from the known foundation; refusing checksum adoption")
	}
	if _, err = tx.ExecContext(ctx, `DROP SCHEMA alexandria_adoption_reference CASCADE; CREATE TABLE alexandria_schema_migrations(version text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, m := range plan[:3] {
		if _, err = tx.ExecContext(ctx, `INSERT INTO alexandria_schema_migrations SELECT version,$2,applied_at FROM loom_schema_migrations WHERE version=$1`, m.Version, m.Checksum); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func legacyShape(ctx context.Context, tx *sql.Tx, namespace string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT entry FROM (
 SELECT 'column:'||table_name||':'||column_name||':'||ordinal_position||':'||data_type||':'||udt_name||':'||is_nullable||':'||is_identity||':'||replace(replace(coalesce(column_default,''),$1||'.',''),'public.','') AS entry
 FROM information_schema.columns WHERE table_schema=$1 AND table_name LIKE 'loom\_%' AND table_name<>'loom_schema_migrations'
 UNION ALL
 SELECT 'constraint:'||c.relname||':'||con.contype::text||':'||replace(replace(pg_get_constraintdef(con.oid),$1||'.',''),'public.','')
 FROM pg_constraint con JOIN pg_class c ON c.oid=con.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=$1 AND c.relname LIKE 'loom\_%' AND c.relname<>'loom_schema_migrations'
 UNION ALL
 SELECT 'index:'||tablename||':'||replace(replace(indexdef,$1||'.',''),'public.','') FROM pg_indexes WHERE schemaname=$1 AND tablename LIKE 'loom\_%' AND tablename<>'loom_schema_migrations'
 UNION ALL
 SELECT 'enum:'||t.typname||':'||e.enumsortorder||':'||e.enumlabel FROM pg_enum e JOIN pg_type t ON t.oid=e.enumtypid JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname=$1 AND t.typname LIKE 'loom\_%'
 UNION ALL
 SELECT 'trigger:'||c.relname||':'||t.tgname FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relname LIKE 'loom\_%' AND NOT t.tgisinternal
 UNION ALL
 SELECT 'rls:'||c.relname||':'||c.relrowsecurity||':'||c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$1 AND c.relkind='r' AND c.relname LIKE 'loom\_%' AND c.relname<>'loom_schema_migrations'
) shape ORDER BY entry`, namespace)
	if err != nil {
		return nil, fmt.Errorf("inspect legacy schema: %w", err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, rows.Err()
}
