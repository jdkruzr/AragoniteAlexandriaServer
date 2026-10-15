package server

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// unknownRequests logs requests nothing on a listener answered (404, 405,
// 501, and 401 under /api/, where BOOX services refuse unknown anonymous
// calls before routing them), so stray BOOX cloud traffic shows up. It records the method, a masked
// path and whether credentials were attached; never header values, cookies,
// bodies or query strings. Each request shape is logged at most once an hour.
type unknownRequests struct {
	logger *slog.Logger
	mu     sync.Mutex
	seen   map[string]time.Time
	now    func() time.Time
}

const (
	unknownEvery = time.Hour
	unknownKeep  = 512 // distinct shapes remembered; beyond that, new ones are dropped
)

func newUnknownRequests(logger *slog.Logger) *unknownRequests {
	return &unknownRequests{logger: logger, seen: map[string]time.Time{}, now: time.Now}
}

func (u *unknownRequests) note(listener string, r *http.Request, status int) {
	if u == nil || !unanswered(r, status) {
		return
	}
	path := maskPath(r.URL.Path)
	credentials := r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != ""
	key := listener + " " + r.Method + " " + path + " " + http.StatusText(status)
	now := u.now()
	u.mu.Lock()
	last, ok := u.seen[key]
	if ok && now.Sub(last) < unknownEvery {
		u.mu.Unlock()
		return
	}
	if !ok && len(u.seen) >= unknownKeep {
		for k, t := range u.seen {
			if now.Sub(t) >= unknownEvery {
				delete(u.seen, k)
			}
		}
		if len(u.seen) >= unknownKeep {
			u.mu.Unlock()
			return
		}
	}
	u.seen[key] = now
	u.mu.Unlock()
	u.logger.Info("unknown request", "listener", listener, "method", r.Method, "path", path, "status", status, "credentials", credentials)
}

// maskPath keeps a path's shape but not identifiers or tokens in it: long or
// digit-heavy segments become ":id", and the result is capped.
func maskPath(p string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		digits := 0
		for _, c := range s {
			if c >= '0' && c <= '9' {
				digits++
			}
		}
		if len(s) > 24 || (len(s) >= 8 && digits*3 >= len(s)) || strings.ContainsAny(s, "=@%") {
			parts[i] = ":id"
		}
	}
	p = strings.Join(parts, "/")
	if len(p) > 160 {
		p = p[:160] + "…"
	}
	return p
}

func unanswered(r *http.Request, status int) bool {
	switch status {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	case http.StatusUnauthorized:
		return strings.HasPrefix(r.URL.Path, "/api/")
	}
	return false
}
