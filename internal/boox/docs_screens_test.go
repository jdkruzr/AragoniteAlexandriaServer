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

// TestWriteDocsScreens renders the BOOX half of the made-up demo library behind
// the README screenshots (see the web package's docsPages and
// scripts/docs-screenshots.sh). The page image, demo-page.png, is drawn from
// docs/screenshots/demo-page.html. Nothing here may come from a real library.
func TestWriteDocsScreens(t *testing.T) {
	now := time.Now()
	chicago, _ := time.LoadLocation("America/Chicago")
	start := time.Date(2026, 10, 14, 18, 2, 0, 0, chicago)
	recs := []minutes.Transcript{
		{Source: minutes.Source{Key: "resource/data/r1.mp3", SHA256: "r1", Bytes: 6815744, Title: "Recording 1", RecordedAt: start, DurationMS: 1702000}, State: "ready", Language: "en",
			Segments: []minutes.Segment{
				{Start: 0, End: 6.1, Text: "Okay, let's start with the compost bins, since the frost is coming early this year."},
				{Start: 6.1, End: 12.8, Text: "If we turn them this weekend they'll be ready to spread on the north beds in spring."},
				{Start: 12.8, End: 19.5, Text: "The city moved the water shut-off to November first, so hoses need to be drained before then."},
				{Start: 19.5, End: 25, Text: "And the seed swap is at the library on the twenty-eighth, six thirty."}}},
		{Source: minutes.Source{Key: "resource/data/r2.mp3", SHA256: "r2", Bytes: 1048576, Title: "Recording 2", RecordedAt: start.Add(31 * time.Minute), DurationMS: 262000}, State: "ready", Language: "en",
			Segments: []minutes.Segment{{Start: 0, End: 7, Text: "One more thing: we still need two people for the tool shed on Saturdays."}}},
	}
	ready := minutes.Stored{State: "ready", Model: "qwen2.5-7b-instruct", Recordings: recs, FinishedAt: sql.NullTime{Time: start.Add(50 * time.Minute), Valid: true},
		Minutes: minutes.Minutes{Topic: "Preparing the community garden for winter", Keywords: []string{"compost", "garlic", "water shut-off", "seed swap", "volunteers"},
			KeyPoints: []minutes.Point{
				{Text: "Turn the compost bins this weekend so they are ready for the north beds in spring.", From: "both"},
				{Text: "The north beds get garlic and fava beans; seed garlic and straw mulch still need ordering.", From: "notes"},
				{Text: "The water shut-off moved to November 1, so hoses must be drained before then.", From: "both"},
				{Text: "The seed swap is at the library on October 28 at 6:30 PM.", From: "recording"},
				{Text: "Two volunteers are still needed for Saturday tool shed shifts.", From: "recording"}},
			Conclusion: "Finish fall chores before November 1: compost, garlic planting and draining the hoses. Recruit two shed volunteers at the seed swap."},
		Metadata: minutes.Metadata{Title: "Garden Committee", Folder: "Garden", Created: start, PageCount: 2, PagesWithText: 2, Summarizer: "qwen2.5-7b-instruct", Transcriber: "whisper-large-v3-turbo",
			Recordings: []minutes.RecordingSummary{{Title: "Recording 1", RecordedAt: start, DurationMS: 1702000}, {Title: "Recording 2", RecordedAt: start.Add(31 * time.Minute), DurationMS: 262000}}}}
	ms := func(d time.Duration) int64 { return now.Add(-d).UnixMilli() }
	listing := listview.State{Sort: "modified", Order: "desc", Total: 214, Start: 1, End: 8, Page: 1, Pages: 27}
	views := []struct {
		name, path, title string
		tmpl              *template.Template
		data              any
	}{
		{"boox-notebooks", "/boox", "BOOX notebooks", libraryTemplate, libraryPage{All: true, Category: "all", Listing: listing, Entries: []notebookEntry{
			{ID: "garden", Title: "Garden Committee", Kind: "Meeting note", Pages: 2, CreatedAt: ms(50 * time.Hour), ModifiedAt: ms(6 * time.Minute)},
			{ID: "g2", Title: "Garlic varieties to try", Kind: "Handwritten note", Pages: 3, CreatedAt: ms(400 * time.Hour), ModifiedAt: ms(26 * time.Hour)},
			{ID: "g3", Title: "Book club: The Overstory", Kind: "Handwritten note", Pages: 5, CreatedAt: ms(900 * time.Hour), ModifiedAt: ms(30 * time.Hour)},
			{ID: "g4", Title: "Rain barrel options", Kind: "Text note", Pages: 1, CreatedAt: ms(120 * time.Hour), ModifiedAt: ms(70 * time.Hour)},
			{ID: "g5", Title: "Weekly plan", Kind: "Handwritten note", Pages: 12, CreatedAt: ms(2000 * time.Hour), ModifiedAt: ms(80 * time.Hour)},
			{ID: "g6", Title: "Sketches", Kind: "Handwritten note", Pages: 18, CreatedAt: ms(3000 * time.Hour), ModifiedAt: ms(150 * time.Hour)},
			{ID: "g7", Title: "Recipes from the plot", Kind: "Handwritten note", Pages: 7, CreatedAt: ms(2500 * time.Hour), ModifiedAt: ms(200 * time.Hour)},
			{ID: "g8", Title: "Spring planting plan", Kind: "Handwritten note", Pages: 4, CreatedAt: ms(4000 * time.Hour), ModifiedAt: ms(260 * time.Hour)}}}},
		{"boox-notebook", "/boox/notebook", "Garden Committee", notebookTemplate, notebookView{ID: "garden", Title: "Garden Committee", Page: "p1", Number: 1, Count: 2, Next: "#", Zoom: "fit", Image: "demo-page.png",
			RecognitionEnabled: true, RecognitionState: "ready",
			RecognizedText: "Garden committee · Oct 14\n- Compost bins: turn before first frost\n- North beds → garlic + fava beans\n- Water shut-off moved to Nov 1\n- Need 2 volunteers for the tool shed\nOrder seed garlic + straw mulch\nEmail Priya the bed map\nOct 28, 6:30pm: seed swap at the library\nNext meeting: bring the rain barrel quote",
			ActionsEnabled: true,
			Actions: []pageAction{
				{Color: "red", Action: "task", State: "ready", Detail: "Made 2 tasks.", Text: "Order seed garlic + straw mulch\nEmail Priya the bed map"},
				{Color: "blue", Action: "event", State: "ready", Detail: "Made 1 event.", Text: "Oct 28, 6:30pm: seed swap at the library"}},
			ActionItems: []pageActionItem{{Kind: "task", ID: "t1", Title: "Order seed garlic + straw mulch"}, {Kind: "task", ID: "t2", Title: "Email Priya the bed map"}, {Kind: "event", ID: "e1", Title: "Seed swap at the library"}},
			Recordings:  2, MinutesState: "ready", MinutesTopic: ready.Topic, PageRecordings: []minutes.Source{recs[0].Source},
			PageTags: []string{"garden", "fall planting"}}},
		{"boox-minutes", "/boox/minutes", "Minutes", minutesTemplate, minutesView{ID: "garden", Title: "Garden Committee", Has: true, Stored: ready, Recordings: 2,
			OnPages: map[string][]int{"resource/data/r1.mp3": {1}, "resource/data/r2.mp3": {2}}, Keywords: []minutesKeyword{{"compost", ""}, {"garlic", ""}, {"water shut-off", ""}, {"seed swap", ""}, {"volunteers", ""}}}},
	}
	dir := os.Getenv("ALEXANDRIA_DOCS_SCREENS_DIR")
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
