package library

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/schema"
)

func TestAdmissionReusesHealthySession(t *testing.T) {
	r, _, _ := fixture(t)
	ctx := context.Background()
	var first int
	for i := 0; i < 20; i++ {
		conn, done, err := r.admit(ctx, Read)
		if err != nil {
			t.Fatal(err)
		}
		var pid int
		err = conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid)
		done()
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = pid
		} else if pid != first {
			t.Fatal("healthy session discarded")
		}
	}
	if stats := r.db.Stats(); stats.Idle != 1 || stats.InUse != 0 {
		t.Fatal(stats)
	}
}

func TestAdmissionDiscardsCancelledAcquisition(t *testing.T) {
	r, db, _ := fixture(t)
	ctx := context.Background()
	// Shadow only this fixture session's acquisition function. It takes the real
	// session lock and then delays its reply, making cancellation's outcome uncertain.
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA admission_test;
 CREATE FUNCTION admission_test.pg_try_advisory_lock_shared(bigint) RETURNS boolean LANGUAGE plpgsql AS $$
 BEGIN PERFORM pg_catalog.pg_advisory_lock_shared($1); PERFORM pg_sleep(1); RETURN true; END $$`); err != nil {
		t.Fatal(err)
	}
	conn, err := r.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var first int
	if err = conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `SET search_path=admission_test,pg_catalog,public`); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	cancelled, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	if _, done, err := r.admit(cancelled, Read); err == nil {
		done()
		t.Fatal("delayed acquisition succeeded")
	}
	next, done, err := r.admit(ctx, Read)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	err = next.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid)
	done()
	if err != nil {
		t.Fatal(err)
	}
	if pid == first {
		t.Fatal("uncertain session returned to pool")
	}
	check, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var locked bool
	if err = check.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, schema.LockID).Scan(&locked); err != nil || !locked {
		check.Close()
		t.Fatal("cancelled session leaked migration lock", err)
	}
	schema.ReleaseLock(check, false)
}
func TestAdmissionDiscardsFailedRelease(t *testing.T) {
	r, _, _ := fixture(t)
	ctx := context.Background()
	conn, done, err := r.admit(ctx, Read)
	if err != nil {
		t.Fatal(err)
	}
	var first int
	if err = conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `SELECT pg_advisory_unlock_shared($1)`, schema.LockID); err != nil {
		t.Fatal(err)
	}
	done() // An unconfirmed unlock must discard this connection.
	conn, done, err = r.admit(ctx, Read)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	var pid int
	if err = conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if pid == first {
		t.Fatal("failed-release session reused")
	}
}
func TestOwnerPrecheckRetainsCustomAuthentication(t *testing.T) {
	r, db, _ := fixture(t)
	ctx := context.Background()
	token, err := CreateToken(ctx, db, "owner-policy")
	if err != nil {
		t.Fatal(err)
	}
	if w := request(r, "GET", "/jobs", "", token); w.Code != 401 {
		t.Fatal("token admitted as owner", w.Code)
	}
	var accountCalls, generalCalls int
	r.cfg.AuthenticateAccount = func(context.Context, *http.Request, string) error { accountCalls++; return nil }
	r.cfg.Authenticate = func(context.Context, *http.Request, string) error {
		generalCalls++
		return errors.New("synthetic denied")
	}
	if w := request(r, "GET", "/jobs", "", ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if accountCalls != 1 || generalCalls != 1 {
		t.Fatal(fmt.Sprintf("custom callbacks: %d %d", accountCalls, generalCalls))
	}
}
