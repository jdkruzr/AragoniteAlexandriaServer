package boox

import (
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"
)

// BOOX pages share the site shell, so they allow its fonts and scripts from
// this server; inline styles stay limited to the notebook's typed text.
func TestBOOXPagesAllowTheShellOnly(t *testing.T) {
	s := Service{Config: Config{PublicURL: "https://boox.example"}}
	content := template.Must(template.New("synthetic").Parse(`{{define "content"}}<p>Synthetic page</p>{{end}}`))
	for path, inline := range map[string]bool{"/boox/tags": false, "/boox/cleanup": false, "/boox/books/upload": false, "/boox/notebook": true} {
		w := httptest.NewRecorder()
		s.renderPage(w, httptest.NewRequest("GET", path, nil), content, "Synthetic", nil)
		csp := w.Header().Get("Content-Security-Policy")
		for _, want := range []string{"default-src 'none'", "font-src 'self'", "script-src 'self'", "connect-src 'self'", "style-src 'self'"} {
			if !strings.Contains(csp, want) {
				t.Errorf("%s: CSP lacks %q: %s", path, want, csp)
			}
		}
		if strings.Contains(csp, "style-src-attr") != inline {
			t.Errorf("%s: inline styles allowed=%v: %s", path, !inline, csp)
		}
		if body := w.Body.String(); w.Code != 200 || !strings.Contains(body, "Synthetic page") || !strings.Contains(body, `class="source-switch"`) {
			t.Errorf("%s: %d, shell or source switch missing", path, w.Code)
		}
	}
}
