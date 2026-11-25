package subscriptions

import (
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"time"
)

func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || (u.Port() != "" && u.Port() != "443") {
		return errors.New("invalid_url")
	}
	if a, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(a) {
		return errors.New("invalid_url")
	}
	return nil
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001::/23"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
}

func publicIP(a netip.Addr) bool {
	a = a.Unmap()
	if a.Is6() && !netip.MustParsePrefix("2000::/3").Contains(a) {
		return false
	}
	if !a.IsValid() || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range blockedPrefixes {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// PublicClient resolves and validates at each connection, then dials the checked
// numeric address: DNS rebinding and redirects cannot bypass destination checks.
// Proxy environment variables are deliberately not inherited for untrusted URLs.
func PublicClient() *http.Client { return publicClient(25 * time.Second) }

// DownloadClient is PublicClient with time for a file rather than a feed.
func DownloadClient() *http.Client { return publicClient(3 * time.Minute) }

func publicClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 64 << 10, DisableKeepAlives: true}
	tr.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("fetch_failed")
		}
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil || len(ips) == 0 {
			return nil, errors.New("fetch_failed")
		}
		for _, ip := range ips {
			if !publicIP(ip) {
				return nil, errors.New("blocked_address")
			}
		}
		d := net.Dialer{Timeout: 8 * time.Second}
		for _, ip := range ips {
			c, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return c, nil
			}
		}
		return nil, errors.New("fetch_failed")
	}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 4 {
			return errors.New("fetch_failed")
		}
		// Validators belong to the original representation, not another origin.
		if len(via) > 0 && r.URL.Host != via[0].URL.Host {
			r.Header.Del("If-None-Match")
			r.Header.Del("If-Modified-Since")
		}
		return ValidateURL(r.URL.String())
	}}
}

type fetched struct {
	raw            []byte
	etag, modified string
	unchanged      bool
	html           bool
	finalURL       *url.URL
}

func fetch(ctx context.Context, client *http.Client, s Subscription) (fetched, string) {
	return fetchDocument(ctx, client, s, false)
}

func fetchDocument(ctx context.Context, client *http.Client, s Subscription, allowHTML bool) (fetched, string) {
	var f fetched
	if ValidateURL(s.feedURL) != nil {
		return f, "invalid_url"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", s.feedURL, nil)
	if err != nil {
		return f, "invalid_url"
	}
	req.Header.Set("Accept", "text/calendar, application/ics;q=0.9, text/plain;q=0.5")
	if allowHTML {
		req.Header.Set("Accept", "text/calendar, text/html;q=0.8, text/plain;q=0.5")
	}
	req.Header.Set("User-Agent", "Alexandria-Calendar/1.0")
	if s.LastSuccess != nil {
		if s.etag != "" {
			req.Header.Set("If-None-Match", s.etag)
		}
		if s.modified != "" {
			req.Header.Set("If-Modified-Since", s.modified)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return f, "fetch_failed"
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && s.LastSuccess != nil {
		f.unchanged = true
		return f, ""
	}
	if resp.StatusCode != http.StatusOK {
		return f, httpCode(resp.StatusCode)
	}
	typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch strings.ToLower(typ) {
	case "text/html", "application/xhtml+xml":
		if !allowHTML {
			return f, "html_page"
		}
		f.html = true
	case "text/calendar", "application/ics", "application/icalendar", "text/plain", "application/octet-stream", "":
	default:
		return f, "invalid_feed"
	}
	f.finalURL = resp.Request.URL
	f.raw, err = io.ReadAll(io.LimitReader(resp.Body, MaxFeedBytes+1))
	if err != nil {
		return f, "fetch_failed"
	}
	if len(f.raw) > MaxFeedBytes {
		return f, "too_large"
	}
	f.etag, f.modified = resp.Header.Get("ETag"), resp.Header.Get("Last-Modified")
	if len(f.etag) > 1024 || len(f.modified) > 1024 {
		return fetched{}, "invalid_feed"
	}
	return f, ""
}

// File is a downloaded file: its bytes and the name its address gave it
// (from Content-Disposition, else the last part of the final address).
type File struct {
	Raw  []byte
	Name string
}

// Download fetches a public https file of at most max bytes, whatever its
// type. Failures are the fetch codes: invalid_url, fetch_failed, http_<status>
// or too_large.
func Download(ctx context.Context, client *http.Client, raw string, max int64) (File, string) {
	if ValidateURL(raw) != nil {
		return File{}, "invalid_url"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return File{}, "invalid_url"
	}
	req.Header.Set("User-Agent", "Alexandria/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return File{}, "fetch_failed"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return File{}, httpCode(resp.StatusCode)
	}
	if resp.ContentLength > max {
		return File{}, "too_large"
	}
	var f File
	if f.Raw, err = io.ReadAll(io.LimitReader(resp.Body, max+1)); err != nil {
		return File{}, "fetch_failed"
	}
	if int64(len(f.Raw)) > max {
		return File{}, "too_large"
	}
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		f.Name = path.Base(strings.ReplaceAll(params["filename"], "\\", "/"))
	}
	if f.Name == "" || f.Name == "." || f.Name == "/" {
		f.Name = path.Base(resp.Request.URL.Path)
	}
	if f.Name == "." || f.Name == "/" {
		f.Name = ""
	}
	return f, ""
}
