// Package web is the Alexandria Server's browser UI: notebooks and Books,
// search, tasks, devices, settings and the OAuth consent page. Views follow
// UltraBridge's ForestNote, Books, devices and settings pages (internal/web,
// Apache-2.0) but are rebuilt small: server-rendered HTML, one stylesheet, no
// JavaScript, a strict content security policy, and a same-origin check on
// every state-changing request (UltraBridge's forms had none).
//
// Authentication happens before this handler (account Basic or API bearer);
// every page runs on the request's admitted library connection.
package web

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/oauth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

//go:embed templates/*.html static/*
var assets embed.FS

var funcs = template.FuncMap{
	"tip":     tip,
	"version": func() string { return Version },
	"listJobStates": func() []string {
		return []string{"queued", "processing", "ready", "blocked", "failed", "paused", "cancelled"}
	},
	"ms": func(ms int64) string {
		if ms <= 0 {
			return "—"
		}
		return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
	},
	"time": func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Local().Format("2006-01-02 15:04")
	},
	"bytes": func(n int64) string {
		switch {
		case n >= 1<<20:
			return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
		case n >= 1<<10:
			return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
		}
		return fmt.Sprintf("%d B", n)
	},
	"ago": func(t time.Time) string {
		if t.IsZero() {
			return "never"
		}
		switch d := time.Since(t); {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return plural(int(d/time.Minute), "minute") + " ago"
		case d < 48*time.Hour:
			return plural(int(d/time.Hour), "hour") + " ago"
		default:
			return plural(int(d/(24*time.Hour)), "day") + " ago"
		}
	},
	"count": func(n int) string {
		if n < 0 {
			return "—"
		}
		return fmt.Sprintf("%d", n)
	},
	"deref": func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	},
	// clock formats milliseconds (int64) or seconds (float64) as m:ss or h:mm:ss.
	"clock": func(v any) string {
		var d time.Duration
		switch n := v.(type) {
		case int64:
			d = time.Duration(n) * time.Millisecond
		case float64:
			d = time.Duration(n * float64(time.Second))
		case time.Duration:
			d = n
		}
		s := int(d.Round(time.Second) / time.Second)
		if s >= 3600 {
			return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
		}
		return fmt.Sprintf("%d:%02d", s/60, s%60)
	},
	"utc": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.UTC().Format("2006-01-02 15:04") + " UTC"
	},
}

// Funcs are the layout's template functions. Content templates grafted into
// the layout (BOOX views) parse with these so names resolve identically.
func Funcs() template.FuncMap { return funcs }

var layout = template.Must(template.New("layout.html").Funcs(funcs).ParseFS(assets, "templates/layout.html"))

// Status describes optional, operator-configured features for display.
type Status struct {
	NativeBOOX bool
	OCR        string // recognition model, "" when off
	Embedding  string // embedding model, "" when off
}

type Deps struct {
	LocalPasswords     bool
	RecognitionCatalog recognition.Catalog
	Providers          *providers.Store
	DB                 pg.DB
	Objects            blob.Store
	Search             notes.Searcher
	PublicURL          string
	Status             Status
	Logger             *slog.Logger
	// BOOXSummary reports the native source for the overview; nil without BOOX.
	BOOXSummary func(context.Context) (SourceSummary, error)
}

type page struct {
	SourceTab    string
	ClientSource bool
	NativeBOOX   bool
	Title        string
	Section      string // nav highlight
	Base         string // public origin, for copyable URLs
	Data         any
	Notice       string
}

func (d Deps) render(w http.ResponseWriter, r *http.Request, name, title, section string, data any) {
	t, err := layout.Clone()
	if err == nil {
		_, err = t.ParseFS(assets, "templates/"+name+".html")
	}
	d.renderTemplate(w, r, t, err, title, section, data)
}

// RenderContent gives feature-owned views the same site shell as the core pages.
func (d Deps) RenderContent(w http.ResponseWriter, r *http.Request, content *template.Template, title, section string, data any) {
	t, err := layout.Clone()
	if err == nil {
		_, err = t.AddParseTree("content", content.Lookup("content").Tree)
	}
	d.renderTemplate(w, r, t, err, title, section, data)
}

func (d Deps) renderTemplate(w http.ResponseWriter, r *http.Request, t *template.Template, err error, title, section string, data any) {
	var buf bytes.Buffer
	if err == nil {
		err = t.ExecuteTemplate(&buf, "layout.html", page{SourceTab: sourceTab(r.URL.Path, section), ClientSource: section == "notebooks" || section == "books" || section == "devices" || section == "client-settings", NativeBOOX: d.Status.NativeBOOX, Title: title, Section: section, Base: oauth.BaseURL(d.PublicURL, r), Data: data,
			Notice: r.URL.Query().Get("notice")})
	}
	if err != nil {
		d.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

func (d Deps) fail(w http.ResponseWriter, err error) {
	if d.Logger != nil {
		d.Logger.Error("web page failed", "error", err.Error())
	}
	http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
}

// sameOrigin rejects cross-site state changes. Browsers send Origin (and
// Sec-Fetch-Site) on every POST, so a missing pair is refused as well.
func sameOrigin(public string, r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	origin := r.Header.Get("Origin")
	return origin != "" && origin == oauth.BaseURL(public, r)
}

// back redirects to a same-site path after a form post.
func back(w http.ResponseWriter, r *http.Request, path, notice string) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		path = "/"
	}
	if notice != "" {
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + "notice=" + url.QueryEscape(notice)
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// Handler serves the UI for one admitted, authenticated request.
func Handler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", d.overview)
	mux.Handle("GET /static/", http.FileServerFS(assets))
	mux.HandleFunc("GET /help", func(w http.ResponseWriter, r *http.Request) {
		d.render(w, r, "help", "Help", "help", map[string]any{"NativeBOOX": d.Status.NativeBOOX})
	})
	mux.HandleFunc("GET /sources", func(w http.ResponseWriter, r *http.Request) { d.render(w, r, "sources", "Add source", "sources", nil) })
	mux.HandleFunc("GET /sources/client", func(w http.ResponseWriter, r *http.Request) {
		d.render(w, r, "client-settings", "Alexandria Client settings", "client-settings", nil)
	})
	d.notebookRoutes(mux)
	d.bookRoutes(mux)
	d.searchRoutes(mux)
	d.taskRoutes(mux)
	d.deviceRoutes(mux)
	d.settingsRoutes(mux)
	d.providerRoutes(mux)
	d.jobRoutes(mux)
	d.authorizeRoutes(mux)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; frame-ancestors 'none'; form-action 'self' https: http://localhost:* http://127.0.0.1:*")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(d.PublicURL, r) {
			http.Error(w, "This form must be submitted from the Alexandria web page.", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// Owns reports whether the web UI serves a path (the API, sync, CalDAV and
// MCP keep their own routes).
func Owns(path string) bool {
	for _, prefix := range []string{"/sources", "/files/", "/search", "/tasks", "/settings", "/static/", "/authorize", "/help", "/jobs"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return path == "/"
}

func sourceTab(path, section string) string {
	switch section {
	case "settings":
		switch path {
		case "/settings/search", "/settings/recognition", "/settings/integrations":
			return strings.TrimPrefix(path, "/settings/")
		}
		return "account"
	case "boox":
		switch path {
		case "/boox/reading", "/boox/books/upload", "/boox/reading/refresh":
			return "reading"
		case "/boox/devices", "/boox/enroll":
			return "devices"
		case "/boox/activity", "/boox/settings":
			return "settings"
		}
		return "notebooks"
	}
	return section
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
