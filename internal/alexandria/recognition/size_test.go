package recognition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestNotebookSizeAdmissionAndPersistentOverride(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	n := Notebook{ID: "large", Title: "Synthetic large notebook"}
	var ids []string
	for i := 1; i <= 101; i++ {
		id := fmt.Sprintf("page-%d", i)
		ids = append(ids, id)
		n.Pages = append(n.Pages, Page{ID: id, Number: i})
	}
	body, _ := json.Marshal(map[string]any{"uniqueId": n.ID, "type": 1, "status": 1, "title": n.Title, "pageNameList": ids})
	if _, e := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES('large','1-a','notebook','1',$1)`, body); e != nil {
		t.Fatal(e)
	}
	p, e := s.SizePolicy(ctx, "boox", n.ID, 0)
	if e != nil || p.Pages != 101 || p.Limit != 100 || !p.Limited {
		t.Fatal(p, e)
	}
	for i := 0; i < 3; i++ {
		if _, _, e = s.Queue(ctx, "boox", n, 1, 1, false, uuid.NewString()); !errors.Is(e, ErrNotebookTooLarge) {
			t.Fatal(e)
		}
	}
	// A stale partial catalog cannot evade the authoritative full size.
	partial := n
	partial.Pages = n.Pages[:1]
	if _, _, e = s.Queue(ctx, "boox", partial, 1, 1, false, uuid.NewString()); !errors.Is(e, ErrNotebookTooLarge) {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `INSERT INTO boox_page_index(notebook_id,page_id,page_number) VALUES('large','page-1',1)`); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	rows, e := s.Oversized(ctx, "Synthetic")
	if e != nil || len(rows) != 1 {
		t.Fatal(rows, e)
	}
	if e = s.AllowLarge(ctx, "boox", n.ID, true); e != nil {
		t.Fatal(e)
	}
	// Persisted across new store instances; source-scoped and independent of skips.
	p, e = (Store{DB: db}).SizePolicy(ctx, "boox", n.ID, 0)
	if e != nil || !p.AlwaysAllow || p.Limited {
		t.Fatal(p, e)
	}
	p, e = s.SizePolicy(ctx, "client", n.ID, 101)
	if e != nil || !p.Limited {
		t.Fatal(p, e)
	}
	if _, e = s.SkipNotebook(ctx, "boox", n.ID, true); e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.Queue(ctx, "boox", n, 1, 1, false, uuid.NewString()); !errors.Is(e, ErrNotebookSkipped) {
		t.Fatal(e)
	}
	if _, e = s.SkipNotebook(ctx, "boox", n.ID, false); e != nil {
		t.Fatal(e)
	}
	if _, count, e = s.Queue(ctx, "boox", n, 1, 101, false, uuid.NewString()); e != nil || count != 101 {
		t.Fatal(count, e)
	}
	// Model failed/paused work before revoking the override. Rejected controls
	// must report zero newly queued jobs rather than counting a cancellation.
	if _, e = db.ExecContext(ctx, `UPDATE boox_page_index SET state='failed' WHERE page_id='page-1'; UPDATE boox_page_index SET state='paused' WHERE page_id='page-2'`); e != nil {
		t.Fatal(e)
	}
	if e = s.AllowLarge(ctx, "boox", n.ID, false); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM boox_page_index WHERE state IN ('queued','processing')`).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM boox_page_index WHERE state='queued'`).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	for _, test := range []struct{ page, action string }{{"page-1", "retry"}, {"page-2", "resume"}} {
		var job string
		if e = db.QueryRowContext(ctx, `SELECT job_id::text FROM boox_page_index WHERE page_id=$1`, test.page).Scan(&job); e != nil {
			t.Fatal(e)
		}
		if count, e := s.Control(ctx, job, false, test.action); e != nil || count != 0 {
			t.Fatal("rejected control counted as queued", test.action, count, e)
		}
	}
	if _, e = db.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id='large'`); e != nil {
		t.Fatal(e)
	}
	if e = s.AllowLarge(ctx, "boox", n.ID, true); !errors.Is(e, ErrNotebookDeleted) {
		t.Fatal(e)
	}
}
func TestNotebookSizeBoundarySettingsAndGrowth(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	if _, e := db.ExecContext(ctx, `INSERT INTO fn_notebook(id,name,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('note','Size fixture',1,1,'test'); INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) SELECT 'p'||i,'note',i,1,1,'test' FROM generate_series(1,100) i`); e != nil {
		t.Fatal(e)
	}
	n, e := ClientNotebook(ctx, db, "note")
	if e != nil {
		t.Fatal(e)
	}
	// Existing sync admission may have queued these; a full 100-page notebook is allowed.
	if e = CheckSize(ctx, db, "client", "note", len(n.Pages)); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('p101','note',101,1,1,'test')`); e != nil {
		t.Fatal(e)
	}
	var active, before int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_page_dirty WHERE state IN ('queued','processing')`).Scan(&active); e != nil || active != 0 {
		t.Fatal(active, e)
	}
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `UPDATE alexandria_page_dirty SET state='queued',dirtied_at=clock_timestamp(); UPDATE alexandria_page_dirty SET state='queued',dirtied_at=clock_timestamp()`); e != nil {
		t.Fatal(e)
	}
	var after int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&after); e != nil || after != before {
		t.Fatal(before, after, e)
	}
	for _, limit := range []int{0, -1, 20001} {
		if e = s.SetPageLimit(ctx, limit, 100); e == nil {
			t.Fatal("accepted invalid cap", limit)
		}
	}
	if e = s.SetPageLimit(ctx, 101, 99); !errors.Is(e, ErrSelectionChanged) {
		t.Fatal(e)
	}
	if e = s.SetPageLimit(ctx, 101, 100); e != nil {
		t.Fatal(e)
	}
	if e = CheckSize(ctx, db, "client", "note", 101); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_page_dirty WHERE state='queued'`).Scan(&active); e != nil || active != 0 {
		t.Fatal("raising cap queued work", active, e)
	}
	n, e = ClientNotebook(ctx, db, "note")
	if e != nil {
		t.Fatal(e)
	}
	if _, active, e = s.Queue(ctx, "client", n, 1, 101, false, uuid.NewString()); e != nil || active != 101 {
		t.Fatal(active, e)
	}
	if e = s.SetPageLimit(ctx, 100, 101); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_page_dirty WHERE state='queued'`).Scan(&active); e != nil || active != 0 {
		t.Fatal(active, e)
	}
}

func TestLibrarySizeLimitUsesWholeNotebookAndExcludesWithoutJobs(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	if _, e := db.ExecContext(ctx, `UPDATE boox_identity SET native_uid='owner' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	n := Notebook{ID: "large", Title: "Synthetic large notebook"}
	for i := 1; i <= 101; i++ {
		n.Pages = append(n.Pages, Page{ID: fmt.Sprint(i), Number: i})
	}
	if _, e := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('large','1-a','notebook','1','owner','{"uniqueId":"large","type":1,"status":1}')`); e != nil {
		t.Fatal(e)
	}
	cat := func(context.Context, string, string) (Notebook, error) { return n, nil }
	id, e := s.CreateRun(ctx, "boox", uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if _, e = s.StepRun(ctx, cat, false); e != nil {
			t.Fatal(e)
		}
	}
	r, e := s.GetRun(ctx, id)
	if e != nil || len(r.Notebooks) != 1 || r.Notebooks[0].State != "excluded" {
		t.Fatal(r, e)
	}
	// An already approved run is also checked using its full size before chunking.
	if e = s.SetPageLimit(ctx, 200, 100); e != nil {
		t.Fatal(e)
	}
	id, e = s.CreateRun(ctx, "boox", uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if _, e = s.StepRun(ctx, cat, false); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.ApproveRun(ctx, id, nil, false); e != nil {
		t.Fatal(e)
	}
	if e = s.SetPageLimit(ctx, 100, 200); e != nil {
		t.Fatal(e)
	}
	if _, e = s.StepRun(ctx, cat, true); e != nil {
		t.Fatal(e)
	}
	r, e = s.GetRun(ctx, id)
	if e != nil || r.Queued != 0 || r.Notebooks[0].State != "excluded" {
		t.Fatal(r, e)
	}
	var jobs int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&jobs); e != nil || jobs != 0 {
		t.Fatal(jobs, e)
	}
}

func TestSizePolicyChangeWaitsForAdmittedQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	if _, e := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES('note','1-a','notebook','1','{"uniqueId":"note","type":1,"status":1,"pageNameList":["p1","p2"]}')`); e != nil {
		t.Fatal(e)
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if e = LockSizePolicy(ctx, tx); e != nil {
		t.Fatal(e)
	}
	var pid int
	if e = tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- s.SetPageLimit(ctx, 1, 100) }()
	for {
		var waiting bool
		if e = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case e := <-done:
			t.Fatal("policy changed before admission finished", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	n := Notebook{ID: "note", Pages: []Page{{ID: "p1", Number: 1}, {ID: "p2", Number: 2}}}
	if _, _, e = queueTx(ctx, tx, "boox", n, 1, 2, false, uuid.NewString(), false); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	var count int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM boox_page_index WHERE state IN ('queued','processing')`).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
}
