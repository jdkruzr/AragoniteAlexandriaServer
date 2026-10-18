package web

import (
	"os"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
)

// docsPages is the made-up demo library behind the README screenshots
// (scripts/docs-screenshots.sh). Unlike previewPages, which cover every state,
// these show one believable library: a community-garden volunteer with a BOOX
// tablet. Nothing here may come from a real library.
func docsPages() []previewPage {
	now := time.Now()
	chicago, _ := time.LoadLocation("America/Chicago")
	at := func(month time.Month, day, hour, min int) time.Time {
		return time.Date(2026, month, day, hour, min, 0, 0, chicago)
	}
	garden := &NoteSource{NotebookID: "garden", PageID: "p1", Notebook: "Garden Committee"}
	due1, due2 := at(10, 21, 0, 0), at(10, 24, 0, 0)
	shed := "Two volunteers needed; ask at Saturday work day."
	open := []tasksvc.Task{
		{ID: "t1", Title: "Order seed garlic + straw mulch", Status: tasksvc.StatusNeedsAction, DueAt: &due1, CreatedAt: now.Add(-50 * time.Hour), UpdatedAt: now.Add(-50 * time.Hour)},
		{ID: "t2", Title: "Email Priya the bed map", Status: tasksvc.StatusNeedsAction, CreatedAt: now.Add(-50 * time.Hour), UpdatedAt: now.Add(-50 * time.Hour)},
		{ID: "t3", Title: "Sign up for the tool shed shift", Status: tasksvc.StatusNeedsAction, DueAt: &due2, Detail: &shed, CreatedAt: now.Add(-26 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
		{ID: "t4", Title: "Return The Overstory to the library", Status: tasksvc.StatusNeedsAction, CreatedAt: now.Add(-80 * time.Hour), UpdatedAt: now.Add(-80 * time.Hour)},
	}
	done := []tasksvc.Task{{ID: "t5", Title: "Get rain barrel quote", Status: tasksvc.StatusCompleted, CreatedAt: now.Add(-120 * time.Hour), UpdatedAt: now.Add(-5 * time.Hour)}}
	row := func(e events.Event, src *NoteSource) calendarRow {
		e.TZID = "America/Chicago"
		return calendarRow{Event: e, When: eventWhen(e, chicago), Source: src}
	}
	upcoming := []calendarRow{
		row(events.Event{ID: "e1", Title: "Seed swap at the library", Location: "Branch library, community room", Start: at(10, 28, 18, 30), End: at(10, 28, 19, 30)}, garden),
		row(events.Event{ID: "e2", Title: "Water shut-off at the garden", Start: at(11, 1, 0, 0), End: at(11, 2, 0, 0), AllDay: true}, nil),
		row(events.Event{ID: "e3", Title: "Book club: The Overstory", Start: at(11, 6, 19, 0), End: at(11, 6, 21, 0)}, nil),
	}
	past := []calendarRow{row(events.Event{ID: "e4", Title: "Garden committee", Start: at(10, 14, 18, 0), End: at(10, 14, 19, 30)}, nil)}
	models := &providers.View{Revision: 12, CanSave: true, HasKey: true, ActiveModel: "nomic-embed-text:v1.5", IndexState: "ready", IndexedPages: 1874,
		Availability:   providers.Availability{Label: "Ready", Detail: "The saved provider is reachable and this model is installed.", Ready: true},
		SavedEmbedding: providers.EmbedConfig{Enabled: true, URL: "http://models.home.arpa:11434", Model: "nomic-embed-text:v1.5"},
		Config: providers.Config{
			OCR:           providers.OCRConfig{Enabled: true, Format: "openai", URL: "http://models.home.arpa:8000", Model: "qwen2.5-vl-7b-instruct"},
			Embedding:     providers.EmbedConfig{Enabled: true, URL: "http://models.home.arpa:11434", Model: "nomic-embed-text:v1.5"},
			Transcription: providers.TranscriptionConfig{Enabled: true, URL: "http://models.home.arpa:9000", Model: "whisper-large-v3-turbo"},
			Summary:       providers.SummaryConfig{Enabled: true, Format: "openai", URL: "http://models.home.arpa:8000", Model: "qwen2.5-7b-instruct"},
			Automatic:     providers.AutomaticConfig{}},
		Coverage: []providers.Coverage{{Label: "Alexandria Client pages", Texts: 64}, {Label: "BOOX reading records", Texts: 212}, {Label: "BOOX notebook pages", Texts: 1598}}}
	jobs := recognition.View{Total: 3, Counts: map[string]int{"ready": 2, "processing": 1}, Jobs: []recognition.Job{
		{ID: "j1", Source: "boox", Notebook: "garden", Title: "Garden Committee", Number: 1, State: "ready", Detail: "Recognized", Model: "qwen2.5-vl-7b-instruct", ImageWidth: 1404, ImageHeight: 1872, ImageBytes: 211000, Created: now.Add(-9 * time.Minute), Updated: now.Add(-8 * time.Minute)},
		{ID: "j2", Source: "boox", Notebook: "garden", Title: "Garden Committee", Number: 2, State: "processing", Model: "qwen2.5-vl-7b-instruct", Created: now.Add(-time.Minute), Updated: now.Add(-time.Minute)},
		{ID: "j3", Source: "client", Notebook: "sketch", Title: "Bed layout sketches", Number: 4, State: "ready", Detail: "Recognized", Model: "qwen2.5-vl-7b-instruct", ImageWidth: 1240, ImageHeight: 1754, ImageBytes: 158000, Created: now.Add(-40 * time.Minute), Updated: now.Add(-39 * time.Minute)}}}
	return []previewPage{
		{"overview", "overview", "Library", "overview", "/library", overviewData{NativeBOOX: true, OCR: true, Semantic: true, JobsKnown: true, Active: 1,
			Client: SourceSummary{Notebooks: 12, Books: 9, Devices: 1, LastActivity: now.Add(-2 * time.Hour), ActivityLabel: "Last sync"},
			BOOX:   &SourceSummary{Notebooks: 214, Books: 47, Devices: 2, LastActivity: now.Add(-6 * time.Minute), ActivityLabel: "Last upload"}}},
		{"search", "search", "Search", "search", "/search", map[string]any{"Query": "garlic", "Semantic": true, "BOOX": true, "Results": []notes.Result{
			{Source: "boox", Kind: "Notebook page", NotebookName: "Garden Committee", PageNumber: 1, Snippet: "…North beds → garlic + fava beans. Order seed garlic + straw mulch…", URL: "#", Tags: []string{"garden", "fall planting"}},
			{Source: "boox", Kind: "Highlight", NotebookName: "The Market Gardener", Snippet: "…plant garlic in the fall, a few weeks before the ground freezes…", URL: "#"},
			{Source: "client", Kind: "Notebook page", NotebookName: "Bed layout sketches", PageNumber: 4, Snippet: "…bed 3: alliums (garlic, shallots), rotate from last year's tomatoes…", URL: "#"}},
			"Native": []sourceMatch{{Title: "Garlic varieties to try", Kind: "Notebook", URL: "#"}}}},
		{"tasks", "tasks", "Tasks", "tasks", "/", map[string]any{"Open": open, "Done": done, "View": taskView{Sort: "due", Dir: "asc"}, "Sorts": taskSorts,
			"Sources": map[string]*NoteSource{"t1": garden, "t2": garden}}},
		{"calendar", "calendar", "Calendar", "calendar", "/calendar", map[string]any{"View": calendarView{"month", "upcoming", "sunday", at(10, 18, 0, 0), 30}, "Days": calendarDays(calendarView{"month", "upcoming", "sunday", at(10, 18, 0, 0), 30}, upcoming, now), "Upcoming": upcoming, "Past": past, "Trash": false, "Zone": "America/Chicago", "Today": "2026-10-16"}},
		{"settings-recognition", "settings-recognition", "AI tools and recognition", "settings", "/settings/recognition", map[string]any{"Providers": models,
			"DefaultOCRPrompt": notes.DefaultPrompt, "DefaultMinutesInstructions": minutes.DefaultInstructions, "MinutesFormat": minutes.ResponseFormat}},
		{"jobs", "jobs", "Recognition jobs", "jobs", "/jobs", map[string]any{"View": jobs, "Filter": recognition.Filter{}, "ProgressTotal": 3, "ProgressDone": 2, "Start": 1, "End": 3, "Refresh": "/jobs",
			"Work": WorkSummary{Actions: WorkGroup{Counts: map[string]int{"ready": 31}}, Minutes: WorkGroup{Counts: map[string]int{"ready": 6}}}}},
	}
}

// TestWriteDocsScreens writes the demo pages when ALEXANDRIA_DOCS_SCREENS_DIR
// is set; scripts/docs-screenshots.sh turns them into docs/images.
func TestWriteDocsScreens(t *testing.T) {
	pages := docsPages()
	dir := os.Getenv("ALEXANDRIA_DOCS_SCREENS_DIR")
	if dir != "" {
		writePreviewStatic(t, dir)
	}
	for _, p := range pages {
		html := renderPreview(t, p)
		if dir != "" {
			writePreviewFile(t, dir, p.file, html)
		}
	}
}
