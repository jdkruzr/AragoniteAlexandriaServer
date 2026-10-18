package web

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func legacySources(d Deps, w http.ResponseWriter, r *http.Request) {
	base, err := layout.Clone()
	if err == nil {
		_, err = base.ParseFS(assets, "templates/sources.html")
	}
	d.renderTemplate(w, r, base, err, "<unsafe>", "sources", nil)
}
func TestCachedTemplatesPreserveLocalizedOutput(t *testing.T) {
	for i := 0; i < 24; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			lang := "en"
			if i%2 != 0 {
				lang = "es"
			}
			d := Deps{PublicURL: fmt.Sprintf("https://library-%d.example.test", i), Status: Status{NativeBOOX: i%3 == 0}}
			r := httptest.NewRequest("GET", "/sources", nil)
			r.Header.Set("Accept-Language", lang)
			expected, actual := httptest.NewRecorder(), httptest.NewRecorder()
			expected.Header().Set("Refresh", "7")
			actual.Header().Set("Refresh", "7")
			legacySources(d, expected, r)
			d.render(actual, r, "sources", "<unsafe>", "sources", nil)
			if actual.Code != 200 || actual.Body.String() != expected.Body.String() || actual.Header().Get("Content-Language") != lang {
				t.Fatal("localized render changed", actual.Code)
			}
			if strings.Contains(actual.Body.String(), "<unsafe>") {
				t.Fatal("unescaped title")
			}
		})
	}
}
func BenchmarkPageRendering(b *testing.B) {
	d := Deps{}
	r := httptest.NewRequest("GET", "/sources", nil)
	for _, cached := range []bool{false, true} {
		name := "parse-each-request"
		if cached {
			name = "cached-base"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				w := httptest.NewRecorder()
				if cached {
					d.render(w, r, "sources", "<unsafe>", "sources", nil)
				} else {
					legacySources(d, w, r)
				}
				if w.Code != 200 {
					b.Fatal(w.Code)
				}
			}
		})
	}
}

// Keep the bases unexecuted: html/template refuses cloning executed templates.
func TestAllCachedBasesRemainCloneable(t *testing.T) {
	for name, base := range contentTemplates {
		var clone *template.Template
		clone, err := base.Clone()
		if err != nil || clone.Lookup("content") == nil {
			t.Fatal(name, err)
		}
	}
}
