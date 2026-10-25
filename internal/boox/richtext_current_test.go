package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

var testTime = time.UnixMilli(1791669879703)

// insertTextNote stores a server-built text note as notebook "note".
func insertTextNote(t *testing.T, s Service, record map[string]any, assets []textAsset) {
	t.Helper()
	raw, _ := json.Marshal(record)
	if _, e := s.DB.ExecContext(context.Background(), `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('note','1-a','notebook','1',$1,$2)`, s.uid(), raw); e != nil {
		t.Fatal(e)
	}
	for _, a := range assets {
		if rel, ok := strings.CutPrefix(a.Key, s.uid()+"/note/note/"); ok {
			putNoteBlob(t, s, rel, a.Body)
		}
	}
}

// noteAsset is the body of a note asset whose key ends with suffix.
func noteAsset(t *testing.T, assets []textAsset, match func(string) bool) (string, []byte) {
	t.Helper()
	for _, a := range assets {
		if match(a.Key) {
			return a.Key, a.Body
		}
	}
	t.Fatal("asset missing")
	return "", nil
}

// noteBlobs writes blobs into notebook "note".
func noteBlobs(t *testing.T, s Service) func(rel string, b []byte) {
	return func(rel string, b []byte) { putNoteBlob(t, s, rel, b) }
}

// blobsIn writes blobs into any notebook, keyed relative to its folder.
func blobsIn(t *testing.T, s Service, doc string) func(rel string, b []byte) {
	return func(rel string, b []byte) {
		t.Helper()
		ctx := context.Background()
		h := hash(b)
		if _, e := s.Objects.Put(ctx, "boox/bodies/"+h, "application/octet-stream", bytes.NewReader(b), int64(len(b))); e != nil {
			t.Fatal(e)
		}
		k := s.uid() + "/note/" + doc + "/" + rel
		var v int64
		if e := s.DB.QueryRowContext(ctx, `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes) VALUES($1,$2,$3,$4) RETURNING id`, k, h, strings.Repeat("0", 32), len(b)).Scan(&v); e != nil {
			t.Fatal(e)
		}
		if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_blob_live(native_key,version_id) VALUES($1,$2) ON CONFLICT(native_key) DO UPDATE SET version_id=excluded.version_id`, k, v); e != nil {
			t.Fatal(e)
		}
	}
}

// deviceSave mimics a device saving a text note it has opened: a new editor
// file, a resource list restating the previous file as removed and adding the
// new one, and a new revision archive of the text shape. A save that only
// moved the cursor keeps the editor content.
func deviceSave(t *testing.T, put func(string, []byte), assets []textAsset, prev, next string, at int64, editor []byte) {
	t.Helper()
	put("resource/data/"+next+".html", editor)
	var list []byte
	for _, r := range []struct {
		id      string
		created int64
		status  uint64
	}{{prev, 1, booxpage.ResourceRemoved}, {next, at, 0}} {
		var e []byte
		e = pbString(e, 1, r.id)
		e = pbString(e, 2, "note")
		e = pbVarint(e, 3, uint64(r.created))
		e = pbVarint(e, 4, uint64(at))
		e = pbVarint(e, 7, booxpage.ResourceRichText)
		e = pbString(e, 8, "/"+r.id+".html")
		if r.status != 0 {
			e = pbVarint(e, 13, r.status)
		}
		list = pbBytes(list, 1, e)
	}
	ts := strconv.FormatInt(at, 10)
	put("resource/pb/"+next+"-list#"+ts, list)
	key, zipped := noteAsset(t, assets, func(k string) bool { return strings.Contains(k, "/shape/") })
	_, rest, _ := strings.Cut(key, "/shape/")
	page, _, _ := strings.Cut(rest, "#")
	put("shape/"+page+"#"+next+"-rev#"+ts+".zip", zipped)
}

func cursorMoved(editor []byte, pos int) []byte {
	return bytes.Replace(editor, []byte(`"currentPos":1,`), []byte(`"currentPos":`+strconv.Itoa(pos)+`,`), 1)
}

// Two devices open a server-written note and move the cursor; each save
// leaves its predecessor in place. Only the newest editor file is current:
// search, the inventory and the notebook page each show the text once.
func TestTypedTextUsesOnlyTheCurrentSave(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	ids := newTextIDs()
	ids.Doc = "note"
	record, assets, e := buildTextNote(s.uid(), TextNote{Title: "Synthetic plan", Blocks: []TextBlock{{Kind: "heading", Text: "Goals"}, {Kind: "bullet", Text: "launch the zeppelin"}}}, ids, testTime)
	if e != nil {
		t.Fatal(e)
	}
	insertTextNote(t, s, record, assets)
	_, editor := noteAsset(t, assets, func(k string) bool { return strings.HasSuffix(k, ".html") })
	if !bytes.Contains(cursorMoved(editor, 9), []byte(`"currentPos":9,`)) {
		t.Fatal("fixture: no cursor position to move")
	}
	deviceSave(t, noteBlobs(t, s), assets, ids.Resource, "second", testTime.UnixMilli()+1000, cursorMoved(editor, 9))
	deviceSave(t, noteBlobs(t, s), assets, "second", "third", testTime.UnixMilli()+2000, cursorMoved(editor, 30))

	n, e := s.notebookSnapshot(ctx, "note")
	if e != nil {
		t.Fatal(e)
	}
	docs, warning := s.currentRichText(ctx, n)
	if warning != "" || len(docs) != 1 || docs[0].Key != "resource/data/third.html" {
		t.Fatalf("%+v %q", docs, warning)
	}
	if text := s.typedText(ctx, n); strings.Count(text, "launch the zeppelin") != 1 {
		t.Fatalf("search text %q", text)
	}
	inv, e := s.RichTextInventory(ctx)
	if e != nil || inv.Resources != 1 {
		t.Fatalf("inventory %+v %v", inv, e)
	}

	w := httptest.NewRecorder()
	s.browser(w, httptest.NewRequest("GET", "/boox/notebook?id=note&page=1", nil))
	page := w.Body.String()
	if w.Code != 200 || strings.Count(page, "launch the zeppelin") != 1 || !strings.Contains(page, `<article class="typed-text"`) || !strings.Contains(page, "<h2>Synthetic plan</h2>") {
		t.Fatalf("%d\n%s", w.Code, page)
	}
	// The handwriting page is blank, so no page image; the typed text needs
	// its generated styles allowed, nothing else.
	if strings.Contains(page, "/boox/page.png") || !strings.Contains(w.Header().Get("Content-Security-Policy"), "style-src-attr 'unsafe-inline'") {
		t.Fatalf("page image or CSP: %q", w.Header().Get("Content-Security-Policy"))
	}
	if strings.Contains(page, booxpage.RichTextNotebookWarning) {
		t.Fatal("rich-text warning kept although the text is shown")
	}
}

// The newest save stands alone when the history is ambiguous; legacy notes
// without resource lists keep every editor file.
func TestTypedTextFallbacks(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	ids := newTextIDs()
	ids.Doc = "note"
	record, assets, e := buildTextNote(s.uid(), TextNote{Title: "Synthetic plan", Blocks: []TextBlock{{Kind: "paragraph", Text: "first text"}}}, ids, testTime)
	if e != nil {
		t.Fatal(e)
	}
	insertTextNote(t, s, record, assets)
	_, editor := noteAsset(t, assets, func(k string) bool { return strings.HasSuffix(k, ".html") })
	at := testTime.UnixMilli() + 1000
	deviceSave(t, noteBlobs(t, s), assets, ids.Resource, "second", at, bytes.Replace(editor, []byte("first text"), []byte("second text"), 1))
	// Another list claims the same moment with a different record.
	var e2 []byte
	e2 = pbString(e2, 1, "second")
	e2 = pbVarint(e2, 3, uint64(at))
	e2 = pbVarint(e2, 4, uint64(at))
	e2 = pbVarint(e2, 7, booxpage.ResourceRichText)
	e2 = pbString(e2, 8, "/other.html")
	putNoteBlob(t, s, "resource/pb/conflict#"+strconv.FormatInt(at, 10), pbBytes(nil, 1, e2))
	n, _ := s.notebookSnapshot(ctx, "note")
	docs, warning := s.currentRichText(ctx, n)
	if warning != RichTextAmbiguousWarning || len(docs) != 1 || docs[0].Key != "resource/data/second.html" {
		t.Fatalf("%+v %q", docs, warning)
	}

	legacy := testService(t)
	putNoteBlob(t, legacy, "resource/data/a.html", []byte(`<p>alpha</p>`))
	putNoteBlob(t, legacy, "resource/data/b.html", []byte(`<p>beta</p>`))
	if _, e = legacy.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('note','1-a','notebook','1',$1,'{"uniqueId":"note","title":"Old","type":1,"status":1,"activeScene":1}')`, legacy.uid()); e != nil {
		t.Fatal(e)
	}
	n, e = legacy.notebookSnapshot(ctx, "note")
	if e != nil {
		t.Fatal("legacy snapshot", e)
	}
	if docs, warning := legacy.currentRichText(ctx, n); warning != "" || len(docs) != 2 {
		t.Fatalf("legacy %+v %q", docs, warning)
	}
}

// Images in typed text are served only when a current document refers to
// them, and only as raster images.
func TestTypedResourceServesReferencedImages(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	doc := `{"content":{"type":"doc","content":[{"type":"onyxImg","attrs":{"src":"/storage/emulated/0/x/pic.png","dataKey":"pic.png"}}]},"docVersion":2}`
	putNoteBlob(t, s, "resource/data/t.html", []byte(doc))
	pngBytes := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")
	putNoteBlob(t, s, "resource/data/pic.png", pngBytes)
	putNoteBlob(t, s, "resource/data/secret.png", pngBytes)
	putNoteBlob(t, s, "resource/data/page.html", []byte("<script>x()</script>"))
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('note','1-a','notebook','1',$1,'{"uniqueId":"note","title":"Pic","type":1,"status":1,"activeScene":1}')`, s.uid()); e != nil {
		t.Fatal(e)
	}
	n, _ := s.notebookSnapshot(ctx, "note")
	get := func(name, version string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.browser(w, httptest.NewRequest("GET", "/boox/resource?id=note&version="+url.QueryEscape(version)+"&name="+url.QueryEscape(name), nil))
		return w
	}
	if w := get("pic.png", n.Version); w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), pngBytes) || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("referenced image: %d %q", w.Code, w.Header())
	}
	for _, name := range []string{"secret.png", "page.html", "t.html", "../t.html"} {
		if w := get(name, n.Version); w.Code != 404 {
			t.Fatalf("%s served: %d", name, w.Code)
		}
	}
	if w := get("pic.png", "stale"); w.Code != 409 {
		t.Fatalf("stale version: %d", w.Code)
	}
}
