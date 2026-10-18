package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func calTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
func TestCalendarPreferencesAndBoundaries(t *testing.T) {
	now := calTime("2024-02-29T12:00:00Z")
	req := httptest.NewRequest("GET", "/calendar", nil)
	v := readCalendarView(req, now, time.UTC)
	if v.View != "month" || v.Order != "upcoming" || v.Week != "sunday" || v.Horizon != 30 {
		t.Fatal(v)
	}
	v.View = "week"
	v.Order = "calendar"
	v.Week = "monday"
	w := httptest.NewRecorder()
	v.remember(w, req)
	fresh := httptest.NewRequest("GET", "/calendar?date=2026-10-18&view=day", nil)
	fresh.AddCookie(w.Result().Cookies()[0])
	got := readCalendarView(fresh, now, time.UTC)
	if got.View != "day" || got.Order != "calendar" || got.Date.Year() != 2026 {
		t.Fatal(got)
	}
	landing := httptest.NewRequest("GET", "/calendar", nil)
	landing.AddCookie(w.Result().Cookies()[0])
	got = readCalendarView(landing, now, time.UTC)
	if got.Date.Day() != 29 {
		t.Fatal("browsed date persisted")
	}
	if readCalendarView(req, now, time.UTC).Order != "upcoming" {
		t.Fatal("browser preferences leaked")
	}
	for _, tc := range []struct{ date, week, from, to string }{
		{"2024-02-29T00:00:00Z", "sunday", "2024-01-28", "2024-03-03"},
		{"2024-02-29T00:00:00Z", "monday", "2024-01-29", "2024-03-04"},
		{"2026-02-01T00:00:00Z", "sunday", "2026-02-01", "2026-03-01"},
		{"2026-12-31T00:00:00Z", "sunday", "2026-11-29", "2027-01-03"},
	} {
		v := calendarView{"month", "upcoming", tc.week, calTime(tc.date), 30}
		w := v.Window()
		if w.From.Format("2006-01-02") != tc.from || w.To.Format("2006-01-02") != tc.to {
			t.Fatal(tc, w)
		}
	}
	bad := httptest.NewRequest("GET", "/calendar?view=evil&week=evil&days=9999999&date=garbage", nil)
	got = readCalendarView(bad, now, time.UTC)
	if got.View != "month" || got.Horizon != 30 {
		t.Fatal(got)
	}
}
func TestCalendarLayoutAndDST(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	date := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	v := calendarView{"day", "upcoming", "sunday", date, 30}
	rows := []calendarRow{}
	for i := 0; i < 5; i++ {
		e := events.Event{ID: fmt.Sprint(i), Title: "overlap", Start: date.Add(8 * time.Hour), End: date.Add(9 * time.Hour)}
		if i == 4 {
			e.End = e.Start
		}
		rows = append(rows, makeCalendarRow(events.Occurrence{Event: e}, v.URL(), loc))
	}
	days := calendarDays(v, rows, date)
	if len(days[0].Slots) != 25 || len(days[0].Timed) != 5 {
		t.Fatal(days[0])
	}
	if days[0].Slots[1].Label == days[0].Slots[2].Label {
		t.Fatal("repeated hour indistinguishable")
	}
	for _, b := range days[0].Timed {
		if !strings.Contains(string(b.Style), "/ 5)") {
			t.Fatal(b.Style)
		}
	}
	v.View = "month"
	days = calendarDays(v, rows, date)
	for _, d := range days {
		if d.Key == "2026-11-01" && (d.More != 2 || len(d.Rows) != 3) {
			t.Fatal(d)
		}
	}
	v.View = "day"
	v.Date = time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	if len(calendarDays(v, nil, v.Date)[0].Slots) != 23 {
		t.Fatal("spring gap")
	}
	e := events.Event{Start: date.Add(-time.Hour), End: date}
	if events.Overlaps(e, events.Window{From: date, To: date.AddDate(0, 0, 1)}, loc) {
		t.Fatal("midnight end duplicated")
	}
}
func TestCalendarLocalTimeInput(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	form := func(date, start, offset string) (events.Event, string) {
		r := httptest.NewRequest("POST", "/calendar", strings.NewReader(url.Values{"title": {"Synthetic"}, "date": {date}, "start": {start}, "offset": {offset}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return parseEventForm(r, loc)
	}
	if _, problem := form("2026-03-08", "02:30", ""); problem == "" {
		t.Fatal("accepted nonexistent local time")
	}
	a, p := form("2026-11-01", "01:30", "-18000")
	if p != "" {
		t.Fatal(p)
	}
	b, p := form("2026-11-01", "01:30", "-21600")
	if p != "" || b.Start.Sub(a.Start) != time.Hour {
		t.Fatal(a, b, p)
	}
	e, p := form("2026-03-08", "", "")
	if p != "" || !e.AllDay || e.End.Sub(e.Start) != 24*time.Hour {
		t.Fatal(e, p)
	}
}
func calendarFixture(t *testing.T) (Deps, *events.Store) {
	t.Helper()
	db := testenv.Database(t).DB
	d := Deps{DB: db, PublicURL: "https://calendar.example.test"}
	ctx := context.Background()
	if err := settings.Set(ctx, db, settings.TimeZone, "America/Chicago", false); err != nil {
		t.Fatal(err)
	}
	store := events.NewStore(db)
	loc, _ := time.LoadLocation("America/Chicago")
	today := midnight(time.Now(), loc)
	for i := 0; i < 6; i++ {
		e := events.Event{ID: fmt.Sprintf("synthetic-%d", i), Title: fmt.Sprintf("Synthetic appointment %d", i), Start: today.Add(time.Duration(9*60+i*15) * time.Minute), End: today.Add(time.Duration(11*60+i*15) * time.Minute), Detail: "Synthetic calendar browser fixture"}
		if i == 0 {
			e.AllDay = true
			e.Start = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
			e.End = e.Start.AddDate(0, 0, 3)
		}
		if err := store.Create(ctx, &e); err != nil {
			t.Fatal(err)
		}
	}
	blob := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:synthetic-series\r\nSUMMARY:Synthetic recurring appointment\r\nDTSTART;TZID=America/Chicago:20200101T130000\r\nDTEND;TZID=America/Chicago:20200101T140000\r\nRRULE:FREQ=DAILY\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	e := events.Event{ID: "synthetic-series", Title: "Synthetic recurring appointment", Start: calTime("2020-01-01T19:00:00Z"), End: calTime("2020-01-01T20:00:00Z"), Blob: blob}
	if err := store.Create(ctx, &e); err != nil {
		t.Fatal(err)
	}
	return d, store
}
func TestCalendarRoutes(t *testing.T) {
	d, store := calendarFixture(t)
	h := Handler(d)
	call := func(method, path string, values url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", d.PublicURL)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, view := range []string{"day", "week", "month"} {
		for _, order := range []string{"upcoming", "calendar"} {
			w := call("GET", "/calendar?view="+view+"&order="+order, nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Synthetic recurring appointment") {
				t.Fatal(view, order, w.Code, w.Body.String())
			}
			a, b := strings.Index(w.Body.String(), `id="upcoming"`), strings.Index(w.Body.String(), `id="calendar-grid"`)
			if (a < b) != (order == "upcoming") {
				t.Fatal("section order", order)
			}
		}
	}
	for _, action := range []string{"edit", "delete"} {
		w := call("POST", "/calendar/synthetic-series/"+action, url.Values{"title": {"flatten"}, "date": {"2026-10-18"}})
		if w.Code != 409 {
			t.Fatal("series guard", action, w.Code)
		}
	}
	before, _ := store.Get(context.Background(), "synthetic-series")
	w := call("GET", "/calendar/synthetic-series/edit", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), `name="title"`) || !strings.Contains(w.Body.String(), "Edit this series in your CalDAV app.") {
		t.Fatal(w.Code, w.Body.String())
	}
	after, _ := store.Get(context.Background(), "synthetic-series")
	if before.Blob != after.Blob {
		t.Fatal("read changed blob")
	}
	ret := "/calendar?view=week&date=2026-11-01&order=calendar&week=monday&days=60"
	values := url.Values{"title": {"Created synthetically"}, "date": {"2026-11-01"}, "start": {"01:30"}, "offset": {"-21600"}, "return": {ret}}
	w = call("POST", "/calendar", values)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), ret+"&") {
		t.Fatal(w.Code, w.Header())
	}
	values.Set("date", "2026-03-08")
	values.Set("start", "02:30")
	w = call("POST", "/calendar", values)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "That local time does not exist") || !strings.Contains(w.Body.String(), "Created synthetically") {
		t.Fatal(w.Code, w.Body.String())
	}
	// Simulate a CalDAV replacement between the handler's read and write.
	original, _ := store.Get(context.Background(), "synthetic-1")
	changed := original
	changed.Blob = before.Blob
	if err := store.Update(context.Background(), &changed); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateOrdinary(context.Background(), &original, original); err != events.ErrChanged {
		t.Fatal("edit race", err)
	}
	if err := store.DeleteOrdinary(context.Background(), original); err != events.ErrChanged {
		t.Fatal("delete race", err)
	}
	for _, path := range []string{"/tasks", "/calendar", "/settings/integrations"} {
		w := call("GET", path, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), d.PublicURL+"/caldav/") {
			t.Fatal(path, w.Code)
		}
	}
}

// Opt-in live browser fixture; every record and database is synthetic and is
// removed by testenv cleanup after the driver creates the stop file.
func TestCalendarBrowserFixture(t *testing.T) {
	dir := os.Getenv("ALEXANDRIA_CALENDAR_BROWSER")
	if dir == "" {
		t.Skip("browser driver not requested")
	}
	d, _ := calendarFixture(t)
	d.PublicURL = ""
	srv := httptest.NewServer(Handler(d))
	defer srv.Close()
	if err := os.WriteFile(filepath.Join(dir, "url"), []byte(srv.URL), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("browser driver timeout")
}

func TestCalDAVConnectionsAndReturnSanitization(t *testing.T) {
	c := connection("https://library.example/prefix/", "calendar")
	if c.Discovery != "https://library.example/prefix/caldav/" || c.Calendar != "https://library.example/prefix/caldav/user/calendars/events/" || c.Tasks != "https://library.example/prefix/caldav/user/calendars/tasks/" {
		t.Fatal(c)
	}
	for _, bad := range []string{"https://evil.example/calendar", "//evil.example/calendar", "/calendar-evil", "/calendar/new"} {
		r := httptest.NewRequest("GET", "/calendar/new?return="+url.QueryEscape(bad), nil)
		if calendarReturn(r) != "/calendar" {
			t.Fatal(bad)
		}
	}
}

func TestCalendarHalfHourDSTInput(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Lord_Howe")
	var starts []time.Time
	for _, offset := range []string{"39600", "37800"} {
		r := httptest.NewRequest("POST", "/calendar", strings.NewReader(url.Values{"title": {"Half-hour transition"}, "date": {"2026-04-05"}, "start": {"01:45"}, "offset": {offset}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e, problem := parseEventForm(r, loc)
		if problem != "" {
			t.Fatal(problem)
		}
		starts = append(starts, e.Start)
	}
	if starts[1].Sub(starts[0]) != 30*time.Minute {
		t.Fatal(starts)
	}
}
