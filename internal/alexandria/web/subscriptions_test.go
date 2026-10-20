package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
)

func subscriptionFixture(t *testing.T) (Deps, string, string) {
	t.Helper()
	d, _ := calendarFixture(t)
	ctx := context.Background()
	s := subscriptions.Store{DB: d.DB}
	id, err := s.Create(ctx, "Community calendar", "purple", "https://calendar.example/public.ics?token=never-render")
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("America/Chicago")
	today := midnight(time.Now(), loc)
	body := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//synthetic//EN\r\nX-WR-TIMEZONE:America/Chicago\r\n"
	for i := -1; i < 5; i++ {
		body += fmt.Sprintf("BEGIN:VEVENT\r\nUID:imported-%d\r\nDTSTAMP:20261019T000000Z\r\nSUMMARY:Subscribed event %d\r\nDTSTART:%sT090000\r\nDURATION:PT1H\r\nDESCRIPTION:Synthetic public event\r\nURL:https://example.org/event\r\nEND:VEVENT\r\n", i, i, today.AddDate(0, 0, i).Format("20060102"))
	}
	body += "BEGIN:VEVENT\r\nUID:recurring\r\nDTSTAMP:20261019T000000Z\r\nSUMMARY:Subscribed recurring event\r\nDTSTART:20200101T093000\r\nDURATION:PT1H\r\nRRULE:FREQ=DAILY\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	parsed, err := subscriptions.Parse(ctx, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	var key string
	for _, v := range parsed {
		if v.UID == "imported-0" {
			key = v.Key
		}
		if _, err = d.DB.ExecContext(ctx, `INSERT INTO alexandria_calendar_import(subscription_id,uid,series_key,ical_blob) VALUES($1,$2,$3,$4)`, id, v.UID, v.Key, v.Blob); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = d.DB.ExecContext(ctx, `UPDATE alexandria_calendar_subscription SET last_success=now(),next_at=now()+interval '30 minutes',error_code='http_error' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	return d, id, key
}

func syntheticSubscriptionClient(t *testing.T) *http.Client {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/calendar-page" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<iframe src="https://calendar.google.com/calendar/embed?src=a%40b"></iframe>`)
			return
		}
		if r.URL.Path == "/multiple" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<iframe src="https://calendar.google.com/calendar/embed?src=a%40b&amp;src=c%40d"></iframe>`)
			return
		}
		if r.URL.Path == "/page" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "<html>No feed</html>")
			return
		}
		w.Header().Set("Content-Type", "text/calendar")
		fmt.Fprint(w, "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//synthetic//EN\r\nEND:VCALENDAR\r\n")
	}))
	t.Cleanup(srv.Close)
	transport := srv.Client().Transport.(*http.Transport).Clone()
	target, _ := url.Parse(srv.URL)
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.ServerName = target.Hostname()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, target.Host)
	}
	return &http.Client{Transport: transport}
}

func TestSubscriptionPagesAndMutations(t *testing.T) {
	d, id, key := subscriptionFixture(t)
	d.calendarHTTPClient = syntheticSubscriptionClient(t)
	h := Handler(d)
	call := func(method, path, lang, origin string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		r.Header.Set("Accept-Language", lang)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	get := func(path, lang string, cookie *http.Cookie) string {
		w := call("GET", path, lang, "", nil, cookie)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "never-render") {
			t.Fatal("feed URL exposed")
		}
		return w.Body.String()
	}
	for _, lang := range []string{"en", "es"} {
		for _, view := range []string{"day", "week", "month"} {
			body := get("/calendar?view="+view, lang, nil)
			for _, want := range []string{"Subscribed event 0", "Subscribed event -1", "Community calendar", "Subscribed recurring event"} {
				if !strings.Contains(body, want) {
					t.Fatal(lang, view, "missing", want)
				}
			}
			if strings.Contains(body, `action="/calendar/subscription:`) {
				t.Fatal("imported mutation form")
			}
		}
		get("/settings/integrations?section=subscriptions", lang, nil)
	}
	if _, err := d.DB.ExecContext(context.Background(), `UPDATE alexandria_calendar_import SET ical_blob=replace(ical_blob,'SUMMARY:Subscribed event 4','SUMMARY:') WHERE subscription_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for lang, title := range map[string]string{"en": "Untitled event", "es": "Evento sin título"} {
		if !strings.Contains(get("/calendar", lang, nil), title) {
			t.Fatal("missing localized untitled event", lang)
		}
	}
	body := get("/settings/integrations", "en", nil)
	if strings.Contains(body, `id="subscriptions" open`) {
		t.Fatal("tile should start collapsed")
	}
	loc, _ := time.LoadLocation("America/Chicago")
	detail := "/calendar/subscriptions/" + id + "/events/" + key + "?on=" + time.Now().In(loc).Format("2006-01-02") + "&return=" + url.QueryEscape("/calendar?view=week&order=calendar")
	body = get(detail, "en", nil)
	if !strings.Contains(body, "Subscribed event 0") || !strings.Contains(body, "is read-only") || strings.Contains(body, `name="title"`) || !strings.Contains(body, "view=week&amp;order=calendar") {
		t.Fatal(body)
	}
	// Direct submissions cannot mutate the separate imported store, including
	// attempts using a publisher UID and the internal scoped projection ID.
	for _, eventID := range []string{"imported-0", subscriptions.EventID(id, key)} {
		for _, action := range []string{"edit", "delete", "restore", "purge"} {
			call("POST", "/calendar/"+url.PathEscape(eventID)+"/"+action, "en", d.PublicURL, url.Values{"title": {"overwrite"}, "date": {"2026-10-19"}, "confirm": {"yes"}}, nil)
		}
	}
	if !strings.Contains(get(detail, "en", nil), "Subscribed event 0") {
		t.Fatal("import mutated")
	}
	if !strings.Contains(get("/calendar/synthetic-1/edit", "en", nil), `name="title"`) {
		t.Fatal("ordinary editing lost")
	}
	ret := "/calendar?view=week&order=calendar&date=2026-11-01"
	w := call("POST", "/calendar/subscriptions/visibility", "en", d.PublicURL, url.Values{"visible": {"local"}, "return": {ret}}, nil)
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == sourceCookie {
			cookie = c
		}
	}
	if cookie == nil || !strings.HasPrefix(w.Header().Get("Location"), ret+"&") {
		t.Fatal(w.Header())
	}
	if strings.Contains(get("/calendar", "en", cookie), "Subscribed event 0") {
		t.Fatal("source visibility ignored")
	}
	if !strings.Contains(get("/calendar", "en", nil), "Subscribed event 0") {
		t.Fatal("visibility leaked between browsers")
	}
	forged := &http.Cookie{Name: sourceCookie, Value: "garbage"}
	if !strings.Contains(get("/calendar", "en", forged), "Subscribed event 0") {
		t.Fatal("invalid preference affected display")
	}
	if w = call("POST", "/calendar/subscriptions/"+id+"/remove", "en", "https://evil.example", url.Values{"revision": {"1"}, "confirm": {"yes"}}, nil); w.Code != 403 {
		t.Fatal("origin guard", w.Code)
	}
	s := subscriptions.Store{DB: d.DB}
	ctx := context.Background()
	call("POST", "/calendar/subscriptions/"+id+"/remove", "en", d.PublicURL, url.Values{"revision": {"1"}}, nil)
	if _, err := s.Get(ctx, id); err != nil {
		t.Fatal("removed without confirmation")
	}
	call("POST", "/calendar/subscriptions/"+id+"/update", "en", d.PublicURL, url.Values{"revision": {"1"}, "name": {"Paused calendar"}, "color": {"green"}}, nil)
	sub, err := s.Get(ctx, id)
	if err != nil || sub.Enabled || sub.Revision != 2 {
		t.Fatal(sub, err)
	}
	if strings.Contains(get("/calendar", "en", nil), "Subscribed event 0") {
		t.Fatal("paused source visible")
	}
	call("POST", "/calendar/subscriptions/"+id+"/remove", "en", d.PublicURL, url.Values{"revision": {"1"}, "confirm": {"yes"}}, nil)
	if _, err = s.Get(ctx, id); err != nil {
		t.Fatal("stale removal accepted")
	}
	call("POST", "/calendar/subscriptions/"+id+"/remove", "en", d.PublicURL, url.Values{"revision": {"2"}, "confirm": {"yes"}}, nil)
	if _, err = s.Get(ctx, id); err == nil {
		t.Fatal("unsubscribe failed")
	}
	if !strings.Contains(get("/calendar/synthetic-1/edit", "en", nil), `name="title"`) {
		t.Fatal("unsubscribe damaged local calendar")
	}
	if !strings.Contains(get("/calendar", "en", &http.Cookie{Name: sourceCookie, Value: "local"}), "calendar-sources") {
		t.Fatal("hidden local calendar cannot be restored after unsubscribe")
	}
	for _, raw := range []string{"http://example.org/feed", "https://127.0.0.1/feed"} {
		call("POST", "/calendar/subscriptions", "en", d.PublicURL, url.Values{"name": {"Invalid"}, "color": {"blue"}, "feed_url": {raw}}, nil)
	}
	all, err := s.List(ctx)
	if err != nil || len(all) != 0 {
		t.Fatal(all, err)
	}
	w = call("POST", "/calendar/subscriptions", "es", d.PublicURL, url.Values{"name": {"No feed"}, "color": {"blue"}, "feed_url": {"https://example.org/page"}}, nil)
	redirect, _ := url.Parse(w.Header().Get("Location"))
	redirect.Fragment = ""
	if !strings.Contains(get(redirect.String(), "es", nil), "No se encontró") {
		t.Fatal("missing translated discovery error")
	}
	call("POST", "/calendar/subscriptions", "en", d.PublicURL, url.Values{"name": {"New calendar"}, "color": {"blue"}, "feed_url": {"https://calendar.google.com/calendar?cid=a%40b"}}, nil)
	all, err = s.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatal(all, err)
	}
	get("/calendar", "es", nil) // initial refresh/no-snapshot notice
}

func TestSubscriptionBrowserFixture(t *testing.T) {
	dir := os.Getenv("ALEXANDRIA_SUBSCRIPTIONS_BROWSER")
	if dir == "" {
		t.Skip("browser driver not requested")
	}
	d, _, _ := subscriptionFixture(t)
	d.calendarHTTPClient = syntheticSubscriptionClient(t)
	d.PublicURL = ""
	srv := httptest.NewServer(Handler(d))
	defer srv.Close()
	if err := os.WriteFile(filepath.Join(dir, "url"), []byte(srv.URL), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(12 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser driver timeout")
}
