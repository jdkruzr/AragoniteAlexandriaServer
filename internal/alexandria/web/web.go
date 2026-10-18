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
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/diagnostics"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/oauth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

//go:embed templates/*.html static/*
var assets embed.FS

// funcs are declared with the English number/time formats merged in, so the
// layout (parsed during package initialization) sees every name.
var funcs = withFormats(template.FuncMap{
	"tip": tip,
	// t translates owner-facing text; renderTemplate binds it to the
	// request's language. This English default lets every template parse.
	"t": i18n.English.T,
	// th translates a message that carries its own inline markup (links,
	// emphasis, code) so a sentence isn't split around it. Catalogs are
	// reviewed files in the repository, so their markup is trusted.
	"th": htmlT(i18n.English),
	// td shows a stored job or page detail in the reader's language when it
	// is a known sentence (see details.go); other text is shown as stored.
	"td": detailIn(i18n.English),
	// languages lists the available languages by their own names.
	"languages": i18n.Choices,
	"version":   func() string { return Version },
	"listJobStates": func() []string {
		return []string{"queued", "processing", "ready", "blocked", "failed", "paused", "cancelled", "skipped"}
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
	// num groups thousands: 1624 → "1,624".
	// until is a rounded time left before t, such as "2 h 5 min".
	// taskRow pairs a task with the Tasks view its row links back to.
	"taskRow": func(t tasksvc.Task, v taskView, sources ...map[string]*NoteSource) any {
		var src *NoteSource
		if len(sources) == 1 {
			src = sources[0][t.ID]
		}
		return struct {
			Task   tasksvc.Task
			View   taskView
			Source *NoteSource
		}{t, v, src}
	},
	// eventRow pairs a Calendar row with whether it is in the trash.
	"eventRow": func(r calendarRow, trash bool) any {
		return struct {
			Row   calendarRow
			Trash bool
		}{r, trash}
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
})

func withFormats(f template.FuncMap) template.FuncMap {
	for k, v := range formatFuncs(i18n.English) {
		f[k] = v
	}
	return f
}

// Funcs are the layout's template functions. Content templates grafted into
// the layout (BOOX views) parse with these so names resolve identically.
func Funcs() template.FuncMap { return funcs }

var layout = template.Must(template.New("layout.html").Funcs(funcs).ParseFS(assets, "templates/layout.html"))

// Bases are immutable and never executed; each render clones before binding
// request-specific localization and performing html/template escaping.
var contentTemplates = func() map[string]*template.Template {
	names, err := fs.Glob(assets, "templates/*.html")
	if err != nil {
		panic(err)
	}
	out := make(map[string]*template.Template, len(names))
	for _, name := range names {
		if path.Base(name) == "layout.html" {
			continue
		}
		base := template.Must(layout.Clone())
		out[strings.TrimSuffix(path.Base(name), ".html")] = template.Must(base.ParseFS(assets, name))
	}
	return out
}()

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
	// BOOXWork lists note actions and minutes on the Jobs page; nil without BOOX.
	BOOXWork BOOXWorkFunc
}

type page struct {
	Lang         string // HTML lang attribute
	SourceTab    string
	ClientSource bool
	NativeBOOX   bool
	Title        string
	Section      string // nav highlight
	Base         string // public origin, for copyable URLs
	Data         any
	Notice       string
	// Live is the update interval in seconds for a page with ongoing work.
	// live.js refreshes its data-live-region elements in place; without
	// JavaScript a <noscript> meta refresh reloads the whole page.
	Live int
}

func (d Deps) render(w http.ResponseWriter, r *http.Request, name, title, section string, data any) {
	base, ok := contentTemplates[name]
	if !ok {
		d.fail(w, fmt.Errorf("unknown content template"))
		return
	}
	t, err := base.Clone()
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
		// Handlers ask for updates with a Refresh header; turn it into live
		// regions so browsers don't reload the whole page.
		live, _ := strconv.Atoi(w.Header().Get("Refresh"))
		w.Header().Del("Refresh")
		loc := d.Localizer(r)
		t.Funcs(template.FuncMap{"t": loc.T, "th": htmlT(loc), "td": detailIn(loc), "tip": tipIn(loc)}).Funcs(formatFuncs(loc))
		w.Header().Set("Content-Language", loc.Lang())
		w.Header().Add("Vary", "Accept-Language")
		err = t.ExecuteTemplate(&buf, "layout.html", page{Lang: loc.Lang(), Live: live, SourceTab: sourceTab(r.URL.Path, section), ClientSource: section == "notebooks" || section == "books" || section == "devices" || section == "client-settings", NativeBOOX: d.Status.NativeBOOX, Title: pageTitle(loc, title), Section: section, Base: oauth.BaseURL(d.PublicURL, r), Data: data,
			Notice: notice(loc, r)})
	}
	if err != nil {
		d.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// htmlT renders a message whose catalog text carries trusted inline markup.
// Values inserted into it are not trusted: strings are HTML-escaped (a
// notebook title or client name can't add markup); template.HTML values are
// already safe and numbers are formatted by T.
func htmlT(l i18n.Localizer) func(string, ...any) template.HTML {
	return func(id string, args ...any) template.HTML {
		safe := make([]any, len(args))
		for i, a := range args {
			switch v := a.(type) {
			case template.HTML:
				safe[i] = string(v)
			case string:
				if len(args) > 1 && i%2 == 0 {
					safe[i] = v // a name in a name/value pair
				} else {
					safe[i] = template.HTMLEscapeString(v)
				}
			default:
				safe[i] = a
			}
		}
		return template.HTML(l.T(id, safe...))
	}
}

// LanguageCookie holds the language chosen in Settings, per browser.
const LanguageCookie = "alexandria_lang"

// Localizer picks the language for owner-facing text: the one chosen in
// Settings, then the browser's preference, then English.
func (d Deps) Localizer(r *http.Request) i18n.Localizer { return Localizer(r) }

// Localizer is Deps.Localizer for code that renders outside a Deps.
func Localizer(r *http.Request) i18n.Localizer {
	var chosen string
	if c, err := r.Cookie(LanguageCookie); err == nil {
		chosen = c.Value
	}
	return i18n.For(chosen, r.Header.Get("Accept-Language"))
}

// T translates one message for the request's language.
func T(r *http.Request, id string, args ...any) string { return Localizer(r).T(id, args...) }

// verbatim marks a page title that is the owner's own content (a notebook or
// book name), so it is shown as written rather than looked up as a message.
const verbatim = "\ue000"

// Verbatim returns a page title that is never translated.
func Verbatim(title string) string { return verbatim + title }

func pageTitle(l i18n.Localizer, title string) string {
	if strings.HasPrefix(title, verbatim) {
		return strings.TrimPrefix(title, verbatim)
	}
	return l.T(title)
}

// notice translates a redirect notice; notice_n carries its count, so the
// message can pluralize in any language.
func notice(l i18n.Localizer, r *http.Request) string {
	q := r.URL.Query()
	n := q.Get("notice")
	if c, err := strconv.Atoi(q.Get("notice_n")); err == nil {
		return l.T(n, c)
	}
	return l.T(n)
}

func (d Deps) fail(w http.ResponseWriter, err error) {
	if d.Logger != nil {
		d.Logger.Error("web page failed", "code", diagnostics.Code(err))
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

// back redirects to a same-site path after a form post. notice is English
// message text (see i18n.M); an optional count is its plural count.
func back(w http.ResponseWriter, r *http.Request, path, notice string, count ...int) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		path = "/"
	}
	if notice != "" {
		// The query goes before any #fragment, or the notice is lost in it.
		var frag string
		path, frag, _ = strings.Cut(path, "#")
		sep := "?"
		if strings.Contains(path, "?") {
			sep = "&"
		}
		path += sep + "notice=" + url.QueryEscape(notice)
		if len(count) == 1 {
			path += "&notice_n=" + strconv.Itoa(count[0])
		}
		if frag != "" {
			path += "#" + frag
		}
	}
	http.Redirect(w, r, path, http.StatusSeeOther)
}

// Handler serves the UI for one admitted, authenticated request.
func Handler(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /library", d.overview)
	mux.Handle("GET /static/", http.FileServerFS(assets))
	mux.HandleFunc("GET /help", func(w http.ResponseWriter, r *http.Request) {
		d.render(w, r, "help", "Help", "help", map[string]any{"NativeBOOX": d.Status.NativeBOOX})
	})
	mux.HandleFunc("GET /sources", func(w http.ResponseWriter, r *http.Request) { d.render(w, r, "sources", "Add Source", "sources", nil) })
	mux.HandleFunc("GET /sources/client", func(w http.ResponseWriter, r *http.Request) {
		d.render(w, r, "client-settings", "Alexandria Client Settings", "client-settings", nil)
	})
	d.notebookRoutes(mux)
	d.bookRoutes(mux)
	d.searchRoutes(mux)
	d.taskRoutes(mux)
	d.calendarRoutes(mux)
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
	for _, prefix := range []string{"/sources", "/files/", "/search", "/tasks", "/calendar", "/settings", "/static/", "/authorize", "/help", "/jobs"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return path == "/" || path == "/library"
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
		case "/boox/tags":
			return "tags"
		case "/boox/devices", "/boox/enroll":
			return "devices"
		case "/boox/activity", "/boox/settings":
			return "settings"
		}
		return "notebooks"
	}
	return section
}
