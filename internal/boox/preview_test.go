package boox

import (
	"bytes"
	"database/sql"
	"html/template"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/web"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

// TestBOOXViewsRender executes every BOOX view with synthetic data through the
// shared site shell. With ALEXANDRIA_UI_PREVIEW_DIR set it also writes them
// next to the core web previews for screenshot review (docs/web-ui.md).
func TestBOOXViewsRender(t *testing.T) {
	now := time.Now()
	type device struct {
		ID, Model  string
		Revoked    bool
		LastUpload sql.NullTime
	}
	type diag struct {
		ID, Revision, Domain, Title string
		Supported                   bool
	}
	listing := listview.State{Sort: "modified", Order: "desc", Total: 2, Start: 1, End: 2, Page: 1, Pages: 1}
	views := []struct {
		name, path, title string
		tmpl              *template.Template
		data              any
	}{
		{"boox-notebooks", "/boox", "BOOX notebooks", libraryTemplate, libraryPage{All: true, Category: "all", Listing: listing, Entries: []notebookEntry{
			{ID: "a", Title: "20261008 Synthetic sync", Kind: "Handwritten note", Pages: 2, CreatedAt: 1, ModifiedAt: 2},
			{ID: "b", Title: "Synthetic meeting", Kind: "Meeting note", Pages: 4, CreatedAt: 1, ModifiedAt: 2}}}},
		{"boox-folders", "/boox", "BOOX notebooks", libraryTemplate, libraryPage{Category: "handwritten", Listing: listing, Folder: "f", Crumbs: []notebookEntry{{ID: "f", Title: "Work"}},
			Entries: []notebookEntry{{ID: "g", Title: "Projects", Folder: true, Kind: "Folder"}}}},
		{"boox-notebook", "/boox/notebook", "Synthetic sync", notebookTemplate, notebookView{ID: "a", Title: "20261008 Synthetic sync", Page: "p1", Number: 1, Count: 2, Next: "#", Zoom: "fit", Image: "static/apple-touch-icon.png", RecognitionEnabled: true}},
		{"boox-reading", "/boox/reading", "Reading", readingTemplate, readingView{Listing: listing, Items: []readingItem{{ID: "r", Title: "Synthetic book", Kind: "Book", CreatedAt: 1, ModifiedAt: 2}, {ID: "h", Title: "Synthetic book", Kind: "Annotation", Quote: "A quoted passage."}}}},
		{"boox-devices", "/boox/devices", "BOOX devices", devicesTemplate, []device{{ID: "8bc4cf5c-0000-4000-8000-000000000001", Model: "Synthetic Go 10.3", LastUpload: sql.NullTime{Time: now, Valid: true}}, {ID: "8bc4cf5c-0000-4000-8000-000000000002", Model: "Retired tablet", Revoked: true}}},
		{"boox-enroll", "/boox/enroll", "Connect a BOOX device", setupTemplate, "ABCD-1234"},
		{"boox-settings", "/boox/settings", "BOOX settings", booxSettings, nil},
		{"boox-diagnostics", "/boox/activity", "BOOX diagnostics", browseTemplate, struct {
			Items                  []diag
			Domains                []string
			Domain, Previous, Next string
			Start, End, Total      int
		}{Items: []diag{{ID: "n1", Revision: "3-abc", Domain: "notebook", Title: "Synthetic sync", Supported: true}, {ID: "m1", Revision: "1-def", Domain: "message", Title: "m1"}}, Domains: []string{"message", "notebook"}, Start: 1, End: 2, Total: 2}},
		{"boox-cleanup", "/boox/cleanup", "Clean up BOOX notebooks", cleanupTemplate, cleanupPage{Token: "t", Total: 2, Items: []cleanupItem{{ID: "a", Title: "Notebook-1", Choice: "a", Pages: 1, Created: 1, Modified: 2}, {ID: "b", Title: "Notebook-23", Choice: "b", Pages: 3}}}},
		{"boox-upload", "/boox/books/upload", "Upload a book", uploadBookTemplate, struct{ Error, State, Name, ID string }{}},
	}
	dir := os.Getenv("ALEXANDRIA_UI_PREVIEW_DIR")
	for _, v := range views {
		w := httptest.NewRecorder()
		web.Deps{PublicURL: "https://library.example", Status: web.Status{NativeBOOX: true}}.RenderContent(w, httptest.NewRequest("GET", v.path, nil), v.tmpl, v.title, "boox", v.data)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", v.name, w.Code, w.Body.String())
		}
		if dir != "" {
			html := bytes.ReplaceAll(w.Body.Bytes(), []byte(`"/static/`), []byte(`"static/`))
			if err := os.WriteFile(filepath.Join(dir, v.name+".html"), html, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}
