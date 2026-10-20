package subscriptions

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func feed(body string) string {
	return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//synthetic//EN\r\n" + strings.ReplaceAll(body, "\n", "\r\n") + "END:VCALENDAR\r\n"
}
func event(uid, title string) string {
	return "BEGIN:VEVENT\nDTSTAMP:20261019T000000Z\nUID:" + uid + "\nSUMMARY:" + title + "\nDTSTART:20261020T090000Z\nDTEND:20261020T100000Z\nEND:VEVENT\n"
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testFeed(t *testing.T, handler http.HandlerFunc) *http.Client {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	transport := srv.Client().Transport
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		u := *r.URL
		u.Host = target.Host
		r.URL = &u
		return transport.RoundTrip(r)
	}), Timeout: 3 * time.Second}
}
func TestFeedValidationAndRecurrence(t *testing.T) {
	ctx := context.Background()
	body := `X-WR-TIMEZONE:America/Chicago
BEGIN:VEVENT
DTSTAMP:20261019T000000Z
UID:annual
SUMMARY:Annual event
DTSTART:20201020T090000
DURATION:PT1H
RRULE:FREQ=YEARLY;COUNT=10
END:VEVENT
BEGIN:VEVENT
DTSTAMP:20261019T000000Z
UID:annual
RECURRENCE-ID:20261020T090000
DTSTART:20261021T100000
DTEND:20261021T110000
SUMMARY:Moved
END:VEVENT
`
	series, err := Parse(ctx, []byte(feed(body)))
	if err != nil || len(series) != 1 {
		t.Fatal(series, err)
	}
	e, err := events.FromSeries("scoped", series[0].Blob, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	x := events.Expand(ctx, []events.Event{e}, []events.Window{{From: from, To: from.AddDate(0, 0, 5)}}, time.UTC)
	if len(x.Incomplete) != 0 || len(x.Occurrences) != 1 || x.Occurrences[0].Event.Title != "Moved" || x.Occurrences[0].Event.Start.UTC().Hour() != 15 {
		t.Fatal(x)
	}
	for name, raw := range map[string]string{
		"html": "<html>login</html>", "truncated": strings.TrimSuffix(feed(event("x", "x")), "END:VCALENDAR\r\n"),
		"duplicate": feed(event("x", "x") + event("x", "y")), "missing uid": feed(strings.Replace(event("x", "x"), "UID:x\n", "", 1)), "two calendars": feed("") + feed(""),
		"missing start": feed(strings.Replace(event("x", "x"), "DTSTART:20261020T090000Z\n", "", 1)),
		"oversize":      strings.Repeat("a", MaxFeedBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(ctx, []byte(raw)); err == nil {
				t.Fatal("accepted invalid feed")
			}
		})
	}
	empty, err := Parse(ctx, []byte(feed("")))
	if err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = Parse(ctx, []byte(feed(event("x", "x")))); err == nil {
		t.Fatal("ignored cancellation")
	}
	unknown := strings.Replace(feed(event("x", "x")), "DTSTART:20261020T090000Z", "DTSTART;TZID=Custom/Unmapped:20261020T090000", 1)
	parsed, err := Parse(context.Background(), []byte(unknown))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = events.FromSeries("scoped", parsed[0].Blob, time.UTC); err == nil {
		t.Fatal("guessed unknown zone")
	}
}
func TestPublicFetchDestinations(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "192.0.2.5", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "64:ff9b::7f00:1", "2002:7f00:1::", "2001:db8::1"} {
		if publicIP(netip.MustParseAddr(ip)) {
			t.Fatal("accepted", ip)
		}
	}
	for _, ip := range []string{"8.8.8.8", "2606:4700:4700::1111"} {
		if !publicIP(netip.MustParseAddr(ip)) {
			t.Fatal("refused", ip)
		}
	}
	for _, raw := range []string{"http://example.org/x", "https://user:pass@example.org/x", "https://127.0.0.1/x", "https://example.org:8443/x", "file:///etc/passwd", "https://example.org/x#fragment"} {
		if ValidateURL(raw) == nil {
			t.Fatal("accepted", raw)
		}
	}
	c := PublicClient()
	tr := c.Transport.(*http.Transport)
	if tr.Proxy != nil {
		t.Fatal("inherits proxy")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, err := tr.DialContext(ctx, "tcp", "127.0.0.1:443"); err == nil {
		conn.Close()
		t.Fatal("dialed private address")
	}
	req, _ := http.NewRequest("GET", "https://127.0.0.1/private", nil)
	if c.CheckRedirect(req, nil) == nil {
		t.Fatal("accepted private redirect")
	}
}
func TestSnapshotRefreshAndSourceIsolation(t *testing.T) {
	db := testenv.Database(t).DB
	s := Store{DB: db}
	ctx := context.Background()
	var body atomic.Value
	body.Store(feed(event("same", "First")))
	var status atomic.Int32
	status.Store(200)
	var calls atomic.Int32
	c := testFeed(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "text/calendar")
		w.Header().Set("ETag", `"v1"`)
		if status.Load() == 304 && r.Header.Get("If-None-Match") != `"v1"` {
			t.Error("lost validator")
		}
		w.WriteHeader(int(status.Load()))
		fmt.Fprint(w, body.Load().(string))
	})
	a, err := s.Create(ctx, "A", "blue", "https://calendar.example/a.ics?private=never-print")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Create(ctx, "B", "green", "https://calendar.example/b.ics")
	if err != nil {
		t.Fatal(err)
	}
	process := func() {
		t.Helper()
		if found, err := s.Process(ctx, c); err != nil || !found {
			t.Fatal(found, err)
		}
	}
	due := func(id string) {
		t.Helper()
		if _, err := db.Exec(`UPDATE alexandria_calendar_subscription SET next_at=now() WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
	}
	process()
	process()
	read := func() []Imported {
		t.Helper()
		v, issues, err := s.Read(ctx, []string{a, b}, time.UTC)
		if err != nil || len(issues) != 0 {
			t.Fatal(issues, err)
		}
		return v
	}
	got := read()
	if len(got) != 2 || got[0].Event.ID == got[1].Event.ID {
		t.Fatal(got)
	}
	var raw string
	db.QueryRow(`SELECT original_feed FROM alexandria_calendar_subscription WHERE id=$1`, a).Scan(&raw)
	if raw != body.Load().(string) {
		t.Fatal("original feed changed")
	}
	status.Store(304)
	due(a)
	process()
	if len(read()) != 2 {
		t.Fatal("304 lost data")
	}
	status.Store(200)
	body.Store("<html>upstream broke</html>")
	due(a)
	process()
	sub, _ := s.Get(ctx, a)
	if sub.ErrorCode != "invalid_feed" || sub.LastSuccess == nil || len(read()) != 2 {
		t.Fatal(sub.Status())
	}
	body.Store(feed(event("same", "Changed")))
	due(a)
	process()
	got = read()
	for _, v := range got {
		if v.SubscriptionID == a && v.Event.Title != "Changed" {
			t.Fatal(got)
		}
	}
	body.Store(feed(""))
	due(a)
	process()
	if len(read()) != 1 {
		t.Fatal("valid empty snapshot did not clear source A")
	}
	body.Store(feed(event("same", "Restored")))
	due(a)
	process()
	if len(read()) != 2 {
		t.Fatal("publisher restoration blocked")
	}
	var local int
	db.QueryRow(`SELECT count(*) FROM alexandria_events`).Scan(&local)
	if local != 0 {
		t.Fatal("leaked to local events")
	}
	sub, _ = s.Get(ctx, a)
	if err := s.Update(ctx, a, sub.Revision, "Renamed", "purple", false); err != nil {
		t.Fatal(err)
	}
	if len(read()) != 1 {
		t.Fatal("paused source visible")
	}
	sub, _ = s.Get(ctx, a)
	if err := s.Remove(ctx, a, sub.Revision); err != nil {
		t.Fatal(err)
	}
	var cached int
	db.QueryRow(`SELECT count(*) FROM alexandria_calendar_import WHERE subscription_id=$1`, a).Scan(&cached)
	if cached != 0 {
		t.Fatal("unsubscribe leaked cache")
	}
	if calls.Load() != 7 {
		t.Fatal("unexpected fetch count", calls.Load())
	}
}
func TestRefreshFencesRemovalConfigurationAndLease(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	s := Store{DB: db}
	for _, action := range []string{"remove", "pause", "lease"} {
		t.Run(action, func(t *testing.T) {
			id, err := s.Create(ctx, action, "blue", "https://calendar.example/test.ics")
			if err != nil {
				t.Fatal(err)
			}
			sub, err := s.claim(ctx)
			if err != nil || sub.ID != id {
				t.Fatal(sub, err)
			}
			switch action {
			case "remove":
				err = s.Remove(ctx, id, sub.Revision)
			case "pause":
				err = s.Update(ctx, id, sub.Revision, "paused", "gray", false)
			case "lease":
				_, err = db.Exec(`UPDATE alexandria_calendar_subscription SET lease_until=now()-interval '1 second' WHERE id=$1`, id)
			}
			if err != nil {
				t.Fatal(err)
			}
			raw := []byte(feed(event("x", "late")))
			series, parseErr := Parse(ctx, raw)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if err = s.finish(ctx, sub, fetched{raw: raw}, series, ""); err != nil {
				t.Fatal(err)
			}
			var n int
			db.QueryRow(`SELECT count(*) FROM alexandria_calendar_import WHERE subscription_id=$1`, id).Scan(&n)
			if n != 0 {
				t.Fatal("late snapshot published")
			}
			if action == "lease" {
				next, err := s.claim(ctx)
				if err != nil || next.token == sub.token {
					t.Fatal(next, err)
				}
				if err = s.finish(ctx, sub, fetched{raw: raw}, series, ""); err != nil {
					t.Fatal(err)
				}
				if err = s.finish(ctx, next, fetched{raw: raw}, series, ""); err != nil {
					t.Fatal(err)
				}
				db.QueryRow(`SELECT count(*) FROM alexandria_calendar_import WHERE subscription_id=$1`, id).Scan(&n)
				if n != 1 {
					t.Fatal("reclaimed worker failed")
				}
			}
		})
	}
}

// Opt-in, read-only interoperability probe; public network availability is not a CI dependency.
func TestPublicCalendarSample(t *testing.T) {
	rawURL := os.Getenv("ALEXANDRIA_PUBLIC_CALENDAR_SAMPLE")
	if rawURL == "" {
		t.Skip("public calendar sample not requested")
	}
	ctx := context.Background()
	f, code := fetch(ctx, PublicClient(), Subscription{feedURL: rawURL})
	if code != "" {
		t.Fatal(code)
	}
	series, err := Parse(ctx, f.raw)
	if err != nil {
		t.Fatal(err)
	}
	loc, _ := time.LoadLocation("America/Chicago")
	var input []events.Event
	for _, s := range series {
		e, err := events.FromSeries(s.Key, s.Blob, loc)
		if err != nil {
			t.Fatalf("unsupported sample series: %v", err)
		}
		input = append(input, e)
	}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
	x := events.Expand(ctx, input, []events.Window{{From: from, To: from.AddDate(0, 3, 0)}}, loc)
	t.Logf("bytes=%d series=%d occurrences_oct_dec_2026=%d incomplete=%d", len(f.raw), len(series), len(x.Occurrences), len(x.Incomplete))
	if len(x.Incomplete) != 0 {
		t.Fatalf("incomplete reasons: %v", x.Incomplete)
	}
}
