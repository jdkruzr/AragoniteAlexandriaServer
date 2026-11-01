package boox

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// Connect a BOOX shows its code once, refreshes while the code waits, and
// says which tablet connected; Settings → Devices lists it as new.
func TestPairingPageAndDevices(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	page := func(method string) string {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/boox/enroll", nil)
		r.Header.Set("Origin", "https://boox.example")
		s.Admin(w, r)
		if w.Code != 200 {
			t.Fatalf("%s /boox/enroll: %d %s", method, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if body := page("GET"); strings.Contains(body, `class="pair-code"`) || strings.Contains(body, "alexandria-live") || !strings.Contains(body, `class="settings-nav"`) {
		t.Fatal("an idle pairing page shows a code or refreshes")
	}
	body := page("POST")
	start := strings.Index(body, `class="pair-code">`)
	if start < 0 || !strings.Contains(body, "alexandria-live") {
		t.Fatal("a new code is shown and the page waits for the tablet")
	}
	code := body[start+len(`class="pair-code">`):][:8]
	if d, e := s.Devices(ctx); e != nil || !d.Waiting || len(d.Devices) != 0 {
		t.Fatal("waiting state", d, e)
	}
	payload := map[string]string{"code": code, "installationId": uuid.NewString(), "model": "Synthetic Go 7"}
	if w := request(t, s, "POST", "/api/v1/boox/enroll", payload, nil); w.Code != 200 {
		t.Fatalf("enroll: %d", w.Code)
	}
	d, e := s.Devices(ctx)
	if e != nil || d.Waiting || len(d.Live()) != 1 || !d.Live()[0].New || len(d.Revoked()) != 0 {
		t.Fatal("connected state", d, e)
	}
	if body := page("GET"); !strings.Contains(body, d.Live()[0].Model+" connected.") || strings.Contains(body, "alexandria-live") {
		t.Fatal("connected tablet not announced, or the page still refreshes")
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/boox/revoke", strings.NewReader(url.Values{"deviceId": {d.Live()[0].ID}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://boox.example")
	s.Admin(w, r)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/settings/devices?") {
		t.Fatalf("revoke: %d %s", w.Code, w.Header().Get("Location"))
	}
	if d, e = s.Devices(ctx); e != nil || len(d.Revoked()) != 1 || len(d.Live()) != 0 {
		t.Fatal("revoked device not listed apart", e)
	}
	if _, e = s.OperatorFacts(ctx); e != nil {
		t.Fatal(e)
	}
}
