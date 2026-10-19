package boox

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"testing"
	"time"
)

func TestDeletedNotebookDoesNotCreateRecognitionAttempt(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	if err := s.QueuePage(ctx, "note", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE boox_page_index SET state='ready',text='synthetic transcript'`); err != nil {
		t.Fatal(err)
	}
	var original string
	if err := s.DB.QueryRowContext(ctx, `SELECT job_id::text FROM boox_page_index`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{status}','0'),revision='2-deleted' WHERE document_id='note'`); err != nil {
		t.Fatal(err)
	}
	var state, job, text string
	if err := s.DB.QueryRowContext(ctx, `SELECT state,job_id::text,text FROM boox_page_index`).Scan(&state, &job, &text); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" || job != original || text != "" {
		t.Fatalf("delete created work: state=%s sameJob=%v textCleared=%v", state, job == original, text == "")
	}
	ocr := &testOCR{}
	if did, err := s.ProcessPageIndex(ctx, ocr, ""); err != nil || did || ocr.calls != 0 {
		t.Fatalf("deleted work claimed: did=%v calls=%d err=%v", did, ocr.calls, err)
	}
}

func TestDeletedNotebookIgnoresLateChangesAndRestores(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	exec := func(q string) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.QueuePage(ctx, "note", 1); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id='note'`)
	var version int64
	var job string
	if err := s.DB.QueryRowContext(ctx, `SELECT request_version,job_id::text FROM boox_page_index`).Scan(&version, &job); err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE boox_projection SET body=jsonb_set(body,'{title}','"Late metadata"'),revision='3-late' WHERE document_id='note'`)
	putNoteBlob(t, s, "resource/late.txt", []byte("late synthetic file"))
	// Direct stale inserts/retries must also leave no attempt behind.
	exec(`INSERT INTO boox_page_index(notebook_id,page_id,page_number) VALUES('note','late-page',2)`)
	exec(`UPDATE boox_page_index SET state='queued' WHERE notebook_id='note'`)
	var state string
	var gotVersion int64
	var attempts int
	if err := s.DB.QueryRowContext(ctx, `SELECT state,request_version,(SELECT count(*) FROM alexandria_recognition_job) FROM boox_page_index`).Scan(&state, &gotVersion, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" || gotVersion != version || attempts != 1 {
		t.Fatalf("late changes churned: %s version %d/%d jobs %d", state, gotVersion, version, attempts)
	}
	if err := s.QueuePage(ctx, "note", 1); !errors.Is(err, recognition.ErrNotebookDeleted) {
		t.Fatal("deleted page accepted", err)
	}
	store := recognition.Store{DB: s.DB}
	stale := recognition.Notebook{ID: "note", Pages: []recognition.Page{{ID: "late-page", Number: 1}}}
	if _, _, err := store.Queue(ctx, "boox", stale, 1, 1, false, uuid.NewString()); !errors.Is(err, recognition.ErrNotebookDeleted) {
		t.Fatal("stale catalog accepted", err)
	}
	var batches int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_batch`).Scan(&batches); err != nil || batches != 0 {
		t.Fatal("empty batch created", batches, err)
	}
	exec(`UPDATE boox_projection SET body=jsonb_set(body,'{status}','1'),revision='4-restored' WHERE document_id='note'`)
	var newJob string
	if err := s.DB.QueryRowContext(ctx, `SELECT state,job_id::text,(SELECT count(*) FROM alexandria_recognition_job) FROM boox_page_index`).Scan(&state, &newJob, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "queued" || job == newJob || attempts != 2 {
		t.Fatal("restore did not create one new attempt", state, attempts)
	}
	exec(`UPDATE boox_page_index SET next_at=now()`)
	ocr := &testOCR{}
	if did, err := s.ProcessPageIndex(ctx, ocr, ""); err != nil || !did || ocr.calls != 1 {
		t.Fatal(did, ocr.calls, err)
	}
}

func TestDeletionFencesInFlightRecognition(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	if err := s.QueuePage(ctx, "note", 1); err != nil {
		t.Fatal(err)
	}
	ocr := &testOCR{hook: func() {
		if _, err := s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{status}','0'),revision='2-deleted' WHERE document_id='note'`); err != nil {
			t.Fatal(err)
		}
	}}
	if did, err := s.ProcessPageIndex(ctx, ocr, ""); err != nil || !did {
		t.Fatal(did, err)
	}
	var state, text, token string
	var jobs int
	if err := s.DB.QueryRowContext(ctx, `SELECT state,text,lease_token,(SELECT count(*) FROM alexandria_recognition_job) FROM boox_page_index`).Scan(&state, &text, &token, &jobs); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" || text != "" || token != "" || jobs != 1 {
		t.Fatal("late inference published or requeued", state, text, token, jobs)
	}
}

func TestDeletionPreservesManualRecognitionChoices(t *testing.T) {
	for _, state := range []string{"paused", "skipped", "cancelled"} {
		t.Run(state, func(t *testing.T) {
			s := indexFixture(t)
			ctx := context.Background()
			if err := s.QueuePage(ctx, "note", 1); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.ExecContext(ctx, `UPDATE boox_page_index SET state=$1,detail='Owner choice',text='old text'`, state); err != nil {
				t.Fatal(err)
			}
			for _, status := range []int{0, 1} {
				if _, err := s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{status}',to_jsonb($1::int)) WHERE document_id='note'`, status); err != nil {
					t.Fatal(err)
				}
				var got, detail, text string
				var jobs int
				if err := s.DB.QueryRowContext(ctx, `SELECT state,detail,text,(SELECT count(*) FROM alexandria_recognition_job) FROM boox_page_index`).Scan(&got, &detail, &text, &jobs); err != nil {
					t.Fatal(err)
				}
				if got != state || detail != "Owner choice" || text != "" || jobs != 1 {
					t.Fatal(got, detail, text, jobs)
				}
			}
		})
	}
}

func TestQueuePageUsesOneAdmittedConnection(t *testing.T) {
	s := indexFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := s.DB.(*sql.DB).Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	s.DB = conn
	if err = s.QueuePage(ctx, "note", 1); err != nil {
		t.Fatal(err)
	}
}
