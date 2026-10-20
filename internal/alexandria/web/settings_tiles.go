package web

import "net/http"

// settingsSection keeps categories closed on a fresh visit. Explicit links and
// form results open only their category; no browser preference is retained.
// Fragments alone never reach the server, so in-app links also use ?section=.
func settingsSection(r *http.Request) string {
	if r.Method == http.MethodPost {
		switch r.URL.Path {
		case "/settings/password":
			return "password"
		case "/settings/minutes":
			return "minutes"
		case "/settings/providers":
			if r.PostForm.Get("action") == "reprocess" {
				return "reprocess"
			}
			if p := r.PostForm.Get("page"); p == "embedding" || p == "search" || r.PostForm.Get("action") == "test_embedding" {
				return "embedding"
			}
			return "provider"
		}
	}
	allowed := map[string][]string{
		"/settings":              {"password", "language", "about"},
		"/settings/search":       {"semantic-search", "embedding-model"},
		"/settings/integrations": {"caldav", "subscriptions", "api"},
		"/settings/recognition":  {"page-limit", "provider", "minutes", "embedding", "automatic", "note-actions", "reprocess"},
		"/sources/client":        {"devices", "recognition-search"},
		"/boox/settings":         {"connection", "maintenance", "diagnostics"},
	}
	section := r.URL.Query().Get("section")
	for _, id := range allowed[r.URL.Path] {
		if section == id {
			return section
		}
	}
	return ""
}
