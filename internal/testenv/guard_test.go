package testenv

import (
	"context"
	"strings"
	"testing"
)

func TestGuardCatchesQueriesWhileRowsAreOpen(t *testing.T) {
	db := Database(t).DB
	ctx := context.Background()
	// A pool hands the second query another connection, as before.
	rows, err := db.QueryContext(ctx, `SELECT generate_series(1,3)`)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.QueryRowContext(ctx, `SELECT 1`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	rows.Close()
	// One request connection: the same pattern panics, naming the mistake.
	// (Not closed: the panic leaves database/sql's hold on it; the test
	// database is dropped at cleanup.)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = conn.QueryContext(ctx, `SELECT generate_series(1,3)`)
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if p, _ := recover().(string); !strings.Contains(p, "earlier rows are still open") || !strings.Contains(p, "SELECT 2") {
				t.Fatalf("no guard panic: %q", p)
			}
		}()
		_ = conn.QueryRowContext(ctx, `SELECT 2`).Scan(&n)
	}()
	// Rows read to the end free the connection again.
	conn2 := Request(t, db)
	rows, err = conn2.QueryContext(ctx, `SELECT generate_series(1,3)`)
	if err == nil {
		for rows.Next() {
		}
		err = conn2.QueryRowContext(ctx, `SELECT 4`).Scan(&n)
	}
	if err != nil || n != 4 {
		t.Fatal(n, err)
	}
}
