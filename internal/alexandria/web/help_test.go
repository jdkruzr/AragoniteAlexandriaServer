package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Every tip's "Learn more" lands on a section of Help, the menu lists the
// sections, and the version moved from the old site footer into Help.
func TestHelpAnchorsAndMenu(t *testing.T) {
	for _, boox := range []bool{true, false} {
		w := httptest.NewRecorder()
		Handler(Deps{Status: Status{NativeBOOX: boox}}).ServeHTTP(w, httptest.NewRequest("GET", "/help", nil))
		body := w.Body.String()
		if w.Code != 200 || strings.Contains(body, "site-footer") || !strings.Contains(body, `<code data-version>`) || !strings.Contains(body, `<nav class="settings-nav help-nav"`) {
			t.Fatal(boox, w.Code)
		}
		for key, tip := range helpTips {
			booxOnly := tip.Guide == "boox" || tip.Guide == "minutes" || tip.Guide == "actions"
			if (boox || !booxOnly) && !strings.Contains(body, `id="`+tip.Guide+`"`) {
				t.Errorf("tip %s links to missing #%s (BOOX %v)", key, tip.Guide, boox)
			}
		}
		for _, section := range []string{"start", "library", "recognition", "tasks", "search", "devices", "diagnostics"} {
			if !strings.Contains(body, `<li><a href="#`+section+`">`) || !strings.Contains(body, `id="`+section+`"`) {
				t.Errorf("menu or section %s missing", section)
			}
		}
		if strings.Contains(body, `href="#minutes"`) != boox || strings.Contains(body, `href="#actions"`) != boox {
			t.Error("BOOX sections follow the source", boox)
		}
	}
}
