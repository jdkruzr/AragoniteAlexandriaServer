package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	ocrapi "github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/ocr"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/proto"
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
	// One typed-text shape, so the page isn't blank and goes to the model.
	putNoteBlob(t, s, "shape/page#ink#1.zip", inkShapeZip(t))
	return s
}

func inkShapeZip(t *testing.T) []byte {
	t.Helper()
	shape := &pb.ShapeInfoProto{UniqueId: "ink", ShapeType: 6, Text: "synthetic", BoundingRect: `{"left":10,"top":10,"right":90,"bottom":30}`, MatrixValues: `{"values":[1,0,0,0,1,0,0,0,1]}`, TextStyle: `{"textSize":10,"textSpacing":1}`, Color: -16777216}
	b, e := proto.Marshal(&pb.ShapeInfoProtoList{Proto: []*pb.ShapeInfoProto{shape}})
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	f, _ := z.Create("shapes")
	f.Write(b)
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
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
	if state != "blocked" || !strings.Contains(detail, "preview is incomplete: "+NoPageFilesWarning) || o.calls != 1 {
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

func TestRecognitionCancellationFencesProviderResult(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	var id string
	if e := s.DB.QueryRowContext(ctx, `SELECT job_id::text FROM boox_page_index`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	o := &testOCR{hook: func() {
		if _, e := (recognition.Store{DB: s.DB}).Control(ctx, id, false, "cancel"); e != nil {
			t.Fatal(e)
		}
	}}
	if _, e := s.ProcessPageIndex(ctx, o, ""); e != nil {
		t.Fatal(e)
	}
	var state, text string
	if e := s.DB.QueryRowContext(ctx, `SELECT state,text FROM boox_page_index`).Scan(&state, &text); e != nil {
		t.Fatal(e)
	}
	if state != "cancelled" || text != "" {
		t.Fatalf("late result published: %s %q", state, text)
	}
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_projection SET revision='2-b' WHERE document_id='note'`); e != nil {
		t.Fatal(e)
	}
	if e := s.DB.QueryRowContext(ctx, `SELECT state FROM boox_page_index`).Scan(&state); e != nil || state != "cancelled" {
		t.Fatal("source edit revived cancellation", state, e)
	}
}

type rejectedOCR struct{}

func (rejectedOCR) Model() string { return "synthetic" }
func (rejectedOCR) Recognize(context.Context, []byte, string) (string, error) {
	return "", &ocrapi.HTTPError{Status: 400}
}
func TestBOOXPermanentFailureAndExpiredLeaseRecovery(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_page_index SET state='processing',lease_token='crashed-worker',lease_until=now()-interval '1 second'`); e != nil {
		t.Fatal(e)
	}
	if found, e := s.ProcessPageIndex(ctx, rejectedOCR{}, ""); e != nil || !found {
		t.Fatal(found, e)
	}
	var state string
	var attempts int
	if e := s.DB.QueryRowContext(ctx, `SELECT state,attempts FROM boox_page_index`).Scan(&state, &attempts); e != nil || state != "failed" || attempts != 1 {
		t.Fatal(state, attempts, e)
	}
	if found, e := s.ProcessPageIndex(ctx, rejectedOCR{}, ""); e != nil || found {
		t.Fatal("permanent failure was retried", found, e)
	}
}

// A text notebook's blank handwriting page is never sent to the model; its
// typed text is kept with that page so search finds it.
func TestTextNotebookTypedTextIsSearchableWithoutRecognition(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	ids := newTextIDs()
	ids.Doc = "note"
	record, assets, e := buildTextNote(s.uid(), TextNote{Title: "Synthetic plan", Blocks: []TextBlock{{Kind: "heading", Text: "Goals"}, {Kind: "bullet", Text: "launch the zeppelin"}, {Label: "Owner:", Text: "nobody"}}}, ids, time.UnixMilli(1791669879703))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(record)
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('note','1-a','notebook','1',$1,$2)`, s.uid(), raw); e != nil {
		t.Fatal(e)
	}
	for _, a := range assets {
		if rel, ok := strings.CutPrefix(a.Key, s.uid()+"/note/note/"); ok {
			putNoteBlob(t, s, rel, a.Body)
		}
	}
	if e = s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	ocr := &testOCR{}
	if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || !found {
		t.Fatal(found, e)
	}
	var state, text, detail string
	s.DB.QueryRowContext(ctx, `SELECT state,text,detail FROM boox_page_index WHERE notebook_id='note'`).Scan(&state, &text, &detail)
	want := "Synthetic plan\nGoals\n• launch the zeppelin\nOwner: nobody"
	if state != "ready" || text != want || !strings.Contains(detail, "Typed text") || ocr.calls != 0 {
		t.Fatalf("%s %q %q calls=%d", state, text, detail, ocr.calls)
	}
	d := web.Deps{DB: s.DB, Search: notes.Searcher{DB: s.DB}, Status: web.Status{NativeBOOX: true}}
	w := httptest.NewRecorder()
	web.Handler(d).ServeHTTP(w, httptest.NewRequest("GET", "/search?q=zeppelin&source=boox", nil))
	if !strings.Contains(w.Body.String(), "Synthetic plan") {
		t.Fatal("typed text not searchable")
	}
}

func TestRichTextReadsEditorDocuments(t *testing.T) {
	got := booxpage.RichText(richTextDocument("Title", []TextBlock{{Text: "para"}, {Kind: "bullet", Text: "one"}, {Kind: "bullet", Label: "Two:", Text: "b"}}))
	if got != "Title\npara\n• one\n• Two: b" {
		t.Fatalf("%q", got)
	}
	if booxpage.RichText([]byte("<p>old</p><p>html</p>")) != "old\n\nhtml" {
		t.Fatal("html fallback", booxpage.RichText([]byte("<p>old</p><p>html</p>")))
	}
}

func TestBlankPageSkipsRecognition(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	if _, e := s.DB.ExecContext(ctx, `DELETE FROM boox_blob_live WHERE native_key LIKE '%/shape/%'`); e != nil {
		t.Fatal(e)
	}
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	ocr := &testOCR{}
	if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || !found {
		t.Fatal(found, e)
	}
	var state, text, detail string
	s.DB.QueryRowContext(ctx, `SELECT state,text,detail FROM boox_page_index WHERE notebook_id='note'`).Scan(&state, &text, &detail)
	if state != "ready" || text != "" || !strings.HasPrefix(detail, "No handwriting") || ocr.calls != 0 {
		t.Fatalf("%s %q %q calls=%d", state, text, detail, ocr.calls)
	}
}

func TestPageWithNoFilesStaysBlockedWithAPlainReason(t *testing.T) {
	// Seen on device: a notebook created and never written has no shapes and
	// no template. From the server it looks like a page still syncing, so it
	// stays blocked, and says so.
	s := indexFixture(t)
	ctx := context.Background()
	if _, e := s.DB.ExecContext(ctx, `DELETE FROM boox_blob_live WHERE native_key LIKE '%/shape/%' OR native_key LIKE '%/template/%'`); e != nil {
		t.Fatal(e)
	}
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	ocr := &testOCR{}
	if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || !found {
		t.Fatal(found, e)
	}
	var state, detail string
	s.DB.QueryRowContext(ctx, `SELECT state,detail FROM boox_page_index WHERE notebook_id='note'`).Scan(&state, &detail)
	if state != "blocked" || !strings.Contains(detail, NoPageFilesWarning) || ocr.calls != 0 {
		t.Fatal(state, detail, ocr.calls)
	}
}

func TestRetryLimitKeepsTheLastProblem(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	// The fifth failure stops retrying; it must say what went wrong, not
	// blame the provider, and must not leak the provider's raw error.
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_page_index SET attempts=4`); e != nil {
		t.Fatal(e)
	}
	if found, e := s.ProcessPageIndex(ctx, &testOCR{fail: true}, ""); e != nil || !found {
		t.Fatal(found, e)
	}
	var state, detail string
	s.DB.QueryRowContext(ctx, `SELECT state,detail FROM boox_page_index WHERE notebook_id='note'`).Scan(&state, &detail)
	if state != "failed" || !strings.HasPrefix(detail, "Stopped after 5 attempts. Last problem: ") || len(detail) < 60 || strings.Contains(detail, "private") {
		t.Fatal(state, detail)
	}
}

func TestBlockedDetailNamesWarnings(t *testing.T) {
	d := blockedDetail([]string{"Native shape type 77 is not rendered yet.", "Native shape type 77 is not rendered yet.", "A shape references an unknown layer."})
	if d != "Not sent for recognition because the preview is incomplete: Native shape type 77 is not rendered yet. A shape references an unknown layer." {
		t.Fatal(d)
	}
	if long := blockedDetail([]string{strings.Repeat("é", 400)}); len(long) > 610 || !utf8.ValidString(long) {
		t.Fatal(len(long))
	}
}

func TestBlockedCoverageReportsOffendingShapes(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	odd := &pb.ShapeInfoProto{UniqueId: "odd", ShapeType: 77, Text: "private words", Extra: `{"mystery":1}`}
	b, _ := proto.Marshal(&pb.ShapeInfoProtoList{Proto: []*pb.ShapeInfoProto{odd}})
	var z bytes.Buffer
	w := zip.NewWriter(&z)
	f, _ := w.Create("shapes")
	f.Write(b)
	w.Close()
	putNoteBlob(t, s, "shape/page#odd#2.zip", z.Bytes())
	if e := s.QueuePage(ctx, "note", 1); e != nil {
		t.Fatal(e)
	}
	ocr := &testOCR{}
	if found, e := s.ProcessPageIndex(ctx, ocr, ""); e != nil || !found || ocr.calls != 0 {
		t.Fatal(found, e, ocr.calls)
	}
	var detail string
	s.DB.QueryRowContext(ctx, `SELECT detail FROM boox_page_index WHERE state='blocked'`).Scan(&detail)
	if !strings.Contains(detail, "Native shape type 77") {
		t.Fatal("detail", detail)
	}
	report, e := s.BlockedCoverage(ctx, false)
	if e != nil || len(report) != 1 || report[0].Page != 1 || len(report[0].Issues) != 1 || report[0].Issues[0].ShapeType != 77 || report[0].ShapeTypes["6"] != 1 || report[0].Error != "" {
		t.Fatalf("%+v %v", report, e)
	}
	if raw, _ := json.Marshal(report); bytes.Contains(raw, []byte("private")) {
		t.Fatal("report leaked text")
	}
}
