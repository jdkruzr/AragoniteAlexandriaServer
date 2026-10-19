package boox

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

// Opt-in browser fixture: synthetic database and objects are owned by testenv.
// The driver writes stop to release the fixture and perform its normal cleanup.
func TestNotebookBrowserFixture(t *testing.T) {
	dir := os.Getenv("ALEXANDRIA_NOTEBOOK_BROWSER")
	if dir == "" {
		t.Skip("browser driver not requested")
	}
	s, g := managementFixture(t)
	for _, id := range []string{"first", "second", "ghost"} {
		extra := map[string]any{}
		if id == "ghost" {
			extra["status"] = 0
		}
		g.docs[id] = cleanupFixture(t, s, id, "Synthetic notebook "+id, extra)
	}
	g.docs["folder"] = cleanupFixture(t, s, "folder", "Synthetic folder", map[string]any{"type": 0})
	static := web.Handler(web.Deps{})
	var srv *httptest.Server
	srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/static/"):
			static.ServeHTTP(w, r)
		case r.URL.Path == "/__work" && r.Method == "POST":
			if _, e := s.Observe(r.Context()); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			if _, e := s.PublishOne(r.Context()); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			w.WriteHeader(204)
		case r.URL.Path == "/boox":
			s.notebooks(w, r)
		default:
			s.Admin(w, r)
		}
	}))
	srv.Start()
	defer srv.Close()
	s.Config.PublicURL = srv.URL
	if e := os.WriteFile(filepath.Join(dir, "url"), []byte(srv.URL), 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(dir, "stop")); e == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser driver timeout")
}
