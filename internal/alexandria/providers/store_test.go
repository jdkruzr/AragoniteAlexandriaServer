package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestWorkspaceDiagnosticDoesNotExposeProviderBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"message":"Send anthropic-workspace-id header for this API key. private-secret"}}`))
	}))
	defer srv.Close()
	v := Snapshot{Config: Config{OCR: OCRConfig{URL: srv.URL, Model: "model", Format: "anthropic"}}}
	err := v.TestConnection(context.Background(), "ocr")
	if err == nil || !strings.Contains(err.Error(), "workspace ID") || strings.Contains(err.Error(), "private-secret") {
		t.Fatalf("unsafe or missing diagnostic: %v", err)
	}
}

func TestWorkspaceValidation(t *testing.T) {
	for _, id := range []string{"", "wrkspc_test123"} {
		if err := Validate(Config{OCR: OCRConfig{Format: "anthropic", AnthropicWorkspace: id}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"wrkspc_", "invalid", "wrkspc_x\r\nInjected: true"} {
		if err := Validate(Config{OCR: OCRConfig{Format: "anthropic", AnthropicWorkspace: id}}); err == nil {
			t.Fatalf("accepted %q", id)
		}
	}
}

func TestMinutesProvidersKeepSeparateCredentialsAndRecognitionSettings(t *testing.T) {
	ctx := context.Background()
	s := fixture(t)
	v, _ := s.Load(ctx)
	v.Config.OCR = OCRConfig{Enabled: true, URL: "http://ocr.local", Model: "vision", Format: "openai"}
	c, e := s.Candidate(ctx, v.Revision, v.Config, "private-ocr-key", "keep")
	if e != nil || s.Save(ctx, c) != nil {
		t.Fatal(e)
	}
	v, _ = s.Load(ctx)
	t1 := TranscriptionConfig{Enabled: true, URL: " http://whisper.local:8090/ ", Model: "whisper", Language: "EN"}
	m1 := SummaryConfig{Enabled: true, URL: "http://llm.local", Model: "qwen", Format: "openai", VLLMDisableThinking: true, AnthropicWorkspace: "wrkspc_ignored", SendToBOOX: true, Instructions: " Be terse.\r\nUse British spelling.\r\n"}
	c, e = s.MinutesCandidate(ctx, v.Revision, t1, m1, KeyChange{"private-stt-key", "keep"}, KeyChange{"private-llm-key", "replace"})
	if e != nil {
		t.Fatal(e)
	}
	if c.Config.OCR != v.Config.OCR || c.APIKey != "private-ocr-key" || c.Config.Transcription.URL != "http://whisper.local:8090" || c.Config.Transcription.Language != "en" || c.Config.Summary.AnthropicWorkspace != "" || c.Config.Summary.Instructions != "Be terse.\nUse British spelling." {
		t.Fatalf("candidate %+v", c.Config)
	}
	if e = s.Save(ctx, c); e != nil {
		t.Fatal(e)
	}
	var tb, sb []byte
	s.DB.QueryRowContext(ctx, `SELECT transcription_credential,summary_credential FROM alexandria_provider_settings`).Scan(&tb, &sb)
	if bytes.Contains(tb, []byte("private")) || bytes.Contains(sb, []byte("private")) {
		t.Fatal("plaintext credential")
	}
	// Ciphertexts are bound to their purpose: swapping columns must not decrypt.
	if _, e = s.open(tb, purposeSummary); e == nil {
		t.Fatal("credential purpose not bound")
	}
	got, e := s.Load(ctx)
	if e != nil || got.APIKey != "private-ocr-key" || got.TranscriptionKey != "private-stt-key" || got.SummaryKey != "private-llm-key" {
		t.Fatal("credentials", e)
	}
	// Saving the recognition form keeps the minutes providers.
	got.Config.OCR.Model = "vision-2"
	c, e = s.Candidate(ctx, got.Revision, Config{OCR: got.Config.OCR, Embedding: got.Config.Embedding}, "", "keep")
	if e != nil || s.Save(ctx, c) != nil {
		t.Fatal(e)
	}
	got, _ = s.Load(ctx)
	if got.Config.Transcription.Model != "whisper" || got.Config.Summary.Model != "qwen" || !got.Config.Summary.SendToBOOX || got.Config.Summary.Instructions != "Be terse.\nUse British spelling." || got.SummaryKey != "private-llm-key" || got.Config.OCR.Model != "vision-2" {
		t.Fatalf("recognition save dropped minutes settings %+v", got.Config)
	}
	view, _ := s.View(ctx)
	if !view.HasTranscriptionKey || !view.HasSummaryKey {
		t.Fatal("view key flags")
	}
	// Saving the built-in text unchanged keeps following the default.
	m2 := m1
	m2.Instructions = strings.ReplaceAll(minutes.DefaultInstructions, "\n", "\r\n") + "\r\n"
	if c, e = s.MinutesCandidate(ctx, got.Revision, t1, m2, KeyChange{"", "keep"}, KeyChange{"", "keep"}); e != nil || c.Config.Summary.Instructions != "" {
		t.Fatal("default instructions stored as custom", e)
	}
	m2.Instructions = strings.Repeat("é", minutes.MaxInstructionRunes+1)
	if _, e = s.MinutesCandidate(ctx, got.Revision, t1, m2, KeyChange{"", "keep"}, KeyChange{"", "keep"}); e == nil {
		t.Fatal("oversized instructions accepted")
	}
	// Moving an endpoint requires an explicit key decision.
	t2 := t1
	t2.URL = "http://elsewhere.local"
	if _, e = s.MinutesCandidate(ctx, got.Revision, t2, m1, KeyChange{"", "keep"}, KeyChange{"", "keep"}); e == nil || !strings.Contains(e.Error(), "transcription endpoint") {
		t.Fatal("endpoint move kept key", e)
	}
	for _, bad := range []struct {
		t TranscriptionConfig
		m SummaryConfig
	}{
		{TranscriptionConfig{Enabled: true}, SummaryConfig{}},
		{TranscriptionConfig{Language: "en-US"}, SummaryConfig{}},
		{TranscriptionConfig{}, SummaryConfig{Enabled: true, URL: "http://x", Model: "m"}},
		{TranscriptionConfig{}, SummaryConfig{Format: "anthropic", VLLMDisableThinking: true}},
		{TranscriptionConfig{URL: "ftp://x"}, SummaryConfig{}},
	} {
		if _, e = s.MinutesCandidate(ctx, got.Revision, bad.t, bad.m, KeyChange{"", "keep"}, KeyChange{"", "keep"}); e == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if _, e = s.MinutesCandidate(ctx, got.Revision-1, t1, m1, KeyChange{"", "keep"}, KeyChange{"", "keep"}); e != ErrStale {
		t.Fatal("stale revision", e)
	}
}

func TestMinutesConnectionTestsUseSyntheticInputs(t *testing.T) {
	var sawAudio, sawChat bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			f, h, err := r.FormFile("file")
			if err == nil {
				b := make([]byte, 4)
				f.Read(b)
				sawAudio = string(b) == "RIFF" && h.Size == 32044
			}
			w.Write([]byte(`{"text":"","segments":[]}`))
		case "/v1/chat/completions":
			var body struct {
				Messages []struct{ Role string } `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			sawChat = len(body.Messages) == 2 && body.Messages[0].Role == "system"
			w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
		default:
			http.Error(w, "private provider detail", 404)
		}
	}))
	defer srv.Close()
	v := Snapshot{Config: Config{Transcription: TranscriptionConfig{URL: srv.URL, Model: "w"}, Summary: SummaryConfig{URL: srv.URL, Model: "m", Format: "openai"}}}
	if e := v.TestConnection(context.Background(), "transcription"); e != nil || !sawAudio {
		t.Fatal(e, sawAudio)
	}
	if e := v.TestConnection(context.Background(), "summary"); e != nil || !sawChat {
		t.Fatal(e, sawChat)
	}
	v.Config.Summary.Format = "anthropic"
	if e := v.TestConnection(context.Background(), "summary"); e == nil || strings.Contains(e.Error(), "private") || !strings.Contains(e.Error(), "Summary model test failed (HTTP 404)") {
		t.Fatal(e)
	}
}

func TestFirstBuildReportsItsOwnProgress(t *testing.T) {
	ctx := context.Background()
	s := fixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{1, 0, 0}}})
	}))
	defer srv.Close()
	v, _ := s.Load(ctx)
	v.Config.Embedding = EmbedConfig{Enabled: true, URL: srv.URL, Model: "first"}
	if e := s.Save(ctx, v); e != nil {
		t.Fatal(e)
	}
	for i := range 3 {
		if _, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_note_content(note_key,page,body_text,source) VALUES($1,0,'synthetic searchable text','forestnote')`, fmt.Sprintf("forestnote://nb/p%d", i)); e != nil {
			t.Fatal(e)
		}
	}
	v, _ = s.Load(ctx)
	if e := s.BuildIndex(ctx, v.Revision); e != nil {
		t.Fatal(e)
	}
	if found, e := s.ProcessIndex(ctx); e != nil || !found {
		t.Fatal(found, e)
	}
	// Seen live: a first build said "0 records indexed" while it was well
	// under way, because only an active index was counted.
	if _, e := s.DB.ExecContext(ctx, `UPDATE alexandria_embedding_generation SET created_at=now()-interval '5 minutes' WHERE state='building'`); e != nil {
		t.Fatal(e)
	}
	view, e := s.View(ctx)
	if e != nil || view.IndexState != "building" || view.BuildDone != 1 || view.BuildPending != 2 || view.BuildPercent() != 33 || view.IndexedPages != 0 {
		t.Fatal(e, view.IndexState, view.BuildDone, view.BuildPending, view.IndexedPages)
	}
	if view.BuildPerMinute < 0.15 || view.BuildPerMinute > 0.25 || view.BuildETA.Before(time.Now().Add(9*time.Minute)) {
		t.Fatal(view.BuildPerMinute, view.BuildETA)
	}
}
