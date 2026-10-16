package boox

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
)

// putTag stores a synthetic NOTE_TREE tag record as ksync writes it.
func putTag(t *testing.T, s Service, id, notebook, page, content string, status int) {
	t.Helper()
	b := map[string]any{"_id": id, "uniqueId": id, "modeType": 1, "dbId": s.uid() + "-NOTE_TREE", "user": s.uid(), "documentId": notebook, "pageUniqueId": page, "content": content, "status": status, "createdAt": 1791928241345, "updatedAt": 1791928241345, "revisionId": "rev"}
	if !noteTag(b, s.uid()) {
		t.Fatal("synthetic tag not recognized")
	}
	raw, _ := json.Marshal(b)
	if _, e := s.DB.ExecContext(context.Background(), `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body,supported) VALUES($1,'1-t','notebook','1/<nil>',$2,$3,true) ON CONFLICT(document_id) DO UPDATE SET body=excluded.body`, id, s.uid(), raw); e != nil {
		t.Fatal(e)
	}
}

func TestNoteTagRecognition(t *testing.T) {
	uid := "u"
	tag := map[string]any{"_id": "t", "uniqueId": "t", "modeType": float64(1), "dbId": "u-NOTE_TREE", "user": "u", "content": "mulch", "status": float64(0)}
	if !noteTag(tag, uid) {
		t.Fatal("tag rejected")
	}
	for name, change := range map[string]func(map[string]any){
		"notebook":   func(m map[string]any) { m["type"] = float64(1) },
		"annotation": func(m map[string]any) { m["dbId"] = "u-READER_LIBRARY" },
		"other user": func(m map[string]any) { m["user"] = "v" },
		"no content": func(m map[string]any) { delete(m, "content") },
		"commit":     func(m map[string]any) { m["commitType"] = "x" },
	} {
		m := map[string]any{}
		for k, v := range tag {
			m[k] = v
		}
		change(m)
		if noteTag(m, uid) {
			t.Fatal("accepted", name)
		}
	}
}

func TestBOOXTagsSearchAndPages(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_page_index(notebook_id,page_id,page_number,state,text,input_version) VALUES('note','page',1,'ready','Cluster scheduler upgrade notes for the spring maintenance window.','v1')`); e != nil {
		t.Fatal(e)
	}
	putTag(t, s, "tag-person", "note", "page", "Marisol Vega", 0)
	putTag(t, s, "tag-topic", "note", "page", "mulch", 0)
	putTag(t, s, "tag-gone", "note", "page", "Abandoned", 2)
	putTag(t, s, "tag-common", "", "", "Telescope time", 0)
	putTag(t, s, "tag-elsewhere", "missing-notebook", "p", "Orphan", 0)
	var live int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_tag`).Scan(&live); e != nil || live != 4 {
		t.Fatal("live tags", live, e)
	}
	keywords := func() string {
		var k string
		if e := s.DB.QueryRowContext(ctx, `SELECT keywords FROM alexandria_note_content WHERE source='boox-page' AND source_document_id='note'`).Scan(&k); e != nil {
			t.Fatal(e)
		}
		return k
	}
	if k := keywords(); k != "Marisol Vega mulch" {
		t.Fatalf("page keywords %q", k)
	}
	// A tag finds its page although the page text doesn't mention it.
	search := func(q string) []notes.Result {
		r, e := notes.Searcher{DB: s.DB}.SearchScoped(ctx, q, 10, false, "boox")
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	if r := search("Vega"); len(r) != 1 || r[0].PageID != "page" || strings.Join(r[0].Tags, "|") != "Marisol Vega|mulch" {
		t.Fatalf("tag search %+v", r)
	}
	if r := search("Abandoned"); len(r) != 0 {
		t.Fatal("removed tag searchable", r)
	}

	get := func(target string) string {
		w := httptest.NewRecorder()
		s.browser(w, httptest.NewRequest("GET", target, nil))
		if w.Code != 200 {
			t.Fatal(target, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	list := get("/boox/tags")
	for _, want := range []string{`<a href="/boox/tags" aria-current="page">Tags</a>`, `href="/boox/tags?tag=Marisol%20Vega"`, "Telescope time", "Not on any page", "Orphan"} {
		if !strings.Contains(list, want) {
			t.Fatal("tag list lacks", want, list)
		}
	}
	if strings.Contains(list, "Abandoned") {
		t.Fatal("removed tag listed")
	}
	if body := get("/boox/tags?tag=MULCH"); !strings.Contains(body, `href="/boox/notebook?id=note&amp;page_id=page"`) || !strings.Contains(body, "Synthetic, page 1") || !strings.Contains(body, "Cluster scheduler upgrade") {
		t.Fatal("tagged pages", body)
	}
	if body := get("/boox/tags?tag=Orphan"); !strings.Contains(body, "No live notebook page has this tag.") {
		t.Fatal("orphan tag pages", body)
	}
	if body := get("/boox/notebook?id=note&page=1"); !strings.Contains(body, "On this page") || !strings.Contains(body, `href="/boox/tags?tag=mulch"`) {
		t.Fatal("notebook view tags", body)
	}

	// Removing a tag on the device takes it out of search and the page.
	putTag(t, s, "tag-person", "note", "page", "Marisol Vega", 1)
	if k := keywords(); k != "mulch" {
		t.Fatalf("keywords after removal %q", k)
	}
	if r := search("Vega"); len(r) != 0 {
		t.Fatal("removed tag still finds the page", r)
	}
	if _, e := s.DB.ExecContext(ctx, `DELETE FROM boox_projection WHERE document_id='tag-topic'`); e != nil {
		t.Fatal(e)
	}
	if k := keywords(); k != "" {
		t.Fatalf("keywords after delete %q", k)
	}
}
