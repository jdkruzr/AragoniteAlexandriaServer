package caldav

import (
	"context"
	"database/sql"
	gocaldav "github.com/emersion/go-webdav/caldav"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
	"testing"
)

func TestTaskQueryAppliesNestedPropertyFilter(t *testing.T) {
	ctx := context.Background()
	store := newMockTaskStore()
	for _, title := range []string{"Alpha", "Beta"} {
		if err := store.Create(ctx, &taskstore.Task{TaskID: title, Title: sql.NullString{String: title, Valid: true}}); err != nil {
			t.Fatal(err)
		}
	}
	b := NewBackend(store, "/caldav", "Tasks", "preserve", nil)
	q := &gocaldav.CalendarQuery{CompFilter: gocaldav.CompFilter{Name: "VCALENDAR", Comps: []gocaldav.CompFilter{{Name: "VTODO", Props: []gocaldav.PropFilter{{Name: "SUMMARY", TextMatch: &gocaldav.TextMatch{Text: "Alpha"}}}}}}}
	got, err := b.QueryCalendarObjects(ctx, "", q)
	if err != nil {
		t.Fatal(err)
	}
	all, err := b.ListCalendarObjects(ctx, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := gocaldav.Filter(q, all)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(expected) != 1 {
		t.Fatalf("filter mismatch: actual=%d expected=%d", len(got), len(expected))
	}
	t.Logf("SUMMARY=Alpha actual=%d reference matcher=%d", len(got), len(expected))
}
