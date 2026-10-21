package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testEPUB(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, f := range [][2]string{{"mimetype", "application/epub+zip"}, {"META-INF/container.xml", `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`}, {"content.opf", `<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="id">synthetic</dc:identifier><dc:title>Alexandria upload test</dc:title><dc:language>en</dc:language></metadata><manifest><item id="page" href="page.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="page"/></spine></package>`}, {"page.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Test</title></head><body><h1>Alexandria upload test</h1><p>This book came from the server.</p></body></html>`}} {
		w, e := z.Create(f[0])
		if e != nil {
			t.Fatal(e)
		}
		io.WriteString(w, f[1])
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}
func TestBookValidationAndFingerprint(t *testing.T) {
	raw := testEPUB(t)
	if kind, e := validateBook("test.epub", raw); e != nil || kind != "epub" {
		t.Fatal(kind, e)
	}
	for _, c := range []struct {
		name string
		raw  []byte
	}{{"test.exe", raw}, {"../test.epub", raw}, {"bad.epub", []byte("PK junk")}, {"bad.pdf", []byte("%PDF-1.4 broken")}} {
		if _, e := validateBook(c.name, c.raw); e == nil {
			t.Fatalf("accepted %s", c.name)
		}
	}
	small := []byte("abc")
	if bookFingerprint(small) != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatal("small fingerprint")
	}
	large := bytes.Repeat([]byte("0123456789abcdef"), 1024)
	sample := append(append(append([]byte{}, large[:512]...), large[len(large)/2-256:len(large)/2+256]...), large[len(large)-512:]...)
	want := md5.Sum(sample)
	if bookFingerprint(large) != hex.EncodeToString(want[:]) {
		t.Fatal("sampled fingerprint")
	}
}
func TestBookPublicationDurableAndDuplicateSafe(t *testing.T) {
	s := testService(t)
	if _, e := s.DB.ExecContext(context.Background(), `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	raw := testEPUB(t)
	id, e := s.QueueBook(ctx, "test.epub", raw)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.QueueBook(ctx, "renamed.epub", raw)
	if e != nil || again != id {
		t.Fatal("duplicate upload", again, e)
	}
	var count int
	s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_operation`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	var key, sha string
	s.DB.QueryRowContext(ctx, `SELECT payload->>'nativeKey',payload->>'sha256' FROM boox_operation WHERE id=$1`, id).Scan(&key, &sha)
	body, _, e := s.Objects.Get(ctx, "boox/bodies/"+sha)
	if e != nil {
		t.Fatal(e)
	}
	got, _ := io.ReadAll(body)
	body.Close()
	if !bytes.Equal(raw, got) {
		t.Fatal("stored bytes")
	}
	documents := map[string]map[string]any{}
	puts := 0
	failMessage := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		doc := strings.TrimPrefix(r.URL.Path, "/neocloud/")
		if r.Method == "GET" {
			b, ok := documents[doc]
			if !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(b)
			return
		}
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			t.Error("bad json")
			w.WriteHeader(400)
			return
		}
		if b["msgType"] != nil && failMessage {
			w.WriteHeader(503)
			return
		}
		puts++
		b["_rev"] = "1-test"
		documents[doc] = b
		json.NewEncoder(w).Encode(map[string]string{"rev": "1-test"})
	}))
	defer server.Close()
	s.Config.GatewayAdminURL = server.URL
	if _, e = s.PublishOne(ctx); e == nil {
		t.Fatal("expected injected interruption")
	}
	if len(documents) != 2 {
		t.Fatal("expected metadata + commit before interruption", len(documents))
	}
	failMessage = false
	if worked, e := s.PublishOne(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	if puts != 3 {
		t.Fatal("replay duplicated publication", puts)
	}
	var state string
	s.DB.QueryRowContext(ctx, `SELECT state FROM boox_operation WHERE id=$1`, id).Scan(&state)
	if state != "published" {
		t.Fatal(state)
	}
	found := false
	for _, b := range documents {
		if b["modeType"] == float64(4) {
			found = true
			if b["hashTag"] != bookFingerprint(raw) || b["status"] != float64(0) || b["userDataSyncStatus"] != float64(1) || b["nativeAbsolutePath"] == nil || b["nativeAbsolutePath"] != b["idString"] {
				t.Fatal("native metadata")
			}
		}
	}
	if !found {
		t.Fatal("no book metadata")
	}
	// Same sampled native identity but different whole bytes must not create a
	// second book. This is why SHA-256 alone is insufficient for native matching.
	padded := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("x"), 5000)...)
	padded = append(padded, []byte("\n%%EOF")...)
	if _, e = s.QueueBook(ctx, "sample.pdf", padded); e != nil {
		t.Fatal(e)
	}
	padded[1000] = 'y'
	if _, e = s.QueueBook(ctx, "collision.pdf", padded); !errors.Is(e, errBookConflict) {
		t.Fatal("native hash collision not rejected", e)
	}
}
func TestBookUploadOriginAndReadingVisibility(t *testing.T) {
	s := testService(t)
	if _, e := s.DB.ExecContext(context.Background(), `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); e != nil {
		t.Fatal(e)
	}
	var buf bytes.Buffer
	m := multipart.NewWriter(&buf)
	f, _ := m.CreateFormFile("book", "test.epub")
	f.Write(testEPUB(t))
	m.Close()
	r := httptest.NewRequest("POST", "/boox/books/upload", bytes.NewReader(buf.Bytes()))
	r.Header.Set("Content-Type", m.FormDataContentType())
	w := httptest.NewRecorder()
	s.Admin(w, r)
	if w.Code != 403 {
		t.Fatal("missing origin accepted", w.Code)
	}
	r = httptest.NewRequest("POST", "/boox/books/upload", bytes.NewReader(buf.Bytes()))
	r.Header.Set("Content-Type", m.FormDataContentType())
	r.Header.Set("Origin", s.Config.PublicURL)
	w = httptest.NewRecorder()
	s.Admin(w, r)
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, v := range []struct {
		id           string
		mode, status int
	}{{"livebook", 4, 0}, {"deletedbook", 4, 1}, {"livehighlight", 1, 0}, {"deletedhighlight", 1, 1}, {"livebookmark", 2, 0}, {"deletedbookmark", 2, 1}} {
		b, _ := json.Marshal(map[string]any{"uniqueId": v.id, "documentId": "livebook", "modeType": v.mode, "status": v.status, "title": v.id})
		if _, e := s.DB.ExecContext(context.Background(), `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-x','reading','',$2,$3)`, s.uid()+"#"+v.id, s.uid(), b); e != nil {
			t.Fatal(e)
		}
	}
	w = httptest.NewRecorder()
	s.Admin(w, httptest.NewRequest("GET", "/boox/reading?record="+s.uid()+"%23livebook", nil))
	html := w.Body.String()
	if w.Code != 200 || !strings.Contains(html, "livebook") || !strings.Contains(html, "livehighlight") || !strings.Contains(html, "livebookmark") || strings.Contains(html, "deletedbook") || strings.Contains(html, "deletedhighlight") || strings.Contains(html, "deletedbookmark") {
		t.Fatal("reading lifecycle filter", w.Code)
	}
}
