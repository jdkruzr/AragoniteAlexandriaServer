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

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
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
	start := time.Date(2026, 10, 13, 21, 50, 41, 0, time.UTC)
	recs := []minutes.Transcript{
		{Source: minutes.Source{Key: "resource/data/a.mp3", SHA256: "a", Bytes: 742656, Title: "Recording1", RecordedAt: start, DurationMS: 185400}, State: "ready", Language: "en", Text: "Synthetic discussion.",
			Segments: []minutes.Segment{{Start: 0, End: 4.2, Text: "Let's review the synthetic schedule."}, {Start: 4.2, End: 9, Text: "We agreed to ship on Friday."}}},
		{Source: minutes.Source{Key: "resource/data/b.mp3", SHA256: "b", Bytes: 120000, Title: "Recording2", RecordedAt: start.Add(10 * time.Minute), DurationMS: 30000}, State: "ready", Language: "en", Text: "A short follow-up."},
	}
	ready := minutes.Stored{State: "ready", Model: "synthetic-llm", Recordings: recs, FinishedAt: sql.NullTime{Time: start.Add(time.Hour), Valid: true},
		Minutes: minutes.Minutes{Topic: "Synthetic release planning", Keywords: []string{"schedule", "Friday", "testing", "release notes"},
			KeyPoints:  []minutes.Point{{Text: "The release moves to Friday so testing can finish.", From: "both"}, {Text: "Release notes need an owner.", From: "notes"}, {Text: "Support asked for a rollback plan.", From: "recording"}},
			Conclusion: "Ship on Friday once testing passes; assign release notes and write a rollback plan."},
		Metadata: minutes.Metadata{Title: "Synthetic meeting", Folder: "Work", Created: start, PageCount: 4, PagesWithText: 3, Summarizer: "synthetic-llm", Transcriber: "whisper-large-v3-turbo",
			Recordings: []minutes.RecordingSummary{{Title: "Recording1", RecordedAt: start, DurationMS: 185400}, {Title: "Recording2", RecordedAt: start.Add(10 * time.Minute), DurationMS: 30000}}}}
	progress := ready
	progress.State = "processing"
	progress.Recordings = []minutes.Transcript{recs[0], {Source: recs[1].Source, State: "processing"}}
	failed := minutes.Stored{State: "failed", Detail: "A recording could not be transcribed: Transcription provider returned HTTP 503.", Recordings: progress.Recordings}
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
		{"boox-notebook-minutes", "/boox/notebook", "Synthetic meeting", notebookTemplate, notebookView{ID: "b", Title: "Synthetic meeting", Page: "p1", Number: 1, Count: 4, Next: "#", Zoom: "fit", Image: "static/apple-touch-icon.png", Recordings: 2, MinutesState: "ready", MinutesTopic: ready.Topic}},
		{"boox-notebook-recordings", "/boox/notebook", "Synthetic meeting", notebookTemplate, notebookView{ID: "b", Title: "Synthetic meeting", Page: "p1", Number: 1, Count: 4, Zoom: "fit", Recordings: 1}},
		{"boox-minutes", "/boox/minutes", "Minutes", minutesTemplate, minutesView{ID: "b", Title: "Synthetic meeting", Has: true, Stored: ready, Recordings: 2}},
		{"boox-minutes-stale", "/boox/minutes", "Minutes", minutesTemplate, minutesView{ID: "b", Title: "Synthetic meeting", Has: true, Stale: true, Stored: ready, Recordings: 2}},
		{"boox-minutes-progress", "/boox/minutes", "Minutes", minutesTemplate, minutesView{ID: "b", Title: "Synthetic meeting", Has: true, Progress: "Transcribing recordings: 1 of 2 done.", Stored: progress, Recordings: 2}},
		{"boox-minutes-failed", "/boox/minutes", "Minutes", minutesTemplate, minutesView{ID: "b", Title: "Synthetic meeting", Has: true, Stored: failed, Recordings: 2}},
		{"boox-minutes-empty", "/boox/minutes", "Minutes", minutesTemplate, minutesView{ID: "b", Title: "Synthetic meeting", Recordings: 1}},
		{"boox-minutes-new", "/boox/minutes/new", "Summarize", minutesRequestTemplate, minutesRequestView{ID: "b", Title: "Synthetic meeting", Version: "v", Request: "r", Transcriber: "whisper-large-v3-turbo", Summarizer: "synthetic-llm", Enabled: true, RecognitionEnabled: true, Recordings: []minutes.Source{recs[0].Source, {Key: "resource/data/c.mp3", Title: "c", Bytes: 2048}}, Pages: 4, WithText: 1, Missing: 2, InProgress: 1}},
		{"boox-text-note", "/boox/text-notes/new", "Create a test text note", textNoteTemplate, textNoteView{Request: "r", Title: "Synthetic_Minutes", ID: "op", State: "published", Folders: []struct{ ID, Title string }{{"f1", "Work"}, {"f2", "Personal"}}}},
		{"boox-minutes-new-off", "/boox/minutes/new", "Summarize", minutesRequestTemplate, minutesRequestView{ID: "b", Title: "Synthetic meeting"}},
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
