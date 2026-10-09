package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

func TestReadOnlyNotebookBrowserSnapshotAndSearch(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	meta := map[string]any{"uniqueId": "note", "title": "Synthetic Browser Note", "type": 1, "status": 1, "parentUniqueId": "folder", "pageNameList": map[string]any{"pageNameList": []string{"page"}}, "notePageInfo": map[string]any{"pageInfoMap": map[string]any{"page": map[string]any{"width": 100, "height": 120}}}}
	insert := func(id string, m map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(m)
		_, e := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-a','notebook','1',$2,$3)`, id, s.uid(), raw)
		if e != nil {
			t.Fatal(e)
		}
	}
	insert("folder", map[string]any{"uniqueId": "folder", "title": "Synthetic folder", "type": 0, "status": 1})
	insert("note", meta)
	insert("deleted", map[string]any{"uniqueId": "deleted", "title": "Synthetic deleted", "type": 1, "status": 0})
	body := []byte(`{"properties":{"layoutType":"LayoutBlank"}}`)
	sha := hash(body)
	_, e := s.Objects.Put(ctx, "boox/bodies/"+sha, "application/json", bytes.NewReader(body), int64(len(body)))
	if e != nil {
		t.Fatal(e)
	}
	key := s.uid() + "/note/note/template/json/page.template_json"
	var vid int64
	e = s.DB.QueryRowContext(ctx, `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes) VALUES($1,$2,$3,$4) RETURNING id`, key, sha, strings.Repeat("0", 32), len(body)).Scan(&vid)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO boox_blob_live(native_key,version_id)VALUES($1,$2)`, key, vid); e != nil {
		t.Fatal(e)
	}
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Admin(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w
	}
	root := get("/boox").Body.String()
	if !strings.Contains(root, "Synthetic folder") || strings.Contains(root, "Synthetic Browser Note") || strings.Contains(root, "Synthetic deleted") {
		t.Fatal("root/child/deletion isolation")
	}
	folder := get("/boox?folder=folder").Body.String()
	if !strings.Contains(folder, "Synthetic Browser Note") {
		t.Fatal("folder missing child")
	}
	page := get("/boox/notebook?id=note").Body.String()
	if !strings.Contains(page, "/boox/page.png") || strings.Contains(page, "preview is not available") {
		t.Fatal("blank native page missing preview")
	}
	n, e := s.notebookSnapshot(ctx, "note")
	if e != nil {
		t.Fatal(e)
	}
	p := get("/boox/page.png?id=note&page=1&version=" + n.Version)
	if !bytes.HasPrefix(p.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatal("not png")
	}
	// Later native publication must not replace an already displayed snapshot.
	if _, e = s.DB.ExecContext(ctx, `UPDATE boox_projection SET revision='2-b' WHERE document_id='note'`); e != nil {
		t.Fatal(e)
	}
	again := get("/boox/page.png?id=note&page=1&version=" + n.Version)
	if !bytes.Equal(p.Body.Bytes(), again.Body.Bytes()) {
		t.Fatal("displayed snapshot mutated")
	}
	var operations, versions int
	s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_operation`).Scan(&operations)
	s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_blob_version`).Scan(&versions)
	if operations != 0 || versions != 1 {
		t.Fatal("viewer wrote native state")
	}
	handler := web.Handler(web.Deps{DB: s.DB, Objects: s.Objects, Search: notes.Searcher{DB: s.DB}, Status: web.Status{NativeBOOX: true}})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/search?q=Synthetic", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Synthetic Browser Note") || strings.Contains(w.Body.String(), "Synthetic deleted") {
		t.Fatalf("global search: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/search?q=Synthetic&source=client", nil))
	if w.Code != 200 || strings.Contains(w.Body.String(), "Synthetic Browser Note") {
		t.Fatal("client filter leaks native data")
	}
	for _, path := range []string{"/boox/reading", "/boox/settings", "/boox/devices", "/boox/activity"} {
		get(path)
	}
}
func TestPreviewCacheLibraryIsolation(t *testing.T) {
	a := Service{LibraryID: "A"}
	b := Service{LibraryID: "B"}
	storePreview(a.previewKey("note", "rev", 1), previewResult{PNG: []byte("private")})
	if _, ok := cachedPreview(b.previewKey("note", "rev", 1)); ok {
		t.Fatal("library cache leak")
	}
}
