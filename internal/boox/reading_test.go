package boox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
)

func readingFixture(t *testing.T) Service {
	t.Helper()
	s := enrolledImportService(t)
	ctx := context.Background()
	for _, book := range []struct {
		id     string
		status int
		stored bool
	}{{"stored", 1, true}, {"elsewhere", 2, false}, {"new", 0, true}} {
		doc := putSearchRecord(t, s, s.uid(), book.id, 4, 0, "")
		raw, _ := json.Marshal(map[string]any{"authors": []string{"Synthetic Author"}, "readingStatus": book.status, "lastAccess": 1760000000000, "progress": "native:1/3"})
		if _, e := s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=body||$2 WHERE document_id=$1`, doc, raw); e != nil {
			t.Fatal(e)
		}
		if book.stored {
			tx, e := s.DB.BeginTx(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			a, _ := s.stagePublicationAsset(ctx, []byte("%PDF-1.4\nfixture\n%%EOF\n"), "application/pdf")
			e = s.bindPublicationAsset(ctx, tx, s.uid()+"/reader/"+book.id+"/book/source.pdf", a)
			if e != nil {
				tx.Rollback()
				t.Fatal(e)
			}
			if e = tx.Commit(); e != nil {
				t.Fatal(e)
			}
		}
	}
	putSearchRecord(t, s, s.uid(), "highlight", 1, 0, "stored")
	putSearchRecord(t, s, s.uid(), "bookmark", 2, 0, "stored")
	putSearchRecord(t, s, s.uid(), "removed", 4, 1, "")
	putSearchRecord(t, s, s.uid(), "orphan", 1, 0, "removed")
	importFixtureFolder(t, s, "shelf", "Synthetic Shelf", "")
	importFixtureFolder(t, s, "long-shelf", "A deliberately long synthetic folder name for narrow upload destinations", "")
	raw, _ := json.Marshal(map[string]any{"modeType": 5, "status": 0, "documentUniqueId": "stored", "libraryUniqueId": "shelf"})
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-test','reading','5',$2,$3)`, s.uid()+"#member", s.uid(), raw); e != nil {
		t.Fatal(e)
	}
	return s
}
func TestReadingBookshelfAvailabilityAndDetails(t *testing.T) {
	s := readingFixture(t)
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Admin(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return w
	}
	html := get("/boox/reading").Body.String()
	if !strings.Contains(html, ">Needle title stored<") || strings.Contains(html, ">Needle title elsewhere<") || strings.Contains(html, ">Needle title highlight<") {
		t.Fatal("stored default or book-only list")
	}
	html = get("/boox/reading?view=metadata").Body.String()
	if strings.Contains(html, ">Needle title stored<") || !strings.Contains(html, ">Needle title elsewhere<") {
		t.Fatal("metadata filter")
	}
	html = get("/boox/reading?view=all&folder=shelf").Body.String()
	if !strings.Contains(html, ">Needle title stored<") || strings.Contains(html, ">Needle title elsewhere<") {
		t.Fatal("folder filter")
	}
	html = get("/boox/reading?view=all&folder=unfiled").Body.String()
	if strings.Contains(html, ">Needle title stored<") || !strings.Contains(html, ">Needle title elsewhere<") {
		t.Fatal("unfiled filter")
	}
	html = get("/boox/reading?view=stats").Body.String()
	if strings.Contains(html, ">Needle title removed<") || !strings.Contains(html, "timed reading history has not been imported.") || strings.Count(html, "<strong>1</strong>") != 5 {
		t.Fatal("stats counts or visibility")
	}
	html = get("/boox/reading?view=all&selected=" + s.uid() + "%23stored").Body.String()
	if !strings.Contains(html, "Highlights &amp; bookmarks") || strings.Count(html, "class=\"note-item\"") != 2 {
		t.Fatal("book detail children")
	}
	html = get("/boox/reading?view=all&q=Synthetic+Author&status=2").Body.String()
	if !strings.Contains(html, ">Needle title elsewhere<") || strings.Contains(html, ">Needle title stored<") {
		t.Fatal("search and status")
	}
	w := get("/boox/reading?view=metadata&layout=list")
	req := httptest.NewRequest("GET", "/boox/reading", nil)
	for _, c := range w.Result().Cookies() {
		req.AddCookie(c)
	}
	w = httptest.NewRecorder()
	s.Admin(w, req)
	if !strings.Contains(w.Body.String(), `class="lib-rows"`) || strings.Contains(w.Body.String(), "cover-grid") || strings.Contains(w.Body.String(), ">Needle title stored<") {
		t.Fatal("browser preferences")
	}
	// A cover binding and native fileSyncStatus never imply the source is stored.
	if _, e := s.DB.ExecContext(context.Background(), `UPDATE boox_projection SET body=body||'{"fileSyncStatus":1,"nativeAbsolutePath":"/local/book.epub"}' WHERE document_id=$1`, s.uid()+"#elsewhere"); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(get("/boox/reading").Body.String(), ">Needle title elsewhere<") {
		t.Fatal("native path treated as server source")
	}
}

func TestReadingBrowserFixture(t *testing.T) {
	dir := os.Getenv("ALEXANDRIA_READING_BROWSER")
	if dir == "" {
		t.Skip("browser driver not requested")
	}
	s := readingFixture(t)
	g := installImportGateway(t, &s)
	static := web.Handler(web.Deps{})
	// Include native fixture folders for revision-fenced destination checks.
	rows, e := s.DB.QueryContext(context.Background(), `SELECT document_id,body FROM boox_projection WHERE body->>'modeType'='3'`)
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id string
		var raw []byte
		rows.Scan(&id, &raw)
		var b map[string]any
		json.Unmarshal(raw, &b)
		b["_rev"] = "1-test"
		g.docs[id] = b
	}
	rows.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/static/") {
			static.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/__work" && r.Method == "POST" {
			if _, e := s.PublishOne(r.Context()); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			if _, e := s.ProcessBookImport(r.Context()); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			g.mu.Lock()
			defer g.mu.Unlock()
			for id, b := range g.docs {
				if b["modeType"] == nil {
					continue
				}
				raw, _ := json.Marshal(b)
				if _, e := s.DB.ExecContext(r.Context(), `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-test','reading','',$2,$3) ON CONFLICT(document_id) DO UPDATE SET body=excluded.body`, id, s.uid(), raw); e != nil {
					http.Error(w, e.Error(), 500)
					return
				}
			}
			w.WriteHeader(204)
			return
		}
		s.Admin(w, r)
	}))
	defer server.Close()
	s.Config.PublicURL = server.URL
	if e := os.WriteFile(filepath.Join(dir, "url"), []byte(server.URL), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dir, "test.epub"), testEPUB(t), 0600); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(15 * time.Minute)
	for time.Now().Before(deadline) {
		if _, e := os.Stat(filepath.Join(dir, "stop")); e == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser driver timeout")
}
