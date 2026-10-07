// Package pg holds the small PostgreSQL seams shared by the Alexandria sync
// packages: one interface that both *sql.DB and an admitted *sql.Conn satisfy,
// and the advisory-lock keys that stand in for SQLite's single writer.
package pg

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
)

// DB is satisfied by *sql.DB and *sql.Conn. Runtime handlers receive the
// admitted connection, so stores must never assume a pool.
type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Querier is satisfied by DB and *sql.Tx.
type Querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Transaction-scoped advisory lock keys. They serialize decisions SQLite made
// by taking its single writer. Values are arbitrary but fixed forever.
const (
	// IdentityLock serializes device enrollment so two connections cannot both
	// observe a free site or token hash and claim it.
	IdentityLock int64 = 0x416C657869640001 // "Alexid" + 1
	// ReaderLock serializes reader materialization (drain) and journal
	// completion across gateway and worker processes. Lock order: generation
	// row, then sync_seq, then ReaderLock.
	ReaderLock int64 = 0x416C657869640002
)

// XactLock takes a transaction-scoped exclusive advisory lock.
func XactLock(ctx context.Context, tx *sql.Tx, key int64) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, key)
	return err
}

// UniqueViolation reports a unique/primary-key constraint failure.
func UniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
