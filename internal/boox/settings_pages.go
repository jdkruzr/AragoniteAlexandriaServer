package boox

import (
	"context"
	"database/sql"
	"html/template"
	"net/http"
	"net/url"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

var setupTemplate = template.Must(template.New("setup.html").Funcs(web.Funcs()).ParseFS(templateFS, "templates/setup.html"))

// renderSettingsPage shows a BOOX page under Settings: pairing, and the
// operator tools reached from Settings → Operator tools.
func (s Service) renderSettingsPage(w http.ResponseWriter, r *http.Request, content *template.Template, title string, data any) {
	s.renderIn(w, r, content, title, "settings", data)
}

// Devices lists BOOX tablets for Settings → Devices, newest enrollments
// marked, with whether a pairing code is still waiting to be used.
func (s Service) Devices(ctx context.Context) (web.BOOXDevices, error) {
	var out web.BOOXDevices
	var err error
	if out.Summary, err = s.Summary(ctx); err != nil {
		return out, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id::text,model,revoked,created_at>now()-interval '10 minutes',(SELECT max(v.created_at) FROM boox_blob_version v WHERE v.device_id=d.id) FROM boox_device d ORDER BY revoked,created_at`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var d web.BOOXDevice
		var last sql.NullTime
		if err = rows.Scan(&d.ID, &d.Model, &d.Revoked, &d.New, &last); err != nil {
			return out, err
		}
		d.New = d.New && !d.Revoked
		d.LastUpload = last.Time
		out.Devices = append(out.Devices, d)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_enrollment WHERE used_at IS NULL AND expires_at>now())`).Scan(&out.Waiting)
	return out, err
}

// OperatorFacts counts the BOOX maintenance work Operator tools offers.
func (s Service) OperatorFacts(ctx context.Context) (web.BOOXOperator, error) {
	var out web.BOOXOperator
	err := s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM boox_projection WHERE `+cleanupPredicate+`),
 (SELECT count(*) FROM boox_projection WHERE `+notebookPredicate+` AND native_uid=$1 AND body->>'status'='0' AND body->>'type'='1')`, s.uid()).Scan(&out.Numbered, &out.Deleted)
	return out, err
}

type pairingView struct {
	// Code is shown once, right after it is created; never in a URL.
	Code      string
	Waiting   bool
	Connected []web.BOOXDevice
}

// pairing is the Connect a BOOX page: POST creates a code, and the page
// refreshes until the code is used or expires (GET renders it without the code).
func (s Service) pairing(w http.ResponseWriter, r *http.Request) {
	var v pairingView
	ctx := r.Context()
	switch r.Method {
	case http.MethodPost:
		code, e := s.IssueCode(ctx)
		if e != nil {
			s.failure(w, "browser", e)
			return
		}
		v.Code = code
	case http.MethodGet:
	default:
		http.NotFound(w, r)
		return
	}
	devices, e := s.Devices(ctx)
	if e != nil {
		s.failure(w, "browser", e)
		return
	}
	v.Waiting = devices.Waiting
	for _, d := range devices.Devices {
		if d.New {
			v.Connected = append(v.Connected, d)
		}
	}
	if v.Waiting {
		w.Header().Set("Refresh", "5")
	}
	s.renderSettingsPage(w, r, setupTemplate, "Connect a BOOX", v)
}

// settingsRedirect sends the retired BOOX settings and device pages to
// their place in Settings.
func settingsRedirect(w http.ResponseWriter, r *http.Request) {
	to := "/settings/devices#boox"
	if r.URL.Path == "/boox/settings" {
		switch r.URL.Query().Get("section") {
		case "maintenance", "diagnostics":
			to = "/settings/operator"
		}
	}
	http.Redirect(w, r, to, http.StatusMovedPermanently)
}

// revokedNotice returns to Settings → Devices after a revoke.
func revokedNotice() string {
	notice := "Device revoked. It can no longer sync."
	return "/settings/devices?" + url.Values{"section": {"boox"}, "notice": {notice}}.Encode() + "#boox"
}
