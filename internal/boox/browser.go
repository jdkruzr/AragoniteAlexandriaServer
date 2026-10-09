package boox

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	"html/template"
	"net/http"
	"strings"
)

var setupTemplate = template.Must(template.New("setup").Parse(`{{define "content"}}
<p class="crumbs"><a href="/boox">Native BOOX</a> / Connect a device</p>
<h1>Connect a BOOX device</h1>
<section class="help">
<h2>Connect with PowerSync</h2>
<p>Install PowerSync on your BOOX device and enter <code>{{.Base}}</code> as the server address. Create an enrollment code below, then enter it in the app.</p>
<p>The code expires after ten minutes and can be used once.</p>
{{if .Data}}<p>Enrollment code:</p><code class="secret">{{.Data}}</code>{{end}}
<form class="actions" method="post" action="/boox/enroll"><button>Create enrollment code</button></form>
</section>
<p class="muted">PowerSync preserves your existing BOOX library identity. Enable notebook and reading sync in PowerSync to send eligible content to Alexandria. Devices must use the same BOOX account for this library.</p>
<p class="muted">Returning to Onyx does not require this server.</p>
<p><a href="/boox/devices">Manage devices</a></p>
{{end}}`))
var devicesTemplate = template.Must(template.New("devices").Parse(`{{define "content"}}
<p class="crumbs"><a href="/boox">Native BOOX</a> / Devices</p>
<h1>BOOX devices</h1><p class="muted">Authorization is separate from connection or download completion. These registrations belong only to BOOX Native.</p>
<div class="actions"><a class="button" href="/boox/enroll">Connect a device</a></div>
{{if not .Data}}<p class="empty">No BOOX devices have connected yet.</p>{{else}}
<table><thead><tr><th>Device</th><th>State</th><th>Device ID</th><th></th></tr></thead><tbody>
{{range .Data}}<tr><td><strong>{{.Model}}</strong></td>
<td>{{if .Revoked}}<span class="badge blank">Revoked</span>{{else}}<span class="badge indexed">Authorized</span>{{end}}</td>
<td class="muted small"><code>{{.ID}}</code></td>
<td>{{if not .Revoked}}<form method="post" action="/boox/revoke"><input type="hidden" name="deviceId" value="{{.ID}}"><button class="destructive">Revoke device</button></form>{{end}}</td></tr>{{end}}
</tbody></table>{{end}}
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
			s.renderPage(w, r, booxSettings, "BOOX Native settings", nil)
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
		rows, e := s.DB.QueryContext(r.Context(), `SELECT id::text,model,revoked FROM boox_device ORDER BY created_at`)
		if e != nil {
			failure(w, e)
			return
		}
		defer rows.Close()
		type item struct {
			ID, Model string
			Revoked   bool
		}
		out := []item{}
		for rows.Next() {
			var v item
			if rows.Scan(&v.ID, &v.Model, &v.Revoked) != nil {
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
