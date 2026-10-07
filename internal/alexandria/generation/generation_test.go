package generation_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

var ctx = context.Background()

const publisher = "0000000000000000000000000A"
const peer = "0000000000000000000000000B"

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func open(t *testing.T, url string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", url)
	check(t, err)
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { db.Close() })
	check(t, identity.EnsureSite(ctx, db))
	check(t, generation.Ensure(ctx, db))
	return db
}
func key(site string) (string, string) {
	raw := identity.TokenPrefix + strings.Repeat(strings.ToLower(site[len(site)-1:]), 64)
	hash := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(hash[:])
}
func enroll(t *testing.T, db *sql.DB, site string) {
	_, hash := key(site)
	check(t, (identity.Store{DB: db}).Enroll(ctx, identity.Enrollment{SiteID: site, TokenHash: hash}))
}
func current(t *testing.T, db *sql.DB) string {
	t.Helper()
	v, err := generation.Current(ctx, db)
	check(t, err)
	return v
}
func fixture(t *testing.T) (*sql.DB, string) {
	lib := testenv.Database(t)
	db := open(t, lib.URL)
	enroll(t, db, publisher)
	enroll(t, db, peer)
	_, err := db.Exec(`CREATE TABLE restore_fixture(value text); INSERT INTO restore_fixture VALUES('newer live content')`)
	check(t, err)
	return db, lib.URL
}
func request(t *testing.T, db *sql.DB, id string) generation.Request {
	return generation.Request{ID: strings.Repeat(id, 64), Expected: current(t, db), SnapshotHash: strings.Repeat("d", 64), Publisher: publisher}
}
func replace(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE restore_fixture SET value='restored backup'`)
	return err
}
func value(t *testing.T, db *sql.DB) string {
	t.Helper()
	var v string
	check(t, db.QueryRow(`SELECT value FROM restore_fixture`).Scan(&v))
	return v
}
func guarded(t *testing.T, db *sql.DB, claim context.Context, site string) error {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	check(t, err)
	defer tx.Rollback()
	return generation.CheckTx(claim, tx, site)
}

func TestReplacementRejectsOldAdmissionAndOldEnrollmentAcrossRestart(t *testing.T) {
	db, url := fixture(t)
	req := request(t, db, "1")
	oldPeer, _, err := generation.Admit(ctx, db, peer)
	check(t, err)
	oldPublisher, _, err := generation.Admit(ctx, db, publisher)
	check(t, err)
	receipt, err := generation.Publish(ctx, db, req, replace)
	check(t, err)
	if receipt.Replayed || receipt.Generation == req.Expected || value(t, db) != "restored backup" {
		t.Fatal("replacement not published")
	}
	for _, claim := range []struct {
		ctx  context.Context
		site string
	}{{oldPeer, peer}, {oldPublisher, publisher}} {
		if err = guarded(t, db, claim.ctx, claim.site); !errors.Is(err, generation.ErrReplaced) {
			t.Fatalf("in-flight request accepted: %v", err)
		}
	}
	if _, active, err := generation.Admit(ctx, db, peer); !errors.Is(err, generation.ErrReplaced) || active != receipt.Generation {
		t.Fatal("old peer admitted")
	}
	now, _, err := generation.Admit(ctx, db, publisher)
	check(t, err)
	check(t, guarded(t, db, now, publisher))
	if err = guarded(t, db, now, peer); !errors.Is(err, generation.ErrReplaced) {
		t.Fatal("borrowed another site's admission")
	}
	_, hash := key(peer)
	if err = (identity.Store{DB: db}).Enroll(ctx, identity.Enrollment{SiteID: peer, TokenHash: hash}); !errors.Is(err, generation.ErrReplaced) {
		t.Fatal("enrollment retry crossed fence")
	}
	db = open(t, url)
	if current(t, db) != receipt.Generation {
		t.Fatal("generation changed at startup")
	}
	if _, _, err = generation.Admit(ctx, db, peer); !errors.Is(err, generation.ErrReplaced) {
		t.Fatal("startup rebound peer")
	}
}

func TestFailedPublicationRollsBackContentGenerationAndReceipt(t *testing.T) {
	db, _ := fixture(t)
	req := request(t, db, "2")
	failure := errors.New("injected storage failure")
	_, err := generation.Publish(ctx, db, req, func(ctx context.Context, tx *sql.Tx) error {
		if err := replace(ctx, tx); err != nil {
			return err
		}
		return failure
	})
	if !errors.Is(err, failure) || current(t, db) != req.Expected || value(t, db) != "newer live content" {
		t.Fatal("failed restore leaked changes")
	}
	var n int
	check(t, db.QueryRow(`SELECT count(*) FROM sync_library_replacement`).Scan(&n))
	if n != 0 {
		t.Fatal("failed receipt persisted")
	}
	_, _, err = generation.Admit(ctx, db, peer)
	check(t, err)
	_, err = generation.Publish(ctx, db, req, replace)
	check(t, err)
}

func TestLostReplyRetriesAreExactAndCannotUndoLaterRestore(t *testing.T) {
	db, _ := fixture(t)
	req := request(t, db, "3")
	first, err := generation.Publish(ctx, db, req, replace)
	check(t, err)
	never := func(context.Context, *sql.Tx) error { t.Fatal("retry executed replacement again"); return nil }
	retry, err := generation.Publish(ctx, db, req, never)
	check(t, err)
	if !retry.Replayed || retry.Generation != first.Generation {
		t.Fatal("lost reply did not return same receipt")
	}
	changed := req
	changed.SnapshotHash = strings.Repeat("e", 64)
	if _, err = generation.Publish(ctx, db, changed, never); !errors.Is(err, generation.ErrConflict) {
		t.Fatal("changed retry accepted")
	}
	next := request(t, db, "4")
	_, err = generation.Publish(ctx, db, next, replace)
	check(t, err)
	if _, err = generation.Publish(ctx, db, req, never); !errors.Is(err, generation.ErrConflict) {
		t.Fatal("old retry reverted later restore")
	}
}

func TestReplacementCannotRemoveReservedPublicationState(t *testing.T) {
	for _, query := range []string{
		`DELETE FROM sync_library_generation`,
		`DELETE FROM sync_device_generation WHERE site_id='` + publisher + `'`,
	} {
		t.Run(query, func(t *testing.T) {
			db, _ := fixture(t)
			req := request(t, db, "a")
			_, err := generation.Publish(ctx, db, req, func(ctx context.Context, tx *sql.Tx) error {
				if err := replace(ctx, tx); err != nil {
					return err
				}
				_, err := tx.ExecContext(ctx, query)
				return err
			})
			if !errors.Is(err, generation.ErrConflict) || current(t, db) != req.Expected || value(t, db) != "newer live content" {
				t.Fatalf("invalid replacement escaped rollback: %v", err)
			}
			_, _, err = generation.Admit(ctx, db, publisher)
			check(t, err)
			_, err = generation.Publish(ctx, db, req, replace)
			check(t, err)
		})
	}
}

func TestCompetingRestoresHaveExactlyOneWinnerAcrossConnections(t *testing.T) {
	db, url := fixture(t)
	other := open(t, url)
	a, b := request(t, db, "5"), request(t, db, "6")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, pair := range []struct {
		db *sql.DB
		r  generation.Request
	}{{db, a}, {other, b}} {
		wg.Add(1)
		go func(i int, db *sql.DB, r generation.Request) {
			defer wg.Done()
			<-start
			_, errs[i] = generation.Publish(ctx, db, r, replace)
		}(i, pair.db, pair.r)
	}
	close(start)
	wg.Wait()
	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
		} else if !errors.Is(err, generation.ErrConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("got %d winners", winners)
	}
}

func TestRevocationAfterAdmissionIsRecheckedInsideTransaction(t *testing.T) {
	db, _ := fixture(t)
	claim, _, err := generation.Admit(ctx, db, peer)
	check(t, err)
	check(t, (identity.Store{DB: db}).Revoke(ctx, peer))
	if err = guarded(t, db, claim, peer); !errors.Is(err, generation.ErrReplaced) {
		t.Fatal("revoked in-flight request accepted")
	}
}

// PostgreSQL-specific: a mutation that reaches its fence check while a restore
// is mid-transaction must wait for it, then fail against the successor.
func TestInFlightMutationWaitsBehindPublishThenFails(t *testing.T) {
	db, url := fixture(t)
	other := open(t, url)
	claim, _, err := generation.Admit(ctx, db, peer)
	check(t, err)
	inside := make(chan struct{})
	release := make(chan struct{})
	published := make(chan error, 1)
	go func() {
		_, err := generation.Publish(ctx, db, request(t, db, "8"), func(ctx context.Context, tx *sql.Tx) error {
			close(inside)
			<-release
			return replace(ctx, tx)
		})
		published <- err
	}()
	<-inside
	guardResult := make(chan error, 1)
	go func() { guardResult <- guarded(t, other, claim, peer) }()
	select {
	case err := <-guardResult:
		t.Fatalf("fence check did not wait for the restore: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	check(t, <-published)
	if err := <-guardResult; !errors.Is(err, generation.ErrReplaced) {
		t.Fatalf("mutation crossed a concurrent restore: %v", err)
	}
}
