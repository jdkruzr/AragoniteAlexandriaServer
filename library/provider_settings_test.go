package library

import (
	"bytes"
	"context"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
	"net/url"
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
	get := b.do("GET", "/settings", "", nil, true)
	if get.Code != 200 || !strings.Contains(get.Body.String(), "Save provider settings") {
		t.Fatal(get.Code, get.Body.String())
	}
	token, e := auth.NewStore(db).CreateToken(ctx, "synthetic caller")
	if e != nil {
		t.Fatal(e)
	}
	if got := b.do("GET", "/settings", "", map[string]string{"Authorization": "Bearer " + token}, false); got.Code != 401 {
		t.Fatal("bearer token admitted owner settings", got.Code)
	}
	form := url.Values{"revision": {"1"}, "action": {"save"}, "ocr_enabled": {"on"}, "ocr_format": {"openai"}, "ocr_url": {"http://localhost:9999"}, "ocr_model": {"synthetic-vision"}, "embed_model": {"synthetic"}, "api_key": {"private-secret-value"}, "key_action": {"replace"}, "anthropic_workspace": {"wrkspc_test123"}}
	if got := b.do("POST", "/settings/providers", form.Encode(), nil, true); got.Code != 403 {
		t.Fatal("CSRF", got.Code)
	}
	if got := b.do("POST", "/settings/providers", form.Encode(), sameSite, true); got.Code != 303 {
		t.Fatal(got.Code, got.Body.String())
	}
	get = b.do("GET", "/settings", "", nil, true)
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
	oldIdentity := p.OCRIdentity
	form.Set("revision", "2")
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
	// Enabling/saving never queues existing content for paid processing.
	var count int
	if e = db.QueryRowContext(ctx, `SELECT count(*) FROM boox_page_index`).Scan(&count); e != nil || count != 0 {
		t.Fatal("implicit OCR queue", count, e)
	}
}
