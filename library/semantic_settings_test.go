package library

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
)

func TestSemanticPanelReadinessActionsAndOwnerBoundary(t *testing.T) {
	r, db, _ := fixture(t)
	ctx := context.Background()
	var inference atomic.Int32
	var offline atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if offline.Load() {
			http.Error(w, "private upstream failure", 503)
			return
		}
		if req.Method == "GET" && req.URL.Path == "/api/tags" {
			w.Write([]byte(`{"models":[{"name":"synthetic:latest"}]}`))
			return
		}
		if req.Method == "POST" && req.URL.Path == "/api/embed" {
			inference.Add(1)
			w.Write([]byte(`{"embeddings":[[1,0,0]]}`))
			return
		}
		t.Error("unexpected provider request", req.Method, req.URL.Path)
		http.NotFound(w, req)
	}))
	defer server.Close()
	r.cfg.ProviderSettings = &ProviderSettings{Key: bytes.Repeat([]byte{9}, 32), Defaults: ProviderConfig{OCR: OCRProviderConfig{Format: "openai"}, Embedding: EmbeddingProviderConfig{URL: server.URL, Model: "synthetic"}}}
	s := providers.Store{DB: db, Bootstrap: *r.cfg.ProviderSettings, LibraryID: r.cfg.ID}
	if err := s.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO alexandria_note_content(note_key,page,body_text,source) VALUES('forestnote://00000000000000000000000NB1/00000000000000000000000PG1',0,'synthetic client text','forestnote')`); err != nil {
		t.Fatal(err)
	}
	if _, err := (relay.Store{DB: db}).AuthorOps(ctx, []relay.Op{
		{Table: "notebook", PK: "00000000000000000000000NB1", Cols: map[string]any{"name": "Synthetic panel notebook", "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "folder_id": nil, "aspect_long_axis": nil, "page_width": nil, "page_height": nil}},
		{Table: "page", PK: "00000000000000000000000PG1", Cols: map[string]any{"notebook_id": "00000000000000000000000NB1", "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "template": nil, "template_pitch_mm": nil}},
	}); err != nil {
		t.Fatal(err)
	}
	b := browser{t, r}
	get := func(want ...string) string {
		t.Helper()
		res := b.do("GET", "/settings/search", "", nil, true)
		if res.Code != 200 {
			t.Fatal(res.Code, res.Body.String())
		}
		html := res.Body.String()
		for _, s := range want {
			if !strings.Contains(html, s) {
				t.Fatal("missing UI state", s)
			}
		}
		return html
	}
	get("Ready", "Not built", "Turning this on sends your existing searchable text", "Enable and build index", "Embedding Provider", "BOOX notebook pages")
	if inference.Load() != 0 {
		t.Fatal("viewing Settings ran inference")
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_embedding_generation`).Scan(&n); err != nil || n != 0 {
		t.Fatal("viewing Settings built an index", err)
	}
	form := url.Values{"revision": {"1"}, "action": {"enable_build"}}
	if res := b.do("POST", "/settings/semantic", form.Encode(), nil, true); res.Code != 403 {
		t.Fatal("CSRF", res.Code)
	}
	if res := b.do("POST", "/settings/semantic", form.Encode(), sameSite, false); res.Code != 401 {
		t.Fatal("anonymous mutation", res.Code)
	}
	token, err := auth.NewStore(db).CreateToken(ctx, "synthetic panel caller")
	if err != nil {
		t.Fatal(err)
	}
	if res := b.do("POST", "/settings/semantic", form.Encode(), map[string]string{"Authorization": "Bearer " + token, "Origin": "http://library.test", "Sec-Fetch-Site": "same-origin"}, false); res.Code != 401 {
		t.Fatal("bearer mutation", res.Code)
	}
	action := func(name string) {
		t.Helper()
		snap, err := s.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		form.Set("revision", strconv.FormatInt(snap.Revision, 10))
		form.Set("action", name)
		res := b.do("POST", "/settings/semantic", form.Encode(), sameSite, true)
		if res.Code != 303 {
			t.Fatal(res.Code, res.Body.String())
		}
		if strings.Contains(res.Header().Get("Location"), "not+changed") {
			t.Fatal(res.Header().Get("Location"))
		}
	}
	// A failed explicit preflight must not turn on the feature or create work.
	offline.Store(true)
	action("enable_build")
	snap, err := s.Load(ctx)
	if err != nil || snap.Config.Embedding.Enabled || snap.Revision != 1 {
		t.Fatal("failed preflight changed settings", err)
	}
	offline.Store(false)
	action("enable_build")
	get("Building", "Turn off", "<strong>1</strong> outstanding")
	if inference.Load() != 1 {
		t.Fatal("unexpected preflight count", inference.Load())
	}
	// Replaying the old enable form is rejected before a provider call.
	res := b.do("POST", "/settings/semantic", form.Encode(), sameSite, true)
	if res.Code != 303 || !strings.Contains(res.Header().Get("Location"), "another+session") || inference.Load() != 1 {
		t.Fatal("stale replay", res.Code)
	}
	offline.Store(true)
	action("disable")
	get("Unavailable", "Build paused", "Turn on and resume")
	if inference.Load() != 1 {
		t.Fatal("disable ran inference")
	}
	offline.Store(false)
	action("enable")
	if ok, err := s.ProcessIndex(ctx); err != nil || !ok {
		t.Fatal(ok, err)
	}
	get("<strong>1</strong> record in the current index", "Build replacement index")
	action("disable")
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_embedding_vectors`).Scan(&n); err != nil || n != 1 {
		t.Fatal("disable deleted vectors", n, err)
	}
	action("enable")
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_embedding_generation`).Scan(&n); err != nil || n != 1 {
		t.Fatal("resume rebuilt index", n, err)
	}
	// An unsaved advanced form cannot change what the panel reports as saved.
	snap, err = s.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	advanced := url.Values{"revision": {strconv.FormatInt(snap.Revision, 10)}, "action": {"test_embedding"}, "page": {"search"}, "ocr_format": {"openai"}, "embed_enabled": {"on"}, "embed_url": {"http://127.0.0.1:1"}, "embed_model": {"unsaved-model"}, "key_action": {"keep"}}
	res = b.do("POST", "/settings/providers", advanced.Encode(), sameSite, true)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "Saved model: <code>synthetic</code>") || !strings.Contains(res.Body.String(), `value="unsaved-model"`) {
		t.Fatal("unsaved provider presented as saved", res.Code)
	}
}
