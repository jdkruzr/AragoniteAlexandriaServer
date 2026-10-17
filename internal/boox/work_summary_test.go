package boox

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The Jobs page lists note actions and minutes that are running, failed, or
// finished with something to say, linked to the page or minutes they belong to.
func TestWorkSummaryListsNoteActionsAndMinutes(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO alexandria_note_action(source,notebook_id,page_id,color,action,state,detail) VALUES('boox','note','page','red','task','ready','Made 1 task.'),('boox','note','page','blue','event','failed','Notebook unavailable.')`,
		`INSERT INTO alexandria_note_minutes(source,notebook_id,request_id,state,detail) VALUES('boox','note','` + uuid.NewString() + `','ready','Couldn''t send these minutes to BOOX automatically. Use Send to BOOX to try again.')`,
	} {
		if _, e := s.DB.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	w, e := s.Work(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if w.Actions.Counts["ready"] != 1 || w.Actions.Counts["failed"] != 1 || w.Actions.Active() != 0 {
		t.Fatal("note action counts", w.Actions.Counts)
	}
	// Finished note actions are only counted; the failed one is listed.
	if len(w.Actions.Rows) != 1 || w.Actions.Rows[0].State != "failed" || w.Actions.Rows[0].Title != "Synthetic" || w.Actions.Rows[0].Color != "blue" ||
		w.Actions.Rows[0].Link != "/boox/notebook?id=note&page_id=page#note-actions" {
		t.Fatalf("note action rows %+v", w.Actions.Rows)
	}
	if w.Minutes.Counts["ready"] != 1 || len(w.Minutes.Rows) != 1 || w.Minutes.Rows[0].Link != "/boox/minutes?id=note" || !strings.HasPrefix(w.Minutes.Rows[0].Detail, "Couldn't send") {
		t.Fatalf("minutes %+v", w.Minutes)
	}
}
