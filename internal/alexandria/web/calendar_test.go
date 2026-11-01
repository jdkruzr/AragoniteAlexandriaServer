package web

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
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
	if v.View != "week" || v.Week != "sunday" || v.Horizon != 7 || v.Past || v.Filters {
		t.Fatal(v)
	}
	v.View = "upcoming"
	v.Week = "monday"
	w := httptest.NewRecorder()
	v.remember(w, req)
	fresh := httptest.NewRequest("GET", "/calendar?date=2026-10-18&view=day", nil)
	fresh.AddCookie(w.Result().Cookies()[0])
	got := readCalendarView(fresh, now, time.UTC)
	if got.View != "day" || got.Week != "monday" || got.Date.Year() != 2026 {
		t.Fatal(got)
	}
	landing := httptest.NewRequest("GET", "/calendar", nil)
	landing.AddCookie(w.Result().Cookies()[0])
	got = readCalendarView(landing, now, time.UTC)
	if got.Date.Day() != 29 || got.View != "upcoming" {
		t.Fatal("browsed date persisted, or the view didn't", got)
	}
	if w := got.Window(); !w.From.Equal(got.Today) || !w.To.Equal(got.Today.AddDate(0, 0, 7)) {
		t.Fatal("upcoming window", w)
	}
	if got.Previous() != "" || !strings.Contains(got.Next(), "days=37") {
		t.Fatal("upcoming only reaches further", got.Previous(), got.Next())
	}
	if readCalendarView(req, now, time.UTC).View != "week" {
		t.Fatal("browser preferences leaked")
	}
	for _, tc := range []struct{ date, week, from, to string }{
		{"2024-02-29T00:00:00Z", "sunday", "2024-01-28", "2024-03-03"},
		{"2024-02-29T00:00:00Z", "monday", "2024-01-29", "2024-03-04"},
		{"2026-02-01T00:00:00Z", "sunday", "2026-02-01", "2026-03-01"},
		{"2026-12-31T00:00:00Z", "sunday", "2026-11-29", "2027-01-03"},
	} {
		v := calendarView{View: "month", Week: tc.week, Date: calTime(tc.date), Horizon: 30}
		w := v.Window()
		if w.From.Format("2006-01-02") != tc.from || w.To.Format("2006-01-02") != tc.to {
			t.Fatal(tc, w)
		}
	}
	bad := httptest.NewRequest("GET", "/calendar?view=evil&week=evil&days=9999999&date=garbage", nil)
	got = readCalendarView(bad, now, time.UTC)
	if got.View != "week" || got.Horizon != 7 {
		t.Fatal(got)
	}
}

// chipsAt makes local event chips for the Day view tests.
func chipAt(id string, start, end time.Time) calendarChip {
	return calendarChip{Kind: "local", Icon: "calendar-blank", Title: id, event: events.Event{ID: id, Title: id, Start: start, End: end}}
}

func TestCalendarLayoutAndDST(t *testing.T) {
	loc, _ := time.LoadLocation("America/Chicago")
	l := i18n.English
	date := time.Date(2026, 11, 1, 0, 0, 0, 0, loc)
	v := calendarView{View: "day", Week: "sunday", Date: date, Today: date, Horizon: 30}
	var chips []calendarChip
	for i := 0; i < 5; i++ {
		end := date.Add(9 * time.Hour)
		if i == 4 {
			end = date.Add(8 * time.Hour)
		}
		chips = append(chips, chipAt(fmt.Sprint(i), date.Add(8*time.Hour), end))
	}
	due := calendarChip{Kind: "task", Icon: "check-square", Title: "due", event: events.Event{AllDay: true, Start: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), End: time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)}}
	day := calendarDays(v, append(chips, due), l)[0]
	if len(day.Slots) != 15 || day.Slots[0].Label != "07:00" || day.Slots[14].Label != "21:00" || day.Height != 15*hourHeight || len(day.Timed) != 5 {
		t.Fatal("default 07:00 to 22:00 grid", day.Slots, len(day.Timed))
	}
	if len(day.AllDay) != 1 || day.AllDay[0].Time != "Due" {
		t.Fatal("a task due goes in the all-day row", day.AllDay)
	}
	for _, b := range day.Timed {
		if !strings.Contains(string(b.Style), "/ 5)") {
			t.Fatal(b.Style)
		}
	}
	// The grid grows to fit early and late events instead of dropping them.
	clock := func(day, h, m int) time.Time { return time.Date(2026, 11, day, h, m, 0, 0, loc) }
	early := chipAt("early", clock(1, 5, 30), clock(1, 6, 0))
	late := chipAt("late", clock(1, 22, 0), clock(2, 2, 0))
	day = calendarDays(v, []calendarChip{early, late}, l)[0]
	if day.Slots[0].Label != "05:00" || day.Slots[len(day.Slots)-1].Label != "23:00" || len(day.Timed) != 2 || len(day.AllDay) != 0 {
		t.Fatal("grid did not grow", day.Slots)
	}
	if day.Timed[1].Chip.Time != "22:00 →" || !strings.HasPrefix(string(day.Timed[0].Style), "top:22px;") {
		t.Fatal(day.Timed[1].Chip.Time, day.Timed[0].Style)
	}
	next := calendarDays(calendarView{View: "day", Date: date.AddDate(0, 0, 1), Today: date}, []calendarChip{late}, l)[0]
	if next.Slots[0].Label != "00:00" || next.Timed[0].Chip.Time != "→ 02:00" {
		t.Fatal("the rest of an overnight event", next.Slots[0], next.Timed[0].Chip.Time)
	}
	v.View = "month"
	for _, d := range calendarDays(v, chips, l) {
		if d.Key == "2026-11-01" && (d.More != 2 || len(d.Chips) != 3) {
			t.Fatal(d)
		}
	}
	// A clock change leaves one slot per clock hour, and new events at it
	// open on that day.
	v.View = "day"
	v.Date = time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
	spring := calendarDays(v, []calendarChip{chipAt("x", v.Date.Add(time.Hour), v.Date.Add(3*time.Hour))}, l)[0]
	if spring.Slots[0].Label != "01:00" || !strings.Contains(spring.Slots[0].URL, "new=2026-03-08") {
		t.Fatal(spring.Slots[0])
	}
	e := events.Event{Start: date.Add(-time.Hour), End: date}
	if events.Overlaps(e, events.Window{From: date, To: date.AddDate(0, 0, 1)}, loc) {
		t.Fatal("midnight end duplicated")
	}
	// Upcoming lists a long event once, under its first day in range.
	long := chipAt("long", date.AddDate(0, 0, -2), date.AddDate(0, 0, 3))
	agenda := calendarAgenda([]calendarChip{long, chips[0]}, date, date.AddDate(0, 0, 7), date, false, l, func(t time.Time) string { return t.Format("Jan 2") })
	if len(agenda) != 1 || len(agenda[0].Chips) != 2 || !agenda[0].Today || agenda[0].Chips[0].Time != "All day" {
		t.Fatal(agenda)
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
	r := httptest.NewRequest("POST", "/calendar", strings.NewReader(url.Values{"title": {"Synthetic"}, "date": {"2026-03-09"}, "start": {"10:00"}, "end": {"11:00"}, "all_day": {"yes"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if e, p := parseEventForm(r, loc); p != "" || !e.AllDay {
		t.Fatal("All day wins over the times", e, p)
	}
	// The editor only carries an offset for the second of a repeated hour.
	if v := eventFormValues(a, loc); v["offset"] != "" || v["start"] != "01:30" {
		t.Fatal(v)
	}
	if v := eventFormValues(b, loc); v["offset"] != "-21600" {
		t.Fatal(v)
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
	for _, view := range []string{"day", "week", "month", "upcoming"} {
		w := call("GET", "/calendar?view="+view, nil)
		if view == "month" && (w.Code != 200 || !strings.Contains(w.Body.String(), "4 more</a>")) {
			t.Fatal("a month cell shows three", w.Body.String())
		} else if view != "month" && (w.Code != 200 || !strings.Contains(w.Body.String(), "Synthetic recurring appointment") || !strings.Contains(w.Body.String(), "Synthetic appointment 3")) {
			t.Fatal(view, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), `id="editor"`) {
			t.Fatal("editor open without asking")
		}
	}
	for _, action := range []string{"edit", "delete"} {
		w := call("POST", "/calendar/synthetic-series/"+action, url.Values{"title": {"flatten"}, "date": {"2026-10-18"}})
		if w.Code != 409 {
			t.Fatal("series guard", action, w.Code)
		}
	}
	// The recurring series opens read-only in the column, from its chip.
	before, _ := store.Get(context.Background(), "synthetic-series")
	body := call("GET", "/calendar?view=day", nil).Body.String()
	i := strings.Index(body, "occurrence=")
	link := body[strings.LastIndex(body[:i], `href="`)+6 : i+strings.Index(body[i:], `"`)]
	w := call("GET", html.UnescapeString(link), nil)
	editor := calendarSection(t, w.Body.String(), "editor")
	if w.Code != 200 || strings.Contains(editor, `name="title"`) || !strings.Contains(editor, "Edit this series in your CalDAV app.") || !strings.Contains(editor, "13:00–14:00") {
		t.Fatal(w.Code, editor)
	}
	after, _ := store.Get(context.Background(), "synthetic-series")
	if before.Blob != after.Blob {
		t.Fatal("read changed blob")
	}
	// Old pages land in the column.
	for path, want := range map[string]string{
		"/calendar/new?date=2026-11-01&start=10:00&return=" + url.QueryEscape("/calendar?view=week"): "new=2026-11-01",
		"/calendar/synthetic-1/edit":                    "edit=synthetic-1",
		"/calendar/synthetic-series/edit?on=2026-11-01": "date=2026-11-01",
	} {
		w := call("GET", path, nil)
		if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), want) {
			t.Fatal(path, w.Code, w.Header())
		}
	}
	editor = calendarSection(t, call("GET", "/calendar?view=week&date=2026-11-01&new=2026-11-01&start=10:00", nil).Body.String(), "editor")
	if !strings.Contains(editor, `name="start" value="10:00"`) || !strings.Contains(editor, `name="end" value="11:00"`) || strings.Contains(editor, "Delete…") || !strings.Contains(editor, `value="/calendar?date=2026-11-01&amp;view=week&amp;week=sunday"`) {
		t.Fatal(editor)
	}
	editor = calendarSection(t, call("GET", "/calendar?edit=synthetic-1", nil).Body.String(), "editor")
	if !strings.Contains(editor, `value="Synthetic appointment 1"`) || !strings.Contains(editor, "Delete…") || !strings.Contains(editor, `action="/calendar/synthetic-1/delete"`) {
		t.Fatal(editor)
	}
	for _, missing := range []string{"edit=no-such-event", "sub=not-a-uuid&key=x", "sub=9ea68f62-ea53-415c-ad76-7d10e4cc156f&key=x"} {
		if editor = calendarSection(t, call("GET", "/calendar?"+missing, nil).Body.String(), "editor"); !strings.Contains(editor, "That event is no longer here.") {
			t.Fatal(missing, editor)
		}
	}
	ret := "/calendar?view=week&date=2026-11-01&week=monday&days=60"
	values := url.Values{"title": {"Created synthetically"}, "date": {"2026-11-01"}, "start": {"01:30"}, "offset": {"-21600"}, "return": {ret}}
	w = call("POST", "/calendar", values)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), ret+"&") {
		t.Fatal(w.Code, w.Header())
	}
	values.Set("date", "2026-03-08")
	values.Set("start", "02:30")
	w = call("POST", "/calendar", values)
	editor = calendarSection(t, w.Body.String(), "editor")
	if w.Code != 400 || !strings.Contains(editor, "That local time does not exist") || !strings.Contains(editor, "Created synthetically") || !strings.Contains(w.Body.String(), "Oct 26 – Nov 1, 2026") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Language") != "en" {
		t.Fatal("the 400 went out before the page's headers", w.Header())
	}
	// Delete from the editor, then find it under Deleted events.
	w = call("POST", "/calendar/synthetic-2/delete", url.Values{"return": {ret}})
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), ret+"&notice=") {
		t.Fatal(w.Code, w.Header())
	}
	if !strings.Contains(call("GET", "/calendar?filters=1", nil).Body.String(), "Deleted events (1)") {
		t.Fatal("deleted count")
	}
	trash := call("GET", "/calendar?show=deleted", nil).Body.String()
	if !strings.Contains(trash, "Synthetic appointment 2") || !strings.Contains(trash, `action="/calendar/synthetic-2/restore"`) {
		t.Fatal(trash)
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
	for _, path := range []string{"/tasks", "/settings/calendar"} {
		w := call("GET", path, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), d.PublicURL+"/caldav/") {
			t.Fatal(path, w.Code)
		}
	}
}

func TestCalendarTaskDueDates(t *testing.T) {
	d, _ := calendarFixture(t)
	ctx := context.Background()
	loc, _ := time.LoadLocation("America/Chicago")
	today := midnight(time.Now(), loc)
	svc := tasksvc.NewTaskService(taskdb.NewStore(d.DB), nil)
	due, timed, done := today, time.Date(today.Year(), today.Month(), today.Day(), 17, 0, 0, 0, loc), today
	open, err := svc.Create(ctx, tasksvc.TaskCreate{Title: "Synthetic task due", DueAt: &due})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, tasksvc.TaskCreate{Title: "Synthetic task at five", DueAt: &timed}); err != nil {
		t.Fatal(err)
	}
	finished, err := svc.Create(ctx, tasksvc.TaskCreate{Title: "Synthetic task finished", DueAt: &done})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Complete(ctx, finished.ID); err != nil {
		t.Fatal(err)
	}
	h := Handler(d)
	get := func(path string, cookies ...*http.Cookie) string {
		r := httptest.NewRequest("GET", path, nil)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
		return w.Body.String()
	}
	for _, view := range []string{"day", "week", "upcoming"} {
		body := get("/calendar?view=" + view)
		if !strings.Contains(body, "Synthetic task due") || !strings.Contains(body, "/?edit="+open.ID) || strings.Contains(body, "Synthetic task finished") {
			t.Fatal(view, body)
		}
	}
	if body := get("/calendar?view=week"); !strings.Contains(body, ">Due 17:00<") || !strings.Contains(body, ">Due<") {
		t.Fatal("due labels", body)
	}
	if strings.Contains(get("/calendar?view=week", &http.Cookie{Name: sourceCookie, Value: "tasks"}), "Synthetic task due") {
		t.Fatal("task due dates not hidden")
	}
	if strings.Contains(get("/calendar?view=week&q=appointment"), "Synthetic task due") || !strings.Contains(get("/calendar?view=week&q=five"), "Synthetic task at five") {
		t.Fatal("search ignores tasks")
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
