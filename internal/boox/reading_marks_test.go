package boox

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// readingFixture has "stored" In Progress, "elsewhere" (no file) Finished and
// "new" New, each as the tablets reported it.
func TestReadingMarksOutrankWhatTabletsReport(t *testing.T) {
	s := readingFixture(t)
	ctx := context.Background()
	page := func(p string) string {
		t.Helper()
		w := httptest.NewRecorder()
		s.Admin(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 200 {
			t.Fatal(p, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	mark := func(mark string, ids ...string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/boox/reading/mark", strings.NewReader(url.Values{"id": ids, "mark": {mark}, "return": {"/boox/reading?view=all"}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", s.Config.PublicURL)
		w := httptest.NewRecorder()
		s.Admin(w, r)
		return w
	}
	counts := func() string {
		t.Helper()
		var out []string
		for _, status := range []string{"new", "reading", "finished", "not_reading"} {
			_, total, e := s.Books(ctx, BookFilter{Status: status, Limit: 20})
			if e != nil {
				t.Fatal(e)
			}
			out = append(out, fmt.Sprint(total))
		}
		return strings.Join(out, " ")
	}
	if got := counts(); got != "1 1 1 0" {
		t.Fatal("as reported", got)
	}

	// A book with no file here can be marked too; a removed book, a highlight
	// and an unknown ID cannot.
	if w := mark("finished", "stored", "removed", "highlight", "nope"); w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/boox/reading?view=all" {
		t.Fatal(w.Code, w.Header())
	}
	if w := mark("not_reading", "elsewhere"); w.Code != http.StatusSeeOther {
		t.Fatal(w.Code)
	}
	var marks int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_reading_mark`).Scan(&marks); e != nil || marks != 2 {
		t.Fatal("only visible books are marked", marks, e)
	}
	if got := counts(); got != "1 0 1 1" {
		t.Fatal("marked", got)
	}
	stats := page("/boox/reading?view=stats")
	for _, want := range []string{`status=1"><strong>0</strong>`, `status=2"><strong>1</strong>`, `status=3"><strong>1</strong>`} {
		if !strings.Contains(stats, want) {
			t.Fatal("Your Reading figures", want)
		}
	}
	if html := page("/boox/reading?view=all&status=2"); !strings.Contains(html, ">Needle title stored<") || strings.Contains(html, ">Needle title elsewhere<") {
		t.Fatal("Finished filter follows marks")
	}
	if html := page("/boox/reading?view=all&status=3"); !strings.Contains(html, ">Needle title elsewhere<") || strings.Contains(html, ">Needle title stored<") {
		t.Fatal("Not Reading filter")
	}
	preview := page("/boox/reading?view=all&selected=" + url.QueryEscape(s.uid()+"#stored"))
	if !strings.Contains(preview, "marked here") || !strings.Contains(preview, `value="none"`) || !strings.Contains(preview, `value="not_reading">`) || strings.Contains(preview, `name="mark" value="finished">`) {
		t.Fatal("preview offers the other mark and removal")
	}

	// The tablet opens the book again and reports In Progress: the mark holds.
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_projection SET revision='2-test',body=body||'{"readingStatus":1,"progress":"2/3","lastAccess":4102444800000}' WHERE document_id=$1`, s.uid()+"#stored"); e != nil {
		t.Fatal(e)
	}
	b, e := s.Book(ctx, s.uid()+"#stored")
	if e != nil || b.Status != "Finished" {
		t.Fatal("a later tablet save cleared the mark", b.Status, e)
	}
	// Marking the same way again keeps the first date; removing restores what
	// the tablets say.
	var before, after string
	s.DB.QueryRowContext(ctx, `SELECT marked_at::text FROM boox_reading_mark WHERE book_id='stored'`).Scan(&before)
	mark("finished", "stored")
	s.DB.QueryRowContext(ctx, `SELECT marked_at::text FROM boox_reading_mark WHERE book_id='stored'`).Scan(&after)
	if before == "" || before != after {
		t.Fatal("marked_at moved", before, after)
	}
	// Not Reading lapses once a tablet opens the book after the mark, and
	// counts again when marked afresh.
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_reading_mark SET marked_at=now()-interval '1 day' WHERE book_id='elsewhere'`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=body||jsonb_build_object('lastAccess',(extract(epoch FROM now()-interval '1 hour')*1000)::bigint) WHERE document_id=$1`, s.uid()+"#elsewhere"); e != nil {
		t.Fatal(e)
	}
	if got := counts(); got != "1 0 2 0" {
		t.Fatal("picked up again", got)
	}
	mark("not_reading", "elsewhere")
	if got := counts(); got != "1 0 1 1" {
		t.Fatal("set aside again", got)
	}
	mark("none", "stored", "elsewhere")
	if got := counts(); got != "1 1 1 0" {
		t.Fatal("unmarked", got)
	}
	if w := mark("abandoned", "stored"); w.Code != http.StatusBadRequest {
		t.Fatal("unknown mark", w.Code)
	}
	w := httptest.NewRecorder()
	s.Admin(w, httptest.NewRequest("GET", "/boox/reading/mark", nil))
	if w.Code != http.StatusNotFound {
		t.Fatal("GET", w.Code)
	}
}

func TestMoveRefusesABookWithNoFileHere(t *testing.T) {
	s := readingFixture(t)
	form := url.Values{"id": {"stored", "elsewhere"}, "action": {"move"}, "inline": {"1"}, "token": {"6f0b3a53-6f6c-4c0e-9d0b-0d4b0d6f3a11"}, "folder": {"shelf"}, "return": {"/boox/reading?view=all"}}
	r := httptest.NewRequest("POST", "/boox/books/manage", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", s.Config.PublicURL)
	w := httptest.NewRecorder()
	s.Admin(w, r)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "Only books whose file is stored here can be moved") {
		t.Fatal(w.Code, w.Body.String())
	}
	var queued int
	if e := s.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM boox_operation WHERE kind LIKE 'library_%'`).Scan(&queued); e != nil || queued != 0 {
		t.Fatal("nothing is queued", queued, e)
	}
}

func TestUploadCarriesTheMarkOfTheRecordItReplaces(t *testing.T) {
	raw := []byte(syntheticPDF("marked"))
	s, g := adoptFixture(t, raw, map[string]map[string]any{"live": {"lastAccess": 100, "progress": "5/10", "readingStatus": 1}})
	managementObserve(t, s)
	ctx := context.Background()
	if e := s.setReadingMarks(ctx, []string{"live"}, markFinished); e != nil {
		t.Fatal(e)
	}
	davExpect(t, davDo(t, s, "PUT", "/dav/books/Author/Book.pdf", string(raw)), http.StatusCreated)
	libraryDrain(t, s)
	id := fmt.Sprint(newBook(t, s, g, "Book.pdf")["uniqueId"])
	var mark string
	if e := s.DB.QueryRowContext(ctx, `SELECT mark FROM boox_reading_mark WHERE native_uid=$1 AND book_id=$2`, s.uid(), id).Scan(&mark); e != nil || mark != markFinished {
		t.Fatal("the new book is not marked", mark, e)
	}
}
