package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestImportPathsAndLimits(t *testing.T) {
	for _, p := range []string{"../a.pdf", "/a.pdf", "a//b.pdf", "a/./b.pdf", "C:/a.pdf", "a\\b.pdf", "a/\nb.pdf", strings.Repeat("x/", 33) + "a.pdf", "a/ /b.pdf"} {
		if validImportPath(p) {
			t.Errorf("unsafe path accepted: %q", p)
		}
	}
	if !validImportPath("Biblioteca/Ciencia/Física.epub") {
		t.Fatal("unicode path rejected")
	}
	v, e := importFiles(importRequest{Files: []importFile{{"books/a.epub", 10}, {"books/no.txt", 10}, {"books/huge.pdf", maxObject + 1}}})
	if e != nil || v[0].State != "waiting" || v[1].State != "skipped" || v[2].State != "skipped" {
		t.Fatal(v, e)
	}
	if _, e = importFiles(importRequest{Files: []importFile{{"a.epub", 10}, {"a.epub", 10}}}); e == nil {
		t.Fatal("duplicate paths accepted")
	}
}

func enrolledImportService(t *testing.T) Service {
	t.Helper()
	s := testService(t)
	if _, e := s.DB.ExecContext(context.Background(), `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); e != nil {
		t.Fatal(e)
	}
	if e := s.ResolveIdentity(context.Background()); e != nil {
		t.Fatal(e)
	}
	return s
}
func importFixtureFolder(t *testing.T, s Service, id, name, parent string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"_id": s.uid() + "#" + id, "uniqueId": id, "user": s.uid(), "dbId": s.uid() + "-READER_LIBRARY", "modeType": 3, "status": 0, "name": name, "parentUniqueId": parent, "fetchSource": 0})
	if _, e := s.DB.ExecContext(context.Background(), `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-test','reading','3',$2,$3)`, s.uid()+"#"+id, s.uid(), raw); e != nil {
		t.Fatal(e)
	}
}

func TestImportFolderResolutionAndSingleConnection(t *testing.T) {
	s := enrolledImportService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Runtime admits a single connection: never query s.DB while its transaction is open.
	db := s.DB
	conn, e := db.(interface {
		Conn(context.Context) (*sql.Conn, error)
	}).Conn(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	s.DB = conn
	importFixtureFolder(t, s, "f1", "Books", "")
	importFixtureFolder(t, s, "f2", "Books", "")
	var generation string
	if e = s.DB.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1`).Scan(&generation); e != nil {
		t.Fatal(e)
	}
	req := importRequest{RequestID: uuid.NewString(), Files: []importFile{{"Books/SciFi/test.pdf", 25}, {"Books/readme.txt", 5}}}
	preview, e := s.planBookImport(ctx, req, generation)
	if e != nil || preview.Ready || len(preview.Items[0].Choices) != 2 {
		t.Fatal(preview, e)
	}
	req.Choices = map[string]string{"Books": "f1"}
	v, e := s.createBookImport(ctx, req)
	if e != nil || v.ID == "" {
		t.Fatal(v, e)
	}
	again, e := s.createBookImport(ctx, req)
	if e != nil || again.ID != v.ID {
		t.Fatal("request retry", again, e)
	}
	req.RequestID = uuid.NewString()
	other, e := s.createBookImport(ctx, req)
	if e != nil || other.Items[1].NativeID != v.Items[1].NativeID {
		t.Fatal("shared folder reservation", other, e)
	}
}

type importGateway struct {
	mu            sync.Mutex
	docs          map[string]map[string]any
	puts          int
	loseFolderACK bool
	failMessage   bool
}

func (g *importGateway) handler(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := strings.TrimPrefix(r.URL.Path, "/neocloud/")
	if r.Method == "GET" {
		if b := g.docs[id]; b != nil {
			json.NewEncoder(w).Encode(b)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	var b map[string]any
	if json.NewDecoder(r.Body).Decode(&b) != nil {
		w.WriteHeader(400)
		return
	}
	if g.failMessage && b["msgType"] != nil {
		w.WriteHeader(503)
		return
	}
	if g.docs[id] != nil {
		w.WriteHeader(409)
		return
	}
	b["_rev"] = "1-test"
	g.docs[id] = b
	g.puts++
	if g.loseFolderACK && b["modeType"] == float64(3) {
		g.loseFolderACK = false
		w.WriteHeader(503)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"rev": "1-test"})
}
func installImportGateway(t *testing.T, s *Service) *importGateway {
	t.Helper()
	g := &importGateway{docs: map[string]map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(g.handler))
	t.Cleanup(server.Close)
	s.Config.GatewayAdminURL = server.URL
	return g
}
func runImport(t *testing.T, s Service, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if _, e := s.DB.ExecContext(context.Background(), `UPDATE boox_book_import_item SET next_attempt_at=now()`); e != nil {
			t.Fatal(e)
		}
		if _, e := s.ProcessBookImport(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
}

func TestImportPublicationResumeAndNoResurrection(t *testing.T) {
	ctx := context.Background()
	s := enrolledImportService(t)
	g := installImportGateway(t, &s)
	raw := testEPUB(t)
	req := importRequest{RequestID: uuid.NewString(), Files: []importFile{{"Books/Science/test.epub", int64(len(raw))}, {"Books/Science/copy.epub", int64(len(raw))}}}
	v, e := s.createBookImport(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	op, e := s.acceptImportFile(ctx, v.ID, req.Files[0].Path, raw)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.acceptImportFile(ctx, v.ID, req.Files[0].Path, raw)
	if e != nil || op != again {
		t.Fatal("file retry", again, e)
	}
	duplicate, e := s.acceptImportFile(ctx, v.ID, req.Files[1].Path, raw)
	if e != nil || duplicate != op {
		t.Fatal("duplicate bytes", duplicate, e)
	}
	if _, e = s.PublishOne(ctx); e != nil {
		t.Fatal(e)
	}
	g.mu.Lock()
	g.loseFolderACK = true
	g.mu.Unlock()
	runImport(t, s, 8)
	status, e := s.bookImportStatus(ctx, v.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range status.Items {
		if i.State != "published" {
			t.Fatal(i)
		}
	}
	modes := map[string]int{}
	g.mu.Lock()
	for _, b := range g.docs {
		modes[fmt.Sprint(b["modeType"])]++
		if b["modeType"] == float64(3) {
			if b["storageId"] != nil {
				t.Error("native internal storage must be null")
			}
			if b["name"] == "Books" && b["parentUniqueId"] != nil {
				t.Error("native root must be null")
			}
		}
	}
	puts := g.puts
	g.mu.Unlock()
	if modes["3"] != 2 || modes["4"] != 1 || modes["5"] != 1 {
		t.Fatal("native tree or duplicate identity", modes)
	}
	runImport(t, s, 2)
	if g.puts != puts {
		t.Fatal("completed import republished")
	}
	// Deleting an already-created native shelf must block a later batch, not restore it.
	g.mu.Lock()
	for _, b := range g.docs {
		if b["modeType"] == float64(3) && b["name"] == "Science" {
			b["status"] = float64(1)
		}
	}
	g.mu.Unlock()
	req.RequestID = uuid.NewString()
	req.Files = req.Files[:1]
	next, e := s.createBookImport(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.acceptImportFile(ctx, next.ID, req.Files[0].Path, raw); e != nil {
		t.Fatal(e)
	}
	runImport(t, s, 4)
	status, e = s.bookImportStatus(ctx, next.ID)
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range status.Items {
		if i.Kind == "file" && i.State != "blocked" {
			t.Fatal("deleted shelf was restored", i)
		}
	}
}

func TestImportGenerationAndChangedBytes(t *testing.T) {
	s := enrolledImportService(t)
	ctx := context.Background()
	raw := []byte("%PDF-1.4\nsynthetic\n%%EOF\n")
	req := importRequest{RequestID: uuid.NewString(), Files: []importFile{{"a.pdf", int64(len(raw))}}}
	v, e := s.createBookImport(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.acceptImportFile(ctx, v.ID, "a.pdf", append(raw, 'x')); e == nil {
		t.Fatal("changed size accepted")
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE sync_library_generation SET generation=$1`, strings.Repeat("f", 64)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.acceptImportFile(ctx, v.ID, "a.pdf", raw); e == nil {
		t.Fatal("old generation accepted")
	}
}
