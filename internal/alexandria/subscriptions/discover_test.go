package subscriptions

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestGoogleCalendarLinks(t *testing.T) {
	id := "calendar@example.org"
	want := "https://calendar.google.com/calendar/ical/" + url.PathEscape(id) + "/public/basic.ics"
	for _, raw := range []string{
		"https://calendar.google.com/calendar/u/0/r?cid=" + url.QueryEscape(id),
		"https://calendar.google.com/calendar/u/0?cid=" + base64.RawStdEncoding.EncodeToString([]byte(id)),
		"https://www.google.com/calendar/embed?src=" + url.QueryEscape(id) + "&ctz=America%2FChicago",
		"https://calendar.google.com/calendar/embed?src=" + url.QueryEscape(id) + "&src=" + url.QueryEscape(id),
	} {
		u, _ := url.Parse(raw)
		feeds, ok := googleFeeds(u)
		got, err := oneFeed(feeds)
		if !ok || err != nil || got != want {
			t.Fatalf("conversion: %q %v %v", got, ok, err)
		}
	}
	holiday, _ := url.Parse("https://calendar.google.com/calendar/embed?src=en.usa%23holiday%40group.v.calendar.google.com")
	candidates, _ := googleFeeds(holiday)
	if got, err := oneFeed(candidates); err != nil || got != "https://calendar.google.com/calendar/ical/en.usa%23holiday@group.v.calendar.google.com/public/basic.ics" {
		t.Fatal("escaped ID", got, err)
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		u, _ := url.Parse("https://calendar.google.com/calendar?cid=" + url.QueryEscape(enc.EncodeToString([]byte(id))))
		feeds, _ := googleFeeds(u)
		if got, err := oneFeed(feeds); err != nil || got != want {
			t.Fatal(got, err)
		}
	}
	for _, raw := range []string{want, "https://calendar.google.com.evil.example/calendar?cid=x@y", "https://evil.example/calendar?cid=x@y", "https://calendar.google.com/calendar-fake?cid=x@y"} {
		u, _ := url.Parse(raw)
		if _, ok := googleFeeds(u); ok {
			t.Fatal("interpreted non-share URL", raw)
		}
	}
	for _, id := range []string{"", "not-base64", "../../x@y", "x@y?z", "x@y\n", "@example.org"} {
		u, _ := url.Parse("https://calendar.google.com/calendar?cid=" + url.QueryEscape(id))
		feeds, ok := googleFeeds(u)
		if !ok || len(feeds) != 0 {
			t.Fatal("accepted invalid calendar ID")
		}
	}
}

func TestResolveCalendarSources(t *testing.T) {
	good := feed(event("example", "Synthetic"))
	for _, tc := range []struct {
		name, input, page, code, want string
		calls, status                 int
	}{
		{name: "direct", input: "/feed.ics?secret=private", want: "https://example.org/feed.ics?secret=private", calls: 1},
		{name: "Google share", input: "https://calendar.google.com/calendar/u/0/r?cid=" + base64.RawStdEncoding.EncodeToString([]byte("a@b")), want: "https://calendar.google.com/calendar/ical/a@b/public/basic.ics", calls: 1},
		{name: "embed", input: "/page", page: `<iframe src="https://calendar.google.com/calendar/embed?src=a%40b&amp;ctz=America/Chicago"></iframe>`, want: "https://calendar.google.com/calendar/ical/a@b/public/basic.ics", calls: 2},
		{name: "relative ICS", input: "/nested/page", page: `<a href="../feed.ics">ICS</a>`, want: "https://example.org/feed.ics", calls: 2},
		{name: "typed link", input: "/page", page: `<link type="text/calendar" href="/feed">`, want: "https://example.org/feed", calls: 2},
		{name: "duplicate links", input: "/page", page: `<iframe src="https://calendar.google.com/calendar/embed?src=a%40b"></iframe><a href="https://calendar.google.com/calendar?cid=YUBi">Subscribe</a>`, want: "https://calendar.google.com/calendar/ical/a@b/public/basic.ics", calls: 2},
		{name: "multiple", input: "/page", page: `<iframe src="https://calendar.google.com/calendar/embed?src=a%40b&amp;src=c%40d"></iframe>`, code: "multiple_feeds", calls: 1},
		{name: "multiple share", input: "https://calendar.google.com/calendar/embed?src=a%40b&src=c%40d", code: "multiple_feeds", calls: 0},
		{name: "no calendar", input: "/page", page: `<html>Just a page</html>`, code: "no_feed", calls: 1},
		{name: "scripts ignored", input: "/page", page: `<script>document.write('<a href="https://example.org/feed.ics">')</script>`, code: "no_feed", calls: 1},
		{name: "private link", input: "/page", page: `<a href="https://127.0.0.1/feed.ics">ICS</a>`, code: "no_feed", calls: 1},
		{name: "credentials", input: "/page", page: `<a href="https://user:pass@example.org/feed.ics">ICS</a>`, code: "no_feed", calls: 1},
		{name: "no crawling", input: "/page", page: `<iframe src="https://example.org/another-page"></iframe>`, code: "no_feed", calls: 1},
		{name: "private calendar", input: "https://calendar.google.com/calendar?cid=a%40b", status: 404, code: "http_error", calls: 1},
		{name: "upstream HTML", input: "https://calendar.google.com/calendar?cid=a%40b", page: `<html>Login</html>`, code: "html_page", calls: 1},
		{name: "oversized", input: "/page", page: strings.Repeat("x", MaxFeedBytes+1), code: "too_large", calls: 1},
		{name: "private input", input: "https://127.0.0.1/feed.ics", code: "invalid_url", calls: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := testFeed(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				if calls == 1 && tc.page != "" {
					w.Header().Set("Content-Type", "text/html")
					fmt.Fprint(w, tc.page)
					return
				}
				w.Header().Set("Content-Type", "text/calendar")
				fmt.Fprint(w, good)
			})
			raw := tc.input
			if strings.HasPrefix(raw, "/") {
				raw = "https://example.org" + raw
			}
			got, err := Resolve(context.Background(), raw, client)
			if tc.code != "" {
				var problem *InputError
				if !errors.As(err, &problem) || problem.Code != tc.code {
					t.Fatalf("expected %s, got %v", tc.code, err)
				}
			} else if err != nil || got != tc.want {
				t.Fatal(got, err)
			}
			if calls != tc.calls {
				t.Fatal("fetch count", calls, tc.calls)
			}
		})
	}
}

func TestResolveRejectsInvalidFeedsAndCancellation(t *testing.T) {
	for _, raw := range []string{"not a feed", strings.TrimSuffix(feed(event("x", "X")), "END:VCALENDAR\r\n")} {
		c := testFeed(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/calendar")
			fmt.Fprint(w, raw)
		})
		if _, err := Resolve(context.Background(), "https://example.org/feed", c); err == nil || err.Error() != "invalid_feed" {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Resolve(ctx, "https://example.org/feed", PublicClient()); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestSubscribeResolvesBeforeSaving(t *testing.T) {
	db := testenv.Database(t).DB
	s := Store{DB: db}
	ctx := context.Background()
	calls := 0
	c := testFeed(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/calendar")
		fmt.Fprint(w, feed(event("x", "X")))
	})
	id, err := s.Subscribe(ctx, "Synthetic", "blue", "https://calendar.google.com/calendar?cid=a%40b", c)
	if err != nil {
		t.Fatal(err)
	}
	sub, err := s.Get(ctx, id)
	if err != nil || sub.feedURL != "https://calendar.google.com/calendar/ical/a@b/public/basic.ics" {
		t.Fatal("did not store resolved address", err)
	}
	if _, err = s.Subscribe(ctx, "", "blue", "https://example.org", c); !errors.Is(err, ErrInvalid) || calls != 1 {
		t.Fatal("validated after network", err, calls)
	}
	bad := testFeed(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>No calendar</html>")
	})
	if _, err = s.Subscribe(ctx, "Broken", "blue", "https://example.org", bad); err == nil {
		t.Fatal("saved invalid source")
	}
	all, err := s.List(ctx)
	if err != nil || len(all) != 1 {
		t.Fatal("failed resolution saved a row", err)
	}
	if found, err := s.Process(ctx, c); err != nil || !found {
		t.Fatal(found, err)
	}
	sub, err = s.Get(ctx, id)
	if err != nil || sub.LastSuccess == nil {
		t.Fatal("resolved subscription did not refresh", err)
	}
}

func TestPublicCalendarDiscoverySample(t *testing.T) {
	raw := os.Getenv("ALEXANDRIA_PUBLIC_CALENDAR_DISCOVERY")
	if raw == "" {
		t.Skip("public discovery probe not requested")
	}
	if _, err := Resolve(context.Background(), raw, nil); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryRedirects(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		calls := 0
		c := testFeed(t, func(w http.ResponseWriter, r *http.Request) {
			calls++
			switch r.URL.Path {
			case "/start":
				target := "/nested/page"
				if blocked {
					target = "https://127.0.0.1/private.ics"
				}
				http.Redirect(w, r, target, http.StatusFound)
			case "/nested/page":
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<a href="feed.ics">Calendar</a>`)
			case "/nested/feed.ics":
				w.Header().Set("Content-Type", "text/calendar")
				fmt.Fprint(w, feed(event("redirect", "Redirected")))
			default:
				t.Error("unexpected discovery destination")
			}
		})
		c.CheckRedirect = PublicClient().CheckRedirect
		got, err := Resolve(context.Background(), "https://example.org/start", c)
		if blocked {
			if err == nil || err.Error() != "fetch_failed" || calls != 1 {
				t.Fatal("followed private redirect", err, calls)
			}
		} else if err != nil || got != "https://example.org/nested/feed.ics" || calls != 3 {
			t.Fatal("lost redirected base", got, err, calls)
		}
	}
}
