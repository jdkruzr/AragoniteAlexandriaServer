package server

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
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

func TestRuntimeGatewayPreservesWebSocketUpgrade(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = rw.Flush()
	}))
	defer upstream.Close()
	target, _ := url.Parse(upstream.URL)
	g := NewRuntimeGateway(nil, ":0", ":0", time.Second, httputil.NewSingleHostReverseProxy(target), slog.Default())
	edge := httptest.NewServer(g.main.Handler)
	defer edge.Close()
	address, _ := url.Parse(edge.URL)
	conn, err := net.DialTimeout("tcp", address.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprintf(conn, "GET /boox-neocloud/_blipsync HTTP/1.1\r\nHost: native.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 {
		t.Fatalf("upgrade status %d", response.StatusCode)
	}
}
