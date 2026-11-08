package web

import (
	"bytes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/books"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/oauth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
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
		{"search", "search", "Search", "search", "/search", searchPreview("trellis", "all", map[string]int{"notebooks": 1, "pages": 1, "books": 1, "tasks": 1}, []previewHit{
			{"Notebook", "notebook", "Synthetic trellis notes", "", []string{"BOOX", "3 pages"}, nil},
			{"Page text", "text-aa", "Synthetic garden plan, p. 2", "…build the bean trellis before May…", []string{"BOOX", "recognized"}, []string{"Bean Trellis", "garden"}},
			{"Book", "book", "The Synthetic Trellis Handbook", "A. Gardener", []string{"Client", "EPUB"}, nil},
			{"Task", "check-square", "Buy trellis netting", "Two rolls, <b>not</b> the plastic kind", []string{"No due date", "Done"}, nil}}, nil)},
		{"search-tasks-empty", "search", "Search", "search", "/search", searchPreview("trellis", "tasks", map[string]int{"notebooks": 1, "pages": 1}, nil, nil)},
		{"search-none", "search", "Search", "search", "/search", searchPreview("quasar", "all", nil, nil, map[string]any{"DateSort": true})},
		{"search-start", "search", "Search", "search", "/search", map[string]any{"State": searchState{Type: "all"}, "Semantic": true, "BOOX": true, "Sorts": searchSorts}},
		{"calendar-week", "calendar", "Calendar", "calendar", "/calendar", calendarPreview("week", nil, nil)},
		{"calendar-day-editor", "calendar", "Calendar", "calendar", "/calendar", calendarPreview("day", &calendarEditor{Mode: "edit", Event: events.Event{ID: "team-sync", Title: "Team sync"},
			Form: map[string]string{"date": "2026-10-27", "start": "10:00", "end": "11:00"}, Offsets: []zoneOffset{{"-18000", "CDT -0500"}}, Source: &NoteSource{NotebookID: "n", PageID: "p", Notebook: "Q4 planning — meetings", Page: 12}}, nil)},
		{"calendar-new-problem", "calendar", "Calendar", "calendar", "/calendar", calendarPreview("week", &calendarEditor{Mode: "new", Event: events.Event{Title: "Synthetic night ferry", URL: "https://example.org/ferry"},
			Form: map[string]string{"date": "2026-03-08", "start": "02:30", "end_date": "2026-03-09"}, Problem: "That local time does not exist in this time zone, or the UTC offset does not match. Choose another time or offset.", Offsets: []zoneOffset{{"-21600", "CST -0600"}, {"-18000", "CDT -0500"}}}, nil)},
		{"calendar-month", "calendar", "Calendar", "calendar", "/calendar", calendarPreview("month", nil, nil)},
		{"calendar-upcoming", "calendar", "Calendar", "calendar", "/calendar", calendarPreview("upcoming", &calendarEditor{Mode: "recurring", Event: events.Event{ID: "s", Title: "Garden committee", Location: "Community hall"}, When: "Tue, Oct 27 · 18:00–19:30"}, nil)},
		{"calendar-filters", "calendar", "Calendar", "calendar", "/calendar", func() map[string]any {
			d := calendarPreview("week", &calendarEditor{Mode: "subscribed", Event: events.Event{Title: "Halloween", Detail: "Synthetic public holiday"}, Detail: "Synthetic public holiday", When: "Sat, Oct 31 · All day", Snapshot: "Oct 19",
				Subscription: subscriptions.Subscription{Name: "School term dates", Color: "green", Enabled: true, ErrorCode: "http_404"}}, nil)
			v := d["View"].(calendarView)
			v.Filters, v.Filter = true, calendarFilter{Hidden: "tasks", Query: "garden"}
			d["View"], d["Filters"] = v, filterForm(v, nil)
			last := now.Add(-13 * 24 * time.Hour)
			d["Notices"] = []feedNotice{{Subscription: subscriptions.Subscription{Name: "School term dates", Enabled: true, ErrorCode: "http_404", LastSuccess: &last}, Snapshot: "Oct 19"}}
			return d
		}()},
		{"calendar-deleted", "calendar", "Deleted Events", "calendar", "/calendar?show=deleted", calendarPreview("week", nil, map[string]any{"Trash": true, "Deleted": []deletedEvent{
			{Event: events.Event{ID: "d1", Title: "Synthetic dentist"}, When: "Thu, Oct 22 · 08:30–09:30", Source: &NoteSource{NotebookID: "n", PageID: "p", Notebook: "Synthetic errands", Page: 3}},
			{Event: events.Event{ID: "d2", Title: "Synthetic standup"}, When: "Mon, Oct 19 · 09:00–09:15", Recurring: true}}})},
		{"settings-account", "settings", "Account", "settings", "/settings?section=language&saved=1&notice=Language+saved.", map[string]any{"LocalPasswords": true}},
		{"settings-devices", "settings-devices", "Devices", "settings", "/settings/devices", map[string]any{"Devices": devicesView{
			BOOX: &BOOXDevices{Summary: SourceSummary{Notebooks: 1038, Books: 130, Devices: 2, LastActivity: now.Add(-4 * time.Minute)}, Waiting: true, Devices: []BOOXDevice{
				{ID: "8bc4cf5c-0000-4000-8000-000000000001", Model: "Synthetic Air4 C", LastUpload: now.Add(-4 * time.Minute)},
				{ID: "8bc4cf5c-0000-4000-8000-000000000002", Model: "Synthetic Go 7", New: true},
				{ID: "8bc4cf5c-0000-4000-8000-000000000003", Model: "Retired tablet", Revoked: true}}},
			Client: SourceSummary{Notebooks: 4, Books: 2, Devices: 2, LastActivity: now.Add(-time.Hour)},
			ClientDevices: []relay.Device{{SiteID: "01SYNTHETIC", Name: "Synthetic tablet", Enrolled: true, LastSeenMs: now.Add(-time.Hour).UnixMilli(), FirstSeenMs: now.Add(-30 * 24 * time.Hour).UnixMilli()},
				{SiteID: "01SYNTHETIC2", Name: "Synthetic phone", Enrolled: true, NeedsAdoption: true, PendingOps: 14, LastSeenMs: now.Add(-6 * 24 * time.Hour).UnixMilli(), FirstSeenMs: now.Add(-40 * 24 * time.Hour).UnixMilli()}}}}},
		{"settings-models", "settings-models", "AI models", "settings", "/settings/models?section=provider&saved=1&notice=Provider+settings+saved.", providerPreview(provider)},
		{"settings-automatic", "settings-automatic", "Automatic processing", "settings", "/settings/automatic", providerPreview(provider)},
		{"settings-actions", "settings-actions", "Note actions", "settings", "/settings/actions", providerPreview(provider)},
		{"settings-search", "settings-search", "Search by meaning", "settings", "/settings/search", providerPreview(provider)},
		{"settings-search-building", "settings-search", "Search by meaning", "settings", "/settings/search", providerPreview(building)},
		{"settings-calendar", "settings-calendar", "Calendar & tasks", "settings", "/settings/calendar", map[string]any{"Collection": "Tasks", "EventsCollection": "Calendar", "DueMode": "preserve", "ServerZone": "America/Chicago",
			"Subscriptions": []subscriptions.Subscription{{ID: "s1", Name: "Synthetic holidays", Color: "blue", Enabled: true, LastSuccess: &now}}, "SubscriptionColors": subscriptions.Colors}},
		{"settings-api", "settings-api", "API & MCP", "settings", "/settings/api", map[string]any{"NewToken": "alx_synthetic_token_value", "NewLabel": "Synthetic script", "NewReadOnly": true, "OAuthLabel": oauth.TokenLabel,
			"Tokens": []apiToken{{Hash: "h", Label: "Synthetic script", ReadOnly: true, Created: now}, {Hash: "h2", Label: "Synthetic assistant", OAuth: true, Created: now, LastUsed: &now}, {Hash: "h3", Label: oauth.TokenLabel, OAuth: true, Created: now}}}},
		{"authorize", "authorize", "Allow Access?", "", "/authorize", map[string]any{"Request": oauth.Request{ClientName: "Synthetic assistant", RedirectURI: "https://assistant.example/callback", Scope: auth.ScopeRead}, "Query": "synthetic"}},
		{"settings-operator", "settings-operator", "Operator tools", "settings", "/settings/operator", func() map[string]any {
			m := providerPreview(provider)
			m["Operator"] = operatorView{SyncDevices: []relay.Device{{SiteID: "01SYNTHETIC", Name: "Synthetic tablet", Enrolled: true, LastSeenMs: now.UnixMilli()}},
				BOOX: &BOOXOperator{Numbered: 3, Deleted: 12}, Obsolete: 4, Server: serverFacts{Database: 245 << 20, Files: 1869 << 20, Postgres: "17.11"}}
			return m
		}()},
		{"jobs-size", "jobs-size", "Notebook size limits", "jobs", "/jobs/size", map[string]any{"Limit": 100, "Rows": []recognition.SizeChoice{{Choice: recognition.Choice{Source: "boox", ID: "n", Title: "Synthetic archive"}, Pages: 240}}}},
		{"jobs-active", "jobs", "Recognition", "jobs", "/jobs", recognitionFixture("active")},
		{"jobs-attention", "jobs", "Recognition", "jobs", "/jobs", recognitionFixture("attention")},
		{"jobs-skipped", "jobs", "Recognition", "jobs", "/jobs", recognitionFixture("skipped")},
		{"jobs-finished", "jobs", "Recognition", "jobs", "/jobs", recognitionFixture("finished")},
		{"jobs-work", "jobs", "Recognition", "jobs", "/jobs", recognitionFixture("work")},
		{"jobs-pages", "jobs-pages", "Every job", "jobs", "/jobs", map[string]any{"View": jobs, "Filter": recognition.Filter{}, "ProgressTotal": 2, "ProgressDone": 2, "Start": 1, "End": 2, "Refresh": "/jobs", "Work": WorkSummary{
			Actions: WorkGroup{Counts: map[string]int{"ready": 14, "failed": 1}, Rows: []WorkRow{{Title: "20261016 Synthetic plans", Link: "#", State: "failed", Detail: "Notebook unavailable.", Color: "blue", Action: "event", Updated: time.Date(2026, 10, 16, 22, 11, 0, 0, time.UTC)}}},
			Minutes: WorkGroup{Counts: map[string]int{"ready": 2, "processing": 1}, Rows: []WorkRow{{Title: "Synthetic meeting", Link: "#", State: "processing", Detail: "Transcribing recordings.", Updated: time.Date(2026, 10, 17, 1, 5, 0, 0, time.UTC)}}}}}},
		{"jobs-cleanup", "jobs-cleanup", "Clean up recognition jobs", "jobs", "/jobs/cleanup", recognition.CleanupPreview{Token: strings.Repeat("a", 64), Items: []recognition.CleanupItem{{Title: "Synthetic deleted notebook", Number: 1, Reason: "Notebook deleted"}, {Title: "Synthetic removed page", Number: 4, Reason: "Page removed"}}}},
		{"jobs-cleanup-empty", "jobs-cleanup", "Clean up recognition jobs", "jobs", "/jobs/cleanup", recognition.CleanupPreview{}},
		{"jobs-library-start", "jobs-library", "Recognize a library", "jobs", "/jobs/library", map[string]any{"Source": "boox", "NativeBOOX": true, "Enabled": true}},
		{"jobs-library-review", "jobs-library", "Recognize a library", "jobs", "/jobs/library", map[string]any{"Run": run, "Source": "boox", "NativeBOOX": true, "Enabled": true, "Model": "synthetic-vision"}},
		{"jobs-library-progress", "jobs-library", "Recognize a library", "jobs", "/jobs/library", map[string]any{"Run": &progress, "Source": "boox", "NativeBOOX": true, "Enabled": true, "Model": "synthetic-vision"}},
		{"client-notebooks", "notebooks", "Notebooks", "notebooks", "/files/forestnote", clientListPreview(true, false)},
		{"client-notebooks-selected", "notebooks", "Notebooks", "notebooks", "/files/forestnote", clientListPreview(true, true)},
		{"client-notebooks-folders", "notebooks", "Notebooks", "notebooks", "/files/forestnote", clientListPreview(false, false)},
		{"client-notebook", "notebook", "Synthetic notebook", "notebooks", "/files/forestnote", notebookReader("n", "Synthetic notebook", clientPages, nil, url.Values{"page": {"p2"}}, true)},
		{"client-notebook-grid", "notebook", "Synthetic notebook", "notebooks", "/files/forestnote", notebookReader("n", "Synthetic notebook", clientPages, nil, url.Values{"view": {"grid"}}, true)},
		{"client-notebooks-empty", "notebooks", "Notebooks", "notebooks", "/files/forestnote", clientList{All: true}},
		{"client-books", "books", "Books", "books", "/files/forestnote/books", clientBooksPreview(false)},
		{"client-books-selected", "books", "Books", "books", "/files/forestnote/books", clientBooksPreview(true)},
		{"tasks", "tasks", "Today", "tasks", "/", todayPreview(openTasks, doneTasks, taskView{Sort: "due", Dir: "asc"}, now, map[string]any{
			"Events":       []todayEvent{{Day: "Tue 27", Time: "14:00", Title: "Synthetic review", From: "Synthetic notebook", Link: "#"}, {Day: "Thu 29", Time: "All day", Title: "Synthetic holiday", Link: "#"}},
			"NeedsRestore": 1})},
		{"tasks-done", "tasks", "Today", "tasks", "/?done=show", todayPreview(openTasks, doneTasks, taskView{Sort: "due", Dir: "asc", ShowDone: true}, now, nil)},
		{"tasks-edit", "tasks", "Today", "tasks", "/?edit=t1", todayPreview(openTasks, doneTasks, taskView{Sort: "updated", Dir: "desc", Edit: "t1"}, now, nil)},
		{"tasks-empty", "tasks", "Today", "tasks", "/", todayPreview(nil, nil, taskView{Sort: "due", Dir: "asc"}, now, nil)},
		{"tasks-trash", "tasks", "Deleted tasks", "tasks", "/?show=deleted", todayPreview(trashTasks, nil, taskView{Trash: true, Sort: "updated", Dir: "desc"}, now, nil)},
		{"help", "help", "Help", "help", "/help", map[string]any{"NativeBOOX": true}},
	}
}

// clientListPreview is the Client's Notebooks tab with synthetic notebooks:
// all of them or the root folder, optionally with one selected.
func clientListPreview(all, selected bool) clientList {
	day := time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC).UnixMilli()
	nb := notes.Entry{ID: "n", Name: "Synthetic notebook", Status: "partial", PageCount: 12, Indexed: 9, CreatedAt: day, ModifiedAt: day, FolderName: "Synthetic folder"}
	v := clientList{All: all, Listing: listing.Parse(url.Values{})}
	v.Entries = []notes.Entry{nb, {ID: "n2", Name: "Second synthetic notebook", Status: "indexed", PageCount: 1, Indexed: 1, ModifiedAt: day - 86400000}}
	q := url.Values{}
	if !all {
		v.Entries = append([]notes.Entry{{IsFolder: true, ID: "f", Name: "Synthetic folder", Notebooks: 1}}, v.Entries[1:]...)
		q.Set("view", "folders")
	}
	if selected {
		q.Set("selected", "n")
		p := &clientPreview{Entry: nb, Open: "/files/forestnote?notebook=n", PDF: "/files/forestnote/export?notebook=n", More: 4}
		for i := 1; i <= 8; i++ {
			p.Thumbs = append(p.Thumbs, pageThumb{Number: i, Image: "/files/forestnote/thumb?page=p" + strconv.Itoa(i), Href: "/files/forestnote?notebook=n&n=" + strconv.Itoa(i)})
		}
		v.Selected = p
	}
	v.Listing.Finish(q, "/files/forestnote", len(v.Entries))
	v.Return = "/files/forestnote?" + q.Encode()
	return v
}

// clientBooksPreview is the Client's Books tab with synthetic books.
func clientBooksPreview(selected bool) clientBooks {
	b := books.LibraryBook{ID: "b", Title: "Synthetic book", Authors: "A. Author", Format: "EPUB", FileState: "ready", ByteLength: 2 << 20, AnnotationCount: 2}
	v := clientBooks{Listing: listing.Parse(url.Values{}), Entries: []books.LibraryBook{b, {ID: "c", Title: "Second synthetic book", Format: "PDF", FileState: "uploading"}}}
	q := url.Values{}
	if selected {
		q.Set("selected", "b")
		v.Selected = &books.LibraryBookDetail{Book: b, Annotations: []books.LibraryAnnotation{
			{ID: "a1", Location: "Chapter 2", Passage: "A synthetic passage that was highlighted.", Highlighted: true},
			{ID: "a2", Location: "Chapter 3", Sticky: true, InkStrokes: 1, RecognizedText: "check this later", TextSource: "Recognized from handwriting · synthetic-model"}}}
	}
	v.Listing.Finish(q, "/files/forestnote/books", len(v.Entries))
	return v
}

// providerPreview is the data the AI model sections render with.
func providerPreview(v *providers.View) map[string]any {
	a := v.Config.Actions
	return map[string]any{"Providers": v, "DefaultOCRPrompt": notes.DefaultPrompt, "DefaultMinutesInstructions": minutes.DefaultInstructions, "MinutesFormat": minutes.ResponseFormat,
		"DefaultTaskInstructions": "Synthetic task instructions.", "DefaultEventInstructions": "Synthetic event instructions.",
		"ActionColors": []struct{ Name, Action string }{{"red", a.For("red")}, {"blue", a.For("blue")}}, "ActionsSince": "2026-10-06 12:00 UTC", "AutoSince": "2026-10-06 12:00 UTC"}
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
		r := httptest.NewRequest("GET", p.path, nil)
		tab, source := libraryTab(r)
		q := r.URL.Query()
		err = clone.ExecuteTemplate(&buf, "layout.html", page{Title: p.title, Section: p.section, SourceTab: sourceTab(r.URL.Path, p.section),
			NativeBOOX: true, Base: "https://library.example", Data: p.data, LibraryTab: tab, LibrarySource: source,
			SettingsSection: settingsSection(r), Notice: q.Get("notice"), Saved: q.Get("saved") == "1"})
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
	err := fs.WalkDir(assets, "static", func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return os.MkdirAll(filepath.Join(dir, path), 0o755)
		}
		b, err := fs.ReadFile(assets, path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, path), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
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

// todayPreview is the Today page's data for synthetic tasks, as the handler
// builds it (English, America/Chicago). extra adds or replaces fields.
func todayPreview(open, done []tasksvc.Task, v taskView, now time.Time, extra map[string]any) map[string]any {
	loc, _ := time.LoadLocation("America/Chicago")
	l := i18n.For("en", "")
	rows := func(list []tasksvc.Task) []todayTask {
		out := []todayTask{}
		for _, t := range list {
			row := todayTask{Task: t, Editing: v.Edit == t.ID}
			if t.DueAt != nil {
				row.Due, row.Urgent = dueLabel(l, *t.DueAt, now, loc)
				row.DueInput = t.DueAt.In(loc).Format("2006-01-02")
			}
			if f := t.ForestNote; f != nil {
				row.Source = &pageLink{URL: "#", Notebook: f.NotebookName, Page: 3}
			}
			out = append(out, row)
		}
		return out
	}
	d := map[string]any{"Open": rows(open), "Done": rows(done), "DoneCount": len(done), "Deleted": 1, "Trash": v.Trash, "View": v, "Sorts": taskSorts,
		"Date": dateWords{l}.long(now.In(loc))}
	for k, val := range extra {
		d[k] = val
	}
	return d
}

// recognitionFixture is the Recognition page on one tab, with synthetic
// work in every tab's counts.
func recognitionFixture(tab string) recognitionView {
	at := time.Date(2026, 10, 26, 22, 41, 0, 0, time.UTC)
	v := recognitionView{Tab: tab, Sources: true, WorkTab: true, Mode: "auto", Model: "synthetic-vl", Return: "/jobs?tab=" + tab,
		Tally: recognition.Tally{Active: 2, Attention: 2, Failed: 1, Blocked: 1, Skipped: 1}}
	v.Runs = []recognition.Run{{ID: "11111111-1111-1111-1111-111111111111", State: "queued", Source: "boox", Created: at, Queued: 120, Ready: 40, Pending: 80, Finished: 40},
		{ID: "22222222-2222-2222-2222-222222222222", State: "queued", Source: "boox", Created: at.Add(-240 * time.Hour), Queued: 300, Ready: 298, Failed: 2}}
	switch tab {
	case "active":
		v.Groups = []recognition.Group{{Key: "b:x", Source: "boox", Notebook: "a", Title: "Synthetic field journal", Batch: "x", Total: 24, Done: 8, Ready: 8, Processing: 1, Queued: 15},
			{Key: "a:client:2026-10-26:b", Source: "client", Notebook: "b", Title: "Synthetic sketchbook", Automatic: true, Total: 3, Done: 1, Ready: 1, Paused: 2}}
	case "finished":
		v.Groups = []recognition.Group{{Key: "b:y", Source: "boox", Notebook: "c", Title: "Synthetic garden plan 2027", Batch: "y", Total: 12, Done: 12, Ready: 12, Updated: at},
			{Key: "b:z", Source: "boox", Notebook: "d", Title: "Synthetic budget", Batch: "z", Total: 5, Done: 5, Ready: 3, Cancelled: 1, Attention: 1, Updated: at.Add(-48 * time.Hour)}}
	case "attention":
		v.Pages = []recognition.Job{{ID: "j1", Source: "boox", Notebook: "a", Title: "Synthetic field journal", Number: 15, State: "failed", Detail: "Provider timed out after 60 s", Attempts: 3, Model: "synthetic-vl:7b", ImageWidth: 1404, ImageHeight: 1872, ImageBytes: 421888, Updated: at, Batch: "x"},
			{ID: "j2", Source: "boox", Notebook: "e", Title: "Synthetic sketchbook", Number: 3, State: "blocked", Detail: "Waiting for page files from the tablet", Updated: at}}
	case "skipped":
		v.Pages = []recognition.Job{{ID: "j3", Source: "client", Notebook: "b", Title: "Synthetic scratch pad", Number: 2, State: "skipped", Updated: at}}
	case "work":
		v.Work = WorkSummary{Actions: WorkGroup{Counts: map[string]int{"ready": 14, "failed": 1}, Rows: []WorkRow{{Title: "20261016 Synthetic plans", Link: "#", State: "failed", Detail: "Notebook unavailable.", Color: "blue", Action: "event", Updated: at}}},
			Minutes: WorkGroup{Counts: map[string]int{"ready": 2, "processing": 1}, Rows: []WorkRow{{Title: "Synthetic meeting", Link: "#", State: "processing", Detail: "Transcribing recordings.", Updated: at}}}}
	}
	return v
}
