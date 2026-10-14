package recognition

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func pageState(t *testing.T, db *sql.DB, page string) (string, string) {
	t.Helper()
	var state, detail string
	if e := db.QueryRowContext(context.Background(), `SELECT state,detail FROM boox_page_index WHERE page_id=$1`, page).Scan(&state, &detail); e != nil {
		t.Fatal(page, e)
	}
	return state, detail
}

func jobOf(t *testing.T, db *sql.DB, page string) string {
	t.Helper()
	var id string
	if e := db.QueryRowContext(context.Background(), `SELECT job_id::text FROM boox_page_index WHERE page_id=$1`, page).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}

// changeOnDevice fires the BOOX invalidation trigger the way sync does.
func changeOnDevice(t *testing.T, db *sql.DB, notebook string) {
	t.Helper()
	if _, e := db.ExecContext(context.Background(), `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,$2,'notebook','1','u','{}')
 ON CONFLICT(document_id) DO UPDATE SET revision=EXCLUDED.revision`, notebook, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
}

func TestSkippedPagesStayOutOfRecognition(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	n := Notebook{ID: "nb", Title: "Synthetic"}
	for i := 1; i <= 3; i++ {
		n.Pages = append(n.Pages, Page{ID: fmt.Sprintf("p%d", i), Number: i})
	}
	if _, _, e := s.Queue(ctx, "boox", n, 1, 3, false, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ExecContext(ctx, `UPDATE boox_page_index SET state='blocked' WHERE page_id IN ('p1','p2')`); e != nil {
		t.Fatal(e)
	}
	if c, e := s.Control(ctx, jobOf(t, db, "p1"), false, "skip"); e != nil || c != 1 {
		t.Fatal(c, e)
	}
	if st, d := pageState(t, db, "p1"); st != "skipped" || d != PageSkippedDetail {
		t.Fatal(st, d)
	}
	// Bulk skip acts on the rest of the blocked list.
	if c, e := s.ActOnMatching(ctx, Filter{State: "blocked"}, 1, "skip"); e != nil || c != 1 {
		t.Fatal(c, e)
	}
	// Nothing re-queues a skipped page: a new library request, a retry of
	// everything failed or blocked, or a change on the device.
	if _, _, e := s.Queue(ctx, "boox", n, 1, 3, false, uuid.NewString()); e != nil && !errors.Is(e, ErrAlreadyActive) {
		t.Fatal(e)
	}
	changeOnDevice(t, db, "nb")
	for _, p := range []string{"p1", "p2"} {
		if st, _ := pageState(t, db, p); st != "skipped" {
			t.Fatal(p, st)
		}
	}
	if st, _ := pageState(t, db, "p3"); st != "queued" {
		t.Fatal("unskipped page not invalidated", st)
	}
	v, e := s.List(ctx, Filter{State: "skipped"})
	if e != nil || v.Total != 2 {
		t.Fatal(v.Total, e)
	}
	// Only un-skip brings a page back.
	if c, e := s.Control(ctx, jobOf(t, db, "p1"), false, "unskip"); e != nil || c != 1 {
		t.Fatal(c, e)
	}
	if st, _ := pageState(t, db, "p1"); st != "queued" {
		t.Fatal(st)
	}
}

func TestSkippedNotebooksStayOutOfRecognition(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	n := Notebook{ID: "nb", Title: "Synthetic"}
	for i := 1; i <= 4; i++ {
		n.Pages = append(n.Pages, Page{ID: fmt.Sprintf("p%d", i), Number: i})
	}
	if _, _, e := s.Queue(ctx, "boox", n, 1, 3, false, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ExecContext(ctx, `UPDATE boox_page_index SET state='ready',text='synthetic' WHERE page_id='p1'; UPDATE boox_page_index SET state='failed' WHERE page_id='p2'`); e != nil {
		t.Fatal(e)
	}
	// p3 was skipped by hand before the notebook was.
	if _, e := s.Control(ctx, jobOf(t, db, "p3"), false, "skip"); e != nil {
		t.Fatal(e)
	}
	if c, e := s.SkipNotebook(ctx, "boox", "nb", true); e != nil || c != 1 {
		t.Fatal(c, e)
	}
	if on, e := s.NotebookSkipped(ctx, "boox", "nb"); e != nil || !on {
		t.Fatal(on, e)
	}
	if st, _ := pageState(t, db, "p1"); st != "ready" {
		t.Fatal("recognized page lost its text", st)
	}
	if st, d := pageState(t, db, "p2"); st != "skipped" || d != NotebookSkippedDetail {
		t.Fatal(st, d)
	}
	if _, _, e := s.Queue(ctx, "boox", n, 1, 4, false, uuid.NewString()); !errors.Is(e, ErrNotebookSkipped) {
		t.Fatal("skipped notebook queued", e)
	}
	// A page that reaches the queue by any other path starts skipped.
	if _, e := db.ExecContext(ctx, `INSERT INTO boox_page_index(notebook_id,page_id,page_number) VALUES('nb','p4',4)`); e != nil {
		t.Fatal(e)
	}
	if st, _ := pageState(t, db, "p4"); st != "skipped" {
		t.Fatal(st)
	}
	// A change on the device doesn't bring it back.
	changeOnDevice(t, db, "nb")
	for _, p := range []string{"p1", "p2", "p3", "p4"} {
		if st, _ := pageState(t, db, p); st != "skipped" {
			t.Fatal(p, st)
		}
	}
	// Letting the notebook back in releases what the notebook skipped; the
	// page skipped by hand stays skipped.
	if c, e := s.SkipNotebook(ctx, "boox", "nb", false); e != nil || c != 3 {
		t.Fatal(c, e)
	}
	if st, _ := pageState(t, db, "p3"); st != "skipped" {
		t.Fatal(st)
	}
	if st, _ := pageState(t, db, "p2"); st != "cancelled" {
		t.Fatal(st)
	}
	if _, _, e := s.Queue(ctx, "boox", n, 1, 4, false, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if st, _ := pageState(t, db, "p2"); st != "queued" {
		t.Fatal(st)
	}
}

func TestSkippedClientNotebookAndPages(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	if _, e := db.ExecContext(ctx, `INSERT INTO fn_notebook(id,name,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('cn','Client',1,1,'test');
 INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('c1','cn',0,1,1,'test'),('c2','cn',1,1,1,'test')`); e != nil {
		t.Fatal(e)
	}
	n := Notebook{ID: "cn", Title: "Client", Pages: []Page{{ID: "c1", Number: 1}}}
	if _, _, e := s.Queue(ctx, "client", n, 1, 1, false, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	state := func(page string) string {
		var st string
		if e := db.QueryRowContext(ctx, `SELECT state FROM alexandria_page_dirty WHERE page_id=$1`, page).Scan(&st); e != nil {
			t.Fatal(e)
		}
		return st
	}
	var job string
	db.QueryRowContext(ctx, `SELECT job_id::text FROM alexandria_page_dirty WHERE page_id='c1'`).Scan(&job)
	if _, e := s.Control(ctx, job, false, "skip"); e != nil {
		t.Fatal(e)
	}
	// An edit synced from a device, or "reprocess", doesn't undo the skip.
	if _, e := db.ExecContext(ctx, `UPDATE alexandria_page_dirty SET state='queued',dirtied_at=clock_timestamp() WHERE page_id='c1'`); e != nil {
		t.Fatal(e)
	}
	if st := state("c1"); st != "skipped" {
		t.Fatal(st)
	}
	// A new page synced into a skipped notebook starts skipped.
	if _, e := s.SkipNotebook(ctx, "client", "cn", true); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ExecContext(ctx, `INSERT INTO alexandria_page_dirty(page_id) VALUES('c2')`); e != nil {
		t.Fatal(e)
	}
	if st := state("c2"); st != "skipped" {
		t.Fatal(st)
	}
}
