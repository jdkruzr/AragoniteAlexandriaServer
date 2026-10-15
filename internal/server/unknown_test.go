package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Unanswered requests are logged once per shape, without query strings, header
// values or identifiers in the path.
func TestUnknownRequestsAreLoggedOnceWithoutSecrets(t *testing.T) {
	var out bytes.Buffer
	runtime := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/known" {
			w.WriteHeader(200)
			return
		}
		http.NotFound(w, r)
	})
	g := NewRuntimeGateway(nil, ":0", ":0", time.Second, runtime, slog.New(slog.NewTextHandler(&out, nil)))
	send := func(h http.Handler, path string, auth bool) {
		r := httptest.NewRequest("GET", path, nil)
		if auth {
			r.Header.Set("Authorization", "Bearer synthetic-secret-token")
			r.Header.Set("Cookie", "session=synthetic-cookie")
		}
		h.ServeHTTP(httptest.NewRecorder(), r)
	}
	send(g.main.Handler, "/known", true)
	send(g.main.Handler, "/api/1/writeTemplates?model=Go103&lang=en", false)
	send(g.main.Handler, "/api/1/writeTemplates?model=Palma2", false)
	send(g.main.Handler, "/api/v1/firmware/update/5f2b9c0e8d7a4b1c9e3f6a2d1b0c7e8f", true)
	send(g.spc.Handler, "/anything", false)
	log := out.String()
	for _, secret := range []string{"synthetic-secret-token", "synthetic-cookie", "Go103", "Palma2", "5f2b9c0e8d7a4b1c9e3f6a2d1b0c7e8f", "/known"} {
		if strings.Contains(log, secret) {
			t.Errorf("log contains %q:\n%s", secret, log)
		}
	}
	if n := strings.Count(log, "/api/1/writeTemplates"); n != 1 {
		t.Errorf("writeTemplates logged %d times:\n%s", n, log)
	}
	for _, want := range []string{"path=/api/v1/firmware/update/:id status=404 credentials=true", "listener=spc method=GET path=/anything status=501 credentials=false"} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
}

func TestMaskPath(t *testing.T) {
	for in, want := range map[string]string{
		"/api/v1/users/syncToken":         "/api/v1/users/syncToken",
		"/api/1/writeTemplates":           "/api/1/writeTemplates",
		"/api/v1/devices/20261015123456":  "/api/v1/devices/:id",
		"/api/v1/users/someone@email.com": "/api/v1/users/:id",
	} {
		if got := maskPath(in); got != want {
			t.Errorf("maskPath(%q) = %q, want %q", in, got, want)
		}
	}
}
