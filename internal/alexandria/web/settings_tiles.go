package web

import "net/http"

// settingsSection names the Settings group a form result belongs to, so its
// notice shows there. Fragments never reach the server, so links that carry a
// result also use ?section=.
func settingsSection(r *http.Request) string {
	if r.Method == http.MethodPost {
		switch r.URL.Path {
		case "/settings/password":
			return "password"
		case "/settings/minutes":
			return "minutes"
		case "/settings/tokens/create":
			return "api"
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
		"/settings":           {"password", "language", "theme", "about"},
		"/settings/devices":   {"boox", "client", "add"},
		"/settings/models":    {"provider", "minutes", "embedding"},
		"/settings/automatic": {"automatic"},
		"/settings/actions":   {"note-actions"},
		"/settings/search":    {"semantic-search"},
		"/settings/calendar":  {"caldav", "subscriptions"},
		"/settings/api":       {"api"},
		"/settings/operator":  {"sync", "library", "recognition", "reprocess", "server"},
		"/jobs/size":          {"page-limit"},
	}
	section := r.URL.Query().Get("section")
	for _, id := range allowed[r.URL.Path] {
		if section == id {
			return section
		}
	}
	return ""
}
