package web

import (
	"context"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestSearchTabsHighlightsAndPaging(t *testing.T) {
	db := testenv.Database(t).DB
	for _, sql := range []string{
		// BOOX: a notebook titled for the query, with hostile page text, and
		// one with 30 matching pages for paging.
		`INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES
 ('nb','1-a','notebook','1','{"uniqueId":"nb","type":1,"status":1,"title":"Budget 2026","pageNameList":["p1"]}'),
 ('pile','1-a','notebook','1',jsonb_build_object('uniqueId','pile','type',1,'status',1,'title','Pile','pageNameList',(SELECT jsonb_agg('q'||i ORDER BY i) FROM generate_series(1,30)i)))`,
		`INSERT INTO boox_page_index(notebook_id,page_id,page_number,state,text,input_version) VALUES
 ('nb','p1',1,'ready','Quarterly budget <script>alert(1)</script> review ` + "\uE000" + `sneaky` + "\uE001" + ` text','synthetic')`,
		`INSERT INTO boox_page_index(notebook_id,page_id,page_number,state,text,input_version) SELECT 'pile','q'||i,i,'ready','ledger entry '||i,'synthetic' FROM generate_series(1,30)i`,
		// BOOX reading: a book and a highlight in it.
		`INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES
 ('test#book','1-a','reading','','test','{"uniqueId":"book","modeType":4,"title":"The Budget Book","authors":["A. Author"],"status":0}'),
 ('test#hl','1-a','reading','','test','{"uniqueId":"hl","modeType":1,"documentId":"book","quote":"budget cuts hurt","pageNumber":"12","status":0}')`,
		// Client: a notebook with a matching page, and a book.
		`INSERT INTO fn_notebook(id,name,created_at,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('cn','Budget notes',1700000000000,1,1,'test')`,
		`INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('cp','cn',0,1,1,'test')`,
		`INSERT INTO alexandria_note_content(note_key,page,body_text,source) VALUES('forestnote://cn/cp',0,'A client budget page.','forestnote')`,
		`INSERT INTO fn_reader_book(id,asset_id,byte_length,media_type,metadata_json,lww_op_ts,lww_op_seq,lww_site_id) VALUES('cb','asset',1,'application/epub+zip','{"version":1,"title":"Budgeting for Gardeners","creators":["P. Planter"]}',1,1,'test')`,
		// Tasks: from a BOOX note action, finished, deleted, and from a Client page.
		`INSERT INTO alexandria_tasks(task_id,title,detail,status,due_time,created_at,updated_at,deleted,forestnote_notebook_id) VALUES
 ('t1','Send revised budget to Maya',NULL,'needsAction',1793000000000,1,1,false,NULL),
 ('t2','Budget review',NULL,'completed',0,1,1,false,NULL),
 ('t3','Budget deleted',NULL,'needsAction',0,1,1,true,NULL),
 ('t4','Call plumber','Ask about the <b>budget</b> overrun','needsAction',0,1,1,false,'cn')`,
		`INSERT INTO alexandria_note_action_item(source,notebook_id,page_id,color,fingerprint,kind,item_id) VALUES('boox','nb','p1','red','f','task','t1')`,
	} {
		if _, err := db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	// Like production, the handler gets one connection, which can't run a
	// second query while it reads rows.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	h := Handler(Deps{DB: conn, Search: notes.Searcher{DB: conn}, Status: Status{NativeBOOX: true}})
	get := func(path string) string {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: HTTP %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	counts := regexp.MustCompile(`>(All|Notebooks|Pages|Books &amp; highlights|Tasks) <span class="seg-count">(\d+)</span>`)
	tabs := func(body string) string {
		var out []string
		for _, m := range counts.FindAllStringSubmatch(body, -1) {
			out = append(out, m[1]+" "+m[2])
		}
		return strings.Join(out, ", ")
	}
	body := get("/search?q=budget")
	if got := tabs(body); got != "All 10, Notebooks 2, Pages 2, Books &amp; highlights 3, Tasks 3" {
		t.Fatal(got)
	}
	for _, want := range []string{
		`<mark>Budget</mark> 2026`, `Quarterly <mark>budget</mark>  alert(1)  review sneaky text`, // highlighted; markers in stored text are dropped
		`The <mark>Budget</mark> Book, p. 12`, `href="/boox/reading?selected=test%23book"`, // a highlight opens its book
		`<mark>Budget</mark>ing for Gardeners`, `href="/files/forestnote/books?selected=cb"`,
		`<mark>Budget</mark> notes`, `href="/files/forestnote?notebook=cn"`,
		`Ask about the &lt;b&gt;<mark>budget</mark>&lt;/b&gt; overrun`, `10 matches across your library`, // stored markup stays text
	} {
		if !strings.Contains(body, want) {
			t.Fatal("missing", want)
		}
	}
	if strings.Contains(body, "<script>alert") || strings.Contains(body, "\uE000") || strings.Contains(body, "<mark>sneaky") || strings.Contains(body, "Budget deleted") {
		t.Fatal("unsafe or deleted text in results")
	}
	if body = get("/search?q=budget&type=tasks"); !strings.Contains(body, `<mark>Budget</mark> review`) || !strings.Contains(body, "Done") || strings.Contains(body, "2026") {
		t.Fatal("tasks tab", body)
	}
	// A task belongs to a source when that source's page made it.
	if got := tabs(get("/search?q=budget&source=boox")); got != "All 5, Notebooks 1, Pages 1, Books &amp; highlights 2, Tasks 1" {
		t.Fatal("BOOX", got)
	}
	if got := tabs(get("/search?q=budget&source=client")); got != "All 4, Notebooks 1, Pages 1, Books &amp; highlights 1, Tasks 1" {
		t.Fatal("Client", got)
	}
	// Without BOOX, every task is still searched.
	w := httptest.NewRecorder()
	Handler(Deps{DB: conn, Search: notes.Searcher{DB: conn}}).ServeHTTP(w, httptest.NewRequest("GET", "/search?q=budget", nil))
	if got := tabs(w.Body.String()); got != "All 6, Notebooks 1, Pages 1, Books &amp; highlights 1, Tasks 3" {
		t.Fatal("Client only", got)
	}
	// One list, 20 to a page; the old per-group offsets start over.
	page := regexp.MustCompile(`Pile, p\. (\d+)`)
	numbers := func(body string) []string {
		var out []string
		for _, m := range page.FindAllStringSubmatch(body, -1) {
			out = append(out, m[1])
		}
		return out
	}
	body = get("/search?q=ledger&type=pages&sort=title_asc")
	if got := numbers(body); len(got) != 20 || got[0] != "1" || got[1] != "2" || got[9] != "10" || !strings.Contains(body, "1–20 of 30") || !strings.Contains(body, `href="/search?offset=20&amp;q=ledger&amp;sort=title_asc&amp;type=pages#results"`) {
		t.Fatal("first page", got)
	}
	if got := numbers(get("/search?q=ledger&type=pages&sort=title_asc&offset=20")); !reflect.DeepEqual(got, []string{"21", "22", "23", "24", "25", "26", "27", "28", "29", "30"}) {
		t.Fatal("second page", got)
	}
	if body = get("/search?q=ledger&type=pages&pages_offset=20&offset=bad"); !strings.Contains(body, "1–20 of 30") {
		t.Fatal("old offsets")
	}
	// Descending titles reverse the pages too; Spanish labels the sort.
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/search?q=ledger&sort=title_desc", nil)
	r.Header.Set("Accept-Language", "es")
	h.ServeHTTP(w, r)
	if got := numbers(w.Body.String()); w.Code != 200 || got[0] != "30" || !strings.Contains(w.Body.String(), `value="title_desc" selected>Título: Z–A`) {
		t.Fatal("Spanish descending", w.Code, got)
	}
	// An empty tab points back to All.
	if body = get("/search?q=ledger&type=tasks"); !strings.Contains(body, "Nothing of this kind matched.") || !strings.Contains(body, "See all 30 matches") {
		t.Fatal("empty tab")
	}
	for _, bad := range []string{"/search?q=x&source=nope", "/search?q=" + strings.Repeat("x", 257)} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", bad, nil))
		if w.Code != 400 {
			t.Fatal(bad, w.Code)
		}
	}
}

func TestSearchMergesAndMarks(t *testing.T) {
	hit := func(key string) searchHit { return searchHit{key: key} }
	merged := fuse(searchList{Hits: []searchHit{hit("a1"), hit("a2"), hit("a3")}, Total: 7}, searchList{}, searchList{Hits: []searchHit{hit("c1")}, Total: 1})
	var keys []string
	for _, h := range merged.Hits {
		keys = append(keys, h.key)
	}
	if !reflect.DeepEqual(keys, []string{"a1", "c1", "a2", "a3"}) || merged.Total != 8 {
		t.Fatal(keys, merged.Total)
	}
	if got := markTerms("Garden gardens GARDENING ítem", []string{"garden", "gar", "ítem"}); !reflect.DeepEqual(got, []markPart{
		{"Garden", true}, {" ", false}, {"garden", true}, {"s ", false}, {"GARDEN", true}, {"ING ", false}, {"ítem", true}}) {
		t.Fatal(got)
	}
	start, stop := notes.MarkStart, notes.MarkStop
	if got := parseMarks("a " + start + "hit" + stop + " b " + start + "open"); !reflect.DeepEqual(got, []markPart{{"a ", false}, {"hit", true}, {" b ", false}, {"open", true}}) {
		t.Fatal(got)
	}
	if got := around("x "+strings.Repeat("filler ", 20)+"the budget line and "+strings.Repeat("more ", 20), []string{"budget"}, 30); !strings.HasPrefix(got, "…") || !strings.Contains(got, "budget") || !strings.HasSuffix(got, "…") {
		t.Fatal(got)
	}
	if around("nothing here", []string{"budget"}, 30) != "" {
		t.Fatal("no match should give no snippet")
	}
}
