package web

import (
	"context"
	"html"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestRecognitionTabs(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	d := Deps{DB: db, PublicURL: "https://recognition.example.test", Status: Status{NativeBOOX: true, OCR: "synthetic-vl"}}
	s := recognition.Store{DB: db}
	n := recognition.Notebook{ID: "rec-demo", Title: "Synthetic recognition notebook"}
	for _, p := range []string{"p1", "p2", "p3"} {
		n.Pages = append(n.Pages, recognition.Page{ID: p, Number: len(n.Pages) + 1})
	}
	batch, _, err := s.Queue(ctx, "boox", n, 1, 3, false, uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	h := Handler(d)
	call := func(method, path string, values url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", d.PublicURL)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	get := func(path string) string {
		t.Helper()
		w := call("GET", path, nil)
		if w.Code != 200 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		return html.UnescapeString(w.Body.String())
	}
	// Nothing needs attention, so the page opens on Active with the batch.
	page := get("/jobs")
	for _, want := range []string{"Pages are recognized when you ask · synthetic-vl", `aria-current="page">Active 1`, "Synthetic recognition notebook", "0 of 3 pages", `name="key" value="b:` + batch + `"`, `href="/jobs?view=pages"`, `>All</a>`} {
		if !strings.Contains(page, want) {
			t.Errorf("active tab lacks %q", want)
		}
	}
	// A failed page moves the default to Needs attention.
	if _, err = db.ExecContext(ctx, `UPDATE boox_page_index SET state='failed',detail='Synthetic failure' WHERE notebook_id='rec-demo' AND page_id='p2'`); err != nil {
		t.Fatal(err)
	}
	page = get("/jobs")
	for _, want := range []string{`aria-current="page">Needs attention 1`, "Synthetic recognition notebook, p. 2", "Synthetic failure", "Retry the failed page", `name="return" value="/jobs?tab=attention"`} {
		if !strings.Contains(page, want) {
			t.Errorf("attention tab lacks %q", want)
		}
	}
	// Old links naming a state open its tab; other states keep the full list.
	if !strings.Contains(get("/jobs?state=failed&source=boox"), `aria-current="page">Needs attention 1`) {
		t.Error("state=failed")
	}
	if !strings.Contains(get("/jobs?state=ready"), "Every job") || !strings.Contains(get("/jobs?batch="+batch), "Every job") {
		t.Error("full list")
	}
	if strings.Contains(get("/jobs?source=client&tab=attention"), "Synthetic failure") {
		t.Error("source filter")
	}
	// Pausing the group comes back to its tab.
	ret := "/jobs?tab=active"
	w := call("POST", "/jobs/group", url.Values{"key": {"b:" + batch}, "action": {"pause"}, "return": {ret}})
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), ret+"&notice=") {
		t.Fatal(w.Code, w.Header().Get("Location"), w.Body.String())
	}
	if !strings.Contains(get("/jobs?tab=active"), `value="resume"`) {
		t.Error("paused group offers resume")
	}
	// Skipping the failure from its row lands on attention again; the page
	// then shows under Skipped with Un-skip.
	pages, _, err := s.Pages(ctx, "", []string{"failed"}, 0, 10)
	if err != nil || len(pages) != 1 {
		t.Fatal(pages, err)
	}
	w = call("POST", "/jobs/control", url.Values{"id": {pages[0].ID}, "action": {"skip"}, "return": {"/jobs?tab=attention&notice=old"}})
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/jobs?tab=attention&notice=") || strings.Contains(w.Header().Get("Location"), "old") {
		t.Fatal(w.Code, w.Header().Get("Location"))
	}
	if !strings.Contains(get("/jobs?tab=skipped"), `value="unskip"`) {
		t.Error("skipped tab")
	}
	// Another site can't be a way back.
	for _, bad := range []string{"https://evil.example/jobs", "//evil.example/jobs", "/settings"} {
		if got := jobsReturn(bad); got != "" {
			t.Errorf("return %s: %s", bad, got)
		}
	}
}
