package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

type testOCR struct {
	calls int
	hook  func()
	fail  bool
}

func (o *testOCR) Model() string { return "synthetic-recognizer" }
func (o *testOCR) Recognize(ctx context.Context, b []byte, p string) (string, error) {
	o.calls++
	if o.hook != nil {
		o.hook()
	}
	if o.fail {
		return "", errors.New("private provider detail")
	}
	return "synthetic quasar transcript", nil
}
func indexFixture(t *testing.T) Service {
	t.Helper()
	s := testService(t)
	ctx := context.Background()
	m := map[string]any{"uniqueId": "note", "title": "Synthetic", "type": 1, "status": 1, "pageNameList": []string{"page"}, "notePageInfo": map[string]any{"pageInfoMap": map[string]any{"page": map[string]any{"width": 100, "height": 100}}}}
	raw, _ := json.Marshal(m)
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('note','1-a','notebook','1',$1,$2)`, s.uid(), raw); e != nil {
		t.Fatal(e)
	}
	b := []byte(`{"properties":{"layoutType":"LayoutBlank"}}`)
	h := hash(b)
	if _, e := s.Objects.Put(ctx, "boox/bodies/"+h, "application/json", bytes.NewReader(b), int64(len(b))); e != nil {
		t.Fatal(e)
	}
	var v int64
	k := s.uid() + "/note/note/template/json/page.template_json"
	if e := s.DB.QueryRowContext(ctx, `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes) VALUES($1,$2,$3,$4) RETURNING id`, k, h, strings.Repeat("0", 32), len(b)).Scan(&v); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_blob_live(native_key,version_id) VALUES($1,$2)`, k, v); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestPageRecognitionCacheInvalidationAndSearch(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	ocr := &testOCR{}
	step := func() {
		t.Helper()
		if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || !found {
			t.Fatalf("step %v %v", found, e)
		}
	}
	if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || found {
		t.Fatal("implicit OCR work", e)
	}
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	step()
	var state, body string
	read := func() {
		t.Helper()
		if e := s.DB.QueryRowContext(ctx, `SELECT state,text FROM boox_page_index`).Scan(&state, &body); e != nil {
			t.Fatal(e)
		}
	}
	read()
	if state != "ready" || body != "synthetic quasar transcript" || ocr.calls != 1 {
		t.Fatal(state, body, ocr.calls)
	}
	s.QueuePage(ctx, "note", 1)
	step()
	if ocr.calls != 1 {
		t.Fatal("unchanged input re-recognized")
	}
	d := web.Deps{DB: s.DB, Search: notes.Searcher{DB: s.DB}, Status: web.Status{NativeBOOX: true}}
	w := httptest.NewRecorder()
	web.Handler(d).ServeHTTP(w, httptest.NewRequest("GET", "/search?q=quasar&source=boox", nil))
	if !strings.Contains(w.Body.String(), "synthetic quasar transcript") {
		t.Fatal("recognition not searchable", w.Body.String())
	}
	ocr.hook = func() {
		_, e := s.DB.ExecContext(ctx, `UPDATE boox_projection SET revision='2-edit',body=jsonb_set(body,'{title}','"Changed"') WHERE document_id='note'`)
		if e != nil {
			t.Fatal(e)
		}
	}
	// Change prompt so the provider runs and races a native edit.
	s.QueuePage(ctx, "note", 1)
	if _, e := s.ProcessPageIndex(ctx, ocr, "different prompt"); e != nil {
		t.Fatal(e)
	}
	read()
	if state != "queued" || body != "" {
		t.Fatal("stale inference published", state, body)
	}
	w = httptest.NewRecorder()
	web.Handler(d).ServeHTTP(w, httptest.NewRequest("GET", "/search?q=quasar&source=boox", nil))
	if strings.Contains(w.Body.String(), "synthetic quasar transcript") {
		t.Fatal("stale text searchable")
	}
	ocr.hook = nil
	s.QueuePage(ctx, "note", 1)
	step()
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id='note'`); e != nil {
		t.Fatal(e)
	}
	read()
	if state != "queued" || body != "" {
		t.Fatal("deleted text retained in index")
	}
}
func TestPageRecognitionConfigurationWarningsAndRetry(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	s.QueuePage(ctx, "note", 1)
	if _, e := s.ProcessPageIndex(ctx, nil, ""); e != nil {
		t.Fatal(e)
	}
	var state, detail string
	s.DB.QueryRowContext(ctx, `SELECT state,detail FROM boox_page_index`).Scan(&state, &detail)
	if state != "blocked" || !strings.Contains(detail, "provider") {
		t.Fatal(state, detail)
	}
	s.QueuePage(ctx, "note", 1)
	o := &testOCR{fail: true}
	s.ProcessPageIndex(ctx, o, "")
	s.DB.QueryRowContext(ctx, `SELECT state,detail FROM boox_page_index`).Scan(&state, &detail)
	if state != "queued" || strings.Contains(detail, "private") {
		t.Fatal(state, detail)
	}
	if _, e := s.DB.ExecContext(ctx, `DELETE FROM boox_blob_live`); e != nil {
		t.Fatal(e)
	}
	s.QueuePage(ctx, "note", 1)
	o.fail = false
	s.ProcessPageIndex(ctx, o, "")
	s.DB.QueryRowContext(ctx, `SELECT state,detail FROM boox_page_index`).Scan(&state, &detail)
	if state != "blocked" || !strings.Contains(detail, "coverage") || o.calls != 1 {
		t.Fatal("incomplete page recognized", state, detail, o.calls)
	}
}

func TestPreviewRecoversResourceWithoutNewNativeRevision(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	n, e := s.notebookSnapshot(ctx, "note")
	if e != nil {
		t.Fatal(e)
	}
	b := []byte(`{"properties":{"layoutType":"LayoutBlank"}}`)
	key := "boox/bodies/" + hash(b)
	if e = s.Objects.Delete(ctx, key); e != nil {
		t.Fatal(e)
	}
	before, e := s.renderPreview(ctx, n, 1)
	if e != nil || len(before.Warnings) == 0 {
		t.Fatal("missing body did not report coverage gap", e)
	}
	if _, e = s.Objects.Put(ctx, key, "application/json", bytes.NewReader(b), int64(len(b))); e != nil {
		t.Fatal(e)
	}
	after, e := s.renderPreview(ctx, n, 1)
	if e != nil || len(after.Warnings) != 0 {
		t.Fatal("transient coverage gap cached after recovery", e, after.Warnings)
	}
}
