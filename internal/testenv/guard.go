package testenv

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/database"
)

// guardedDriver is pgx with one check added. Production gives each web
// request a single connection (library.Runtime pins one per request), and a
// query sent on a connection that still has unread rows corrupts pgx's state:
// it panics or hangs later, far from the cause. Over a pool the mistake is
// invisible, because database/sql hands the second query another connection.
// Tests open databases through this driver and give handlers one connection
// (Request), so the mistake panics at once, naming itself.
const guardedDriver = "pgx-testenv-guard"

func init() {
	sql.Register(guardedDriver, guardDriver{stdlib.GetDefaultDriver()})
	database.Driver = guardedDriver
}

type guardDriver struct{ driver.Driver }

func (d guardDriver) Open(name string) (driver.Conn, error) {
	c, err := d.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return &guardConn{conn: c}, nil
}

// pgxConn is everything the pgx connection offers database/sql.
type pgxConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.ExecerContext
	driver.QueryerContext
	driver.Pinger
	driver.NamedValueChecker
	driver.SessionResetter
}

type guardConn struct {
	conn driver.Conn
	open atomic.Int32 // rows not yet closed
}

func (c *guardConn) pgx() pgxConn { return c.conn.(pgxConn) }

// idle panics when earlier rows on this connection are still being read.
func (c *guardConn) idle(query string) {
	if c.open.Load() > 0 {
		panic(fmt.Sprintf("testenv: a query was sent on a connection whose earlier rows are still open. "+
			"Each production request has one connection, so read and close rows (or run this query) first. Query: %.200s", query))
	}
}

func (c *guardConn) Prepare(query string) (driver.Stmt, error) {
	c.idle(query)
	return c.conn.Prepare(query)
}
func (c *guardConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	c.idle(query)
	return c.pgx().PrepareContext(ctx, query)
}
func (c *guardConn) Close() error { return c.conn.Close() }
func (c *guardConn) Begin() (driver.Tx, error) {
	c.idle("BEGIN")
	return c.conn.Begin() //nolint:staticcheck // the old interface database/sql may still call
}
func (c *guardConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.idle("BEGIN")
	return c.pgx().BeginTx(ctx, opts)
}
func (c *guardConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	c.idle(query)
	return c.pgx().ExecContext(ctx, query, args)
}
func (c *guardConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.idle(query)
	rows, err := c.pgx().QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	c.open.Add(1)
	return &guardRows{Rows: rows, conn: c}, nil
}
func (c *guardConn) Ping(ctx context.Context) error             { return c.pgx().Ping(ctx) }
func (c *guardConn) CheckNamedValue(v *driver.NamedValue) error { return c.pgx().CheckNamedValue(v) }
func (c *guardConn) ResetSession(ctx context.Context) error     { return c.pgx().ResetSession(ctx) }
func (c *guardConn) IsValid() bool                              { v, ok := c.conn.(driver.Validator); return !ok || v.IsValid() }

// guardRows passes pgx's rows through and marks the connection free on Close,
// which database/sql calls when the rows are closed or read to the end.
type guardRows struct {
	driver.Rows
	conn   *guardConn
	closed bool
}

func (r *guardRows) Close() error {
	if !r.closed {
		r.closed = true
		r.conn.open.Add(-1)
	}
	return r.Rows.Close()
}

// The column details database/sql's ColumnTypes reads.
type columnTypes interface {
	driver.RowsColumnTypeDatabaseTypeName
	driver.RowsColumnTypeLength
	driver.RowsColumnTypePrecisionScale
	driver.RowsColumnTypeScanType
}

func (r *guardRows) ColumnTypeDatabaseTypeName(i int) string {
	return r.Rows.(columnTypes).ColumnTypeDatabaseTypeName(i)
}
func (r *guardRows) ColumnTypeLength(i int) (int64, bool) {
	return r.Rows.(columnTypes).ColumnTypeLength(i)
}
func (r *guardRows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	return r.Rows.(columnTypes).ColumnTypePrecisionScale(i)
}
func (r *guardRows) ColumnTypeScanType(i int) reflect.Type {
	return r.Rows.(columnTypes).ColumnTypeScanType(i)
}

// Request is one connection from db, as production gives a web request.
// Hand it to handlers under test; it is closed when the test ends.
func Request(t testing.TB, db *sql.DB) *sql.Conn {
	t.Helper()
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
