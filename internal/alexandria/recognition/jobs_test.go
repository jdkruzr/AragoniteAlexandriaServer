package recognition

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"testing"
)

func TestBatchControlsHistoryIdempotencyAndPagination(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	n := Notebook{ID: "synthetic", Title: "Test notebook"}
	for i := 1; i <= 55; i++ {
		n.Pages = append(n.Pages, Page{ID: fmt.Sprintf("p%03d", i), Number: i})
	}
	token := uuid.NewString()
	batch, count, e := s.Queue(ctx, "boox", n, 1, 55, false, token)
	if e != nil || count != 55 {
		t.Fatal(count, e)
	}
	_, count, e = s.Queue(ctx, "boox", n, 1, 55, false, token)
	if e != nil || count != 55 {
		t.Fatal("replayed request", count, e)
	}
	v, e := s.List(ctx, Filter{Batch: batch})
	if e != nil || v.Total != 55 || len(v.Jobs) != 50 {
		t.Fatal(v.Total, len(v.Jobs), e)
	}
	v, e = s.List(ctx, Filter{Batch: batch, Offset: 50})
	if e != nil || len(v.Jobs) != 5 {
		t.Fatal(len(v.Jobs), e)
	}
	if count, e := s.Control(ctx, batch, true, "pause"); e != nil || count != 55 {
		t.Fatal(count, e)
	}
	var claim int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM boox_page_index WHERE state IN ('queued','processing')`).Scan(&claim); e != nil || claim != 0 {
		t.Fatal(claim, e)
	}
	if _, _, e = s.Queue(ctx, "boox", n, 1, 55, false, uuid.NewString()); e == nil {
		t.Fatal("paused jobs replaced")
	}
	if _, e = s.Control(ctx, batch, true, "resume"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Control(ctx, batch, true, "cancel"); e != nil {
		t.Fatal(e)
	}
	v, e = s.List(ctx, Filter{Batch: batch})
	if e != nil || v.Counts["cancelled"] != 55 {
		t.Fatal(v.Counts, e)
	}
	batch2, _, e := s.Queue(ctx, "boox", n, 1, 1, false, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `UPDATE boox_page_index SET state='failed',detail='Synthetic permanent failure' WHERE page_id='p001'`); e != nil {
		t.Fatal(e)
	}
	if count, e := s.Control(ctx, batch2, true, "retry"); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	v, e = s.List(ctx, Filter{Batch: batch2})
	if e != nil || v.Total != 1 || v.Counts["queued"] != 1 {
		t.Fatal(v, e)
	}
	v, e = s.List(ctx, Filter{State: "failed", History: true})
	if e != nil || v.Total != 1 {
		t.Fatal("failure history lost", v, e)
	}
	v, e = s.List(ctx, Filter{State: "failed"})
	if e != nil || v.Total != 0 {
		t.Fatal("superseded failure counted as current", v, e)
	}
}

func TestClientQueueHistoryAndCancellationSurviveSourceUpdates(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	// Queue ledger tolerates mirror rows not yet materialized; production catalog validates them.
	n := Notebook{ID: "n", Title: "Synthetic", Pages: []Page{{ID: "p", Number: 1}}}
	b, _, e := s.Queue(ctx, "client", n, 1, 1, false, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Control(ctx, b, true, "cancel"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `UPDATE alexandria_page_dirty SET dirtied_at=clock_timestamp(),next_at=now() WHERE page_id='p'`); e != nil {
		t.Fatal(e)
	}
	v, e := s.List(ctx, Filter{Batch: b})
	if e != nil || v.Counts["cancelled"] != 1 {
		t.Fatal(v, e)
	}
	b2, _, e := s.Queue(ctx, "client", n, 1, 1, false, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `UPDATE alexandria_page_dirty SET state='ready' WHERE page_id='p'`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `DELETE FROM alexandria_page_dirty WHERE page_id='p'`); e != nil {
		t.Fatal(e)
	}
	v, e = s.List(ctx, Filter{Batch: b2})
	if e != nil || v.Counts["ready"] != 1 || v.Jobs[0].Finished == nil {
		t.Fatal(v, e)
	}
}

func TestRetryMatchingRetriesTheWholeFilteredList(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	n := Notebook{ID: "synthetic", Title: "Test notebook"}
	for i := 1; i <= 60; i++ {
		n.Pages = append(n.Pages, Page{ID: fmt.Sprintf("p%03d", i), Number: i})
	}
	if _, _, e := s.Queue(ctx, "boox", n, 1, 60, false, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	// More than one page of results: the button acts on all of them.
	if _, e := db.ExecContext(ctx, `UPDATE boox_page_index SET state='blocked',detail='Synthetic preview gap' WHERE page_number<=55`); e != nil {
		t.Fatal(e)
	}
	f := Filter{State: "blocked"}
	v, e := s.List(ctx, f)
	if e != nil || v.Total != 55 || len(v.Jobs) != 50 {
		t.Fatal(v.Total, len(v.Jobs), e)
	}
	if _, e = s.RetryMatching(ctx, f, 54); e != ErrSelectionChanged {
		t.Fatal("stale count accepted", e)
	}
	if _, e = s.RetryMatching(ctx, Filter{State: "ready"}, 5); e == nil {
		t.Fatal("ready jobs retried")
	}
	if count, e := s.RetryMatching(ctx, f, 55); e != nil || count != 55 {
		t.Fatal(count, e)
	}
	if v, e = s.List(ctx, f); e != nil || v.Total != 0 {
		t.Fatal(v.Total, e)
	}
	if v, e = s.List(ctx, Filter{}); e != nil || v.Counts["queued"] != 60 {
		t.Fatal(v.Counts, e)
	}
	// Earlier attempts are history: retrying them again changes nothing.
	h := Filter{State: "blocked", History: true}
	if v, e = s.List(ctx, h); e != nil || v.Total != 55 {
		t.Fatal(v.Total, e)
	}
	if count, e := s.RetryMatching(ctx, h, 55); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}
