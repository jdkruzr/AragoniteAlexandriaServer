package boox

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	"strings"
	"testing"
	"time"
)

// The assistant's view of BOOX reads the same records as the web pages.
func TestMCPNotebooksPagesAndTags(t *testing.T) {
	s := testService(t)
	previewFixture(t, s)
	putTag(t, s, "tag-bird", "n", "p1", "Birds", 0)
	ctx := context.Background()

	list, total, e := s.Notebooks(ctx, NotebookFilter{Limit: 20})
	if e != nil || total != 1 || len(list) != 1 || list[0].ID != "n" || list[0].Folder != "Synthetic work folder" || list[0].Pages != 2 {
		t.Fatal("flat list", list, total, e)
	}
	if list, _, e = s.Notebooks(ctx, NotebookFilter{Folder: "work", Limit: 20}); e != nil || len(list) != 1 || list[0].ID != "n" {
		t.Fatal("one folder", list, e)
	}
	if list, _, e = s.Notebooks(ctx, NotebookFilter{Folder: "elsewhere", Limit: 20}); e != nil || len(list) != 0 {
		t.Fatal("another folder", list, e)
	}
	if list, total, e = s.Notebooks(ctx, NotebookFilter{Deleted: true, Limit: 20}); e != nil || total != 1 || list[0].ID != "d" || !list[0].Deleted {
		t.Fatal("trash", list, e)
	}
	if list, _, e = s.Notebooks(ctx, NotebookFilter{Query: "nothing like it", Limit: 20}); e != nil || len(list) != 0 {
		t.Fatal("title filter", list, e)
	}
	if _, _, e = s.Notebooks(ctx, NotebookFilter{Kind: "comics", Limit: 20}); e == nil {
		t.Fatal("unknown kind accepted")
	}

	d, e := s.Notebook(ctx, "n")
	if e != nil || d.Title != "Synthetic preview note" || d.Recognized != 1 || len(d.PageStates) != 2 || d.PageStates[0].State != "ready" ||
		d.PageStates[0].Text != "" || len(d.PageStates[0].Tags) != 1 || d.PageStates[1].State != "" {
		t.Fatalf("detail %+v %v", d, e)
	}
	if _, e = s.Notebook(ctx, "no-such-note"); !errors.Is(e, ErrNotFound) {
		t.Fatal("unknown notebook", e)
	}

	text, e := s.PageTexts(ctx, "n", 1, 1)
	if e != nil || text.Total != 2 || len(text.Pages) != 1 || text.Pages[0].Text != "Synthetic text" || text.Pages[0].RecognizedAt.IsZero() {
		t.Fatalf("text %+v %v", text, e)
	}
	if text, e = s.PageTexts(ctx, "n", 2, 0); e != nil || len(text.Pages) != 1 || text.Pages[0].Number != 2 {
		t.Fatalf("from page 2 %+v %v", text, e)
	}

	tags, e := s.Tags(ctx)
	if e != nil || len(tags) != 1 || tags[0].Tag != "Birds" || tags[0].Pages != 1 {
		t.Fatal("tags", tags, e)
	}
	tag, pages, e := s.TaggedPages(ctx, "birds")
	if e != nil || tag.Tag != "Birds" || len(pages) != 1 || pages[0].Number != 1 || pages[0].Snippet != "Synthetic text" {
		t.Fatal("tagged pages", tag, pages, e)
	}
	if _, _, e = s.TaggedPages(ctx, "fish"); !errors.Is(e, ErrNotFound) {
		t.Fatal("unknown tag", e)
	}
}

// A page image waits for the thumbnail slot rather than failing, is scaled
// to the size asked for, and leaves the Reader's slots alone.
func TestMCPPageImage(t *testing.T) {
	s := testService(t)
	previewFixture(t, s)
	forgetPreviews(t)
	ctx := context.Background()
	previewSlots <- struct{}{}
	previewSlots <- struct{}{} // the Reader is busy
	defer func() { <-previewSlots; <-previewSlots }()
	raw, e := s.PageImage(ctx, "n", 2, 600)
	if e != nil {
		t.Fatal(e)
	}
	img, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil || img.Bounds().Dy() != 600 || img.Bounds().Dx() != 500 {
		t.Fatal("scaled", img.Bounds(), e)
	}
	if _, e = s.PageImage(ctx, "n", 3, 600); !errors.Is(e, ErrNotFound) {
		t.Fatal("page 3", e)
	}
	// With the thumbnail slot taken, an undrawn page waits, then gives up.
	forgetPreviews(t)
	thumbSlots <- struct{}{}
	wait := thumbWait
	thumbWait = 50 * time.Millisecond
	defer func() { <-thumbSlots; thumbWait = wait }()
	if _, e = s.PageImage(ctx, "n", 1, 600); !errors.Is(e, errPreviewBusy) {
		t.Fatal("busy slot", e)
	}
}

func TestMCPBooks(t *testing.T) {
	s := readingFixture(t)
	ctx := context.Background()
	all, total, e := s.Books(ctx, BookFilter{Limit: 20})
	if e != nil || total != 3 || len(all) != 3 {
		t.Fatal("books", all, total, e)
	}
	stored, total, e := s.Books(ctx, BookFilter{StoredOnly: true, Limit: 20})
	if e != nil || total != 2 {
		t.Fatal("stored", stored, e)
	}
	shelf, total, e := s.Books(ctx, BookFilter{Shelf: "synthetic shelf", Limit: 20})
	if e != nil || total != 1 || shelf[0].Shelf != "Synthetic Shelf" || shelf[0].Status != "In Progress" {
		t.Fatal("shelf by name", shelf, e)
	}
	if _, total, e = s.Books(ctx, BookFilter{Status: "finished", Limit: 20}); e != nil || total != 1 {
		t.Fatal("finished", total, e)
	}
	if _, _, e = s.Books(ctx, BookFilter{Status: "abandoned", Limit: 20}); e == nil {
		t.Fatal("unknown status accepted")
	}
	b, e := s.Book(ctx, shelf[0].ID)
	if e != nil || len(b.Notes) != 2 || b.Notes[0].Kind == b.Notes[1].Kind || b.Notes[0].Kind+b.Notes[1].Kind != "BookmarkHighlight" && b.Notes[0].Kind+b.Notes[1].Kind != "HighlightBookmark" || b.NativeID != "stored" {
		t.Fatalf("book %+v %v", b, e)
	}
	if _, e = s.Book(ctx, "nope"); !errors.Is(e, ErrNotFound) {
		t.Fatal("unknown book", e)
	}
}

func TestMCPMinutesList(t *testing.T) {
	s := testService(t)
	previewFixture(t, s)
	ctx := context.Background()
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_note_minutes(source,notebook_id,request_id,state,topic,detail) VALUES('boox','n',gen_random_uuid(),'failed','','provider said no'),('boox','d',gen_random_uuid(),'ready','Gone','')`); e != nil {
		t.Fatal(e)
	}
	list, total, e := s.MinutesList(ctx, "", 0, 20)
	if e != nil || total != 1 || list[0].NotebookID != "n" || list[0].State != "failed" || list[0].Detail != "provider said no" {
		t.Fatal("minutes of live notebooks", list, e)
	}
	if _, total, e = s.MinutesList(ctx, "ready", 0, 20); e != nil || total != 0 {
		t.Fatal("state filter", total, e)
	}
	if stale, e := s.MinutesStale(ctx, "n"); e != nil || stale {
		t.Fatal("failed minutes aren't stale", stale, e)
	}
}

func TestMCPMinutesRequestAndCancel(t *testing.T) {
	s := indexFixture(t)
	ctx := context.Background()
	putNoteBlob(t, s, "resource/data/rec.mp3", syntheticONY1(t, []byte("ID3 body")))
	if _, e := s.RequestNotebookMinutes(ctx, "note", true); e == nil {
		t.Fatal("requested without providers")
	}
	s.Minutes = MinutesConfig{Enabled: true, Identity: "t"}
	r, e := s.RequestNotebookMinutes(ctx, "note", true)
	if e != nil || r.Recordings != 1 || r.Recognizing {
		t.Fatalf("request %+v %v", r, e)
	}
	var state string
	if e = s.DB.QueryRowContext(ctx, `SELECT state FROM alexandria_note_minutes WHERE source='boox' AND notebook_id='note'`).Scan(&state); e != nil || state != "queued" {
		t.Fatal("not queued", state, e)
	}
	if done, e := s.CancelNotebookMinutes(ctx, "note"); e != nil || !done {
		t.Fatal("cancel", done, e)
	}
	if done, e := s.CancelNotebookMinutes(ctx, "note"); e != nil || done {
		t.Fatal("cancelled twice", done, e)
	}
	if done, e := s.CancelNotebookMinutes(ctx, "never-asked"); e != nil || done {
		t.Fatal("cancelled nothing", done, e)
	}
	if _, e = s.RequestNotebookMinutes(ctx, "missing", true); !errors.Is(e, ErrNotFound) {
		t.Fatal("unknown notebook", e)
	}
}

func TestMCPCreateTextNote(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); e != nil {
		t.Fatal(e)
	}
	previewFixture(t, s)
	folders, e := s.NoteFolders(ctx)
	if e != nil || len(folders) != 1 || folders[0].ID != "work" || folders[0].Path != "Synthetic work folder" {
		t.Fatal("folders", folders, e)
	}
	id, e := s.CreateTextNote(ctx, "work", "Synthetic plan", "# Day one\n- pack")
	if e != nil {
		t.Fatal(e)
	}
	var kind, title, parent string
	if e = s.DB.QueryRowContext(ctx, `SELECT kind,payload->>'title',coalesce(payload#>>'{record,parentUniqueId}',payload::text) FROM boox_operation WHERE id=$1`, id).Scan(&kind, &title, &parent); e != nil || kind != "create_text_note" || title != "Synthetic plan" || !strings.Contains(parent, "work") {
		t.Fatal("queued note", kind, title, parent, e)
	}
	if again, e := s.CreateTextNote(ctx, "work", "Synthetic plan", "# Day one\n- pack"); e != nil || again == id {
		t.Fatal("a second request is a second note", again, e)
	}
	if _, e = s.CreateTextNote(ctx, "missing", "x", "y"); !errors.Is(e, errTextNoteFolder) {
		t.Fatal("missing folder", e)
	}
}
