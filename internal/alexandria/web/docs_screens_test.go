package web

import (
	"os"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providers"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
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
	return []previewPage{
		{"settings-devices", "settings-devices", "Devices", "settings", "/settings/devices", map[string]any{"Devices": devicesView{
			BOOX: &BOOXDevices{Summary: SourceSummary{Notebooks: 214, Books: 47, Devices: 2, LastActivity: now.Add(-6 * time.Minute)}, Devices: []BOOXDevice{
				{ID: "8bc4cf5c-0000-4000-8000-000000000001", Model: "NoteAir4C", LastUpload: now.Add(-6 * time.Minute)},
				{ID: "8bc4cf5c-0000-4000-8000-000000000002", Model: "Go7", LastUpload: now.Add(-50 * time.Hour)}}},
			Client:        SourceSummary{Notebooks: 12, Books: 9, Devices: 1, LastActivity: now.Add(-2 * time.Hour)},
			ClientDevices: []relay.Device{{SiteID: "01JB2Q6V0000000000000000", Name: "Garden tablet", Enrolled: true, LastSeenMs: now.Add(-2 * time.Hour).UnixMilli(), FirstSeenMs: now.Add(-35 * 24 * time.Hour).UnixMilli()}}}}},
		{"search", "search", "Search", "search", "/search", searchPreview("garlic", "all", map[string]int{"notebooks": 1, "pages": 9, "books": 3, "tasks": 1}, []previewHit{
			{"Notebook", "notebook", "Garlic varieties to try", "", []string{"BOOX", "6 pages"}, nil},
			{"Page text", "text-aa", "Garden Committee, p. 1", "…North beds → garlic + fava beans. Order seed garlic + straw mulch…", []string{"BOOX", "recognized"}, []string{"garden", "fall planting"}},
			{"Highlight", "highlighter", "The Market Gardener, p. 112", "…plant garlic in the fall, a few weeks before the ground freezes…", []string{"BOOX"}, nil},
			{"Task", "check-square", "Order seed garlic + straw mulch", "", []string{"Due Fri"}, nil},
			{"Page text", "text-aa", "Bed layout sketches, p. 4", "…bed 3: alliums (garlic, shallots), rotate from last year's tomatoes…", []string{"Client", "recognized"}, nil},
			{"Page text", "text-aa", "Garden Committee, p. 3", "…cure the garlic in the shed for two weeks before storing…", []string{"BOOX", "recognized"}, []string{"garden"}}}, nil)},
		{"tasks", "tasks", "Today", "tasks", "/", func() map[string]any {
			d := todayPreview(open, done, taskView{Sort: "due", Dir: "asc"}, now, map[string]any{"Events": []todayEvent{
				{Day: "Wed 28", Time: "18:30", Title: "Seed swap at the library", From: "Garden Committee", Link: "#"},
				{Day: "Sun 1", Time: "All day", Title: "Water shut-off at the garden", Link: "#"}}})
			rows := d["Open"].([]todayTask)
			rows[0].Source = &pageLink{URL: "#", Notebook: garden.Notebook, Page: 1}
			rows[1].Source = &pageLink{URL: "#", Notebook: garden.Notebook, Page: 2}
			return d
		}()},
		{"calendar", "calendar", "Calendar", "calendar", "/calendar", calendarPreview("week", nil, nil)},
		{"settings-models", "settings-models", "AI models", "settings", "/settings/models", map[string]any{"Providers": models,
			"DefaultOCRPrompt": notes.DefaultPrompt, "DefaultMinutesInstructions": minutes.DefaultInstructions, "MinutesFormat": minutes.ResponseFormat}},
		{"jobs", "jobs", "Recognition", "jobs", "/jobs", recognitionView{Tab: "active", Sources: true, WorkTab: true, Mode: "auto", Model: "qwen2.5-vl-7b-instruct", Return: "/jobs?tab=active",
			Tally:  recognition.Tally{Active: 1},
			Groups: []recognition.Group{{Key: "a:boox:2026-10-18:garden", Source: "boox", Notebook: "garden", Title: "Garden Committee", Automatic: true, Total: 2, Done: 1, Ready: 1, Processing: 1}}}},
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
