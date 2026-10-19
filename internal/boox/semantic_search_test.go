package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
)

func TestBOOXSemanticLifecycleAndSharedQuery(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(q string) int {
		t.Helper()
		var n int
		if err := s.DB.QueryRowContext(ctx, q).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	putNote := func(status int, pages any) {
		body, _ := json.Marshal(map[string]any{"uniqueId": "semantic-note", "title": "Synthetic semantic notebook", "type": 1, "status": status, "pageNameList": pages})
		exec(`INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('semantic-note','1-test','notebook','',$1,$2) ON CONFLICT(document_id) DO UPDATE SET body=excluded.body,revision='2-test'`, s.uid(), body)
	}
	ready := func(text string) {
		exec(`INSERT INTO boox_page_index(notebook_id,page_id,page_number,state,text,input_version) VALUES('semantic-note','stable-page',1,'ready',$1,'synthetic-version') ON CONFLICT(notebook_id,page_id) DO UPDATE SET state='ready',text=excluded.text,input_version=excluded.input_version`, text)
	}
	putNote(1, []string{"stable-page", "other"})
	ready("Botanical observations about plants in the garden and their seasonal flowering patterns.")
	if n := count(`SELECT count(*) FROM alexandria_note_content WHERE source='boox-page'`); n != 1 {
		t.Fatal("missing derived page", n)
	}
	// Reading children can arrive before their parent; no text leaks until it exists.
	child := putSearchRecord(t, s, s.uid(), "semantic-highlight", 1, 0, "semantic-book")
	exec(`UPDATE boox_projection SET body=body||'{"quote":"Botanical highlights about flowers and garden plants."}' WHERE document_id=$1`, child)
	if n := count(`SELECT count(*) FROM alexandria_note_content WHERE source='boox-reading'`); n != 0 {
		t.Fatal("orphan searchable", n)
	}
	parent := putSearchRecord(t, s, s.uid(), "semantic-book", 4, 0, "")
	if n := count(`SELECT count(*) FROM alexandria_note_content WHERE source='boox-reading'`); n != 2 {
		t.Fatal("parent arrival did not include child", n)
	}
	// One client page shares the same generation and query vector with BOOX.
	if err := identity.EnsureSite(ctx, s.DB); err != nil {
		t.Fatal(err)
	}
	if err := generation.Ensure(ctx, s.DB); err != nil {
		t.Fatal(err)
	}
	if _, err := (relay.Store{DB: s.DB}).AuthorOps(ctx, []relay.Op{
		{Table: "notebook", PK: "00000000000000000000000NB1", Cols: map[string]any{"name": "Synthetic client notebook", "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "folder_id": nil, "aspect_long_axis": nil, "page_width": nil, "page_height": nil}},
		{Table: "page", PK: "00000000000000000000000PG1", Cols: map[string]any{"notebook_id": "00000000000000000000000NB1", "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "template": nil, "template_pitch_mm": nil}},
	}); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO alexandria_note_content(note_key,page,body_text,source) VALUES('forestnote://00000000000000000000000NB1/00000000000000000000000PG1',0,'Botanical observations from the client notebook about a flourishing kitchen garden.','forestnote')`)
	var calls atomic.Int32
	var hook func()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if hook != nil {
			f := hook
			hook = nil
			f()
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"embeddings":[[1,0,0]]}`))
	}))
	defer upstream.Close()
	store := providers.Store{DB: s.DB, LibraryID: "semantic-test", Bootstrap: providers.Bootstrap{Key: bytes.Repeat([]byte{1}, 32), Defaults: providers.Config{OCR: providers.OCRConfig{Format: "openai"}, Embedding: providers.EmbedConfig{URL: upstream.URL, Model: "synthetic"}}}}
	if err := store.Ensure(ctx); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM alexandria_embedding_work`); n != 0 {
		t.Fatal("text ingress implicitly enabled work", n)
	}
	if err := store.EnableAndBuildIndex(ctx, 1); err != nil {
		t.Fatal(err)
	}
	drain := func() {
		t.Helper()
		for i := 0; i < 20; i++ {
			found, err := store.ProcessIndex(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !found {
				return
			}
		}
		t.Fatal("queue did not drain")
	}
	drain()
	client, generation, err := store.ActiveEmbedder(ctx)
	if err != nil || generation == "" {
		t.Fatal("no active generation", err)
	}
	search := notes.Searcher{DB: s.DB, Embedder: client, Generation: generation}
	before := calls.Load()
	results, err := search.SearchScoped(ctx, "verdant ecosystems", 50, true, "")
	if err != nil || len(results) != 4 || calls.Load() != before+1 {
		t.Fatal("global query", len(results), calls.Load()-before, err)
	}
	for _, r := range results {
		if (r.Source == "boox" && !strings.HasPrefix(r.URL, "/boox/")) || (r.Source == "client" && !strings.HasPrefix(r.URL, "/files/forestnote")) {
			t.Fatal("wrong source/link", r)
		}
	}
	results, err = search.SearchScoped(ctx, "plants", 50, true, "client")
	if err != nil || (len(results) != 1 || results[0].Source != "client") {
		t.Fatal("source filter leaked", results, err)
	}
	view, err := store.View(ctx)
	if err != nil || view.IndexedPages != 4 || view.BOOXWithoutText != 1 {
		t.Fatal("coverage", view.IndexedPages, view.BOOXWithoutText, err)
	}
	// The web and JSON surfaces use the same global/source-aware retrieval.
	before = calls.Load()
	w := httptest.NewRecorder()
	web.Handler(web.Deps{DB: s.DB, Search: search, Status: web.Status{NativeBOOX: true, Embedding: "synthetic"}}).ServeHTTP(w, httptest.NewRequest("GET", "/search?q=verdant+ecosystems", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Synthetic client notebook") || !strings.Contains(w.Body.String(), "Synthetic semantic notebook") || calls.Load() != before+1 {
		t.Fatal("global web search", w.Code, calls.Load()-before)
	}
	// Sorting and an out-of-range page still embed the query only once; all
	// result sources share the same candidates and current-source validation.
	for _, order := range []searchorder.Order{"title_asc", "title_desc", "created_asc", "created_desc"} {
		before = calls.Load()
		sorted, total, err := search.SearchPage(ctx, "verdant ecosystems", 50, 500, true, "", order)
		if err != nil || len(sorted) != total || total == 0 || calls.Load() != before+1 {
			t.Fatal("sorted hybrid", order, total, err, calls.Load()-before)
		}
	}

	w = httptest.NewRecorder()
	search.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/search?q=Botanical&source=boox&mode=keyword", nil))
	var resultBody struct{ Results []notes.Result }
	if err := json.Unmarshal(w.Body.Bytes(), &resultBody); err != nil || w.Code != 200 || len(resultBody.Results) != 2 {
		t.Fatal("scoped API", w.Code, err)
	}
	for _, hit := range resultBody.Results {
		if hit.Source != "boox" {
			t.Fatal("API source leak")
		}
	}
	// Parent-only soft deletion removes its child from text, vectors and search.
	exec(`UPDATE boox_projection SET body=jsonb_set(body,'{status}','1') WHERE document_id=$1`, parent)
	if n := count(`SELECT count(*) FROM alexandria_embedding_vectors`); n != 2 {
		t.Fatal("deleted reading vectors retained", n)
	}
	results, err = search.SearchScoped(ctx, "plants", 50, true, "boox")
	if err != nil || len(results) != 1 {
		t.Fatal("deleted parent/child leaked", len(results), err)
	}
	exec(`UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id=$1`, parent)
	drain()
	if n := count(`SELECT count(*) FROM alexandria_embedding_vectors`); n != 4 {
		t.Fatal("restore not embedded", n)
	}
	// Reordering invalidates recognition but keeps the identity when it completes.
	putNote(1, map[string]any{"pageNameList": []string{"other", "stable-page", "stable-page"}})
	if n := count(`SELECT count(*) FROM alexandria_note_content WHERE source='boox-page'`); n != 0 {
		t.Fatal("invalidated recognition remains searchable", n)
	}
	ready("Botanical observations about plants in the garden and their seasonal flowering patterns.")
	drain()
	results, err = search.SearchScoped(ctx, "Botanical observations", 50, false, "boox")
	if err != nil || len(results) != 1 || results[0].PageNumber != 2 || results[0].PageID != "stable-page" || !strings.Contains(results[0].URL, "page_id=stable-page") {
		t.Fatal("stable page location", results, err)
	}
	w = httptest.NewRecorder()
	s.Admin(w, httptest.NewRequest("GET", "/boox/notebook?id=semantic-note&page=1&page_id=stable-page", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `value="2"`) {
		t.Fatal("stable link did not follow reordered page", w.Code)
	}
	// Delete the page during its embedding request. The late vector must be rejected.
	ready("Changed botanical observations about plants, summer gardens and flowering patterns.")
	hook = func() { putNote(1, []string{"other"}) }
	drain()
	if n := count(`SELECT count(*) FROM alexandria_embedding_vectors WHERE note_key LIKE 'boox-page:%'`); n != 0 {
		t.Fatal("late response resurrected page", n)
	}
	w = httptest.NewRecorder()
	s.Admin(w, httptest.NewRequest("GET", "/boox/notebook?id=semantic-note&page=1&page_id=stable-page", nil))
	if w.Code != 404 {
		t.Fatal("removed stable link opened another page", w.Code)
	}
	// A stale recognition completion for the missing page is not searchable either.
	ready("Late recognition that must never reappear in search.")
	if n := count(`SELECT count(*) FROM alexandria_note_content WHERE source='boox-page'`); n != 0 {
		t.Fatal("late OCR resurrected page", n)
	}
	// A delete while embedding the query must also remove pre-fetched lexical hits.
	hook = func() {
		exec(`UPDATE boox_projection SET body=jsonb_set(body,'{status}','1') WHERE document_id=$1`, child)
	}
	results, err = search.SearchScoped(ctx, "Botanical", 50, true, "boox")
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range results {
		if hit.NotebookID == child {
			t.Fatal("query returned a deleted lexical candidate")
		}
	}
	exec(`UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id=$1`, child)
	drain()
	// An unavailable embedder still gives keyword results.
	upstream.Close()
	results, err = search.SearchScoped(ctx, "Botanical", 50, true, "boox")
	if err != nil || len(results) != 1 {
		t.Fatal("keyword fallback", results, err)
	}
	// Physical delete must invalidate a child even with its own retained revision.
	exec(`DELETE FROM boox_projection WHERE document_id=$1`, parent)
	if n := count(`SELECT count(*) FROM alexandria_search_live WHERE source='boox'`); n != 0 {
		t.Fatal("hard-delete visibility", n)
	}
}

func TestBOOXPageListRepresentationsForSearch(t *testing.T) {
	s := testService(t)
	for _, tc := range []struct {
		input string
		count int
	}{
		{`["a","b"]`, 2}, {`{"pageNameList":["a","b"]}`, 2}, {`"[\"a\",\"b\"]"`, 2},
		{`"{\"pageNameList\":[\"a\",\"b\"]}"`, 2}, {`"broken"`, 0}, {`null`, 0}, {`[1]`, 0}, {`{}`, 0},
	} {
		var n int
		if err := s.DB.QueryRowContext(context.Background(), `SELECT jsonb_array_length(boox_search_page_ids($1::jsonb))`, tc.input).Scan(&n); err != nil || n != tc.count {
			t.Fatal(tc.input, n, err)
		}
	}
}
