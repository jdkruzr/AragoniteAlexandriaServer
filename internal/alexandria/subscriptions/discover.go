package subscriptions

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

// InputError is safe to show to the user; it never contains a feed URL or body.
type InputError struct{ Code string }

func (e *InputError) Error() string { return e.Code }

// googleFeeds only interprets calendar links on Google's exact hosts. Calendar
// IDs are data in a fixed HTTPS path, never destinations supplied by a page.
func googleFeeds(u *url.URL) ([]string, bool) {
	host := strings.ToLower(u.Hostname())
	if (host != "calendar.google.com" && host != "www.google.com") || (u.Path != "/calendar" && !strings.HasPrefix(u.Path, "/calendar/")) || strings.HasPrefix(u.Path, "/calendar/ical/") {
		return nil, false
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, true
	}
	ids := append(append([]string{}, q["src"]...), q["cid"]...)
	var feeds []string
	for _, id := range ids {
		if !strings.Contains(id, "@") {
			for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
				if decoded, err := enc.DecodeString(id); err == nil && strings.Contains(string(decoded), "@") {
					id = string(decoded)
					break
				}
			}
		}
		if len(id) > 1024 || strings.Count(id, "@") != 1 || strings.HasPrefix(id, "@") || strings.HasSuffix(id, "@") || strings.ContainsAny(id, "/\\?") || strings.IndexFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
			return nil, true
		}
		feeds = append(feeds, "https://calendar.google.com/calendar/ical/"+url.PathEscape(id)+"/public/basic.ics")
	}
	return feeds, true
}

func oneFeed(feeds []string) (string, error) {
	unique := map[string]bool{}
	for _, f := range feeds {
		unique[f] = true
	}
	if len(unique) == 0 {
		return "", &InputError{"no_feed"}
	}
	if len(unique) > 1 {
		return "", &InputError{"multiple_feeds"}
	}
	for f := range unique {
		return f, nil
	}
	panic("unreachable")
}

// pageFeed scans one bounded HTML document, without executing scripts or crawling
// other pages. Only Google calendar embeds/links and explicit ICS links count.
func pageFeed(ctx context.Context, raw []byte, base *url.URL) (string, error) {
	z := html.NewTokenizer(bytes.NewReader(raw))
	z.SetMaxBuf(256 << 10)
	var feeds []string
	for {
		if err := ctx.Err(); err != nil {
			return "", &InputError{"fetch_failed"}
		}
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				return "", &InputError{"too_large"}
			}
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		t := z.Token()
		attr := map[string]string{}
		for _, a := range t.Attr {
			attr[a.Key] = a.Val
		}
		var ref string
		switch t.Data {
		case "iframe":
			ref = attr["src"]
		case "a", "link":
			ref = attr["href"]
		default:
			continue
		}
		if ref == "" {
			continue
		}
		u, err := base.Parse(ref)
		if err != nil || ValidateURL(u.String()) != nil {
			continue
		}
		if found, google := googleFeeds(u); google {
			// A malformed Google link is not silently ignored alongside another feed.
			if len(found) == 0 {
				return "", &InputError{"no_feed"}
			}
			feeds = append(feeds, found...)
		} else if t.Data != "iframe" && (strings.HasSuffix(strings.ToLower(u.Path), ".ics") || strings.EqualFold(attr["type"], "text/calendar")) {
			feeds = append(feeds, u.String())
		}
		if len(feeds) > 100 {
			return "", &InputError{"multiple_feeds"}
		}
	}
	return oneFeed(feeds)
}

// Resolve verifies a direct ICS feed, Google share/embed URL, or a webpage with
// a single advertised calendar. It performs at most two bounded fetches under
// one deadline; nil uses the same public-network-only client as refreshes.
func Resolve(ctx context.Context, raw string, client *http.Client) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	raw = strings.TrimSpace(raw)
	if ValidateURL(raw) != nil {
		return "", &InputError{"invalid_url"}
	}
	if client == nil {
		client = PublicClient()
	}
	u, _ := url.Parse(raw)
	candidates, google := googleFeeds(u)
	if google {
		var err error
		raw, err = oneFeed(candidates)
		if err != nil {
			return "", err
		}
	}
	f, code := fetchDocument(ctx, client, Subscription{feedURL: raw}, !google)
	if code != "" {
		return "", &InputError{code}
	}
	if f.html {
		var err error
		raw, err = pageFeed(ctx, f.raw, f.finalURL)
		if err != nil {
			return "", err
		}
		f, code = fetch(ctx, client, Subscription{feedURL: raw})
		if code != "" {
			return "", &InputError{code}
		}
	}
	if _, err := Parse(ctx, f.raw); err != nil {
		if err.Error() == "too_large" {
			return "", &InputError{"too_large"}
		}
		return "", &InputError{"invalid_feed"}
	}
	return raw, nil
}

// Subscribe checks input and resolves the feed outside a database transaction.
// Create remains available to callers staging a known feed for worker validation.
func (s Store) Subscribe(ctx context.Context, name, color, raw string, client *http.Client) (string, error) {
	if !valid(strings.TrimSpace(name), color) {
		return "", ErrInvalid
	}
	resolved, err := Resolve(ctx, raw, client)
	if err != nil {
		return "", err
	}
	if err = ctx.Err(); err != nil {
		return "", err
	}
	return s.Create(ctx, name, color, resolved)
}
