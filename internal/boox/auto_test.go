package boox

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestAutomaticProcessingWaitsForQuietNewNotes(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	s.RecognitionEnabled = true
	cfg := MinutesConfig{Enabled: true, Transcriber: "synthetic-whisper", Summarizer: "synthetic-llm", Identity: "t1"}
	// age makes the notebook and every file in it look last changed d ago.
	age := func(d time.Duration) {
		t.Helper()
		ago := fmt.Sprintf("%d seconds", int(d.Seconds()))
		for _, q := range []string{`UPDATE boox_projection SET updated_at=now()-$1::interval WHERE document_id='note'`, `UPDATE boox_blob_version SET created_at=now()-$1::interval WHERE native_key LIKE '%/note/note/%'`} {
			if _, e := s.DB.ExecContext(ctx, q, ago); e != nil {
				t.Fatal(e)
			}
		}
	}
	run := func(a AutoConfig) bool {
		t.Helper()
		found, e := s.AutoProcess(ctx, a, cfg)
		if e != nil {
			t.Fatal(e)
		}
		return found
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if e := s.DB.QueryRowContext(ctx, q).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}
	on := AutoConfig{Recognize: true, Summarize: true, Since: time.Now().Add(-time.Hour)}

	// Changes from before it was turned on are never swept up.
	age(6 * time.Minute)
	if run(AutoConfig{Recognize: true, Summarize: true, Since: time.Now()}) {
		t.Fatal("processed a notebook that changed before automatic processing was on")
	}
	// A notebook still being written isn't touched.
	age(time.Minute)
	if run(on) {
		t.Fatal("processed a notebook that isn't quiet yet")
	}
	// Quiet for five minutes: its new page is queued, once.
	age(6 * time.Minute)
	if !run(on) || count(`SELECT count(*) FROM boox_page_index WHERE notebook_id='note' AND state='queued'`) != 1 {
		t.Fatal("new page not queued")
	}
	if run(on) {
		t.Fatal("queued the same page twice")
	}

	// A recording arrives; ten quiet minutes later the note is summarized.
	putNoteBlob(t, s, "resource/data/rec.mp3", syntheticONY1(t, []byte("ID3 synthetic mp3 body")))
	putNoteBlob(t, s, "resource/pb/res#1", syntheticResource("res", "Standup", "rec.mp3", 1791928241345, 61000))
	age(6 * time.Minute)
	if run(on) {
		t.Fatal("summarized before the note was quiet for ten minutes")
	}
	age(11 * time.Minute)
	if !run(on) || count(`SELECT count(*) FROM alexandria_note_minutes WHERE notebook_id='note' AND state='queued'`) != 1 {
		t.Fatal("note with a new recording not summarized")
	}
	if run(on) {
		t.Fatal("requested minutes again while queued")
	}
	// Finished (or failed) minutes for the same recording are left alone.
	if _, e := s.DB.ExecContext(ctx, `UPDATE alexandria_note_minutes SET state='failed'`); e != nil {
		t.Fatal(e)
	}
	if run(on) {
		t.Fatal("retried failed minutes automatically")
	}
	// A second recording is new: the note is summarized again.
	putNoteBlob(t, s, "resource/data/rec2.mp3", syntheticONY1(t, []byte("ID3 second body")))
	age(11 * time.Minute)
	if !run(on) {
		t.Fatal("new recording not summarized")
	}
	// Skipped notebooks are left alone.
	if _, e := s.DB.ExecContext(ctx, `UPDATE alexandria_note_minutes SET state='ready'; INSERT INTO alexandria_recognition_skip(source,notebook_id) VALUES('boox','note')`); e != nil {
		t.Fatal(e)
	}
	putNoteBlob(t, s, "resource/data/rec3.mp3", syntheticONY1(t, []byte("ID3 third body")))
	age(11 * time.Minute)
	if run(on) {
		t.Fatal("processed a skipped notebook")
	}
}
