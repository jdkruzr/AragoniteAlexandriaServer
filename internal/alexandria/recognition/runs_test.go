package recognition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"testing"
	"time"
)

func TestLibraryReviewDispatchControlsAndRetries(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	// Use one admitted connection, as production does: nested catalog transactions must not deadlock.
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s := Store{DB: conn}
	if _, err = conn.ExecContext(ctx, `UPDATE boox_identity SET native_uid='owner' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c", "foreign", "deleted"} {
		uid, status := "owner", 1
		if id == "foreign" {
			uid = "other"
		}
		if id == "deleted" {
			status = 0
		}
		raw, _ := json.Marshal(map[string]any{"uniqueId": id, "type": 1, "status": status, "title": "Notebook " + id})
		if _, err = conn.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-a','notebook','1',$2,$3)`, id, uid, raw); err != nil {
			t.Fatal(err)
		}
	}
	cat := func(ctx context.Context, source, id string) (Notebook, error) {
		tx, e := conn.BeginTx(ctx, nil)
		if e != nil {
			return Notebook{}, e
		}
		defer tx.Rollback()
		return Notebook{ID: id, Title: "Notebook " + id, Pages: []Page{{ID: id + "1", Number: 1}, {ID: id + "2", Number: 2}}}, tx.Commit()
	}
	id, err := s.CreateRun(ctx, "boox", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.CreateRun(ctx, "boox", id); err != nil || replay != id {
		t.Fatal(replay, err)
	}
	for i := 0; i < 4; i++ {
		if ok, e := s.StepRun(ctx, cat, false); !ok || e != nil {
			t.Fatal(ok, e)
		}
	}
	r, err := s.GetRun(ctx, id)
	if err != nil || r.State != "review" || r.Total != 3 || r.Pages != 6 {
		t.Fatal(r, err)
	}
	var jobs int
	if err = conn.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal("preparation queued OCR", jobs, err)
	}
	// Existing active job stays outside the new run.
	a, _ := cat(ctx, "boox", "a")
	if _, _, err = s.Queue(ctx, "boox", a, 1, 1, false, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveRun(ctx, id, []string{"b"}, false); err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveRun(ctx, id, nil, true); err == nil {
		t.Fatal("approval replay changed selection")
	}
	if ok, e := s.StepRun(ctx, cat, false); ok || e != nil {
		t.Fatal("disabled OCR dispatch", ok, e)
	}
	if _, err = s.ControlRun(ctx, id, "pause"); err != nil {
		t.Fatal(err)
	}
	if ok, e := s.StepRun(ctx, cat, true); ok || e != nil {
		t.Fatal("paused dispatch", ok, e)
	}
	if _, err = s.ControlRun(ctx, id, "resume"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if ok, e := s.StepRun(ctx, cat, true); !ok || e != nil {
			t.Fatal(ok, e)
		}
	}
	r, err = s.GetRun(ctx, id)
	if err != nil || r.Queued != 3 || r.Skipped != 1 || r.Excluded != 1 || r.Pending != 3 {
		t.Fatal(r, err)
	}
	// A worker completion plus two failures, then one-click run retry.
	if _, err = conn.ExecContext(ctx, `UPDATE boox_page_index SET state=CASE WHEN page_id='a2' THEN 'ready' ELSE 'failed' END WHERE page_id<>'a1'`); err != nil {
		t.Fatal(err)
	}
	r, err = s.GetRun(ctx, id)
	if err != nil || r.Ready != 1 || r.Failed != 2 {
		t.Fatal(r, err)
	}
	if count, e := s.ControlRun(ctx, id, "retry"); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	r, err = s.GetRun(ctx, id)
	if err != nil || r.Ready != 1 || r.Pending != 2 || r.Failed != 0 {
		t.Fatal(r, err)
	}
	v, err := s.List(ctx, Filter{Run: id, State: "queued"})
	if err != nil || v.Total != 2 || v.Counts["ready"] != 1 {
		t.Fatal(v, err)
	}
	if count, e := s.ControlRun(ctx, id, "cancel"); e != nil || count != 2 {
		t.Fatal(count, e)
	}
	r, err = s.GetRun(ctx, id)
	if err != nil || r.Cancelled != 2 || r.Ready != 1 {
		t.Fatal(r, err)
	}
	// Seen live: a page in the run failed, then a change on the device
	// re-queued it and it succeeded. The run kept counting the old failure.
	if _, err = conn.ExecContext(ctx, `UPDATE boox_page_index SET state='failed' WHERE page_id='c1'`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `UPDATE boox_page_index SET state='queued' WHERE page_id='c1'`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `UPDATE boox_page_index SET state='ready' WHERE page_id='c1'`); err != nil {
		t.Fatal(err)
	}
	r, err = s.GetRun(ctx, id)
	if err != nil || r.Failed != 0 {
		t.Fatal("replaced failure still counted", r.Failed, err)
	}
	runs, err := s.RecentRuns(ctx)
	if err != nil || len(runs) == 0 || runs[0].Failed != 0 {
		t.Fatal("recent runs count replaced failure", runs, err)
	}
}
func TestLibraryRunCrashLeaseChangedCatalogAndCancellation(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	id := uuid.NewString()
	if _, err := db.ExecContext(ctx, `INSERT INTO alexandria_recognition_run(id,lease_token,lease_until) VALUES($1,'crashed',now()-interval '1 second')`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO alexandria_recognition_run_notebook(run_id,notebook_id,title) VALUES($1,'n','Notebook')`, id); err != nil {
		t.Fatal(err)
	}
	n := Notebook{ID: "n", Title: "Notebook", Pages: []Page{{ID: "p", Number: 1}}}
	cat := func(context.Context, string, string) (Notebook, error) { return n, nil }
	if ok, e := s.StepRun(ctx, cat, false); !ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, e := s.StepRun(ctx, cat, false); e != nil {
		t.Fatal(e)
	}
	if e := s.ApproveRun(ctx, id, nil, false); e != nil {
		t.Fatal(e)
	}
	n.Pages = append(n.Pages, Page{ID: "new", Number: 2})
	if _, e := s.StepRun(ctx, cat, true); e != nil {
		t.Fatal(e)
	}
	r, e := s.GetRun(ctx, id)
	if e != nil || r.Unavailable != 1 || r.Queued != 0 {
		t.Fatal(r, e)
	}
	// Preparation failure is explicit and retryable, then cancellation fences its in-flight completion.
	if _, e = db.ExecContext(ctx, `UPDATE alexandria_recognition_run SET state='review' WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ControlRun(ctx, id, "prepare"); e != nil {
		t.Fatal(e)
	}
	bad := func(context.Context, string, string) (Notebook, error) {
		return Notebook{}, errors.New("synthetic private error")
	}
	if _, e = s.StepRun(ctx, bad, false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.StepRun(ctx, bad, false); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ControlRun(ctx, id, "prepare"); e != nil {
		t.Fatal(e)
	}
	cancel := func(ctx context.Context, _, _ string) (Notebook, error) {
		_, e := s.ControlRun(ctx, id, "cancel")
		return n, e
	}
	if _, e = s.StepRun(ctx, cancel, false); e != nil {
		t.Fatal(e)
	}
	r, e = s.GetRun(ctx, id)
	if e != nil || r.State != "cancelled" || r.Queued != 0 || r.Prepared != 0 {
		t.Fatal(r, e)
	}
}

func TestLibraryDispatchChunksResumeWithoutDuplicatingJobs(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	id := uuid.NewString()
	n := Notebook{ID: "large", Title: "Large synthetic notebook"}
	for i := 1; i <= 235; i++ {
		n.Pages = append(n.Pages, Page{ID: fmt.Sprintf("p%03d", i), Number: i})
	}
	raw, _ := json.Marshal(n.Pages)
	if _, e := db.ExecContext(ctx, `INSERT INTO alexandria_recognition_run(id,state) VALUES($1,'queued')`, id); e != nil {
		t.Fatal(e)
	}
	if _, e := db.ExecContext(ctx, `INSERT INTO alexandria_recognition_run_notebook(run_id,notebook_id,title,state,pages,version) VALUES($1,$2,$3,'ready',$4,$5)`, id, n.ID, n.Title, raw, n.Version()); e != nil {
		t.Fatal(e)
	}
	cat := func(context.Context, string, string) (Notebook, error) { return n, nil }
	for step, want := range []int{100, 200, 235} {
		// A new store instance resumes from committed cursor, without process-local state.
		if ok, e := (Store{DB: db}).StepRun(ctx, cat, true); !ok || e != nil {
			t.Fatal(ok, e)
		}
		r, e := s.GetRun(ctx, id)
		if e != nil || r.Queued != want || r.Awaiting != 235-want || r.WorkTotal() != 235 {
			t.Fatal(step, r, e)
		}
		if step == 0 {
			if _, e = s.ControlRun(ctx, id, "pause"); e != nil {
				t.Fatal(e)
			}
			if ok, e := s.StepRun(ctx, cat, true); ok || e != nil {
				t.Fatal(ok, e)
			}
			if _, e = s.ControlRun(ctx, id, "resume"); e != nil {
				t.Fatal(e)
			}
		}
	}
	var total int
	if e := db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&total); e != nil || total != 235 {
		t.Fatal(total, e)
	}
	if ok, e := s.StepRun(ctx, cat, true); ok || e != nil {
		t.Fatal("completed run dispatched again", ok, e)
	}
	// 60 pages finished over the last five minutes: about 12 a minute.
	if _, e := db.ExecContext(ctx, `UPDATE alexandria_recognition_job j SET state='ready',finished_at=now()-(n.i*interval '5 seconds') FROM (SELECT id,row_number() OVER (ORDER BY page_id) i FROM alexandria_recognition_job) n WHERE j.id=n.id AND n.i<=60`); e != nil {
		t.Fatal(e)
	}
	if r, e := s.GetRun(ctx, id); e != nil || r.Remaining() != 175 || r.PerMinute < 11.5 || r.PerMinute > 12.5 || r.Percent() != 25 ||
		r.ETA.Before(time.Now().Add(13*time.Minute)) || r.ETA.After(time.Now().Add(16*time.Minute)) {
		t.Fatal("pace", r.Remaining(), r.PerMinute, r.Percent(), r.ETA, e)
	}
	if _, e := db.ExecContext(ctx, `UPDATE boox_page_index SET state='ready'`); e != nil {
		t.Fatal(e)
	}
	runs, e := s.RecentRuns(ctx)
	if e != nil || len(runs) != 1 || runs[0].Label() != "Complete" || runs[0].Ready != 235 {
		t.Fatal(runs, e)
	}
}
