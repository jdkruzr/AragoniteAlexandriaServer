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
	req := httptest.NewRequest("GET", "/calendar?grid_hide=&list_q=wood+working&days=14", nil)
	req.AddCookie(&http.Cookie{Name: sourceCookie, Value: "local," + id})
	req.AddCookie(&http.Cookie{Name: sourceCookie + "_list", Value: id})
	v := readCalendarView(req, now, time.UTC)
	if v.ListFilter.Visible(id) || !v.ListFilter.Visible("local") || !v.GridFilter.Visible(id) || v.Horizon != 14 {
		t.Fatal(v)
	}
	for _, raw := range []string{v.URL(), v.Previous(), v.Next(), v.More(), v.Day("2026-11-01")} {
		u, _ := url.Parse(raw)
		q := u.Query()
		if q.Get("list_q") != "wood working" || q.Get("list_hide") != id || !q.Has("grid_hide") || q.Get("grid_hide") != "" {
			t.Fatal("lost filters", raw)
		}
	}
	newURL, _ := url.Parse(v.New("2026-10-19", "09:00", "0"))
	ret, _ := url.Parse(newURL.Query().Get("return"))
	if ret.Query().Get("list_q") != "wood working" {
		t.Fatal("lost form return")
	}
	if got := readCalendarView(httptest.NewRequest("GET", "/calendar", nil), now, time.UTC); got.Horizon != 7 || got.ListFilter.Active() || got.GridFilter.Active() {
		t.Fatal("browser isolation", got)
	}
	for _, days := range []string{"7", "14", "30", "90", "37", "3660"} {
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
	if cleanHidden("local,garbage,"+id+","+id) != id+",local" || cleanHidden(strings.Repeat("x", 1001)) != "" {
		t.Fatal("invalid source state")
	}
	if len([]rune(cleanCalendarQuery(strings.Repeat("á", 300)))) != 200 {
		t.Fatal("query limit")
	}
	row := calendarRow{Event: events.Event{Title: "Woodworking Workshop", Location: "Austin", Detail: "Bring SAFETY glasses"}, CalendarName: "Community"}
	for _, q := range []string{"workshop AUSTIN", "safety community", "woodworking"} {
		if !(calendarFilter{Query: q}).Matches(row, "local") {
			t.Fatal("search", q)
		}
	}
	if (calendarFilter{Query: "woodworking berlin"}).Matches(row, "local") || (calendarFilter{Hidden: "local"}).Matches(row, "local") {
		t.Fatal("filter did not restrict")
	}
}

func TestSeparateCalendarFiltersAndSevenDayHorizon(t *testing.T) {
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
	body := get("/calendar", nil)
	list := calendarSection(t, body, "upcoming")
	if !strings.Contains(list, "last-of-seven") || strings.Contains(list, "after-seven") {
		t.Fatal("seven-day boundary")
	}
	if !strings.Contains(calendarSection(t, get("/calendar?days=14", nil), "upcoming"), "after-seven") {
		t.Fatal("range did not expand")
	}
	body = get("/calendar?list_hide="+id+"&grid_hide=local", nil)
	list = calendarSection(t, body, "upcoming")
	grid := calendarSection(t, body, "calendar-grid")
	if strings.Contains(list, "Subscribed event 0") || !strings.Contains(list, "Synthetic appointment 1") || !strings.Contains(grid, "Subscribed event 0") || strings.Contains(grid, "Synthetic appointment 1") {
		t.Fatal("sources leaked between sections")
	}
	body = get("/calendar?list_q=Subscribed+event+0&grid_q=Synthetic+appointment+1", nil)
	list = calendarSection(t, body, "upcoming")
	grid = calendarSection(t, body, "calendar-grid")
	if !strings.Contains(list, ">Subscribed event 0</a>") || strings.Contains(list, ">Subscribed event 1</a>") || strings.Contains(list, ">Synthetic appointment 1</a>") || !strings.Contains(grid, ">Synthetic appointment 1</strong>") || strings.Contains(grid, ">Subscribed event 0</strong>") {
		t.Fatal("searches leaked between sections")
	}
	// Sources persist separately, search and date are preserved in URLs only.
	ret := "/calendar?view=week&date=2026-11-01&order=calendar&days=14&grid_q=appointment"
	w := call("POST", "/calendar/subscriptions/visibility", url.Values{"scope": {"list"}, "return": {ret}, "q": {"Subscribed"}, "visible": {id}}, nil)
	var saved *http.Cookie
	expired := false
	for _, c := range w.Result().Cookies() {
		if c.Name == sourceCookie+"_list" && c.Path == "/" {
			saved = c
		}
		if c.Name == sourceCookie+"_list" && c.Path == "/calendar" && c.MaxAge < 0 {
			expired = true
		}
	}
	// Site-wide, so Today's Next 7 days follows it; the old /calendar copy is removed.
	if saved == nil || saved.Value != "local" || !expired {
		t.Fatal("source preference not saved for the whole site")
	}
	redirect, _ := url.Parse(w.Header().Get("Location"))
	q := redirect.Query()
	if redirect.Fragment != "upcoming" || q.Get("list_q") != "Subscribed" || q.Get("grid_q") != "appointment" || q.Get("date") != "2026-11-01" || q.Get("days") != "14" {
		t.Fatal("lost navigation", redirect)
	}
	body = get("/calendar", []*http.Cookie{saved})
	if strings.Contains(calendarSection(t, body, "upcoming"), "Synthetic appointment 1") || !strings.Contains(calendarSection(t, body, "calendar-grid"), "Synthetic appointment 1") {
		t.Fatal("preference not isolated")
	}
	explicit := "/calendar?list_hide=&list_q=NoSuchEvent"
	body = get(explicit, []*http.Cookie{saved})
	if !strings.Contains(calendarSection(t, body, "upcoming"), "No events match these filters.") || !strings.Contains(calendarSection(t, body, "calendar-grid"), "Synthetic appointment") {
		t.Fatal("empty search affected grid")
	}
	w = call("POST", "/calendar/subscriptions/visibility", url.Values{"scope": {"list"}, "return": {explicit}, "reset": {"yes"}}, []*http.Cookie{saved})
	redirect, _ = url.Parse(w.Header().Get("Location"))
	if redirect.Query().Get("list_q") != "" || redirect.Query().Get("list_hide") != "" {
		t.Fatal("reset failed")
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == sourceCookie+"_list" && c.Value != "" {
			t.Fatal("reset not remembered")
		}
	}
	// Past uses list filters; no filtering or hiding of deleted events.
	if strings.Contains(calendarSection(t, get("/calendar?list_hide="+id, nil), "past"), "Subscribed event -1") {
		t.Fatal("past ignores list source")
	}
}

// Choices saved under /calendar by earlier releases move to the whole site on
// the next Calendar visit.
func TestMoveFilterCookies(t *testing.T) {
	r := httptest.NewRequest("GET", "/calendar", nil)
	r.AddCookie(&http.Cookie{Name: sourceCookie + "_list", Value: "local"})
	w := httptest.NewRecorder()
	moveFilterCookies(w, r)
	var site, old bool
	for _, c := range w.Result().Cookies() {
		site = site || c.Name == sourceCookie+"_list" && c.Path == "/" && c.Value == "local" && c.MaxAge > 0
		old = old || c.Name == sourceCookie+"_list" && c.Path == "/calendar" && c.MaxAge < 0
	}
	if !site || !old || len(w.Result().Cookies()) != 2 {
		t.Fatalf("%+v", w.Result().Cookies())
	}
}
