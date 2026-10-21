package boox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

func putSearchRecord(t *testing.T, s Service, uid, id string, mode int, status any, parent string) string {
	t.Helper()
	doc := uid + "#" + id
	body := map[string]any{"_id": doc, "uniqueId": id, "user": uid, "dbId": uid + "-READER_LIBRARY", "modeType": mode, "title": "Needle title " + id, "documentId": parent}
	if status != nil {
		body["status"] = status
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(context.Background(), `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-test','reading','',$2,$3) ON CONFLICT(document_id) DO UPDATE SET body=excluded.body`, doc, uid, raw); err != nil {
		t.Fatal(err)
	}
	return doc
}

func searchHTML(t *testing.T, s Service, query string) string {
	t.Helper()
	d := web.Deps{DB: s.DB, Search: notes.Searcher{DB: s.DB}, Status: web.Status{NativeBOOX: true}}
	w := httptest.NewRecorder()
	web.Handler(d).ServeHTTP(w, httptest.NewRequest("GET", "/search?source=boox&q="+url.QueryEscape(query), nil))
	if w.Code != 200 {
		t.Fatalf("search HTTP %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func readingHTML(t *testing.T, s Service, doc string) string {
	t.Helper()
	w := httptest.NewRecorder()
	s.Admin(w, httptest.NewRequest("GET", "/boox/reading?view=all&record="+url.QueryEscape(doc), nil))
	if w.Code != 200 {
		t.Fatalf("reading HTTP %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func assertReadingVisibility(t *testing.T, s Service, doc, id string, want bool) {
	t.Helper()
	title := "Needle title " + id
	surfaces := map[string]string{"search": searchHTML(t, s, title), "direct reading": readingHTML(t, s, doc)}
	var mode string
	if err := s.DB.QueryRowContext(context.Background(), `SELECT body->>'modeType' FROM boox_projection WHERE document_id=$1`, doc).Scan(&mode); err == nil && mode == "4" {
		surfaces["reading"] = readingHTML(t, s, "")
	}
	for surface, html := range surfaces {
		// Search echoes the query in its input; only result labels count.
		if got := strings.Contains(html, ">"+title+"<"); got != want {
			t.Fatalf("%s visibility of %s: got %v want %v", surface, id, got, want)
		}
	}
}

func TestReadingSearchLifecycle(t *testing.T) {
	s := testService(t)
	uid := s.uid()
	for _, mode := range []int{1, 2, 4} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			parent := fmt.Sprintf("parent-%d", mode)
			parentDoc := putSearchRecord(t, s, uid, parent, 4, 0, "")
			for _, tc := range []struct {
				name   string
				status any
				live   bool
			}{
				{"active", 0, true}, {"active-string", "0", true}, {"missing", nil, true},
				{"removed", 1, false}, {"removed-string", "1", false}, {"unknown", 2, false}, {"malformed", "wat", false}, {"boolean", false, false},
			} {
				id := fmt.Sprintf("mode-%d-%s", mode, tc.name)
				doc := putSearchRecord(t, s, uid, id, mode, tc.status, parent)
				assertReadingVisibility(t, s, doc, id, tc.live)
			}
			id := fmt.Sprintf("transition-%d", mode)
			doc := putSearchRecord(t, s, uid, id, mode, 0, parent)
			assertReadingVisibility(t, s, doc, id, true)
			putSearchRecord(t, s, uid, id, mode, 1, parent)
			assertReadingVisibility(t, s, doc, id, false)
			putSearchRecord(t, s, uid, id, mode, 0, parent)
			assertReadingVisibility(t, s, doc, id, true)
			if _, err := s.DB.ExecContext(context.Background(), `DELETE FROM boox_projection WHERE document_id=$1`, doc); err != nil {
				t.Fatal(err)
			}
			assertReadingVisibility(t, s, doc, id, false)
			putSearchRecord(t, s, uid, id, mode, 0, parent)
			assertReadingVisibility(t, s, doc, id, true)
			if mode == 4 {
				return
			}
			putSearchRecord(t, s, uid, parent, 4, 1, "")
			assertReadingVisibility(t, s, doc, id, false)
			putSearchRecord(t, s, uid, parent, 4, 0, "")
			assertReadingVisibility(t, s, doc, id, true)
			// Restoring a book must not resurrect an independently removed child.
			removed := fmt.Sprintf("mode-%d-removed", mode)
			assertReadingVisibility(t, s, uid+"#"+removed, removed, false)
			if _, err := s.DB.ExecContext(context.Background(), `DELETE FROM boox_projection WHERE document_id=$1`, parentDoc); err != nil {
				t.Fatal(err)
			}
			assertReadingVisibility(t, s, doc, id, false)
			// Same uniqueId under another account is not this child's parent.
			putSearchRecord(t, s, "other", parent, 4, 0, "")
			assertReadingVisibility(t, s, doc, id, false)
			putSearchRecord(t, s, uid, parent, 4, 0, "")
			assertReadingVisibility(t, s, doc, id, true)
		})
	}
	for _, mode := range []int{1, 2} {
		id := fmt.Sprintf("early-%d", mode)
		doc := putSearchRecord(t, s, uid, id, mode, 0, "late-parent")
		assertReadingVisibility(t, s, doc, id, false)
	}
	putSearchRecord(t, s, uid, "late-parent", 4, 0, "")
	for _, mode := range []int{1, 2} {
		id := fmt.Sprintf("early-%d", mode)
		assertReadingVisibility(t, s, uid+"#"+id, id, true)
	}
	for _, mode := range []int{3, 5, 7} {
		id := fmt.Sprintf("control-%d", mode)
		doc := putSearchRecord(t, s, uid, id, mode, 0, "")
		assertReadingVisibility(t, s, doc, id, false)
	}
	doc := putSearchRecord(t, s, uid, "commit", 4, 0, "")
	if _, err := s.DB.ExecContext(context.Background(), `UPDATE boox_projection SET body=body||'{"commitType":1}' WHERE document_id=$1`, doc); err != nil {
		t.Fatal(err)
	}
	assertReadingVisibility(t, s, doc, "commit", false)
}

func TestReadingVisibilityBeforePagination(t *testing.T) {
	s := testService(t)
	for i := 0; i < 70; i++ {
		putSearchRecord(t, s, s.uid(), fmt.Sprintf("deleted-%02d", i), 4, 1, "")
	}
	doc := putSearchRecord(t, s, s.uid(), "visible", 4, 0, "")
	assertReadingVisibility(t, s, doc, "visible", true)
	if html := readingHTML(t, s, ""); !strings.Contains(html, "1–1 of 1") {
		t.Fatal("deleted records affected pagination")
	}
	if html := searchHTML(t, s, "Needle"); !strings.Contains(html, ">Needle title visible<") || strings.Contains(html, ">Needle title deleted-") {
		t.Fatal("deleted records affected search limit")
	}
}

func TestObservedReadingDeletionAndHistory(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	uid := s.uid()
	book := "observed-book"
	doc := uid + "#" + book
	child := putSearchRecord(t, s, uid, "observed-child", 1, 0, book)
	var revision int
	var status int
	var tombstone bool
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rev := fmt.Sprintf("%d-synthetic", revision)
		if strings.HasSuffix(r.URL.Path, "/_changes") {
			reply(w, 200, map[string]any{"results": []any{map[string]any{"id": doc, "changes": []any{map[string]any{"rev": rev}}, "deleted": tombstone}}, "last_seq": revision})
			return
		}
		if r.URL.Path != "/neocloud/"+doc {
			http.NotFound(w, r)
			return
		}
		if tombstone {
			if r.URL.Query().Get("rev") == "" {
				http.NotFound(w, r)
				return
			}
			reply(w, 200, map[string]any{"_id": doc, "_rev": rev, "_deleted": true})
			return
		}
		reply(w, 200, map[string]any{"_id": doc, "_rev": rev, "user": uid, "dbId": uid + "-READER_LIBRARY", "uniqueId": book, "modeType": 4, "status": status, "title": "Needle title " + book})
	}))
	defer gateway.Close()
	s.Config.GatewayAdminURL = gateway.URL
	for _, step := range []struct {
		status             int
		tombstone, visible bool
	}{{0, false, true}, {1, false, false}, {0, false, true}, {0, true, false}, {0, false, true}} {
		revision++
		status, tombstone = step.status, step.tombstone
		if n, err := s.Observe(ctx); err != nil || n != 1 {
			t.Fatalf("observe %d: %d %v", revision, n, err)
		}
		assertReadingVisibility(t, s, doc, book, step.visible)
		assertReadingVisibility(t, s, child, "observed-child", step.visible)
	}
	var count int
	if err := s.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM boox_revision WHERE document_id=$1`, doc).Scan(&count); err != nil || count != 5 {
		t.Fatalf("retained revisions: %d %v", count, err)
	}
}

func TestNotebookSearchStatusConventions(t *testing.T) {
	s := indexFixture(t)
	for _, tc := range []struct {
		name   string
		status any
		live   bool
	}{
		{"active", 1, true}, {"active-string", "1", true}, {"missing", nil, true},
		{"removed", 0, false}, {"removed-string", "0", false}, {"unknown", 2, false}, {"malformed", "wat", false}, {"boolean", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(tc.status)
			query := `UPDATE boox_projection SET body=jsonb_set(body,'{status}',$1) WHERE document_id='note'`
			if tc.status == nil {
				query = `UPDATE boox_projection SET body=body-'status' WHERE document_id='note'`
				raw = nil
			}
			var err error
			if raw == nil {
				_, err = s.DB.ExecContext(context.Background(), query)
			} else {
				_, err = s.DB.ExecContext(context.Background(), query, raw)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(searchHTML(t, s, "Synthetic"), ">Synthetic<"); got != tc.live {
				t.Fatalf("notebook visibility %v, want %v", got, tc.live)
			}
		})
	}
}

func TestNotebookSearchInvalidationAndLateRecognition(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"notebook soft deletion", `UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id='note'`},
		{"notebook tombstone", `DELETE FROM boox_projection WHERE document_id='note'`},
		{"page removal", `UPDATE boox_projection SET body=jsonb_set(body,'{pageNameList}','[]') WHERE document_id='note'`},
		{"resource deletion", `DELETE FROM boox_blob_live`},
		{"resource replacement", `WITH v AS (INSERT INTO boox_blob_version(native_key,sha256,md5,bytes) SELECT native_key,repeat('a',64),repeat('b',32),12 FROM boox_blob_live RETURNING id,native_key) UPDATE boox_blob_live l SET version_id=v.id FROM v WHERE l.native_key=v.native_key`},
	} {
		for _, during := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/during-recognition=%v", tc.name, during), func(t *testing.T) {
				s := indexFixture(t)
				ctx := context.Background()
				var original, live []byte
				if err := s.DB.QueryRowContext(ctx, `SELECT body FROM boox_projection WHERE document_id='note'`).Scan(&original); err != nil {
					t.Fatal(err)
				}
				if err := s.DB.QueryRowContext(ctx, `SELECT jsonb_agg(jsonb_build_array(native_key,version_id)) FROM boox_blob_live`).Scan(&live); err != nil {
					t.Fatal(err)
				}
				run := func(o *testOCR, prompt string) {
					t.Helper()
					if found, err := s.ProcessPageIndex(ctx, o, prompt); err != nil || !found {
						t.Fatalf("recognition: %v %v", found, err)
					}
				}
				if err := s.QueuePage(ctx, "note", 1); err != nil {
					t.Fatal(err)
				}
				run(&testOCR{}, "")
				if !strings.Contains(searchHTML(t, s, "quasar"), "synthetic quasar transcript") {
					t.Fatal("initial OCR missing")
				}
				change := func() {
					if _, err := s.DB.ExecContext(ctx, tc.sql); err != nil {
						t.Fatal(err)
					}
				}
				if during {
					if err := s.QueuePage(ctx, "note", 1); err != nil {
						t.Fatal(err)
					}
					run(&testOCR{hook: change}, "changed prompt forces inference")
				} else {
					change()
				}
				assertInvalid := func() {
					t.Helper()
					var state, text string
					if err := s.DB.QueryRowContext(ctx, `SELECT state,text FROM boox_page_index`).Scan(&state, &text); err != nil {
						t.Fatal(err)
					}
					if state == "ready" || text != "" {
						t.Fatalf("stale index: %s %q", state, text)
					}
					if strings.Contains(searchHTML(t, s, "quasar"), "synthetic quasar transcript") {
						t.Fatal("stale OCR searchable")
					}
				}
				assertInvalid()
				if _, err := s.DB.ExecContext(ctx, `UPDATE boox_page_index SET next_at=now()`); err != nil {
					t.Fatal(err)
				}
				o := &testOCR{}
				if tc.name == "notebook soft deletion" {
					if found, err := s.ProcessPageIndex(ctx, o, ""); err != nil || found {
						t.Fatalf("deleted notebook was claimed: %v %v", found, err)
					}
				} else {
					run(o, "")
				}
				if o.calls != 0 {
					t.Fatal("deleted/incomplete content sent to OCR")
				}
				assertInvalid()
				// Restoring both current metadata and resources makes the requested page
				// eligible again; retained cached OCR alone never makes it searchable.
				if _, err := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES('note','restored','notebook','1',$1,$2) ON CONFLICT(document_id) DO UPDATE SET body=excluded.body,revision=excluded.revision`, s.uid(), original); err != nil {
					t.Fatal(err)
				}
				if _, err := s.DB.ExecContext(ctx, `INSERT INTO boox_blob_live(native_key,version_id) SELECT e->>0,(e->>1)::bigint FROM jsonb_array_elements($1::jsonb) e ON CONFLICT(native_key) DO UPDATE SET version_id=excluded.version_id`, live); err != nil {
					t.Fatal(err)
				}
				if err := s.QueuePage(ctx, "note", 1); err != nil {
					t.Fatal(err)
				}
				run(&testOCR{}, "")
				if !strings.Contains(searchHTML(t, s, "quasar"), "synthetic quasar transcript") {
					t.Fatal("restored page not searchable")
				}
			})
		}
	}
}
