package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A file named and sized like a book already on its destination shelf is
// flagged when it's picked, and still imported: the content check decides.
func TestImportFlagsLookAlikeBooks(t *testing.T) {
	ctx := context.Background()
	s := enrolledImportService(t)
	importFixtureFolder(t, s, "shelf", "Work", "")
	book, _ := json.Marshal(map[string]any{"uniqueId": "bk", "modeType": 4, "status": 0, "name": "Dune.epub", "title": "Dune"})
	member, _ := json.Marshal(map[string]any{"uniqueId": "m1", "modeType": 5, "status": 0, "documentUniqueId": "bk", "libraryUniqueId": "shelf"})
	for _, sql := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-test','reading','4',$2,$3),($4,'1-test','reading','5',$2,$5)`, []any{s.uid() + "#bk", s.uid(), book, s.uid() + "#m1", member}},
		{`WITH v AS (INSERT INTO boox_blob_version(native_key,sha256,md5,bytes) VALUES($1,repeat('a',64),repeat('b',32),1234) RETURNING id,native_key) INSERT INTO boox_blob_live(native_key,version_id) SELECT native_key,id FROM v`, []any{s.uid() + "/reader/bk/book/bk.epub"}},
	} {
		if _, e := s.DB.ExecContext(ctx, sql.q, sql.args...); e != nil {
			t.Fatal(e)
		}
	}
	var generation string
	if e := s.DB.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1`).Scan(&generation); e != nil {
		t.Fatal(e)
	}
	flags := func(destination string, files ...importFile) []bool {
		t.Helper()
		v, e := s.planBookImport(ctx, importRequest{RequestID: uuid.NewString(), Destination: destination, Files: files}, generation)
		if e != nil || !v.Ready {
			t.Fatal(v, e)
		}
		var out []bool
		for _, i := range v.Items {
			if i.Kind == "file" {
				if i.State != "waiting" {
					t.Fatal("look-alike skipped", i)
				}
				out = append(out, i.Duplicate)
			}
		}
		return out
	}
	// Same name (ignoring case) and size on that shelf; another size; another shelf.
	if got := flags("shelf", importFile{"dune.EPUB", 1234}, importFile{"Dune.epub", 999}); !got[0] || got[1] {
		t.Fatal("on the shelf", got)
	}
	if got := flags("", importFile{"Dune.epub", 1234}); got[0] {
		t.Fatal("library root has no such book")
	}
	// The page without script can upload onto a shelf too.
	installImportGateway(t, &s)
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	_ = form.WriteField("folder", "shelf")
	part, _ := form.CreateFormFile("book", "Other.epub")
	_, _ = part.Write(testEPUB(t))
	form.Close()
	r := httptest.NewRequest("POST", "/boox/books/upload", &body)
	r.Header.Set("Content-Type", form.FormDataContentType())
	w := httptest.NewRecorder()
	s.uploadBookHTTP(w, r)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/boox/books/upload?operation=") {
		t.Fatal(w.Code, w.Body.String())
	}
	var destination string
	if e := s.DB.QueryRowContext(ctx, `SELECT b.destination FROM boox_book_import b JOIN boox_book_import_item i ON i.import_id=b.id WHERE i.path='Other.epub' AND i.state='queued'`).Scan(&destination); e != nil || destination != "shelf" {
		t.Fatal(destination, e)
	}
}
