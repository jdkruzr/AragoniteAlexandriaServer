package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func TestReaderPage(t *testing.T) {
	s, _ := managementFixture(t)
	previewFixture(t, s)
	ctx := context.Background()
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_page_index SET model='synthetic-vl',updated_at='2026-03-04T12:00:00Z' WHERE notebook_id='n' AND page_id='p1'`); e != nil {
		t.Fatal(e)
	}
	n, e := s.notebookSnapshot(ctx, "n")
	if e != nil {
		t.Fatal(e)
	}
	w := libraryGet(t, s, "/boox/notebook?id=n&page=1&queued=6f2a3b1c-1d2e-4f5a-8b9c-0d1e2f3a4b5c")
	page := html.UnescapeString(w.Body.String())
	choice, _ := json.Marshal(notebookChoice{ID: "n", Revision: n.Revision})
	for _, want := range []string{`href="/boox?selected=n"`, "Handwritten note · 2 pages", `class="filmstrip"`, `<li aria-current="page"><a href="/boox/notebook?id=n&page=1">`,
		"/boox/thumb.png?id=n&page=2&version=" + n.Version, `id="drawer"`, "Synthetic text", "Recognized Mar 4", "synthetic-vl",
		"/boox/notebook.pdf?id=n&version=" + n.Version, `name="selected" value="` + string(choice) + `"`, `name="action" value="rename"`,
		`name="return" value="/boox/notebook?id=n&page=1"`, "Changes queued", `rel="next"`, `href="/boox/notebook?id=n&page=1&details=0"`} {
		if !strings.Contains(page, want) {
			t.Errorf("reader lacks %q", want)
		}
	}
	// Closing the drawer is remembered.
	w = libraryGet(t, s, "/boox/notebook?id=n&page=2&details=0")
	if strings.Contains(w.Body.String(), `id="drawer"`) || !strings.Contains(w.Header().Get("Set-Cookie"), "alexandria_reader_details=0") {
		t.Fatal("drawer not closed")
	}
	if grid := libraryGet(t, s, "/boox/notebook?id=n&view=grid").Body.String(); !strings.Contains(grid, `class="page-grid"`) || strings.Contains(grid, `id="reader"`) {
		t.Fatal("grid view", grid)
	}
	// A deleted notebook offers Restore and goes back to the deleted list.
	dead := html.UnescapeString(libraryGet(t, s, "/boox/notebook?id=d").Body.String())
	if !strings.Contains(dead, `value="restore"`) || !strings.Contains(dead, `href="/boox?show=deleted&selected=d"`) || strings.Contains(dead, `value="rename"`) {
		t.Fatal("deleted reader", dead)
	}
}

func TestNotebookPDF(t *testing.T) {
	s := testService(t)
	previewFixture(t, s)
	n, e := s.notebookSnapshot(context.Background(), "n")
	if e != nil {
		t.Fatal(e)
	}
	w := libraryGet(t, s, "/boox/notebook.pdf?id=n&version="+n.Version)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/pdf" || !strings.Contains(w.Header().Get("Content-Disposition"), "Synthetic preview note.pdf") {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	ctx, e := api.ReadContext(bytes.NewReader(w.Body.Bytes()), model.NewDefaultConfiguration())
	if e != nil {
		t.Fatal(e)
	}
	if e = ctx.EnsurePageCount(); e != nil || ctx.PageCount != 2 {
		t.Fatal("pages", ctx.PageCount, e)
	}
	if w := libraryGet(t, s, "/boox/notebook.pdf?id=n&version=stale"); w.Code != 409 {
		t.Fatal("stale version", w.Code)
	}
	limit := notebookPDFPages
	notebookPDFPages = 1
	defer func() { notebookPDFPages = limit }()
	if w := libraryGet(t, s, "/boox/notebook.pdf?id=n"); w.Code != 413 || !strings.Contains(w.Body.String(), "limited to 1") {
		t.Fatal("cap", w.Code, w.Body.String())
	}
}
