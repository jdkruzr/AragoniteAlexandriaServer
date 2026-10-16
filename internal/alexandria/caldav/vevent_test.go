package caldav

import (
	"strings"
	"testing"
	"time"

	ical "github.com/emersion/go-ical"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
)

func decode(t *testing.T, s string) *ical.Calendar {
	t.Helper()
	cal, err := ical.NewDecoder(strings.NewReader(strings.ReplaceAll(s, "\n", "\r\n"))).Decode()
	if err != nil {
		t.Fatal(err)
	}
	return cal
}

const clientEvent = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Synthetic//EN
BEGIN:VEVENT
UID:client-uid-1
DTSTAMP:20261016T120000Z
DTSTART;TZID=America/Chicago:20261022T150000
DTEND;TZID=America/Chicago:20261022T160000
SUMMARY:Dentist
LOCATION:Main St
RRULE:FREQ=MONTHLY;COUNT=3
BEGIN:VALARM
ACTION:DISPLAY
TRIGGER:-PT15M
END:VALARM
END:VEVENT
END:VCALENDAR
`

func TestVEVENTRoundTrip(t *testing.T) {
	e, err := VEVENTToEvent(decode(t, clientEvent), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 22, 20, 0, 0, 0, time.UTC) // 15:00 CDT
	if !e.Start.Equal(want) || !e.End.Equal(want.Add(time.Hour)) || e.AllDay || e.TZID != "America/Chicago" || e.Title != "Dentist" || e.Location != "Main St" {
		t.Fatalf("parsed %+v", e)
	}
	e.ID, e.UpdatedAt = "x", 1
	out := encodeCalendar(EventToCalendar(e))
	for _, keep := range []string{"RRULE:FREQ=MONTHLY;COUNT=3", "BEGIN:VALARM", "UID:client-uid-1", "DTSTART;TZID=America/Chicago:20261022T150000"} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %q:\n%s", keep, out)
		}
	}
	// Moved in Alexandria: the times are rewritten (UTC), the rest kept.
	e.Start, e.End = e.Start.Add(24*time.Hour), e.End.Add(24*time.Hour)
	out = encodeCalendar(EventToCalendar(e))
	if !strings.Contains(out, "DTSTART:20261023T200000Z") || !strings.Contains(out, "RRULE:") {
		t.Errorf("moved event:\n%s", out)
	}
}

func TestAllDayEvent(t *testing.T) {
	e, err := VEVENTToEvent(decode(t, "BEGIN:VCALENDAR\nVERSION:2.0\nPRODID:x\nBEGIN:VEVENT\nUID:a\nDTSTAMP:20261016T120000Z\nDTSTART;VALUE=DATE:20261022\nSUMMARY:Trip\nEND:VEVENT\nEND:VCALENDAR\n"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Normalize(); err != nil || !e.AllDay || !e.End.Equal(time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v %v", e, err)
	}
	fresh := encodeCalendar(EventToCalendar(events.Event{ID: "n1", Title: "Trip", AllDay: true, Start: e.Start, End: e.End, UpdatedAt: 1}))
	if !strings.Contains(fresh, "DTSTART;VALUE=DATE:20261022") || !strings.Contains(fresh, "DTEND;VALUE=DATE:20261023") || !strings.Contains(fresh, "UID:n1") {
		t.Fatalf("fresh:\n%s", fresh)
	}
}

func TestUnknownTZIDFallsBack(t *testing.T) {
	s := strings.Replace(clientEvent, "America/Chicago", "Central Standard Time", 2)
	e, err := VEVENTToEvent(decode(t, s), time.UTC)
	if err != nil || e.TZID != "" || !e.Start.Equal(time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("%+v %v", e, err)
	}
}
