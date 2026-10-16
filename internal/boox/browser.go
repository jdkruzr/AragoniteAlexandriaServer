package boox

import (
	"context"
	"database/sql"
	"errors"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	"html/template"
	"net/http"
	"strings"
)

var setupTemplate = template.Must(template.New("setup.html").Funcs(web.Funcs()).ParseFS(templateFS, "templates/setup.html"))

// Views are grafted into the shared layout, so they may only use its functions.
var devicesTemplate = template.Must(template.New("devices.html").Funcs(web.Funcs()).ParseFS(templateFS, "templates/devices.html"))

func (s Service) renderPage(w http.ResponseWriter, r *http.Request, content *template.Template, title string, data any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; media-src 'self'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	web.Deps{PublicURL: s.Config.PublicURL, Status: web.Status{NativeBOOX: true}}.RenderContent(w, r, content, title, "boox", data)
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
		case "/boox/settings":
			s.renderPage(w, r, booxSettings, "BOOX Settings", nil)
			return
		case "/boox/reading":
			s.reading(w, r)
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
	case "/boox/cleanup":
		s.cleanupHTTP(w, r)
	case "/boox/audio":
		s.audioRecoveryHTTP(w, r)
	case "/boox/reading/refresh":
		s.readerRefreshHTTP(w, r)
	case "/boox/books/upload":
		s.uploadBookHTTP(w, r)
	case "/boox/text-notes/new":
		s.textNoteHTTP(w, r)
	case "/boox/recognize":
		s.queuePageHTTP(w, r)
	case "/boox/actions":
		s.findActionsHTTP(w, r)
	case "/boox/enroll":
		if r.Method == "POST" {
			code, e := s.IssueCode(r.Context())
			if e != nil {
				failure(w, e)
				return
			}
			s.renderPage(w, r, setupTemplate, "Connect a BOOX Device", code)
		} else if r.Method == "GET" {
			s.renderPage(w, r, setupTemplate, "Connect a BOOX Device", "")
		} else {
			http.NotFound(w, r)
		}
	case "/boox/devices":
		if r.Method != "GET" {
			http.NotFound(w, r)
			return
		}
		rows, e := s.DB.QueryContext(r.Context(), `SELECT id::text,model,revoked,(SELECT max(v.created_at) FROM boox_blob_version v WHERE v.device_id=d.id) FROM boox_device d ORDER BY revoked,created_at`)
		if e != nil {
			failure(w, e)
			return
		}
		defer rows.Close()
		type item struct {
			ID, Model  string
			Revoked    bool
			LastUpload sql.NullTime
		}
		out := []item{}
		for rows.Next() {
			var v item
			if rows.Scan(&v.ID, &v.Model, &v.Revoked, &v.LastUpload) != nil {
				failure(w, errors.New("device read failed"))
				return
			}
			out = append(out, v)
		}
		if rows.Err() != nil {
			failure(w, rows.Err())
			return
		}
		s.renderPage(w, r, devicesTemplate, "BOOX Devices", out)
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
			failure(w, e)
			return
		}
		http.Redirect(w, r, "/boox/devices", 303)
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
