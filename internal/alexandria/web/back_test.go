package web

import (
	"net/http/httptest"
	"testing"
)

func TestBackKeepsNoticeBeforeFragment(t *testing.T) {
	for path, want := range map[string]string{
		"/settings/recognition#note-actions": "/settings/recognition?notice=Saved.#note-actions",
		"/settings/recognition":              "/settings/recognition?notice=Saved.",
		"/jobs?source=boox#runs":             "/jobs?source=boox&notice=Saved.#runs",
	} {
		w := httptest.NewRecorder()
		back(w, httptest.NewRequest("POST", "/x", nil), path, "Saved.")
		if got := w.Header().Get("Location"); got != want {
			t.Errorf("back(%q) = %q, want %q", path, got, want)
		}
	}
}
