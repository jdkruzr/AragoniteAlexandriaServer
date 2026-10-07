package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewGatewayRoutePatternsDoNotConflict(t *testing.T) {
	t.Parallel()

	// Go's ServeMux checks pattern conflicts during registration, so merely
	// constructing the gateway is the regression test for the root/API overlap.
	_ = NewGateway(nil, ":0", ":0", time.Second, nil, slog.Default())
}

// The runtime gateway must hand every library path to the runtime, not just
// /api/: device sync, CalDAV, MCP, OAuth and the web UI live outside it.
func TestRuntimeGatewayRoutesEveryLibraryPathToTheRuntime(t *testing.T) {
	t.Parallel()
	runtime := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) })
	g := NewRuntimeGateway(nil, ":0", ":0", time.Second, runtime, slog.Default())
	for _, path := range []string{"/", "/sync/capabilities", "/sync/v1", "/caldav/user/calendars/tasks/", "/mcp",
		"/.well-known/oauth-authorization-server", "/files/forestnote", "/api/v1/search", "/reader/search"} {
		w := httptest.NewRecorder()
		g.main.Handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 299 {
			t.Errorf("%s: %d, not routed to the runtime", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	g.main.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/livez", nil))
	if w.Code != 200 {
		t.Errorf("/livez: %d", w.Code)
	}
}
