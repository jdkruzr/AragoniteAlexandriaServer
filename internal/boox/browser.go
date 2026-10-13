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

var setupTemplate = template.Must(template.New("setup").Parse(`{{define "content"}}
<p class="crumbs"><a href="/boox/devices">BOOX devices</a> /</p>
<h1>Connect a BOOX device {{template "help" "boox.enroll"}}</h1>
<section class="card">
<ol>
<li>Install <strong>PowerSync</strong> on the tablet.</li>
<li>Enter <code>{{.Base}}</code> as the server address.</li>
<li>Create a code below and type it into PowerSync. It works once and expires after ten minutes.</li>
</ol>
{{if .Data}}<p>Enrollment code:</p><code class="secret">{{.Data}}</code>{{end}}
<form class="actions" method="post" action="/boox/enroll"><button>{{if .Data}}Create another code{{else}}Create enrollment code{{end}}</button></form>
</section>
<p class="small muted">Every tablet must use the same BOOX account. In PowerSync, turn on notebook and reading sync to send content here. Your existing BOOX library is kept, and returning to Onyx doesn't need this server.</p>
{{end}}`))

// Views are grafted into the shared layout, so they may only use its functions.
var devicesTemplate = template.Must(template.New("devices").Parse(`{{define "content"}}
<div class="page-head"><h1>BOOX devices {{template "help" "boox.devices"}}</h1><a class="button" href="/boox/enroll">Connect a device</a></div>
{{if not .Data}}<div class="card"><p class="empty">No BOOX devices have connected yet.</p></div>{{else}}
<div class="table-scroll"><table class="keep-cols"><thead><tr><th>Device</th><th>State</th><th>Last upload received</th><th></th></tr></thead><tbody>
{{range .Data}}<tr><td><strong>{{.Model}}</strong><div class="muted small"><code>{{.ID}}</code></div></td>
<td>{{if .Revoked}}<span class="badge blank">Revoked</span>{{else}}<span class="badge indexed">Authorized</span>{{end}}</td>
<td>{{if .LastUpload.Valid}}{{.LastUpload.Time.UTC.Format "2006-01-02 15:04"}} UTC{{else}}<span class="muted">Nothing yet</span>{{end}}</td>
<td>{{if not .Revoked}}<details><summary>Manage</summary><form method="post" action="/boox/revoke"><input type="hidden" name="deviceId" value="{{.ID}}"><p class="small">Removes this device's access to the server. It does not switch the tablet back to Onyx.</p><button class="destructive">Revoke device</button></form></details>{{end}}</td></tr>{{end}}
</tbody></table></div>{{end}}
{{end}}`))

func (s Service) renderPage(w http.ResponseWriter, r *http.Request, content *template.Template, title string, data any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
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
			s.renderPage(w, r, booxSettings, "BOOX settings", nil)
			return
		case "/boox/reading":
			s.reading(w, r)
			return
		case "/boox/activity":
			s.browse(w, r)
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
	case "/boox/recognize":
		s.queuePageHTTP(w, r)
	case "/boox/enroll":
		if r.Method == "POST" {
			code, e := s.IssueCode(r.Context())
			if e != nil {
				failure(w, e)
				return
			}
			s.renderPage(w, r, setupTemplate, "Connect a BOOX device", code)
		} else if r.Method == "GET" {
			s.renderPage(w, r, setupTemplate, "Connect a BOOX device", "")
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
		s.renderPage(w, r, devicesTemplate, "BOOX devices", out)
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
