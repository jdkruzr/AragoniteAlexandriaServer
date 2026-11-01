package boox

import (
	"context"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	"html/template"
	"net/http"
	"strings"
)

// Views are grafted into the shared layout, so they may only use its functions.
func (s Service) renderPage(w http.ResponseWriter, r *http.Request, content *template.Template, title string, data any) {
	s.renderIn(w, r, content, title, "boox", data)
}

func (s Service) renderIn(w http.ResponseWriter, r *http.Request, content *template.Template, title, section string, data any) {
	w.Header().Set("Cache-Control", "no-store")
	// The shared shell's fonts and scripts (live updates, settings, the book
	// importer) come from this server; nothing else is allowed.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'self'; font-src 'self'; script-src 'self'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	if r.URL.Path == "/boox/notebook" {
		// Typed text sizes and colours (booxpage.RichHTML generates them from
		// validated numbers; no resource markup reaches the page).
		w.Header().Set("Content-Security-Policy", w.Header().Get("Content-Security-Policy")+"; style-src-attr 'unsafe-inline'")
	}
	web.Deps{PublicURL: s.Config.PublicURL, Status: web.Status{NativeBOOX: true}, DB: s.DB, Logger: s.Logger}.RenderContent(w, r, content, title, section, data)
}

// renderPageStatus renders with a non-200 status. Writing the status first
// would send it before renderPage sets the page's own headers (its CSP among
// them), leaving the page unstyled.
func (s Service) renderPageStatus(w http.ResponseWriter, r *http.Request, status int, content *template.Template, title string, data any) {
	s.renderPage(&statusWriter{ResponseWriter: w, status: status}, r, content, title, data)
}

type statusWriter struct {
	http.ResponseWriter
	status int
	sent   bool
}

func (w *statusWriter) WriteHeader(int) {
	if !w.sent {
		w.sent = true
		w.ResponseWriter.WriteHeader(w.status)
	}
}
func (w *statusWriter) Write(b []byte) (int, error) {
	w.WriteHeader(w.status)
	return w.ResponseWriter.Write(b)
}

func (s Service) browser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	if r.Method == "GET" {
		switch r.URL.Path {
		case "/boox/notebook":
			s.notebook(w, r)
			return
		case "/boox/page.png":
			s.pageImage(w, r)
			return
		case "/boox/thumb.png":
			s.thumbImage(w, r)
			return
		case "/boox/resource":
			s.typedResource(w, r)
			return
		case "/boox/settings":
			settingsRedirect(w, r)
			return
		case "/boox/reading":
			s.reading(w, r)
			return
		case "/boox/books/cover":
			s.readingCover(w, r)
			return
		case "/boox/books/download":
			s.bookDownload(w, r)
			return
		case "/boox/notebook.pdf":
			s.notebookPDF(w, r)
			return
		case "/boox/books/annotated.pdf":
			s.bookAnnotated(w, r)
			return
		case "/boox/books/trash":
			s.bookTrash(w, r)
			return
		case "/boox/activity":
			s.browse(w, r)
			return
		case "/boox/tags":
			s.tagsPage(w, r)
			return
		case "/boox/minutes":
			s.minutesPage(w, r)
			return
		case "/boox/minutes/new":
			s.minutesRequestPage(w, r)
			return
		case "/boox/minutes/audio":
			s.minutesAudio(w, r)
			return
		}
	}
	if r.Method == "POST" {
		switch r.URL.Path {
		case "/boox/minutes/new":
			s.minutesRequestHTTP(w, r)
			return
		case "/boox/minutes/control":
			s.minutesControlHTTP(w, r)
			return
		case "/boox/minutes/send":
			s.minutesSendHTTP(w, r)
			return
		}
	}
	switch r.URL.Path {
	case "/boox/manage":
		s.notebookManagementHTTP(w, r)
	case "/boox/cleanup":
		s.cleanupHTTP(w, r)
	case "/boox/audio":
		s.audioRecoveryHTTP(w, r)
	case "/boox/reading/refresh":
		s.readerRefreshHTTP(w, r)
	case "/boox/books/imports", "/boox/books/imports/preview", "/boox/books/imports/file", "/boox/books/imports/retry":
		s.bookImportHTTP(w, r)
	case "/boox/books/upload":
		s.uploadBookHTTP(w, r)
	case "/boox/books/manage":
		s.bookManageHTTP(w, r)
	case "/boox/text-notes/new":
		s.textNoteHTTP(w, r)
	case "/boox/recognize":
		s.queuePageHTTP(w, r)
	case "/boox/actions":
		s.findActionsHTTP(w, r)
	case "/boox/enroll":
		s.pairing(w, r)
	case "/boox/devices":
		if r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		settingsRedirect(w, r)
	case "/boox/revoke":
		if r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil {
			http.Error(w, "invalid device", 400)
			return
		}
		id := r.Form.Get("deviceId")
		if _, e := uuid.Parse(id); e != nil {
			http.Error(w, "invalid device", 400)
			return
		}
		if e := s.revoke(r.Context(), id); e != nil {
			s.failure(w, "browser", e)
			return
		}
		http.Redirect(w, r, revokedNotice(), 303)
	default:
		http.NotFound(w, r)
	}
}
func (s Service) revoke(ctx context.Context, id string) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE boox_device SET revoked=true WHERE id=$1`, id)
	if e != nil {
		return e
	}
	principal := "ps_" + strings.ReplaceAll(id, "-", "")
	_ = s.Config.gateway(ctx, "PUT", "/_user/"+principal, map[string]any{"name": principal, "disabled": true}, nil)
	return nil
}
