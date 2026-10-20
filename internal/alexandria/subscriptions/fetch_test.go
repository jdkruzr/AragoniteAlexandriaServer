package subscriptions

import (
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestFetchFailuresAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		status     int
		typ, body  string
		gzip       bool
	}{
		{"http", "http_error", 503, "text/calendar", "", false},
		{"unsolicited304", "http_error", 304, "", "", false},
		{"html", "invalid_feed", 200, "text/html", "<html>not a calendar</html>", false},
		{"size", "too_large", 200, "text/calendar", strings.Repeat("x", MaxFeedBytes+1), false},
		{"gzip size", "too_large", 200, "text/calendar", strings.Repeat("x", MaxFeedBytes+1), true},
		{"valid gzip", "", 200, "text/calendar", feed(event("x", "x")), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testFeed(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.typ)
				if tc.gzip {
					w.Header().Set("Content-Encoding", "gzip")
				}
				w.WriteHeader(tc.status)
				if tc.gzip {
					z := gzip.NewWriter(w)
					fmt.Fprint(z, tc.body)
					z.Close()
				} else {
					fmt.Fprint(w, tc.body)
				}
			})
			f, code := fetch(context.Background(), c, Subscription{feedURL: "https://calendar.example/feed"})
			if code != tc.want {
				t.Fatal(code, tc.want)
			}
			if code == "" && string(f.raw) != tc.body {
				t.Fatal("decompression altered feed")
			}
		})
	}
	c := testFeed(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, code := fetch(ctx, c, Subscription{feedURL: "https://calendar.example/slow"}); code != "fetch_failed" {
		t.Fatal("ignored deadline", code)
	}
}

func TestFeedLimitsAndEmptyValidation(t *testing.T) {
	for name, raw := range map[string]string{
		"many series":             feed(strings.Repeat(event("unique", "x"), MaxSeries+1)),
		"nested":                  feed(strings.Repeat("begin:valarm\n", 9) + strings.Repeat("end:valarm\n", 9)),
		"folded nested":           feed(strings.Repeat("BE\n GIN:VALARM\n", 9) + strings.Repeat("END:VALARM\n", 9)),
		"empty missing product":   "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nEND:VCALENDAR\r\n",
		"empty duplicate version": feed("VERSION:2.0\n"),
		"nul":                     feed(event("x", "bad\x00title")),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(context.Background(), []byte(raw)); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
		})
	}
	var b strings.Builder
	for i := 0; i <= MaxSeries; i++ {
		b.WriteString(event(fmt.Sprint(i), "bounded"))
	}
	if _, err := Parse(context.Background(), []byte(feed(b.String()))); err == nil || err.Error() != "too_large" {
		t.Fatal("series bound", err)
	}
}
