package events

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func stamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
func series(body string) Event {
	return Event{ID: "series", Title: "Synthetic series", Blob: "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:series\r\nSUMMARY:Synthetic series\r\n" + strings.ReplaceAll(body, "\n", "\r\n") + "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"}
}
func win(a, b string) []Window { return []Window{{stamp(a), stamp(b)}} }
func TestOccurrencesRecurrenceAndExceptions(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"count", "DTSTART:20260101T090000Z\nDTEND:20260101T100000Z\nRRULE:FREQ=DAILY;COUNT=3", []string{"2026-01-01T09:00:00Z", "2026-01-02T09:00:00Z", "2026-01-03T09:00:00Z"}},
		{"until inclusive", "DTSTART:20260101T090000Z\nRRULE:FREQ=DAILY;UNTIL=20260102T090000Z", []string{"2026-01-01T09:00:00Z", "2026-01-02T09:00:00Z"}},
		{"additional excluded deduplicated", "DTSTART:20260101T090000Z\nRRULE:FREQ=DAILY;COUNT=3\nEXDATE:20260102T090000Z\nRDATE:20260103T090000Z,20260104T090000Z", []string{"2026-01-01T09:00:00Z", "2026-01-03T09:00:00Z", "2026-01-04T09:00:00Z"}},
		{"moved out cancelled moved in", "DTSTART:20260101T090000Z\nRRULE:FREQ=DAILY;COUNT=3\nEND:VEVENT\nBEGIN:VEVENT\nUID:series\nRECURRENCE-ID:20260102T090000Z\nDTSTART:20260301T090000Z\nEND:VEVENT\nBEGIN:VEVENT\nUID:series\nRECURRENCE-ID:20260103T090000Z\nSTATUS:CANCELLED\nEND:VEVENT\nBEGIN:VEVENT\nUID:series\nRECURRENCE-ID:20251215T090000Z\nDTSTART:20260104T110000Z", []string{"2026-01-01T09:00:00Z", "2026-01-04T11:00:00Z"}},
		{"this and future", "DTSTART:20260101T090000Z\nDTEND:20260101T100000Z\nRRULE:FREQ=DAILY;COUNT=4\nEND:VEVENT\nBEGIN:VEVENT\nUID:series\nRECURRENCE-ID;RANGE=THISANDFUTURE:20260102T090000Z\nDTSTART:20260102T110000Z\nDTEND:20260102T130000Z\nSUMMARY:Changed", []string{"2026-01-01T09:00:00Z", "2026-01-02T11:00:00Z", "2026-01-03T11:00:00Z", "2026-01-04T11:00:00Z"}},
		{"old series", "DTSTART:20200101T090000Z\nRRULE:FREQ=YEARLY;COUNT=10", []string{"2026-01-01T09:00:00Z"}},
		{"cancel series", "DTSTART:20260101T090000Z\nRRULE:FREQ=DAILY\nSTATUS:CANCELLED", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := series(tc.body)
			before := e.Blob
			r := Expand(context.Background(), []Event{e}, win("2026-01-01T00:00:00Z", "2026-01-05T00:00:00Z"), time.UTC)
			if len(r.Incomplete) != 0 {
				t.Fatal(r.Incomplete)
			}
			if len(r.Occurrences) != len(tc.want) {
				t.Fatalf("got %d want %d: %+v", len(r.Occurrences), len(tc.want), r.Occurrences)
			}
			for i, o := range r.Occurrences {
				if identity(o.Event.Start) != tc.want[i] || !o.Recurring || o.Event.ID != "series" {
					t.Fatal(o)
				}
				if tc.name == "this and future" && i > 0 && (o.Event.Title != "Changed" || o.Event.End.Sub(o.Event.Start) != 2*time.Hour) {
					t.Fatal(o)
				}
			}
			if e.Blob != before {
				t.Fatal("blob changed")
			}
		})
	}
}
func TestOccurrencesZonesAndBoundaries(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	e := series("DTSTART;TZID=America/Chicago:20260307T090000\nDTEND;TZID=America/Chicago:20260307T100000\nRRULE:FREQ=DAILY;COUNT=3")
	r := Expand(context.Background(), []Event{e}, win("2026-03-07T00:00:00Z", "2026-03-10T00:00:00Z"), loc)
	if len(r.Incomplete) > 0 || len(r.Occurrences) != 3 {
		t.Fatal(r)
	}
	if r.Occurrences[1].Event.Start.Sub(r.Occurrences[0].Event.Start) != 23*time.Hour {
		t.Fatal("recurrence lost wall clock")
	}
	e = series("DTSTART;VALUE=DATE:20260228\nDTEND;VALUE=DATE:20260302\nRRULE:FREQ=YEARLY;COUNT=2")
	r = Expand(context.Background(), []Event{e}, win("2026-03-01T06:00:00Z", "2026-03-02T06:00:00Z"), loc)
	if len(r.Occurrences) != 1 || !r.Occurrences[0].Event.AllDay {
		t.Fatal(r)
	}
	r = Expand(context.Background(), []Event{e}, win("2026-03-02T06:00:00Z", "2026-03-03T06:00:00Z"), loc)
	if len(r.Occurrences) != 0 {
		t.Fatal("exclusive all-day end", r)
	}
	e = series("DTSTART;VALUE=DATE:20240229\nRRULE:FREQ=YEARLY;COUNT=3")
	r = Expand(context.Background(), []Event{e}, win("2028-01-01T00:00:00Z", "2029-01-01T00:00:00Z"), loc)
	if len(r.Occurrences) != 1 || r.Occurrences[0].Event.Start.Day() != 29 {
		t.Fatal(r)
	}
	zero := Event{ID: "zero", Start: stamp("2026-01-01T00:00:00Z"), End: stamp("2026-01-01T00:00:00Z")}
	r = Expand(context.Background(), []Event{zero}, win("2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"), loc)
	if len(r.Occurrences) != 1 {
		t.Fatal("zero duration omitted")
	}
}
func TestOccurrenceLimitsUnsupportedAndCancellation(t *testing.T) {
	for _, body := range []string{
		"DTSTART:20200101T090000Z\nRRULE:FREQ=SECONDLY",
		"DTSTART:20260101T090000Z\nRRULE:FREQ=NOPE",
		"DTSTART:20260101T090000Z\nRDATE;VALUE=PERIOD:20260102T090000Z/PT1H",
		"DTSTART;TZID=Unknown/Zone:20260101T090000\nRRULE:FREQ=DAILY",
		"DTSTART:20260101T090000Z\nEXRULE:FREQ=DAILY",
	} {
		r := Expand(context.Background(), []Event{series(body)}, win("2026-01-01T00:00:00Z", "2026-02-01T00:00:00Z"), time.UTC)
		if len(r.Incomplete) != 1 || r.Incomplete[0].ID != "series" {
			t.Fatal(body, r)
		}
	}
	records := make([]Event, MaxOccurrences+1)
	for i := range records {
		records[i] = Event{ID: fmt.Sprint(i), Title: "limit", Start: stamp("2026-01-01T09:00:00Z"), End: stamp("2026-01-01T10:00:00Z")}
	}
	r := Expand(context.Background(), records, win("2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"), time.UTC)
	if len(r.Occurrences) != MaxOccurrences || len(r.Incomplete) != 1 {
		t.Fatal(len(r.Occurrences), r.Incomplete)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = Expand(ctx, records[:1], win("2026-01-01T00:00:00Z", "2026-01-02T00:00:00Z"), time.UTC)
	if len(r.Incomplete) != 1 {
		t.Fatal(r)
	}
}

func TestRecurrenceSkipsNonexistentClockWithoutConsumingCount(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	e := series("DTSTART;TZID=America/Chicago:20260307T023000\nRRULE:FREQ=DAILY;COUNT=3")
	r := Expand(context.Background(), []Event{e}, win("2026-03-07T00:00:00Z", "2026-03-12T00:00:00Z"), loc)
	if len(r.Incomplete) > 0 || len(r.Occurrences) != 3 {
		t.Fatal(r)
	}
	for i, day := range []int{7, 9, 10} {
		got := r.Occurrences[i].Event.Start.In(loc)
		if got.Day() != day || got.Hour() != 2 || got.Minute() != 30 {
			t.Fatalf("occurrence %d: %s", i, got)
		}
	}
}

func TestThisAndFutureAcrossDSTAndShiftedWindow(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	body := "DTSTART;TZID=America/Chicago:20260307T090000\nDTEND;TZID=America/Chicago:20260307T100000\nRRULE:FREQ=DAILY;COUNT=4\nEND:VEVENT\nBEGIN:VEVENT\nUID:series\nRECURRENCE-ID;TZID=America/Chicago;RANGE=THISANDFUTURE:20260307T090000\nDTSTART;TZID=America/Chicago:20260307T110000\nDTEND;TZID=America/Chicago:20260307T130000"
	r := Expand(context.Background(), []Event{series(body)}, win("2026-03-07T00:00:00Z", "2026-03-12T00:00:00Z"), loc)
	if len(r.Incomplete) > 0 || len(r.Occurrences) != 4 {
		t.Fatal(r)
	}
	for _, o := range r.Occurrences {
		if o.Event.Start.In(loc).Hour() != 11 || o.Event.End.Sub(o.Event.Start) != 2*time.Hour {
			t.Fatal(o)
		}
	}
	body = "DTSTART:20260101T090000Z\nRRULE:FREQ=DAILY;COUNT=10\nEND:VEVENT\nBEGIN:VEVENT\nUID:series\nRECURRENCE-ID;RANGE=THISANDFUTURE:20260102T090000Z\nDTSTART:20251228T090000Z"
	r = Expand(context.Background(), []Event{series(body)}, win("2026-01-03T00:00:00Z", "2026-01-04T00:00:00Z"), time.UTC)
	if len(r.Incomplete) > 0 || len(r.Occurrences) != 1 || r.Occurrences[0].Identity != "2026-01-08T09:00:00Z" {
		t.Fatal(r)
	}
	body = "DTSTART:20260101T090000Z\nRRULE:FREQ=DAILY;COUNT=10\nEND:VEVENT\nBEGIN:VEVENT\nUID:series\nRECURRENCE-ID;RANGE=THISANDFUTURE:20260102T090000Z\nSTATUS:CANCELLED"
	r = Expand(context.Background(), []Event{series(body)}, win("2026-01-01T00:00:00Z", "2026-01-12T00:00:00Z"), time.UTC)
	if len(r.Incomplete) > 0 || len(r.Occurrences) != 1 {
		t.Fatal(r)
	}
}
func TestGlobalCandidateBudgetAndOccurrenceIdentity(t *testing.T) {
	e := series("DTSTART:20260101T000000Z\nRRULE:FREQ=SECONDLY;COUNT=60000")
	e2 := e
	e2.ID = "another"
	e2.Blob = strings.ReplaceAll(e.Blob, "UID:series", "UID:another")
	// Most generated candidates are outside the display window; the per-request
	// candidate budget still applies across series.
	r := Expand(context.Background(), []Event{e, e2}, win("2026-01-01T16:00:00Z", "2026-01-01T16:30:00Z"), time.UTC)
	if len(r.Incomplete) != 1 || r.Incomplete[0].ID != "another" || r.Incomplete[0].Reason != "limit" {
		t.Fatal(r.Incomplete)
	}
	seen := map[string]bool{}
	for _, o := range r.Occurrences {
		key := o.Event.ID + o.Identity
		if seen[key] {
			t.Fatal("duplicate occurrence")
		}
		seen[key] = true
	}
}

func TestRecurrenceNominalDurationAcrossDST(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	for _, duration := range []string{"P1D", "PT24H"} {
		e := series("DTSTART;TZID=America/Chicago:20260307T090000\nDURATION:" + duration + "\nRRULE:FREQ=DAILY;COUNT=2")
		r := Expand(context.Background(), []Event{e}, win("2026-03-07T00:00:00Z", "2026-03-10T00:00:00Z"), loc)
		if len(r.Incomplete) > 0 || len(r.Occurrences) != 2 {
			t.Fatal(r)
		}
		want := 23 * time.Hour
		if duration == "PT24H" {
			want = 24 * time.Hour
		}
		if got := r.Occurrences[0].Event.End.Sub(r.Occurrences[0].Event.Start); got != want {
			t.Fatal(duration, got)
		}
	}
}
