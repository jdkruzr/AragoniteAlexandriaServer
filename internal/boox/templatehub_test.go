package boox

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// Only the token-free fields leave the library; the rest, including the
// Template Hub page, stay on its own origin.
func TestProfileKeepsTokenHostsOnTheLibrary(t *testing.T) {
	c := Config{PublicURL: "https://library.example"}
	cluster := c.profile("11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222", "a", "b", "c", "ps_x")["cluster"].(map[string]any)
	h := cluster["clusterHost"].(map[string]string)
	want := map[string]string{
		"onyxHostBaseUrl":          "https://library.example",
		"onyxContentHostBaseUrl":   "https://library.example",
		"onyxSend2BooxHostBaseUrl": "https://library.example",
		"onyxMaterialHostBaseUrl":  "https://library.example/api/v1/boox/template-hub",
		"onyxCloudDataHostBaseUrl": "https://en-data.boox.com",
		"onyxLogHostBaseUrl":       "https://en-lon.boox.com",
		"shopBookUrl":              "https://push.boox.com",
		"onyxCouchbaseWSSHost":     "wss://library.example/boox-neocloud",
		"onyxMessageReplicator":    "wss://library.example/boox-neocloud",
	}
	if len(h) != len(want) {
		t.Fatalf("hosts %v", h)
	}
	for k, v := range want {
		if h[k] != v {
			t.Errorf("%s = %q, want %q", k, h[k], v)
		}
	}
	for k, v := range cluster["ossConfig"].(map[string]string) {
		if strings.HasSuffix(k, "Bucket") == (v == "https://library.example") {
			t.Errorf("ossConfig %s = %q", k, v)
		}
	}
}

func TestTemplateHubPageIsAnonymousAndScriptless(t *testing.T) {
	r := httptest.NewRequest("GET", templateHubPath, nil)
	if !(Service{}).Owns(r) {
		t.Fatal("the BOOX service doesn't own the Template Hub page")
	}
	w := httptest.NewRecorder()
	templateHub(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Template Hub Isn't Available Here") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
		t.Errorf("csp %q", csp)
	}
	w = httptest.NewRecorder()
	templateHub(w, httptest.NewRequest("POST", templateHubPath, nil))
	if w.Code != 405 {
		t.Errorf("POST %d", w.Code)
	}
}
