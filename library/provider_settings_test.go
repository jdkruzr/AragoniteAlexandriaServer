package library

import (
	"bytes"
	"context"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestProviderSettingsOwnerOnlySecretsAndImmediateRefresh(t *testing.T) {
	r, db, _ := fixture(t)
	ctx := context.Background()
	r.cfg.ProviderSettings = &ProviderSettings{Key: bytes.Repeat([]byte{9}, 32), Defaults: ProviderConfig{OCR: OCRProviderConfig{Format: "openai"}, Embedding: EmbeddingProviderConfig{Model: "synthetic"}}}
	s := providers.Store{DB: db, Bootstrap: *r.cfg.ProviderSettings, LibraryID: r.cfg.ID}
	if e := s.Ensure(ctx); e != nil {
		t.Fatal(e)
	}
	b := browser{t, r}
	get := b.do("GET", "/settings/recognition", "", nil, true)
	if get.Code != 200 || !strings.Contains(get.Body.String(), `value="test_ocr"`) {
		t.Fatal(get.Code, get.Body.String())
	}
	token, e := auth.NewStore(db).CreateToken(ctx, "synthetic caller")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/settings", "/settings/search", "/settings/recognition", "/settings/integrations"} {
		if got := b.do("GET", path, "", map[string]string{"Authorization": "Bearer " + token}, false); got.Code != 401 {
			t.Fatal("bearer token admitted owner settings", path, got.Code)
		}
	}
	if got := b.do("POST", "/settings/minutes", "revision=1&action=save", map[string]string{"Authorization": "Bearer " + token, "Origin": sameSite["Origin"], "Sec-Fetch-Site": "same-origin"}, false); got.Code != 401 {
		t.Fatal("bearer token admitted minutes settings", got.Code)
	}
	form := url.Values{"revision": {"1"}, "action": {"save"}, "ocr_enabled": {"on"}, "ocr_format": {"openai"}, "ocr_url": {"http://localhost:9999"}, "ocr_model": {"synthetic-vision"}, "embed_model": {"synthetic"}, "api_key": {"private-secret-value"}, "key_action": {"replace"}, "anthropic_workspace": {"wrkspc_test123"}}
	if got := b.do("POST", "/settings/providers", form.Encode(), nil, true); got.Code != 403 {
		t.Fatal("CSRF", got.Code)
	}
	if got := b.do("POST", "/settings/providers", form.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code, got.Body.String())
	}
	get = b.do("GET", "/settings/recognition", "", nil, true)
	if get.Code != 200 || strings.Contains(get.Body.String(), "private-secret-value") || !strings.Contains(get.Body.String(), "A key is saved") || !strings.Contains(get.Body.String(), "wrkspc_test123") {
		t.Fatal("secret view", get.Code)
	}
	p, _, snap, e := r.pageConfiguration(ctx, db)
	if e != nil || p.OCR == nil || p.OCR.Model() != "synthetic-vision" || snap.Revision != 2 || snap.Config.OCR.AnthropicWorkspace != "wrkspc_test123" {
		t.Fatal("runtime not refreshed", e)
	}
	// A separately admitted runtime configuration reads the same revision.
	other := &Runtime{cfg: r.cfg}
	p2, _, _, e := other.pageConfiguration(ctx, db)
	if e != nil || p2.OCR == nil {
		t.Fatal("worker stale", e)
	}
	// The embedding card (Recognition tab) saves both providers at once:
	// posting its form exactly as rendered (hidden recognition fields included)
	// must leave recognition and the saved key untouched.
	search := b.do("GET", "/settings/recognition", "", nil, true).Body.String()
	if strings.Contains(search, "private-secret-value") {
		t.Fatal("recognition tab exposed the key")
	}
	start := strings.Index(search, `id="semantic-advanced"`)
	end := strings.Index(search[start:], "</form>")
	embedForm := url.Values{"action": {"save"}}
	for _, m := range regexp.MustCompile(`<input (?:type="hidden" |id="[^"]*" |type="url" )*name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(search[start:start+end], -1) {
		embedForm.Set(m[1], html.UnescapeString(m[2]))
	}
	embedForm.Set("embed_model", "synthetic-2")
	if embedForm.Get("ocr_model") != "synthetic-vision" || embedForm.Get("anthropic_workspace") != "wrkspc_test123" || embedForm.Get("page") != "embedding" {
		t.Fatal("search tab does not carry recognition settings", embedForm)
	}
	if got := b.do("POST", "/settings/providers", embedForm.Encode(), sameSite, true); got.Code != 303 || !strings.HasPrefix(got.Header().Get("Location"), "/settings/recognition?notice=") || !strings.HasSuffix(got.Header().Get("Location"), "#embedding") {
		t.Fatal("embedding card save", got.Code, got.Header().Get("Location"))
	}
	p, _, snap, e = r.pageConfiguration(ctx, db)
	if e != nil || p.OCR == nil || p.OCR.Model() != "synthetic-vision" || snap.Config.OCR.AnthropicWorkspace != "wrkspc_test123" || snap.Config.Embedding.Model != "synthetic-2" || snap.Revision != 3 {
		t.Fatal("search tab changed recognition settings", e, snap.Revision)
	}
	if !strings.Contains(b.do("GET", "/settings/recognition", "", nil, true).Body.String(), "A key is saved") {
		t.Fatal("search tab cleared the key")
	}
	form.Set("embed_model", "synthetic-2")
	form.Set("revision", "3")
	oldIdentity := p.OCRIdentity
	form.Set("key_action", "keep")
	form.Set("api_key", "")
	form.Set("ocr_enabled", "")
	if got := b.do("POST", "/settings/providers", form.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code)
	}
	p, _, _, e = r.pageConfiguration(ctx, db)
	if e != nil || p.OCR != nil || p.OCRIdentity != oldIdentity {
		t.Fatal("disable changed recognition identity", e)
	}
	// The minutes form saves only its providers, under the same revision check.
	minutesForm := url.Values{"revision": {"4"}, "action": {"save"}, "t_enabled": {"on"}, "t_url": {"http://whisper.local:8090"}, "t_model": {"synthetic-whisper"}, "t_key_action": {"keep"},
		"s_enabled": {"on"}, "s_format": {"openai"}, "s_url": {"http://llm.local"}, "s_model": {"synthetic-llm"}, "s_key": {"private-summary-key"}, "s_key_action": {"keep"}}
	if got := b.do("POST", "/settings/minutes", minutesForm.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal("minutes save", got.Code, got.Body.String())
	}
	_, _, snap, e = r.pageConfiguration(ctx, db)
	if e != nil || snap.Revision != 5 || snap.Config.OCR.Model != "synthetic-vision" || snap.Config.Embedding.Model != "synthetic-2" || snap.Config.Summary.Model != "synthetic-llm" || snap.SummaryKey != "private-summary-key" || snap.APIKey != "private-secret-value" {
		t.Fatal("minutes save changed other providers", e, snap.Revision)
	}
	if page := b.do("GET", "/settings/recognition", "", nil, true).Body.String(); strings.Contains(page, "private-summary-key") || !strings.Contains(page, "A summary key is saved") || !strings.Contains(page, `value="synthetic-whisper"`) {
		t.Fatal("minutes settings view")
	}
	if got := b.do("POST", "/settings/minutes", minutesForm.Encode(), sameSite, true); got.Code != 200 || !strings.Contains(got.Body.String(), "Settings changed in another session") {
		t.Fatal("stale minutes form accepted", got.Code)
	}
	// Automatic processing saves on its own form, also revision-checked, and
	// leaves the providers alone.
	auto := url.Values{"revision": {"5"}, "a_recognize": {"on"}, "a_summarize": {"on"}}
	if got := b.do("POST", "/settings/automatic", auto.Encode(), nil, true); got.Code != 403 {
		t.Fatal("automatic CSRF", got.Code)
	}
	if got := b.do("POST", "/settings/automatic", auto.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal("automatic save", got.Code, got.Body.String())
	}
	_, _, snap, e = r.pageConfiguration(ctx, db)
	if e != nil || !snap.Config.Automatic.Recognize || !snap.Config.Automatic.Summarize || snap.Config.Automatic.Since.IsZero() || snap.Config.Summary.Model != "synthetic-llm" || snap.SummaryKey != "private-summary-key" {
		t.Fatal("automatic save", e, snap.Config.Automatic)
	}
	if page := b.do("GET", "/settings/recognition", "", nil, true).Body.String(); !strings.Contains(page, `name="a_recognize" checked`) || !strings.Contains(page, "On since") {
		t.Fatal("automatic settings view")
	}
	// Enabling/saving never queues existing content for paid processing.
	var count int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM boox_page_index`).Scan(&count); e != nil || count != 0 {
		t.Fatal("implicit OCR queue", count, e)
	}
}
