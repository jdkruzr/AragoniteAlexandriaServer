package recognition

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"testing"
	"time"
)

func TestQueueSerializesWithNotebookDeletion(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := testenv.Database(t).DB
	if _, err := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES('note','1-a','notebook','1','{"uniqueId":"note","type":1,"status":1}')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var pid int
	if err = tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id='note'`); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, _, e := (Store{DB: db}).Queue(ctx, "boox", Notebook{ID: "note", Pages: []Page{{ID: "p", Number: 1}}}, 1, 1, false, uuid.NewString())
		result <- e
	}()
	// Confirm the request is actually waiting on our source lock before allowing
	// the deletion to commit; no scheduling-dependent sleep proves the race.
	for {
		var waiting bool
		if err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatal("queue bypassed deletion lock", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, ErrNotebookDeleted) {
		t.Fatal(err)
	}
	var jobs, batches int
	if err = db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM alexandria_recognition_job),(SELECT count(*) FROM alexandria_recognition_batch)`).Scan(&jobs, &batches); err != nil || jobs != 0 || batches != 0 {
		t.Fatal(jobs, batches, err)
	}
}

func TestIncompleteMetadataDoesNotMeanDeleted(t *testing.T) {
	db := testenv.Database(t).DB
	ctx := context.Background()
	for _, body := range []string{
		`{"uniqueId":"note","type":1}`, `{"uniqueId":"note","type":1,"status":null}`,
		`{"uniqueId":"note","type":1,"status":"bad"}`, `{"uniqueId":"wrong","type":1,"status":0}`,
		`{"uniqueId":"note","type":1,"status":0,"commitType":1}`, `{"uniqueId":"note","type":0,"status":0}`,
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES('note','1-a','notebook','1',$1) ON CONFLICT(document_id) DO UPDATE SET body=excluded.body`, body); err != nil {
			t.Fatal(err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		err = LockBOOXNotebookTx(ctx, tx, "note")
		tx.Rollback()
		if err != nil {
			t.Fatalf("incomplete record treated as deletion: %s %v", body, err)
		}
		var deleted bool
		if err = db.QueryRowContext(ctx, `SELECT boox_recognition_notebook_deleted('note')`).Scan(&deleted); err != nil || deleted {
			t.Fatal(body, deleted, err)
		}
	}
}

func TestLibraryDispatchExcludesNotebookDeletedAfterCatalog(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	run := uuid.NewString()
	n := Notebook{ID: "note", Pages: []Page{{ID: "p", Number: 1}}}
	pages, err := json.Marshal(n.Pages)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES('note','1-a','notebook','1','{"uniqueId":"note","type":1,"status":1}')`)
	exec(`INSERT INTO alexandria_recognition_run(id,state,source) VALUES($1,'queued','boox')`, run)
	exec(`INSERT INTO alexandria_recognition_run_notebook(run_id,notebook_id,title,state,pages,version) VALUES($1,'note','Synthetic','ready',$2,$3)`, run, pages, n.Version())
	cat := func(context.Context, string, string) (Notebook, error) {
		exec(`UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id='note'`)
		return n, nil // Deliberately return the stale snapshot across the delete.
	}
	if did, err := s.StepRun(ctx, cat, true); err != nil || !did {
		t.Fatal(did, err)
	}
	r, err := s.GetRun(ctx, run)
	if err != nil || r.Excluded != 1 || r.Queued != 0 || r.Unavailable != 0 {
		t.Fatal(r, err)
	}
	var jobs int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatal(jobs, err)
	}
}
