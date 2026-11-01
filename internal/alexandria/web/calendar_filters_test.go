package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"golang.org/x/net/html"
)

func calendarSection(t *testing.T, body, id string) string {
	t.Helper()
	node, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var found *html.Node
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		for _, a := range n.Attr {
			if a.Key == "id" && a.Val == id {
				found = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(node)
	if found == nil {
		t.Fatalf("missing section %s", id)
	}
	var out strings.Builder
	html.Render(&out, found)
	return out.String()
}

func TestCalendarFiltersNavigationAndValidation(t *testing.T) {
	now := calTime("2026-10-19T12:00:00Z")
	id := "9ea68f62-ea53-415c-ad76-7d10e4cc156f"
	// The event list's choice from earlier releases wins over the old shared one.
	req := httptest.NewRequest("GET", "/calendar?q=wood+working&days=14&past=1&filters=1", nil)
	req.AddCookie(&http.Cookie{Name: sourceCookie, Value: "local," + id})
	req.AddCookie(&http.Cookie{Name: sourceCookie + "_list", Value: id})
	v := readCalendarView(req, now, time.UTC)
	if v.Filter.Visible(id) || !v.Filter.Visible("local") || v.Horizon != 14 || !v.Past || !v.Filters {
		t.Fatal(v)
	}
	for _, raw := range []string{v.URL(), v.Previous(), v.Next(), v.Day("2026-11-01"), v.New("2026-10-19", "09:00")} {
		u, _ := url.Parse(raw)
		q := u.Query()
		if q.Get("q") != "wood working" || q.Has("hide") || q.Get("past") != "1" {
			t.Fatal("lost the view", raw)
		}
	}
	if got := readCalendarView(httptest.NewRequest("GET", "/calendar", nil), now, time.UTC); got.Horizon != 7 || got.Filter.Active() {
		t.Fatal("browser isolation", got)
	}
	for _, days := range []string{"14", "30", "90", "37", "3660"} {
		got := readCalendarView(httptest.NewRequest("GET", "/calendar?days="+days, nil), now, time.UTC)
		if u, _ := url.Parse(got.URL()); u.Query().Get("days") != days {
			t.Fatal("range", days)
		}
	}
	for _, days := range []string{"-1", "0", "6", "999999999", "bad"} {
		if got := readCalendarView(httptest.NewRequest("GET", "/calendar?days="+days, nil), now, time.UTC); got.Horizon != 7 {
			t.Fatal("unbounded range", got)
		}
	}
	if cleanHidden("tasks,local,garbage,"+id+","+id) != id+",local,tasks" || cleanHidden(strings.Repeat("x", 1001)) != "" {
		t.Fatal("invalid source state")
	}
	if len([]rune(cleanCalendarQuery(strings.Repeat("á", 300)))) != 200 {
		t.Fatal("query limit")
	}
	texts := []string{"Woodworking Workshop", "Austin", "Bring SAFETY glasses", "Community"}
	for _, q := range []string{"workshop AUSTIN", "safety community", "woodworking"} {
		if !(calendarFilter{Query: q}).Matches("local", texts...) {
			t.Fatal("search", q)
		}
	}
	if (calendarFilter{Query: "woodworking berlin"}).Matches("local", texts...) || (calendarFilter{Hidden: "local"}).Matches("local", texts...) {
		t.Fatal("filter did not restrict")
	}
}

func TestCalendarFiltersAndUpcoming(t *testing.T) {
	d, id, _ := subscriptionFixture(t)
	loc, _ := time.LoadLocation("America/Chicago")
	today := midnight(time.Now(), loc)
	store := events.NewStore(d.DB)
	for _, v := range []struct {
		id  string
		day int
	}{{"last-of-seven", 6}, {"after-seven", 7}} {
		e := events.Event{ID: v.id, Title: v.id, Start: today.AddDate(0, 0, v.day).Add(9 * time.Hour), End: today.AddDate(0, 0, v.day).Add(10 * time.Hour)}
		if err := store.Create(context.Background(), &e); err != nil {
			t.Fatal(err)
		}
	}
	h := Handler(d)
	call := func(method, path string, form url.Values, cookies []*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", d.PublicURL)
		for _, c := range cookies {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code >= 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
		return w
	}
	get := func(path string, cookies []*http.Cookie) string { return call("GET", path, nil, cookies).Body.String() }
	list := calendarSection(t, get("/calendar?view=upcoming", nil), "upcoming")
	if !strings.Contains(list, "last-of-seven") || strings.Contains(list, "after-seven") || strings.Contains(list, "Subscribed event -1") {
		t.Fatal("seven-day boundary")
	}
	if !strings.Contains(calendarSection(t, get("/calendar?view=upcoming&days=14", nil), "upcoming"), "after-seven") {
		t.Fatal("range did not expand")
	}
	past := calendarSection(t, get("/calendar?view=upcoming&past=1", nil), "past")
	if !strings.Contains(past, "Subscribed event -1") || strings.Contains(past, "Subscribed event 0") {
		t.Fatal("past 30 days", past)
	}
	body := get("/calendar?view=upcoming&q=Subscribed+event+0", nil)
	if list = calendarSection(t, body, "upcoming"); !strings.Contains(list, ">Subscribed event 0<") || strings.Contains(list, ">Subscribed event 1<") || strings.Contains(list, "Synthetic appointment 1") {
		t.Fatal("search", list)
	}
	if !strings.Contains(body, "Filter · on") {
		t.Fatal("filter state not shown")
	}
	// One choice for the whole site, kept in the browser; search and the date stay in the URL.
	ret := "/calendar?view=week&date=2026-11-01&days=14&q=appointment"
	old := &http.Cookie{Name: sourceCookie + "_grid", Value: "local"}
	w := call("POST", "/calendar/subscriptions/visibility", url.Values{"return": {ret}, "q": {"Subscribed"}, "visible": {id, "tasks"}, "week": {"monday"}}, []*http.Cookie{old})
	var saved *http.Cookie
	expired, week := 0, false
	for _, c := range w.Result().Cookies() {
		if c.Name == sourceCookie && c.Path == "/" {
			saved = c
		}
		if c.Name == sourceCookie+"_grid" && c.MaxAge < 0 {
			expired++
		}
		week = week || c.Name == calendarCookie && strings.Contains(c.Value, "week=monday")
	}
	if saved == nil || saved.Value != "local" || expired != 2 || !week {
		t.Fatal("source preference not saved for the whole site", w.Result().Cookies())
	}
	redirect, _ := url.Parse(w.Header().Get("Location"))
	q := redirect.Query()
	if q.Get("q") != "Subscribed" || q.Get("date") != "2026-11-01" || q.Get("days") != "14" || q.Get("filters") != "1" || q.Get("week") != "monday" {
		t.Fatal("lost navigation", redirect)
	}
	body = get("/calendar?view=upcoming", []*http.Cookie{saved})
	if strings.Contains(body, "Synthetic appointment 1") || !strings.Contains(body, "Subscribed event 0") {
		t.Fatal("hidden calendar shown")
	}
	w = call("POST", "/calendar/subscriptions/visibility", url.Values{"return": {redirect.String()}, "reset": {"yes"}}, []*http.Cookie{saved})
	redirect, _ = url.Parse(w.Header().Get("Location"))
	if redirect.Query().Get("q") != "" {
		t.Fatal("reset failed")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == sourceCookie && c.Path == "/" && c.Value != "" {
			t.Fatal("reset not remembered")
		}
	}
}

// A choice saved by an earlier release, under /calendar or split into the
// event list's and the grid's, becomes the one site-wide choice.
func TestMigrateCalendarFilter(t *testing.T) {
	r := httptest.NewRequest("GET", "/calendar", nil)
	r.AddCookie(&http.Cookie{Name: sourceCookie + "_list", Value: "local"})
	w := httptest.NewRecorder()
	migrateCalendarFilter(w, r, readCalendarFilter(r))
	var site bool
	old := 0
	for _, c := range w.Result().Cookies() {
		site = site || c.Name == sourceCookie && c.Path == "/" && c.Value == "local" && c.MaxAge > 0
		if c.Name == sourceCookie+"_list" && c.MaxAge < 0 {
			old++
		}
	}
	if !site || old != 2 {
		t.Fatalf("%+v", w.Result().Cookies())
	}
	w = httptest.NewRecorder()
	migrateCalendarFilter(w, httptest.NewRequest("GET", "/calendar", nil), calendarFilter{})
	if len(w.Result().Cookies()) != 0 {
		t.Fatal("a browser without a choice got one")
	}
}
