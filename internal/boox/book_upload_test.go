package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"
)

func testEPUB(t *testing.T) []byte { t.Helper(); return testEPUBMimetype(t, "application/epub+zip") }

func testEPUBMimetype(t *testing.T, mimetype string) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, f := range [][2]string{{"mimetype", mimetype}, {"META-INF/container.xml", `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0"><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`}, {"content.opf", `<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="id">synthetic</dc:identifier><dc:title>Alexandria upload test</dc:title><dc:language>en</dc:language></metadata><manifest><item id="page" href="page.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="page"/></spine></package>`}, {"page.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>Test</title></head><body><h1>Alexandria upload test</h1><p>This book came from the server.</p></body></html>`}} {
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
	// Some publishers end the mimetype file with a line break; NeoReader opens those.
	if kind, e := validateBook("crlf.epub", testEPUBMimetype(t, "application/epub+zip\r\n")); e != nil || kind != "epub" {
		t.Fatal("trailing line break", kind, e)
	}
	if _, e := validateBook("other.epub", testEPUBMimetype(t, "application/zip")); e == nil {
		t.Fatal("accepted a non-EPUB mimetype")
	}
	// Books may exceed the 64 MiB tablet object limit, up to 128 MiB.
	pdf := func(n int) []byte {
		b := make([]byte, n)
		copy(b, "%PDF-1.4\n")
		copy(b[n-6:], "%%EOF\n")
		return b
	}
	if _, e := validateBook("big.pdf", pdf(maxObject+1)); e != nil {
		t.Fatal("refused a book over 64 MiB", e)
	}
	if _, e := validateBook("huge.pdf", pdf(maxBook+1)); e == nil {
		t.Fatal("accepted a book over 128 MiB")
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

// testMOBI builds a minimal PalmDB "BOOKMOBI" container: one text record and
// record 0 carrying a PalmDOC header and the MOBI magic. It only exercises the
// upload validator; NeoReader qualification used a Calibre-generated book.
func testMOBI(encryption uint16) []byte {
	header := make([]byte, 78+16+2)
	copy(header, "Alexandria MOBI test")
	copy(header[60:68], "BOOKMOBI")
	binary.BigEndian.PutUint16(header[76:78], 2)
	first := len(header)
	record0 := make([]byte, 16+232)
	binary.BigEndian.PutUint16(record0[0:2], 1) // no compression
	binary.BigEndian.PutUint16(record0[12:14], encryption)
	copy(record0[16:20], "MOBI")
	binary.BigEndian.PutUint32(header[78:82], uint32(first))
	binary.BigEndian.PutUint32(header[86:90], uint32(first+len(record0)))
	return append(append(header, record0...), []byte("Synthetic MOBI text.")...)
}

func TestMOBIAndAZW3Validation(t *testing.T) {
	for _, name := range []string{"Book.mobi", "Book.AZW3"} {
		kind, e := validateBook(name, testMOBI(0))
		if e != nil || kind != strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")) {
			t.Fatal(name, kind, e)
		}
	}
	drm := testMOBI(2)
	if _, e := validateBook("Locked.azw3", drm); e == nil || !strings.Contains(e.Error(), "DRM") {
		t.Fatal("DRM-protected book accepted", e)
	}
	wrong := testMOBI(0)
	copy(wrong[60:68], "TEXtREAd")
	truncated := testMOBI(0)[:90]
	for _, raw := range [][]byte{wrong, truncated, testEPUB(t)} {
		if _, e := validateBook("Bad.mobi", raw); e == nil {
			t.Fatal("invalid MOBI accepted")
		}
	}
	if bookContentType("mobi") != "application/x-mobipocket-ebook" || bookContentType("azw3") != "application/x-mobi8-ebook" {
		t.Fatal("content types")
	}
	if items, _ := importFiles(importRequest{Files: []importFile{{"Shelf/a.mobi", 10}, {"Shelf/b.azw3", 10}, {"Shelf/c.opf", 10}}}); items[0].State != "waiting" || items[1].State != "waiting" || items[2].State != "skipped" {
		t.Fatal(items)
	}
}

func TestMOBIUploadPublishesItsNativeType(t *testing.T) {
	s, g := libraryFixture(t)
	managementObserve(t, s)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Synthetic.mobi", string(testMOBI(0))), http.StatusCreated)
	libraryDrain(t, s)
	var found map[string]any
	g.mu.Lock()
	for _, b := range g.docs {
		if b["name"] == "Synthetic.mobi" {
			found = b
		}
	}
	g.mu.Unlock()
	if found == nil || found["type"] != "mobi" || !strings.HasSuffix(fmt.Sprint(found["nativeAbsolutePath"]), ".mobi") {
		t.Fatal(found)
	}
	var ct string
	if e := s.DB.QueryRowContext(context.Background(), `SELECT v.content_type FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key LIKE '%/book/%.mobi'`).Scan(&ct); e != nil || ct != "application/x-mobipocket-ebook" {
		t.Fatal(ct, e)
	}
}
