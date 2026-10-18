package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestSameOwnerReclaimRejectsOldAttempt(t *testing.T) {
	db := testenv.Database(t).DB
	ctx := context.Background()
	store := NewStore(db)
	id, _, err := store.Enqueue(ctx, "blob.verify", "lease-test", nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimID(ctx, id, "same-host", time.Minute)
	if err != nil || first == nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE alexandria_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if n, err := store.RequeueExpired(ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	second, err := store.ClaimID(ctx, id, "same-host", time.Minute)
	if err != nil || second == nil {
		t.Fatal(err)
	}
	if second.Attempts != first.Attempts+1 {
		t.Fatal("attempt not advanced")
	}
	if err = store.Complete(ctx, *first, "same-host"); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("stale success accepted", err)
	}
	if err = store.Fail(ctx, *first, "same-host", errors.New("synthetic")); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("stale failure accepted", err)
	}
	if err = store.Fail(ctx, *second, "same-host", errors.New("synthetic terminal")); err != nil {
		t.Fatal(err)
	}
	var status string
	if err = db.QueryRowContext(ctx, `SELECT status FROM alexandria_jobs WHERE id=$1`, id).Scan(&status); err != nil || status != "failed" {
		t.Fatal(status, err)
	}
	if job, err := store.ClaimID(ctx, id, "same-host", time.Minute); err != nil || job != nil {
		t.Fatal("terminal job reclaimed", err)
	}
	id, _, err = store.Enqueue(ctx, "blob.verify", "success-test", nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimID(ctx, id, "same-host", time.Minute)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	if err = store.Complete(ctx, *job, "same-host"); err != nil {
		t.Fatal(err)
	}
	if err = store.Complete(ctx, *job, "same-host"); !errors.Is(err, ErrLeaseLost) {
		t.Fatal("duplicate success accepted", err)
	}
}
