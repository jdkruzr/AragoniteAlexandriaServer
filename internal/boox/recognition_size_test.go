package boox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
)

func TestRecognitionSizePreventsAutomaticWorkAndFencesProvider(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	store := recognition.Store{DB: s.DB}
	s.RecognitionEnabled = true
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{pageNameList}','["page","other"]'),updated_at=now()-interval '6 minutes' WHERE document_id='note'; UPDATE boox_blob_version SET created_at=now()-interval '6 minutes'`); e != nil {
		t.Fatal(e)
	}
	if e := store.SetPageLimit(ctx, 1, 100); e != nil {
		t.Fatal(e)
	}
	if e := s.QueuePage(ctx, "note", 1); !errors.Is(e, recognition.ErrNotebookTooLarge) {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		found, e := s.autoRecognize(ctx, time.Now().Add(-time.Hour))
		if e != nil || found {
			t.Fatal(found, e)
		}
	}
	var jobs int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_recognition_job`).Scan(&jobs); e != nil || jobs != 0 {
		t.Fatal(jobs, e)
	}
	if e := store.AllowLarge(ctx, "boox", "note", true); e != nil {
		t.Fatal(e)
	}
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	ocr := &testOCR{hook: func() {
		if e := store.AllowLarge(ctx, "boox", "note", false); e != nil {
			t.Fatal(e)
		}
	}}
	if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || !found || ocr.calls != 1 {
		t.Fatal(found, e, ocr.calls)
	}
	var state, text string
	if e := s.DB.QueryRowContext(ctx, `SELECT state,text FROM boox_page_index WHERE page_id='page'`).Scan(&state, &text); e != nil || state != "cancelled" || text != "" {
		t.Fatal(state, text, e)
	}
	if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || found || ocr.calls != 1 {
		t.Fatal(found, e, ocr.calls)
	}
	// Re-enable, recognize, then lower the cap: existing text is retained.
	if e := store.AllowLarge(ctx, "boox", "note", true); e != nil {
		t.Fatal(e)
	}
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	ocr.hook = nil
	if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || !found {
		t.Fatal(found, e)
	}
	if e := store.AllowLarge(ctx, "boox", "note", false); e != nil {
		t.Fatal(e)
	}
	if e := s.DB.QueryRowContext(ctx, `SELECT state,text FROM boox_page_index WHERE page_id='page'`).Scan(&state, &text); e != nil || state != "ready" || text == "" {
		t.Fatal(state, text, e)
	}
	// A later source change must invalidate that text, without requeueing it.
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=body||'{"syntheticEdit":true}' WHERE document_id='note'`); e != nil {
		t.Fatal(e)
	}
	if e := s.DB.QueryRowContext(ctx, `SELECT state,text FROM boox_page_index WHERE page_id='page'`).Scan(&state, &text); e != nil || state != "cancelled" || text != "" {
		t.Fatal(state, text, e)
	}
}
