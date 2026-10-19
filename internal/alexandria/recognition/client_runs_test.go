package recognition

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"strings"
	"testing"
)

func TestClientLibraryRunInventoryChunksAndSourceIsolation(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	// This test exercises large-batch behavior with an explicitly raised cap.
	if err := s.SetPageLimit(ctx, 1000, 100); err != nil {
		t.Fatal(err)
	}
	if _, e := db.ExecContext(ctx, `INSERT INTO fn_notebook(id,name,deleted_at,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('note','Client notebook',NULL,1,1,'test'),('gone','Deleted',1,1,1,'test'); INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) SELECT 'p'||i,'note',i,1,1,'test' FROM generate_series(1,205) i; INSERT INTO fn_page(id,notebook_id,deleted_at,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('deleted-page','note',1,1,1,'test'); DELETE FROM alexandria_page_dirty; DELETE FROM alexandria_recognition_job;`); e != nil {
		t.Fatal(e)
	}
	// Identical source-local notebook/page IDs must not cross queue boundaries.
	native := Notebook{ID: "note", Title: "BOOX notebook", Pages: []Page{{ID: "p1", Number: 1}}}
	nativeBatch, _, e := s.Queue(ctx, "boox", native, 1, 1, false, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	id, e := s.CreateRun(ctx, "client", uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateRun(ctx, "boox", id); e == nil {
		t.Fatal("cross-source token replay accepted")
	}
	if _, e = s.CreateRun(ctx, "unknown", uuid.NewString()); e == nil {
		t.Fatal("unknown source accepted")
	}
	cat := func(ctx context.Context, source, id string) (Notebook, error) {
		if source != "client" {
			return Notebook{}, fmt.Errorf("wrong source %q", source)
		}
		return ClientNotebook(ctx, db, id)
	}
	for i := 0; i < 2; i++ {
		if _, e = s.StepRun(ctx, cat, false); e != nil {
			t.Fatal(e)
		}
	}
	r, e := s.GetRun(ctx, id)
	if e != nil || r.Source != "client" || r.Total != 1 || r.Pages != 205 || r.State != "review" {
		t.Fatal(r, e)
	}
	if !strings.HasPrefix(r.NotebookURL("note"), "/files/forestnote?") {
		t.Fatal("wrong notebook link", r.NotebookURL("note"))
	}
	if e = s.ApproveRun(ctx, id, nil, false); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if _, e = s.StepRun(ctx, cat, true); e != nil {
			t.Fatal(e)
		}
	}
	r, e = s.GetRun(ctx, id)
	if e != nil || r.Queued != 205 || r.Pending != 205 {
		t.Fatal(r, e)
	}
	if count, e := s.ControlRun(ctx, id, "pause"); e != nil || count != 205 {
		t.Fatal(count, e)
	}
	nativeView, e := s.List(ctx, Filter{Batch: nativeBatch})
	if e != nil || nativeView.Counts["queued"] != 1 {
		t.Fatal("client control touched BOOX", nativeView, e)
	}
	if _, e = s.ControlRun(ctx, id, "resume"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `UPDATE alexandria_page_dirty SET state='failed' WHERE page_id IN ('p1','p2')`); e != nil {
		t.Fatal(e)
	}
	if count, e := s.ControlRun(ctx, id, "retry"); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	r, e = s.GetRun(ctx, id)
	if e != nil || r.Pending != 205 || r.Failed != 0 {
		t.Fatal(r, e)
	}
	if _, e = s.ControlRun(ctx, id, "cancel"); e != nil {
		t.Fatal(e)
	}
	r, e = s.GetRun(ctx, id)
	if e != nil || r.Cancelled != 205 {
		t.Fatal(r, e)
	}
}
func TestClientLibraryCatalogChangesRequireNewReview(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	if _, e := db.ExecContext(ctx, `INSERT INTO fn_notebook(id,name,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('note','Client notebook',1,1,'test'); INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('p','note',0,1,1,'test'); DELETE FROM alexandria_page_dirty;`); e != nil {
		t.Fatal(e)
	}
	id, e := s.CreateRun(ctx, "client", uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	cat := func(ctx context.Context, source, id string) (Notebook, error) { return ClientNotebook(ctx, db, id) }
	for i := 0; i < 2; i++ {
		if _, e = s.StepRun(ctx, cat, false); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.ApproveRun(ctx, id, nil, false); e != nil {
		t.Fatal(e)
	}
	if _, e = db.ExecContext(ctx, `UPDATE fn_page SET deleted_at=1 WHERE id='p'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.StepRun(ctx, cat, true); e != nil {
		t.Fatal(e)
	}
	r, e := s.GetRun(ctx, id)
	if e != nil || r.Unavailable != 1 || r.Queued != 0 {
		t.Fatal(r, e)
	}
}
