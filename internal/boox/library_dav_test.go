package boox

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func davDo(t *testing.T, s Service, method, target string, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	s.DAV(w, r)
	return w
}
func davExpect(t *testing.T, w *httptest.ResponseRecorder, codes ...int) {
	t.Helper()
	for _, c := range codes {
		if w.Code == c {
			return
		}
	}
	t.Fatalf("status %d, want %v: %s", w.Code, codes, w.Body.String())
}
func davList(t *testing.T, s Service, dir string) string {
	t.Helper()
	w := davDo(t, s, "PROPFIND", dir, "", "Depth", "1")
	davExpect(t, w, http.StatusMultiStatus)
	return w.Body.String()
}
func syntheticPDF(label string) string { return "%PDF-1.4\n% synthetic " + label + "\n%%EOF\n" }

func TestLibraryDAVFileView(t *testing.T) {
	s, g := libraryFixture(t)
	libraryStoredBook(t, s, g, "b1", "One.epub")
	libraryStoredBook(t, s, g, "b2", "Two.epub")
	libraryStoredBook(t, s, g, "b3", "two.epub")
	libraryStoredBook(t, s, g, "b4", "Gone.epub")
	g.docs[s.uid()+"#b4"]["status"] = 1
	libraryRecord(s, g, 4, "hidden", map[string]any{"name": "Metadata only.epub", "type": "epub"})
	libraryRecord(s, g, 3, "A", map[string]any{"name": "Alpha"})
	libraryRecord(s, g, 3, "smart", map[string]any{"name": "Smart", "queryString": "author:x"})
	libraryRecord(s, g, 3, "trash-named", map[string]any{"name": ".Trash"})
	libraryRecord(s, g, 5, "m1", map[string]any{"documentUniqueId": "b1", "libraryUniqueId": "A"})
	libraryRecord(s, g, 5, "m2", map[string]any{"documentUniqueId": "b2", "libraryUniqueId": "smart"})
	// Cloud-account shelves look like ordinary ones apart from their description.
	libraryRecord(s, g, 3, "cloud", map[string]any{"name": "ultrabridge", "description": "WEBDAV-43022fa012224b40", "fetchSource": 0})
	libraryRecord(s, g, 3, "described", map[string]any{"name": "Described", "description": "Reading list - spring", "fetchSource": 0})
	libraryStoredBook(t, s, g, "cloudbook", "Cloud.epub")
	g.docs[s.uid()+"#cloudbook"]["fetchSource"] = 19
	libraryRecord(s, g, 5, "m3", map[string]any{"documentUniqueId": "cloudbook", "libraryUniqueId": "cloud"})
	managementObserve(t, s)

	root := davList(t, s, "/dav/books/")
	for _, want := range []string{"/dav/books/.Trash/", "/dav/books/Alpha/", "/dav/books/Two.epub", "/dav/books/two%20%282%29.epub", "/dav/books/.Trash%20%282%29/", "/dav/books/Described/"} {
		if !strings.Contains(root, want) {
			t.Error("root listing lacks", want)
		}
	}
	for _, hidden := range []string{"Metadata", "Smart", "One.epub", "Gone.epub", "ultrabridge", "Cloud.epub"} {
		if strings.Contains(root, hidden) {
			t.Error("root listing shows", hidden)
		}
	}
	if trash := davList(t, s, "/dav/books/.Trash/"); !strings.Contains(trash, "Gone.epub") {
		t.Fatal(trash)
	}
	w := davDo(t, s, "GET", "/dav/books/Alpha/One.epub", "", "Range", "bytes=0-8")
	davExpect(t, w, http.StatusPartialContent)
	if w.Body.String() != "synthetic" {
		t.Fatal(w.Body.String())
	}
	w = davDo(t, s, "OPTIONS", "/dav/books/", "")
	if !strings.Contains(w.Header().Get("DAV"), "2") {
		t.Fatal("class 2 must be advertised for Finder", w.Header())
	}
	w = davDo(t, s, "PROPPATCH", "/dav/books/Two.epub", `<?xml version="1.0"?><D:propertyupdate xmlns:D="DAV:" xmlns:Z="urn:schemas-microsoft-com:"><D:set><D:prop><Z:Win32LastModifiedTime>Mon, 26 Oct 2026 12:00:00 GMT</Z:Win32LastModifiedTime></D:prop></D:set></D:propertyupdate>`, "Content-Type", "application/xml")
	davExpect(t, w, http.StatusMultiStatus)
	if !strings.Contains(w.Body.String(), "Win32LastModifiedTime") || !strings.Contains(w.Body.String(), "200 OK") {
		t.Fatal(w.Body.String())
	}
}

func TestLibraryDAVWritesThroughTheJournal(t *testing.T) {
	s, g := libraryFixture(t)
	ctx := context.Background()
	libraryStoredBook(t, s, g, "b1", "One.epub")
	libraryRecord(s, g, 3, "A", map[string]any{"name": "Alpha"})
	libraryRecord(s, g, 5, "m1", map[string]any{"documentUniqueId": "b1", "libraryUniqueId": "A"})
	managementObserve(t, s)

	// Explorer: empty create, then the bytes. Finder: LOCK an unmapped URL first.
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Alpha/Fresh.pdf", ""), http.StatusCreated)
	if l := davList(t, s, "/dav/books/Alpha/"); !strings.Contains(l, "Fresh.pdf") {
		t.Fatal("placeholder not listed", l)
	}
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Alpha/Fresh.pdf", syntheticPDF("fresh")), http.StatusCreated, http.StatusNoContent)
	w := davDo(t, s, "LOCK", "/dav/books/Locked.pdf", `<?xml version="1.0"?><D:lockinfo xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype></D:lockinfo>`, "Content-Type", "application/xml")
	davExpect(t, w, http.StatusCreated)
	if !strings.HasPrefix(w.Header().Get("Lock-Token"), "<opaquelocktoken:") {
		t.Fatal(w.Header())
	}
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Locked.pdf", syntheticPDF("locked")), http.StatusCreated, http.StatusNoContent)
	davExpect(t, davDo(t, s, "UNLOCK", "/dav/books/Locked.pdf", ""), http.StatusNoContent)

	w = davDo(t, s, "PUT", "/dav/books/notes.txt", "plain text")
	davExpect(t, w, http.StatusUnsupportedMediaType)
	if !strings.Contains(w.Body.String(), "EPUB and PDF") {
		t.Fatal("refusal must say why", w.Body.String())
	}
	davExpect(t, davDo(t, s, "PUT", "/dav/books/fake.pdf", "not a pdf"), http.StatusUnsupportedMediaType)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/._Fresh.pdf", "apple double"), http.StatusCreated)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Alpha/One.epub", syntheticPDF("overwrite")), http.StatusConflict)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/.Trash/x.pdf", syntheticPDF("trash")), http.StatusForbidden)

	// A shelf created a moment ago accepts uploads and moves before publication.
	davExpect(t, davDo(t, s, "MKCOL", "/dav/books/Alpha/Sub", ""), http.StatusCreated)
	davExpect(t, davDo(t, s, "MKCOL", "/dav/books/Alpha/Sub", ""), http.StatusMethodNotAllowed)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Alpha/Sub/Nested.pdf", syntheticPDF("nested")), http.StatusCreated)
	davExpect(t, davDo(t, s, "MOVE", "/dav/books/Alpha/One.epub", "", "Destination", "http://example.test/dav/books/Alpha/Sub/One.epub"), http.StatusCreated)
	// One shelf per book: no COPY, and identical bytes bound for another shelf are refused.
	davExpect(t, davDo(t, s, "COPY", "/dav/books/Alpha/Fresh.pdf", "", "Destination", "http://example.test/dav/books/Alpha/Sub/Fresh.pdf"), http.StatusForbidden)
	davExpect(t, davDo(t, s, "COPY", "/dav/books/Alpha/Fresh.pdf", "", "Destination", "http://example.test/dav/books/Alpha/Fresh.pdf"), http.StatusNoContent)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Alpha/Sub/Fresh.pdf", syntheticPDF("fresh")), http.StatusConflict)
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Alpha/Fresh.pdf", syntheticPDF("fresh")), http.StatusCreated, http.StatusNoContent)
	davExpect(t, davDo(t, s, "MOVE", "/dav/books/Alpha/Sub/", "", "Destination", "http://example.test/dav/books/Alpha/Sub/Inner/"), http.StatusConflict, http.StatusForbidden)
	sub := davList(t, s, "/dav/books/Alpha/Sub/")
	for _, want := range []string{"Nested.pdf", "One.epub"} {
		if !strings.Contains(sub, want) {
			t.Fatal("pending writes not visible", want, sub)
		}
	}
	libraryDrain(t, s)
	managementObserve(t, s)
	if got := libraryStates(t, s); len(got) != 1 || got["published"] == 0 {
		t.Fatal(got)
	}
	var uploads int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_operation WHERE kind='upload_book' AND state='published'`).Scan(&uploads); e != nil || uploads != 3 {
		t.Fatal("uploads", uploads, e)
	}
	if m := g.record(s, "m1"); m["libraryUniqueId"] == "A" {
		t.Fatal("move did not update the membership", m)
	}

	// Delete from the last shelf trashes; moving out of .Trash restores.
	davExpect(t, davDo(t, s, "DELETE", "/dav/books/Alpha/Sub/One.epub", ""), http.StatusNoContent)
	if trash := davList(t, s, "/dav/books/.Trash/"); !strings.Contains(trash, "One.epub") {
		t.Fatal(trash)
	}
	davExpect(t, davDo(t, s, "DELETE", "/dav/books/.Trash/One.epub", ""), http.StatusForbidden)
	davExpect(t, davDo(t, s, "DELETE", "/dav/books/.Trash/", ""), http.StatusForbidden)
	davExpect(t, davDo(t, s, "MOVE", "/dav/books/.Trash/One.epub", "", "Destination", "http://example.test/dav/books/Alpha/One.epub"), http.StatusCreated)
	if alpha := davList(t, s, "/dav/books/Alpha/"); !strings.Contains(alpha, "Fresh.pdf") || !strings.Contains(alpha, "One.epub") {
		t.Fatal(alpha)
	}
	davExpect(t, davDo(t, s, "MOVE", "/dav/books/Alpha/Sub/", "", "Destination", "http://example.test/dav/books/Renamed/"), http.StatusCreated)
	davExpect(t, davDo(t, s, "MOVE", "/dav/books/Alpha/One.epub", "", "Destination", "http://example.test/dav/books/Alpha/One renamed.epub"), http.StatusCreated)
	davExpect(t, davDo(t, s, "DELETE", "/dav/books/Renamed/", ""), http.StatusNoContent)
	libraryDrain(t, s)
	managementObserve(t, s)
	if got := libraryStates(t, s); len(got) != 1 {
		t.Fatal(got)
	}
	if b := g.record(s, "b1"); b["name"] != "One renamed.epub" || fmt.Sprint(b["status"]) != "0" {
		t.Fatal(b)
	}
	root := davList(t, s, "/dav/books/")
	if !strings.Contains(root, "Nested.pdf") || strings.Contains(root, "Renamed") {
		t.Fatal("a top-level shelf delete leaves its books unshelved", root)
	}
	r := davDo(t, s, "GET", "/dav/books/Nested.pdf", "")
	davExpect(t, r, http.StatusOK)
	if raw, _ := io.ReadAll(r.Body); string(raw) != syntheticPDF("nested") {
		t.Fatal(string(raw))
	}
}
