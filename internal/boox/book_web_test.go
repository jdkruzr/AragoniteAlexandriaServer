package boox

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func bookWeb(t *testing.T, s Service, method, target string, form url.Values, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if method == "POST" {
		r = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", s.Config.PublicURL)
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	s.Admin(w, r)
	return w
}

func TestReadingPageBookActions(t *testing.T) {
	s, g := libraryFixture(t)
	libraryStoredBook(t, s, g, "b1", "One.epub")
	libraryStoredBook(t, s, g, "b2", "Two.epub")
	libraryStoredBook(t, s, g, "cloudbook", "Cloud.epub")
	g.docs[s.uid()+"#cloudbook"]["fetchSource"] = 19
	libraryRecord(s, g, 3, "A", map[string]any{"name": "Alpha"})
	libraryRecord(s, g, 3, "B", map[string]any{"name": "Beta"})
	libraryRecord(s, g, 3, "cloud", map[string]any{"name": "ultrabridge", "description": "WEBDAV-43022fa012224b40", "fetchSource": 0})
	libraryRecord(s, g, 5, "m1", map[string]any{"documentUniqueId": "b1", "libraryUniqueId": "A"})
	managementObserve(t, s)

	if stored, all := bookWeb(t, s, "GET", "/boox/reading?view=stored", nil).Body.String(), bookWeb(t, s, "GET", "/boox/reading?view=all", nil).Body.String(); strings.Contains(stored, "ultrabridge") || !strings.Contains(stored, "Alpha") || !strings.Contains(all, "ultrabridge") {
		t.Fatal("Stored Here lists only native shelves; All Books lists every shelf")
	}
	page := bookWeb(t, s, "GET", "/boox/reading?view=stored&folder=A", nil).Body.String()
	for _, want := range []string{`form="book-bulk"`, "/boox/books/download?book=b1", "/boox/books/trash", "/boox/books/upload?folder=A"} {
		if !strings.Contains(page, want) {
			t.Error("reading page lacks", want)
		}
	}
	detail := bookWeb(t, s, "GET", "/boox/reading?record="+url.QueryEscape(s.uid()+"#b1"), nil).Body.String()
	if !strings.Contains(detail, "action=rename&amp;id=b1") || !strings.Contains(detail, "action=trash&amp;id=b1") {
		t.Fatal("detail page lacks book actions")
	}

	w := bookWeb(t, s, "GET", "/boox/books/download?book=b1", nil, "Range", "bytes=0-8")
	if w.Code != http.StatusPartialContent || w.Body.String() != "synthetic" || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="One.epub"`) || w.Header().Get("Content-Type") != "application/epub+zip" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	for _, id := range []string{"cloudbook", "missing"} {
		if w = bookWeb(t, s, "GET", "/boox/books/download?book="+id, nil); w.Code != 404 {
			t.Fatal("download of", id, w.Code)
		}
	}

	// Bulk move: a review page, a confirmation, then a batch status page.
	w = bookWeb(t, s, "GET", "/boox/books/manage?action=move&id=b1&id=b2&return=%2Fboox%2Freading%3Fview%3Dstored", nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "One.epub") || !strings.Contains(w.Body.String(), "Two.epub") || !strings.Contains(w.Body.String(), "Alpha") {
		t.Fatal(w.Code, w.Body.String())
	}
	form := url.Values{"action": {"move"}, "id": {"b1", "b2"}, "folder": {"B"}, "token": {uuid.NewString()}, "return": {"/boox/reading?view=stored"}}
	if w = bookWeb(t, s, "POST", "/boox/books/manage", form); w.Code != 400 {
		t.Fatal("unconfirmed change", w.Code)
	}
	r := httptest.NewRequest("POST", "/boox/books/manage", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	foreign := httptest.NewRecorder()
	s.Admin(foreign, r)
	if foreign.Code != 403 {
		t.Fatal("cross-origin form", foreign.Code)
	}
	form.Set("confirm", "on")
	w = bookWeb(t, s, "POST", "/boox/books/manage", form)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/boox/books/manage?batch="+form.Get("token")) {
		t.Fatal(w.Code, w.Header().Get("Location"))
	}
	libraryDrain(t, s)
	managementObserve(t, s)
	if m := g.record(s, "m1"); m["libraryUniqueId"] != "B" {
		t.Fatal("membership not moved in place", m)
	}
	status := bookWeb(t, s, "GET", w.Header().Get("Location"), nil).Body.String()
	if strings.Count(status, "Published to native sync") != 2 {
		t.Fatal(status)
	}

	// Rename; an invalid name is explained and nothing is queued.
	bad := url.Values{"action": {"rename"}, "id": {"b2"}, "name": {"Two.pdf"}, "token": {uuid.NewString()}}
	if w = bookWeb(t, s, "POST", "/boox/books/manage", bad); w.Code != 409 || !strings.Contains(w.Body.String(), "keeps its file extension") {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(w.Result().Header.Get("Content-Security-Policy"), "style-src 'self'") {
		t.Fatal("an error page must keep the page's own headers", w.Header())
	}
	good := url.Values{"action": {"rename"}, "id": {"b2"}, "name": {"Renamed Two.epub"}, "token": {uuid.NewString()}}
	if w = bookWeb(t, s, "POST", "/boox/books/manage", good); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Trash both, then restore one onto a shelf from Book Trash.
	trash := url.Values{"action": {"trash"}, "id": {"b1", "b2"}, "confirm": {"on"}, "token": {uuid.NewString()}}
	if w = bookWeb(t, s, "POST", "/boox/books/manage", trash); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	libraryDrain(t, s)
	managementObserve(t, s)
	if b := g.record(s, "b2"); b["name"] != "Renamed Two.epub" || fmt.Sprint(b["status"]) != "1" {
		t.Fatal(b)
	}
	listing := bookWeb(t, s, "GET", "/boox/books/trash", nil).Body.String()
	if !strings.Contains(listing, "Renamed Two.epub") || !strings.Contains(listing, "One.epub") || strings.Contains(listing, "Cloud.epub") {
		t.Fatal(listing)
	}
	restore := url.Values{"action": {"restore"}, "id": {"b1"}, "folder": {"A"}, "confirm": {"on"}, "token": {uuid.NewString()}}
	if w = bookWeb(t, s, "POST", "/boox/books/manage", restore); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	libraryDrain(t, s)
	managementObserve(t, s)
	v, e := s.librarySnapshot(t.Context(), s.DB)
	must(t, e)
	if b, m := g.record(s, "b1"), v.Memberships("b1"); fmt.Sprint(b["status"]) != "0" || len(m) != 1 || m[0].Shelf != "A" {
		t.Fatal("restore onto a shelf leaves one live membership there", b, m)
	}
	// One unknown book refuses the whole batch.
	mixed := url.Values{"action": {"trash"}, "id": {"b1", "missing"}, "confirm": {"on"}, "token": {uuid.NewString()}}
	if w = bookWeb(t, s, "POST", "/boox/books/manage", mixed); w.Code != 409 {
		t.Fatal(w.Code)
	}
	if got := libraryStates(t, s); len(got) != 1 {
		t.Fatal(got)
	}
	if w = bookWeb(t, s, "GET", "/boox/books/upload?folder=A", nil); !strings.Contains(w.Body.String(), `value="A" selected`) {
		t.Fatal("upload page does not preselect the shelf")
	}
}

func TestBookBatchNamesHeldBackBooks(t *testing.T) {
	s, g := libraryFixture(t)
	libraryStoredBook(t, s, g, "b3", "Three.epub")
	libraryStoredBook(t, s, g, "b4", "Four.epub")
	libraryRecord(s, g, 3, "B", map[string]any{"name": "Beta"})
	managementObserve(t, s)
	form := url.Values{"action": {"move"}, "id": {"b3", "b4"}, "folder": {"B"}, "confirm": {"on"}, "token": {uuid.NewString()}}
	w := bookWeb(t, s, "POST", "/boox/books/manage", form)
	if w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	// A tablet removes the shelf before the server publishes the moves.
	g.mu.Lock()
	g.docs[s.uid()+"#B"]["status"] = 1
	g.mu.Unlock()
	libraryDrain(t, s)
	status := bookWeb(t, s, "GET", w.Header().Get("Location"), nil).Body.String()
	if !strings.Contains(status, "Three.epub") || !strings.Contains(status, "Four.epub") || strings.Count(status, "held back") < 2 {
		t.Fatal(status)
	}
}
