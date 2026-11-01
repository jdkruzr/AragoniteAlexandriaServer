package library

import (
	"net/url"
	"strings"
	"testing"
)

// Every Settings section renders in the shared shell; a save reports itself
// beside its button; a new token is shown once on the API page; operator
// tools and the recognition page limit have their new homes.
func TestSettingsSectionsSavedTokenAndOperator(t *testing.T) {
	r, _, _ := fixture(t)
	b := browser{t, r}
	for _, path := range []string{"/settings", "/settings/devices", "/settings/models", "/settings/automatic", "/settings/actions", "/settings/search", "/settings/calendar", "/settings/api", "/settings/operator"} {
		if got := b.do("GET", path, "", nil, true); got.Code != 200 || !strings.Contains(got.Body.String(), `class="settings-nav"`) || !strings.Contains(got.Body.String(), `aria-current="page"`) {
			t.Fatalf("%s: %d", path, got.Code)
		}
	}
	saved := b.do("POST", "/settings/caldav", url.Values{"collection": {"Synthetic tasks"}, "due_mode": {"preserve"}}.Encode(), sameSite, true)
	if saved.Code != 303 || !strings.HasPrefix(saved.Header().Get("Location"), "/settings/calendar?saved=1&section=caldav") {
		t.Fatalf("save: %d %s", saved.Code, saved.Header().Get("Location"))
	}
	if page := b.do("GET", saved.Header().Get("Location"), "", nil, true).Body.String(); !strings.Contains(page, `class="saved"`) || !strings.Contains(page, "CalDAV settings saved.") {
		t.Fatal("saved notice missing beside the button")
	}
	created := b.do("POST", "/settings/tokens/create", "label=Synthetic+script", sameSite, true)
	if created.Code != 200 || !strings.Contains(created.Body.String(), `class="set-panel"`) || !strings.Contains(created.Body.String(), "Synthetic script") || created.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token panel: %d", created.Code)
	}
	if page := b.do("GET", "/settings/api", "", nil, true).Body.String(); strings.Contains(page, `class="set-panel"`) || !strings.Contains(page, "Synthetic script") {
		t.Fatal("token shown again, or not listed")
	}
	if page := b.do("GET", "/settings/operator", "", nil, true).Body.String(); !strings.Contains(page, "PostgreSQL ") || !strings.Contains(page, `href="/jobs/size"`) {
		t.Fatal("operator tools missing server facts or size limits")
	}
	if page := b.do("GET", "/jobs/size", "", nil, true).Body.String(); !strings.Contains(page, `action="/settings/recognition-limit"`) {
		t.Fatal("page limit form not on Notebook size limits")
	}
	limit := b.do("POST", "/settings/recognition-limit", "limit=120&expected=100", sameSite, true)
	if limit.Code != 303 || !strings.HasPrefix(limit.Header().Get("Location"), "/jobs/size?saved=1&section=page-limit") {
		t.Fatalf("page limit: %d %s", limit.Code, limit.Header().Get("Location"))
	}
}
