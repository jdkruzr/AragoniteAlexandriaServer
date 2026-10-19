package recognition

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestCleanupRechecksSourceAndRetainsHistory(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	add := func(id, status, state, detail string, pages any, metadata bool) {
		t.Helper()

		_, _, err := s.Queue(ctx, "boox", Notebook{ID: id, Title: id, Pages: []Page{{ID: "page", Number: 1}}}, 1, 1, false, uuid.NewString())
		if err != nil {
			t.Fatal(err)
		}
		// Recreate legacy blocked jobs; current enqueueing refuses deleted sources.
		if metadata {
			body, _ := json.Marshal(map[string]any{"uniqueId": id, "title": id, "type": 1, "status": status, "pageNameList": pages})
			exec(`INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES($1,'1-a','notebook','1',$2)`, id, body)
		}
		exec(`UPDATE boox_page_index SET state=$2,detail=$3 WHERE notebook_id=$1`, id, state, detail)
	}
	add("deleted", "0", "blocked", "Page was removed.", []string{"page"}, true)
	add("removed-page", "1", "blocked", "Page was removed.", []string{"other"}, true)
	add("active", "1", "blocked", "Page was removed.", []string{"page"}, true)
	add("missing", "0", "blocked", "Page was removed.", nil, false)
	add("malformed", "1", "blocked", "Page was removed.", "bad json", true)
	add("empty", "1", "blocked", "Page was removed.", []string{}, true)
	add("unsupported", "1", "blocked", "Unsupported ink", []string{"page"}, true)
	add("processing", "0", "processing", "Page was removed.", []string{"page"}, true)
	add("failed", "0", "failed", "Provider unavailable", []string{"page"}, true)
	preview, err := s.PreviewCleanup(ctx)
	if err != nil || len(preview.Items) != 2 {
		t.Fatal(preview, err)
	}
	// A concurrent restore makes the submitted preview stale, even if the job
	// still says blocked. Nothing from that submission is hidden.
	exec(`UPDATE boox_projection SET body=jsonb_set(body,'{status}','"1"'),revision='2-restored' WHERE document_id='deleted'`)
	exec(`UPDATE boox_page_index SET state='blocked',detail='Page was removed.' WHERE notebook_id='deleted'`)
	if _, err = s.CleanObsolete(ctx, preview.Token); err != ErrSelectionChanged {
		t.Fatal(err)
	}
	var hidden int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job WHERE cleaned_at IS NOT NULL`).Scan(&hidden); err != nil || hidden != 0 {
		t.Fatal(hidden, err)
	}
	preview, err = s.PreviewCleanup(ctx)
	if err != nil || len(preview.Items) != 1 || preview.Items[0].Notebook != "removed-page" {
		t.Fatal(preview, err)
	}
	if n, err := s.CleanObsolete(ctx, preview.Token); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = s.CleanObsolete(ctx, preview.Token); err != ErrSelectionChanged {
		t.Fatal("replayed preview", err)
	}
	v, err := s.List(ctx, Filter{})
	if err != nil || v.Total != 8 {
		t.Fatal(v.Total, err)
	}
	history, err := s.List(ctx, Filter{History: true})
	if err != nil || history.Total != 10 {
		t.Fatal(history.Total, err)
	} // includes the restore's superseded request
	var batch string
	for _, j := range history.Jobs {
		if j.Cleaned {
			batch = j.Batch
		}
	}
	if batch == "" {
		t.Fatal("cleaned history missing")
	}
	filtered, err := s.List(ctx, Filter{Batch: batch})
	if err != nil || filtered.Total != 0 {
		t.Fatal(filtered.Total, err)
	}
	// Completed library progress must not fall below 100% after cleanup.
	runID := uuid.NewString()
	exec(`INSERT INTO alexandria_recognition_run(id,state) VALUES($1,'queued')`, runID)
	exec(`INSERT INTO alexandria_recognition_run_notebook(run_id,notebook_id,title,state,pages,queued,cursor) VALUES($1,'removed-page','Synthetic','queued','[{"ID":"page","Number":1}]',1,1)`, runID)
	exec(`UPDATE alexandria_recognition_batch SET run_id=$1 WHERE id=$2`, runID, batch)
	run, err := s.GetRun(ctx, runID)
	if err != nil || run.Blocked != 0 || run.Finished != 1 || run.Percent() != 100 || run.Counts["cleared"] != 1 {
		t.Fatal(run, err)
	}
	recent, err := s.RecentRuns(ctx)
	if err != nil || len(recent) != 1 || recent[0].Blocked != 0 || recent[0].Label() != "Complete" {
		t.Fatal(recent, err)
	}
	// Clearing a list never changes native content or removes source queues.
	var state string
	if err = db.QueryRowContext(ctx, `SELECT state FROM boox_page_index WHERE notebook_id='removed-page'`).Scan(&state); err != nil || state != "blocked" {
		t.Fatal(state, err)
	}
	// Real restoration still produces fresh, visible work through existing
	// queue triggers, without resurrecting the cleaned attempt.
	exec(`UPDATE boox_projection SET body=jsonb_set(body,'{pageNameList}','["page"]'),revision='2-returned' WHERE document_id='removed-page'`)
	var fresh int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job WHERE notebook_id='removed-page' AND state='queued' AND cleaned_at IS NULL AND superseded_by IS NULL`).Scan(&fresh); err != nil || fresh != 1 {
		t.Fatal(fresh, err)
	}
}

func TestCleanupTokenTracksIdentityNotJustCount(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	for _, id := range []string{"a", "b"} {

		if _, _, err := s.Queue(ctx, "boox", Notebook{ID: id, Pages: []Page{{ID: "p", Number: 1}}}, 1, 1, false, uuid.NewString()); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"uniqueId": id, "type": 1, "status": 0})
		if _, err := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES($1,'1-a','notebook','1',$2)`, id, body); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE boox_page_index SET state='blocked',detail='Page was removed.' WHERE notebook_id='a'`); err != nil {
		t.Fatal(err)
	}
	p, err := s.PreviewCleanup(ctx)
	if err != nil || len(p.Items) != 1 {
		t.Fatal(p, err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE boox_page_index SET detail='Other failure' WHERE notebook_id='a'; UPDATE boox_page_index SET state='blocked',detail='Page was removed.' WHERE notebook_id='b'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CleanObsolete(ctx, p.Token); err != ErrSelectionChanged {
		t.Fatal(err)
	}
}

func TestCleanupIsBoundedAndCanContinue(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	// This test exercises large-batch behavior with an explicitly raised cap.
	if err := s.SetPageLimit(ctx, 1000, 100); err != nil {
		t.Fatal(err)
	}

	pages := make([]Page, 201)
	for i := range pages {
		pages[i] = Page{ID: uuid.NewString(), Number: i + 1}
	}
	if _, _, err := s.Queue(ctx, "boox", Notebook{ID: "many", Pages: pages}, 1, len(pages), false, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES('many','1-a','notebook','1','{"uniqueId":"many","type":1,"status":0}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE boox_page_index SET state='blocked',detail='Page was removed.' WHERE notebook_id='many'`); err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{200, 1, 0} {
		p, err := s.PreviewCleanup(ctx)
		if err != nil || len(p.Items) != want {
			t.Fatal(len(p.Items), err)
		}
		if n, err := s.CleanObsolete(ctx, p.Token); err != nil || n != want {
			t.Fatal(n, err)
		}
	}
	h, err := s.List(ctx, Filter{History: true})
	if err != nil || h.Total != 201 {
		t.Fatal(h.Total, err)
	}
}
