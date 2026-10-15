package boox

import (
	"html"
	"net/http"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

// templateHubPath is the page the Notes app's Template Hub opens: the
// cluster's material host. The real hub is an Onyx service that needs an Onyx
// sign-in and hands its page the account token, so Alexandria explains
// instead. No scripts may run, which also keeps the page away from the
// app's token bridge.
const templateHubPath = "/api/v1/boox/template-hub"

// templateHubPage is in the tablet's language: its browser sends the
// device language as Accept-Language. Template Hub is BOOX's feature name.
func templateHubPage(l i18n.Localizer) string {
	esc := html.EscapeString
	return `<!doctype html>
<html lang="` + esc(l.Lang()) + `"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Template Hub</title>
<style>body{margin:0;padding:48px 32px;background:#fff;color:#000;font:22px/1.5 sans-serif}h1{font-size:30px;margin:0 0 24px}p{margin:0 0 18px;max-width:34em}</style>
</head><body>
<h1>` + esc(l.T("Template Hub Isn't Available Here")) + `</h1>
<p>` + esc(l.T("This BOOX syncs with your Alexandria library instead of the Onyx cloud. The Template Hub is an Onyx cloud service that needs an Onyx sign-in, so it can't open while you're connected to Alexandria.")) + `</p>
<p>` + esc(l.T("Templates you've already downloaded still work, and your notes keep syncing as usual.")) + `</p>
</body></html>
`
}

func templateHub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Vary", "Accept-Language")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(templateHubPage(i18n.For(r.Header.Get("Accept-Language")))))
}
