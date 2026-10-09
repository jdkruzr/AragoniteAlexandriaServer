package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fixture(t *testing.T) Store {
	t.Helper()
	db := testenv.Database(t)
	s := Store{DB: db.DB, LibraryID: db.ID, Bootstrap: Bootstrap{Key: bytes.Repeat([]byte{7}, 32), Defaults: Config{OCR: OCRConfig{Format: "openai"}, Embedding: EmbedConfig{Model: "synthetic"}}}}
	if e := s.Ensure(context.Background()); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestSettingsEncryptionCASAndSecretActions(t *testing.T) {
	ctx := context.Background()
	s := fixture(t)
	v, e := s.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	v.Config.OCR.URL = "http://localhost:9999"
	v.Config.OCR.Model = "vision"
	v.Config.OCR.Enabled = true
	c, e := s.Candidate(ctx, v.Revision, v.Config, "private-test-key", "keep")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Save(ctx, c); e != nil {
		t.Fatal(e)
	}
	var raw, credential []byte
	if e = s.DB.QueryRowContext(ctx, `SELECT config,credential FROM alexandria_provider_settings`).Scan(&raw, &credential); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte("private-test-key")) || bytes.Contains(credential, []byte("private-test-key")) {
		t.Fatal("plaintext credential")
	}
	view, e := s.View(ctx)
	if e != nil || !view.HasKey {
		t.Fatal(view, e)
	}
	body, _ := json.Marshal(view)
	if bytes.Contains(body, []byte("private-test-key")) {
		t.Fatal("view leaked credential")
	}
	if e = s.Save(ctx, c); e != ErrStale {
		t.Fatal("stale overwrite accepted", e)
	}
	current, _ := s.Load(ctx)
	if current.APIKey != "private-test-key" {
		t.Fatal("decrypt")
	}
	changed := current.Config
	changed.OCR.URL = "http://another-host:9999"
	if _, e = s.Candidate(ctx, current.Revision, changed, "", "keep"); e == nil {
		t.Fatal("saved key forwarded to new host")
	}
	s2 := s
	s2.LibraryID = "different-library"
	if _, e = s2.Load(ctx); e == nil {
		t.Fatal("ciphertext not library-bound")
	}
	s2 = s
	s2.Bootstrap.Defaults.OCR.Model = "changed environment"
	s2.Bootstrap.APIKey = "changed-secret"
	if e = s2.Ensure(ctx); e != nil {
		t.Fatal(e)
	}
	again, _ := s2.Load(ctx)
	if again.APIKey != current.APIKey || again.Config.OCR.Model != current.Config.OCR.Model {
		t.Fatal("environment overrode saved settings")
	}
	cleared, e := s.Candidate(ctx, current.Revision, current.Config, "", "clear")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Save(ctx, cleared); e != nil {
		t.Fatal(e)
	}
	view, _ = s.View(ctx)
	if view.HasKey {
		t.Fatal("key not cleared")
	}
	s.Bootstrap.Locked = true
	if e = s.Save(ctx, cleared); e == nil {
		t.Fatal("deployment lock ignored")
	}
}
func TestConnectionUsesSyntheticImageAndRedactsFailures(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		json.NewDecoder(r.Body).Decode(&b)
		raw, _ := json.Marshal(b)
		captured = string(raw)
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("auth")
		}
		http.Error(w, "secret private upstream failure", 401)
	}))
	defer srv.Close()
	v := Snapshot{Config: Config{OCR: OCRConfig{URL: srv.URL, Model: "vision", Format: "openai"}}, APIKey: "secret"}
	e := v.TestConnection(context.Background(), "ocr")
	if e == nil || !strings.Contains(e.Error(), "HTTP 401") || strings.Contains(e.Error(), "private upstream") || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	if !strings.Contains(captured, "data:image/jpeg;base64,") || !strings.Contains(captured, "synthetic test image") {
		t.Fatal("not synthetic image test")
	}
	for _, u := range []string{"file:///tmp/key", "http://user:pass@localhost", "https://host/path?secret=x"} {
		c := v.Config
		c.OCR.URL = u
		if Validate(c) == nil {
			t.Fatal("bad URL accepted", u)
		}
	}
}
func TestIndexGenerationsKeepOldUntilReadyAndFenceEdits(t *testing.T) {
	ctx := context.Background()
	s := fixture(t)
	dimension := 3
	var hook func()
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hook != nil {
			f := hook
			hook = nil
			f()
		}
		if fail {
			http.Error(w, "private failure", 500)
			return
		}
		v := make([]float32, dimension)
		v[0] = 1
		json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{v}})
	}))
	defer srv.Close()
	v, _ := s.Load(ctx)
	v.Config.Embedding = EmbedConfig{Enabled: true, URL: srv.URL, Model: "first"}
	if e := s.Save(ctx, v); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_note_content(note_key,page,body_text,source) VALUES('forestnote://nb/page',0,'synthetic searchable text','forestnote')`); e != nil {
		t.Fatal(e)
	}
	var calls int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_embedding_work`).Scan(&calls); e != nil || calls != 0 {
		t.Fatal("save silently queued index", calls, e)
	}
	build := func() {
		t.Helper()
		v, _ := s.Load(ctx)
		if e := s.BuildIndex(ctx, v.Revision); e != nil {
			t.Fatal(e)
		}
	}
	step := func() {
		t.Helper()
		if found, e := s.ProcessIndex(ctx); e != nil || !found {
			t.Fatal(found, e)
		}
	}
	active := func() string {
		t.Helper()
		_, id, e := s.ActiveEmbedder(ctx)
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	build()
	if active() != "" {
		t.Fatal("partial index active")
	}
	step()
	old := active()
	if old == "" {
		t.Fatal("completed index not active")
	}
	v, _ = s.Load(ctx)
	v.Config.Embedding.Model = "second"
	if e := s.Save(ctx, v); e != nil {
		t.Fatal(e)
	}
	dimension = 7
	build()
	if active() != old {
		t.Fatal("old index lost during rebuild")
	}
	hook = func() {
		if _, e := s.DB.ExecContext(ctx, `UPDATE alexandria_note_content SET body_text='edited while embedding'`); e != nil {
			t.Error(e)
		}
	}
	step()
	if active() != old {
		t.Fatal("edit-raced replacement promoted")
	}
	// Both old and new generations receive the content edit. Give the original
	// endpoint its original dimension when processing the old generation.
	if _, e := s.DB.ExecContext(ctx, `UPDATE alexandria_embedding_work SET next_at=now()+interval '1 hour' WHERE generation=$1`, old); e != nil {
		t.Fatal(e)
	}
	step()
	if active() == old {
		t.Fatal("replacement not promoted")
	}
	var stale int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_embedding_vectors e JOIN alexandria_note_content c USING(note_key,page) WHERE e.generation=$1 AND e.body_hash<>md5(c.body_text)`, active()).Scan(&stale); e != nil || stale != 0 {
		t.Fatal("stale vectors", stale, e)
	}
	// Failed rebuilds never replace a usable index; explicit retry recovers.
	old = active()
	fail = true
	build()
	step()
	if active() != old {
		t.Fatal("failed generation replaced active index")
	}
	if _, e := s.DB.ExecContext(ctx, `UPDATE alexandria_embedding_work SET failed=true WHERE generation<>$1`, old); e != nil {
		t.Fatal(e)
	}
	fail = false
	if e := s.RetryIndex(ctx); e != nil {
		t.Fatal(e)
	}
	step()
	if active() == old {
		t.Fatal("retry did not activate replacement")
	}
}

func TestGenerationSearchExcludesStaleAndOtherSpaces(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0, 0}}})
	}))
	defer srv.Close()
	for _, q := range []string{`INSERT INTO fn_notebook(id,name,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('nb','Synthetic',1,1,'test')`, `INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('page','nb',0,1,1,'test')`, `INSERT INTO alexandria_note_content(note_key,page,body_text,source) VALUES('forestnote://nb/page',0,'synthetic archival astronomical observations','forestnote')`} {
		if _, e := s.DB.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	v, _ := s.Load(ctx)
	v.Config.Embedding = EmbedConfig{Enabled: true, URL: srv.URL, Model: "model"}
	if e := s.Save(ctx, v); e != nil {
		t.Fatal(e)
	}
	v, _ = s.Load(ctx)
	if e := s.BuildIndex(ctx, v.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ProcessIndex(ctx); e != nil {
		t.Fatal(e)
	}
	emb, id, e := s.ActiveEmbedder(ctx)
	if e != nil {
		t.Fatal(e)
	}
	search := notes.Searcher{DB: s.DB, Embedder: emb, Generation: id}
	hits, e := search.Search(ctx, "galaxies", 10, true)
	if e != nil || len(hits) != 1 {
		t.Fatal("generation vector search", len(hits), e)
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE alexandria_note_content SET body_text='changed independently'`); e != nil {
		t.Fatal(e)
	}
	hits, e = search.Search(ctx, "galaxies", 10, true)
	if e != nil || len(hits) != 0 {
		t.Fatal("stale vector searched", len(hits), e)
	}
}

func TestReprocessOnlyPreviouslyRecognizedClientPages(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	v, _ := s.Load(ctx)
	v.Config.OCR = OCRConfig{Enabled: true, URL: "http://unused.invalid", Model: "synthetic", Format: "openai"}
	if e := s.Save(ctx, v); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{`INSERT INTO fn_notebook(id,name,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('nb','Synthetic',1,1,'test')`, `INSERT INTO fn_page(id,notebook_id,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('seen','nb',1,1,'test'),('untouched','nb',1,1,'test')`, `INSERT INTO alexandria_page_ocr(page_id,input_hash,text) VALUES('seen',repeat('a',64),'synthetic')`} {
		if _, e := s.DB.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	v, _ = s.Load(ctx)
	if e := s.Reprocess(ctx, v.Revision); e != nil {
		t.Fatal(e)
	}
	var ids string
	if e := s.DB.QueryRowContext(ctx, `SELECT string_agg(page_id,',') FROM alexandria_page_dirty`).Scan(&ids); e != nil || ids != "seen" {
		t.Fatal("untouched pages queued", ids, e)
	}
}
