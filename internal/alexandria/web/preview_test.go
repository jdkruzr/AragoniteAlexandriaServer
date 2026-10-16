package web

import (
	"bytes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/books"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

// previewPage is one synthetic page state. Previews never touch a database;
// they exist so people and automation can review layout and copy offline.
type previewPage struct {
	file, template, title, section, path string
	data                                 any
}

func previewPages() []previewPage {
	now := time.Now()
	// A first build under way: nothing searchable by meaning yet.
	building := &providers.View{Revision: 7, CanSave: true, IndexState: "building", BuildDone: 232, BuildPending: 1390, BuildPerMinute: 11.2, BuildETA: time.Date(2026, 10, 15, 0, 25, 0, 0, time.UTC),
		SavedEmbedding: providers.EmbedConfig{Enabled: true, URL: "http://ollama:11434", Model: "nomic-embed-text:v1.5"}}
	provider := &providers.View{Revision: 7, CanSave: true, HasKey: true,
		Availability:   providers.Availability{Label: "Ready", Detail: "The saved Ollama provider is reachable and this model is installed.", Ready: true},
		SavedEmbedding: providers.EmbedConfig{URL: "http://ollama:11434", Model: "nomic-embed-text:v1.5"},
		Config: providers.Config{OCR: providers.OCRConfig{Enabled: true, Format: "anthropic", URL: "https://api.example.com", Model: "vision-model"},
			Embedding: providers.EmbedConfig{URL: "http://ollama:11434", Model: "nomic-embed-text:v1.5"}},
		Coverage:        []providers.Coverage{{Label: "Alexandria Client pages", Texts: 12}, {Label: "BOOX reading records", Texts: 158}, {Label: "BOOX notebook pages"}},
		BOOXWithoutText: 1615}
	jobs := recognition.View{Total: 2, Counts: map[string]int{"ready": 1, "failed": 1}, Jobs: []recognition.Job{
		{ID: "j1", Source: "boox", Notebook: "n", Title: "Synthetic meeting", Number: 1, State: "ready", Detail: "Recognized", Model: "vision-model", ImageWidth: 1404, ImageHeight: 1872, ImageBytes: 412000, Created: now, Updated: now},
		{ID: "j2", Source: "client", Notebook: "c", Title: "Synthetic sketch", Number: 2, State: "failed", Detail: "Provider timed out", Attempts: 2, Created: now, Updated: now}}}
	run := &recognition.Run{ID: "r", State: "review", Source: "boox", Created: now, Total: 3, Pages: 9, Unavailable: 1, Awaiting: 8,
		Notebooks: []recognition.RunNotebook{{ID: "a", Title: "Synthetic planning", State: "ready", Pages: 4}, {ID: "b", Title: "Synthetic retro", State: "ready", Pages: 4}, {ID: "c", Title: "Synthetic missing", State: "unavailable", Detail: "Catalog unavailable"}}}
	progress := *run
	progress.State, progress.Total, progress.Pages, progress.Queued, progress.Awaiting, progress.Ready, progress.Processing, progress.Pending, progress.Failed, progress.Finished = "queued", 1040, 1624, 412, 1212, 389, 1, 21, 1, 390
	progress.PerMinute, progress.ETA = 12.4, now.Add(129*time.Minute)
	progress.Notebooks = []recognition.RunNotebook{{ID: "a", Title: "Synthetic planning", State: "queued", Pages: 4, Queued: 4}, {ID: "b", Title: "Synthetic retro", State: "ready", Pages: 4}, {ID: "c", Title: "Synthetic missing", State: "unavailable", Detail: "Catalog unavailable"}}
	due, detail := now.Add(72*time.Hour), "Bring the synthetic budget sheet"
	openTasks := []tasksvc.Task{
		{ID: "t1", Title: "Synthetic follow-up with storage team", Status: tasksvc.StatusNeedsAction, DueAt: &due, Detail: &detail, CreatedAt: now.Add(-48 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
		{ID: "t2", Title: "Synthetic sketch review", Status: tasksvc.StatusNeedsAction, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now.Add(-24 * time.Hour),
			ForestNote: &tasksvc.TaskForestNote{NotebookID: "n", PageID: "p1", NotebookName: "Synthetic notebook"}}}
	doneTasks := []tasksvc.Task{{ID: "t3", Title: "Synthetic invoice sent", Status: tasksvc.StatusCompleted, CreatedAt: now.Add(-96 * time.Hour), UpdatedAt: now.Add(-30 * time.Minute)}}
	trashTasks := []tasksvc.Task{{ID: "t4", Title: "Synthetic duplicate reminder", Status: tasksvc.StatusNeedsAction, Deleted: true, CreatedAt: now.Add(-72 * time.Hour), UpdatedAt: now.Add(-time.Hour)}}
	clientPages := []notes.Page{{ID: "p1", Number: 1, Key: "forestnote://n/p1", BodyText: "Agenda\n- storage pilot\n- budget review", Model: "vision-model"}, {ID: "p2", Number: 2, Key: "forestnote://n/p2"}, {ID: "p3", Number: 3, Key: "forestnote://n/p3", BodyText: "Follow-ups"}}
	return []previewPage{
		{"overview", "overview", "Library", "overview", "/library", overviewData{NativeBOOX: true, OCR: true, JobsKnown: true, Active: 3, Failed: 1,
			Client: SourceSummary{Notebooks: 4, Books: 2, Devices: 1, LastActivity: now.Add(-3 * time.Hour), ActivityLabel: "Last sync"},
			BOOX:   &SourceSummary{Notebooks: 1038, Books: 130, Devices: 3, LastActivity: now.Add(-20 * time.Minute), ActivityLabel: "Last upload"}}},
		{"overview-empty", "overview", "Library", "overview", "/library", overviewData{Client: SourceSummary{Notebooks: 0, Books: 0, Devices: 0, ActivityLabel: "Last sync"}}},
		{"search", "search", "Search", "search", "/search", map[string]any{"Query": "trellis", "Semantic": true, "BOOX": true,
			"Results": []notes.Result{{Source: "boox", Kind: "Notebook page", NotebookName: "Synthetic storage sync", PageNumber: 2, Snippet: "…build the bean trellis before May…", URL: "#"}},
			"Native":  []sourceMatch{{Title: "Synthetic trellis notes", Kind: "Notebook", URL: "#"}}}},
		{"settings-account", "settings", "Account", "settings", "/settings", map[string]any{"LocalPasswords": true}},
		{"settings-search", "settings-search", "Search by meaning", "settings", "/settings/search", map[string]any{"Providers": provider, "DefaultOCRPrompt": notes.DefaultPrompt, "DefaultMinutesInstructions": minutes.DefaultInstructions, "MinutesFormat": minutes.ResponseFormat}},
		{"settings-search-building", "settings-search", "Search by meaning", "settings", "/settings/search", map[string]any{"Providers": building, "DefaultOCRPrompt": notes.DefaultPrompt, "DefaultMinutesInstructions": minutes.DefaultInstructions, "MinutesFormat": minutes.ResponseFormat}},
		{"settings-recognition", "settings-recognition", "Handwriting recognition", "settings", "/settings/recognition", map[string]any{"Providers": provider, "DefaultOCRPrompt": notes.DefaultPrompt, "DefaultMinutesInstructions": minutes.DefaultInstructions, "MinutesFormat": minutes.ResponseFormat}},
		{"settings-integrations", "settings-integrations", "Tasks & integrations", "settings", "/settings/integrations", map[string]any{"Collection": "Tasks", "DueMode": "preserve", "CalDAV": "/caldav/user/calendars/tasks/", "Tokens": []apiToken{{Hash: "h", Label: "Synthetic script", Created: now}}}},
		{"jobs", "jobs", "Recognition jobs", "jobs", "/jobs", map[string]any{"View": jobs, "Filter": recognition.Filter{}, "ProgressTotal": 2, "ProgressDone": 2, "Start": 1, "End": 2, "Refresh": "/jobs", "LiveURL": "/jobs?live=1"}},
		{"jobs-library-start", "jobs-library", "Recognize a library", "jobs", "/jobs/library", map[string]any{"Source": "boox", "NativeBOOX": true, "Enabled": true}},
		{"jobs-library-review", "jobs-library", "Recognize a library", "jobs", "/jobs/library", map[string]any{"Run": run, "Source": "boox", "NativeBOOX": true, "Enabled": true, "Model": "synthetic-vision"}},
		{"jobs-library-progress", "jobs-library", "Recognize a library", "jobs", "/jobs/library", map[string]any{"Run": &progress, "Source": "boox", "NativeBOOX": true, "Enabled": true, "Model": "synthetic-vision"}},
		{"client-notebooks", "notebooks", "Notebooks", "notebooks", "/files/forestnote", map[string]any{"Listing": listing.State{}, "Entries": []notes.Entry{{IsFolder: true, ID: "f", Name: "Synthetic folder"}, {ID: "n", Name: "Synthetic notebook", Status: "partial", PageCount: 3, CreatedAt: 1, ModifiedAt: 2}}}},
		{"client-notebook", "notebook", "Synthetic notebook", "notebooks", "/files/forestnote", notebookReader("n", "Synthetic notebook", clientPages, nil, url.Values{"page": {"p2"}})},
		{"client-notebook-grid", "notebook", "Synthetic notebook", "notebooks", "/files/forestnote", notebookReader("n", "Synthetic notebook", clientPages, nil, url.Values{"view": {"grid"}})},
		{"client-notebooks-empty", "notebooks", "Notebooks", "notebooks", "/files/forestnote", map[string]any{"Listing": listing.State{}}},
		{"client-books", "books", "Books", "books", "/files/forestnote/books", map[string]any{"Listing": listing.State{}, "Entries": []books.LibraryBook{{ID: "b", Title: "Synthetic book", Authors: "A. Author", Format: "EPUB", FileState: "ready", ByteLength: 2 << 20}}}},
		{"client-devices", "devices", "Client devices", "devices", "/settings/devices", map[string]any{"Devices": []relay.Device{{SiteID: "01SYNTHETIC", Name: "Synthetic tablet", Enrolled: true, LastSeenMs: now.UnixMilli(), FirstSeenMs: 1}}}},
		{"tasks", "tasks", "Tasks", "tasks", "/", map[string]any{"Open": openTasks, "Done": doneTasks, "View": taskView{Sort: "due", Dir: "asc"}, "Sorts": taskSorts}},
		{"tasks-empty", "tasks", "Tasks", "tasks", "/", map[string]any{"View": taskView{Sort: "due", Dir: "asc"}, "Sorts": taskSorts}},
		{"tasks-trash", "tasks", "Tasks", "tasks", "/?show=deleted", map[string]any{"Open": trashTasks, "Trash": true, "View": taskView{Trash: true, Sort: "updated", Dir: "desc"}, "Sorts": taskSorts}},
		{"task-edit", "task-edit", "Edit Task", "tasks", "/tasks/t1/edit", map[string]any{"Task": openTasks[0], "Due": "2026-10-20", "Return": "/?sort=updated"}},
		{"task-purge", "task-purge", "Delete Permanently", "tasks", "/tasks/bulk", map[string]any{"Tasks": trashTasks, "Return": "/?show=deleted"}},
		{"sources", "sources", "Add source", "sources", "/sources", nil},
		{"help", "help", "Help", "help", "/help", map[string]any{"NativeBOOX": true}},
	}
}

// renderPreview executes a page exactly as the server would, minus HTTP.
func renderPreview(t *testing.T, p previewPage) []byte {
	t.Helper()
	clone, err := layout.Clone()
	if err == nil {
		_, err = clone.ParseFS(assets, "templates/"+p.template+".html")
	}
	var buf bytes.Buffer
	if err == nil {
		err = clone.ExecuteTemplate(&buf, "layout.html", page{Title: p.title, Section: p.section, SourceTab: sourceTab(p.path, p.section),
			ClientSource: p.section == "notebooks" || p.section == "books" || p.section == "devices" || p.section == "client-settings",
			NativeBOOX:   true, Base: "https://library.example", Data: p.data})
	}
	if err != nil {
		t.Fatalf("%s: %v", p.file, err)
	}
	return buf.Bytes()
}

var idAttr = regexp.MustCompile(` id="([^"]+)"`)

// Every preview renders, uses only registered tips, and has unique element IDs
// (a duplicated tip on one page would break its popover).
func TestPreviewPagesRender(t *testing.T) {
	for _, p := range previewPages() {
		html := renderPreview(t, p)
		seen := map[string]bool{}
		for _, m := range idAttr.FindAllSubmatch(html, -1) {
			if seen[string(m[1])] {
				t.Errorf("%s: duplicate id %q", p.file, m[1])
			}
			seen[string(m[1])] = true
		}
	}
}

// Every registered tip is used by some template, so help text cannot rot unseen.
func TestHelpTipsAreUsed(t *testing.T) {
	var corpus strings.Builder
	_ = fs.WalkDir(assets, "templates", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(assets, path)
			corpus.Write(b)
		}
		return nil
	})
	// BOOX views are feature-owned templates in that package.
	booxFiles, _ := filepath.Glob("../../boox/templates/*")
	for _, f := range booxFiles {
		if b, err := os.ReadFile(f); err == nil {
			corpus.Write(b)
		}
	}
	for _, key := range helpKeys() {
		if !strings.Contains(corpus.String(), `"help" "`+key+`"`) {
			t.Errorf("help tip %q is never shown", key)
		}
	}
	if _, err := tip("no.such.tip"); err == nil {
		t.Fatal("unknown tips must fail rendering")
	}
}

// TestWritePreviews writes standalone HTML for screenshot review when
// ALEXANDRIA_UI_PREVIEW_DIR is set; see docs/web-ui.md.
func TestWritePreviews(t *testing.T) {
	dir := os.Getenv("ALEXANDRIA_UI_PREVIEW_DIR")
	if dir == "" {
		t.Skip("set ALEXANDRIA_UI_PREVIEW_DIR to write previews")
	}
	writePreviewStatic(t, dir)
	for _, p := range previewPages() {
		writePreviewFile(t, dir, p.file, renderPreview(t, p))
	}
}

func writePreviewStatic(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "static"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := fs.ReadDir(assets, "static")
	for _, e := range entries {
		b, _ := fs.ReadFile(assets, "static/"+e.Name())
		if err := os.WriteFile(filepath.Join(dir, "static", e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// writePreviewFile makes site-absolute static URLs relative so file:// works.
func writePreviewFile(t *testing.T, dir, name string, html []byte) {
	t.Helper()
	html = bytes.ReplaceAll(html, []byte(`"/static/`), []byte(`"static/`))
	if err := os.WriteFile(filepath.Join(dir, name+".html"), html, 0o644); err != nil {
		t.Fatal(err)
	}
}
