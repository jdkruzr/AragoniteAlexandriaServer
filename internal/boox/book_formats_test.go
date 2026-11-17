package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// testZip builds an archive of named entries with small synthetic contents.
func testZip(t *testing.T, names ...string) []byte {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, name := range names {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		io.WriteString(w, "synthetic "+name)
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}

// These fixtures carry only what the upload checks read; none is a book a
// tablet has opened.
func TestEveryBookFormatIsChecked(t *testing.T) {
	rar4 := []byte("Rar!\x1a\x07\x00synthetic")
	rar5 := []byte("Rar!\x1a\x07\x01\x00synthetic")
	comic := testZip(t, "ComicInfo.xml", "pages/001.jpg", "pages/002.PNG")
	ole := append([]byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}, "synthetic"...)
	ooxml := testZip(t, "[Content_Types].xml", "word/document.xml")
	odf := testZip(t, "mimetype", "content.xml")
	palm := make([]byte, 78+8+8)
	copy(palm[60:68], "TEXtREAd")
	palm[77] = 1
	djvu := []byte("AT&TFORM\x00\x00\x00\x10DJVUsynthetic")
	fb2 := []byte("<?xml version=\"1.0\" encoding=\"windows-1251\"?>\n<FictionBook xmlns=\"http://www.gribuser.ru/xml/fictionbook/2.0\"><body>\xcf\xf0\xe8</body></FictionBook>")
	text := []byte("Synthetic text.\n")
	utf16 := []byte{0xff, 0xfe, 'S', 0, 'y', 0}
	binary := []byte("synthetic\x00binary")

	good := map[string][]byte{
		"a.cbz": comic, "a.CBR": rar4, "b.cbr": rar5, "c.cbr": comic,
		"a.azw": testMOBI(0), "a.prc": testMOBI(0), "a.pdb": palm, "b.pdb": testMOBI(0),
		"a.djvu": djvu, "a.fb2": fb2, "a.fbz": testZip(t, "book.fb2"), "a.fb3": ooxml,
		"a.docx": ooxml, "a.pptx": ooxml, "a.ppsx": ooxml, "a.potx": ooxml, "a.pptm": ooxml, "a.ppsm": ooxml, "a.potm": ooxml, "a.ppam": ooxml,
		"a.odt": odf, "a.sxw": odf, "a.doc": ole, "a.ppt": ole, "a.pps": ole, "a.pot": ole, "a.ppa": ole,
		"a.rtf": []byte(`{\rtf1 synthetic}`), "a.chm": []byte("ITSFsynthetic"), "a.zip": testZip(t, "notes.txt"),
		"a.txt": text, "b.txt": utf16, "a.md": text, "a.html": text, "a.htm": text, "a.xhtml": text, "a.xml": text,
	}
	seen := map[string]bool{"pdf": true, "epub": true, "mobi": true, "azw3": true} // covered by the older tests
	for name, raw := range good {
		kind, e := validateBook(name, raw)
		if e != nil || "."+kind != strings.ToLower(name[strings.LastIndex(name, "."):]) {
			t.Errorf("%s: %q %v", name, kind, e)
		}
		seen[kind] = true
	}
	for ext := range bookFormats {
		if !seen[ext[1:]] {
			t.Errorf("no accepted fixture for %s", ext)
		}
	}

	bad := map[string][]byte{
		"rar.cbz": rar4, "epub.cbz": testEPUB(t), "nopages.cbz": testZip(t, "ComicInfo.xml"), "junk.cbz": text,
		"junk.cbr": text, "nopages.cbr": testZip(t, "readme.txt"), "almost.cbr": []byte("Rar!\x1a\x07\x02"),
		"locked.azw": testMOBI(2), "junk.prc": text, "short.pdb": palm[:70], "junk.pdb": make([]byte, 100),
		"junk.djvu": []byte("AT&TFORM\x00\x00\x00\x10PM44"), "junk.fb2": []byte("<html><FictionBook/></html>"), "text.fb2": text,
		"nobook.fbz": testZip(t, "notes.txt"), "junk.docx": odf, "junk.odt": ooxml, "junk.doc": ooxml, "junk.rtf": text,
		"junk.chm": text, "empty.zip": testZip(t), "junk.zip": text, "junk.txt": binary, "junk.html": binary,
		"escape.zip": testZip(t, "../outside.txt"), "a.comic": comic, "a.caj": text, "a.exe": text,
	}
	for name, raw := range bad {
		if kind, e := validateBook(name, raw); e == nil {
			t.Errorf("accepted %s as %s", name, kind)
		}
	}
	if _, e := validateBook("rar.cbz", rar4); e == nil || !strings.Contains(e.Error(), ".cbr") {
		t.Error("a RAR named .cbz should be told to rename", e)
	}
	if _, e := validateBook("locked.azw", testMOBI(2)); e == nil || !strings.Contains(e.Error(), "DRM") {
		t.Error("DRM refusal", e)
	}

	if bookContentType("cbz") != "application/x-cbz" || bookContentType("cbr") != "application/x-cbr" || bookContentType("exe") != "" {
		t.Error("comic content types")
	}
	// Nothing a browser would open as a page is ever served as one.
	for _, c := range [][2]string{{"", "a.html"}, {"text/html", "a.html"}, {"text/html; charset=utf-8", "a.pdf"}, {"", "a.htm"}, {"", "a.xhtml"}, {"application/xhtml+xml", "a.xhtml"}, {"", "a.xml"}, {"text/xml", "a.xml"}, {"", "a.fb2"}, {"application/x-fictionbook+xml", "a.fb2"}, {"image/svg+xml", "a.epub"}, {"", "a.unknown"}, {"not a type", "a.pdf"}} {
		if got := bookServeType(c[0], c[1]); got != "application/octet-stream" {
			t.Errorf("%q %s served as %s", c[0], c[1], got)
		}
	}
	if got := bookServeType("", "a.pdf"); got != "application/pdf" {
		t.Error(got)
	}
	if got := bookServeType("application/epub+zip", "a.epub"); got != "application/epub+zip" {
		t.Error(got)
	}
	if v := (uploadBookView{}); !strings.Contains(v.Accept(), ".cbz,") || !strings.Contains(v.Formats(), "CBR, CBZ") {
		t.Error(v.Accept(), v.Formats())
	}
}

func TestComicAndWebBookUploadsPublish(t *testing.T) {
	s, g := libraryFixture(t)
	managementObserve(t, s)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Synthetic.cbz", string(testZip(t, "001.jpg"))), http.StatusCreated)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Synthetic.cbr", "Rar!\x1a\x07\x00synthetic"), http.StatusCreated)
	page := `<html><body><script>document.title="ran"</script>Synthetic page</body></html>`
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Synthetic.html", page), http.StatusCreated)
	libraryDrain(t, s)
	types := map[string]any{}
	g.mu.Lock()
	for _, b := range g.docs {
		if name := fmt.Sprint(b["name"]); strings.HasPrefix(name, "Synthetic.") {
			types[name] = b["type"]
			if !strings.HasSuffix(fmt.Sprint(b["nativeAbsolutePath"]), "."+fmt.Sprint(b["type"])) {
				t.Error(b["nativeAbsolutePath"])
			}
		}
	}
	g.mu.Unlock()
	if types["Synthetic.cbz"] != "cbz" || types["Synthetic.cbr"] != "cbr" || types["Synthetic.html"] != "html" {
		t.Fatal(types)
	}
	for suffix, want := range map[string]string{"cbz": "application/x-cbz", "cbr": "application/x-cbr", "html": "application/octet-stream"} {
		var ct string
		if e := s.DB.QueryRowContext(context.Background(), `SELECT v.content_type FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key LIKE '%/book/%.'||$1`, suffix).Scan(&ct); e != nil || ct != want {
			t.Fatal(suffix, ct, e)
		}
	}
	// The stored page comes back as a file, never as a page from this server.
	w := davDo(t, s, "GET", "/dav/books/Synthetic.html", "")
	davExpect(t, w, http.StatusOK)
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" || w.Body.String() != page {
		t.Fatal(ct, w.Body.String())
	}
}

func TestTabletsUploadBooksUpTo128MiB(t *testing.T) {
	s := testService(t)
	c := getSTS(t, s, enrollTest(t, s, uuid.NewString()))
	big := make([]byte, maxObject+1)
	copy(big, "synthetic tablet book")
	book := s.uid() + "/reader/tabletbook/book/tabletbook.cbz"
	if w := signed(t, s, c, "PUT", book, "", big, false); w.Code != 200 {
		t.Fatal("book over 64 MiB refused", w.Code, w.Body.String())
	}
	for _, key := range []string{s.uid() + "/note/n/resource/data/big.bin", s.uid() + "/reader/tabletbook/thumbnail/big.png", s.uid() + "/reader/tabletbook/book/extra/deeper.cbz"} {
		if w := signed(t, s, c, "PUT", key, "", big, false); w.Code != 400 {
			t.Fatal("object over 64 MiB accepted", key, w.Code)
		}
	}
	if w := signed(t, s, c, "PUT", s.uid()+"/reader/hugebook/book/hugebook.pdf", "", make([]byte, maxBook+1), false); w.Code != 400 {
		t.Fatal("book over 128 MiB accepted", w.Code)
	}
}
