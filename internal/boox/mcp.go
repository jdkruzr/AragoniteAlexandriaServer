package boox

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"time"

	"golang.org/x/image/draw"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

// The library as an assistant sees it over MCP: notebooks, their pages'
// text and images, tags, books with their highlights, and minutes. These
// read the same records as the web pages; nothing here changes the library.

// ErrNotFound means no live notebook, book or tag of this library has that ID.
var ErrNotFound = errors.New("not found")

// NotebookInfo is one notebook or folder in a listing. Times are ms.
type NotebookInfo struct {
	ID, Title, Kind, Folder string
	IsFolder, Deleted       bool
	Pages, Notebooks        int
	CreatedAt, ModifiedAt   int64
}

// NotebookFilter selects notebooks: in one folder ("" = all, flat), by
// title, by kind (all, handwritten, text, reading, other), in Trash or not,
// sorted by modified, created, name or pages, asc or desc.
type NotebookFilter struct {
	Folder, Query, Kind, Sort, Order string
	Deleted                          bool
	Offset, Limit                    int
}

// Notebooks lists notebooks, and in a folder its folders too, with the total.
func (s Service) Notebooks(ctx context.Context, f NotebookFilter) ([]NotebookInfo, int, error) {
	if f.Kind == "" {
		f.Kind = "all"
	}
	switch f.Kind {
	case "all", "handwritten", "text", "reading", "other":
	default:
		return nil, 0, errors.New("kind must be all, handwritten, text, reading or other")
	}
	sort := listview.State{Sort: f.Sort, Order: f.Order}
	entries, total, e := s.notebookRows(ctx, notebookListQuery{Deleted: f.Deleted, Query: f.Query, Folder: f.Folder, All: f.Folder == "",
		Category: f.Kind, Order: sort.NativeOrder(), Limit: f.Limit, Offset: f.Offset})
	if e != nil {
		return nil, 0, e
	}
	out := make([]NotebookInfo, 0, len(entries))
	for _, v := range entries {
		out = append(out, NotebookInfo{ID: v.ID, Title: v.Title, Kind: v.Kind, Folder: v.FolderTitle, IsFolder: v.Folder, Deleted: f.Deleted,
			Pages: v.Pages, Notebooks: v.Notebooks, CreatedAt: v.CreatedAt, ModifiedAt: v.ModifiedAt})
	}
	return out, total, nil
}

// PageText is a page's recognition: its state ("" when never queued),
// text, model and when it was recognized, with the page's tags.
type PageText struct {
	Number          int
	ID, State, Text string
	Model, Detail   string
	RecognizedAt    time.Time
	Tags            []string
	HasActions      bool
}

// NotebookDetail is one notebook: what a listing shows plus its pages'
// recognition, tags, recordings, minutes and changes waiting for devices.
type NotebookDetail struct {
	NotebookInfo
	Typed      bool // a text note, searchable without recognition
	Recognized int
	Tags       []string
	Recordings int
	Minutes    string   // minutes state, "" for none
	Pending    []string // "rename (queued)" and the like
	PageStates []PageText
}

// Notebook describes one notebook (ErrNotFound when it isn't one).
func (s Service) Notebook(ctx context.Context, id string) (NotebookDetail, error) {
	p, e := s.notebookPreview(ctx, id)
	if e != nil {
		return NotebookDetail{}, e
	}
	if p == nil {
		return NotebookDetail{}, ErrNotFound
	}
	d := NotebookDetail{NotebookInfo: NotebookInfo{ID: id, Title: p.Entry.Title, Kind: p.Entry.Kind, Folder: p.FolderTitle, Deleted: p.Deleted,
		Pages: p.Count, CreatedAt: p.Entry.CreatedAt, ModifiedAt: p.Entry.ModifiedAt},
		Typed: p.TypedText, Recognized: p.Recognized, Tags: p.Tags, Recordings: p.Recordings, Minutes: p.Minutes}
	for _, op := range p.Pending {
		d.Pending = append(d.Pending, op.Action+" ("+op.State+")")
	}
	if d.PageStates, e = s.pageStates(ctx, id); e != nil {
		return NotebookDetail{}, e
	}
	for i := range d.PageStates {
		d.PageStates[i].Text = ""
	}
	return d, nil
}

// pageStates is every page's recognition and tags, in page order, with a
// mark on pages note actions made something from.
func (s Service) pageStates(ctx context.Context, id string) ([]PageText, error) {
	n, e := s.notebookSnapshot(ctx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	out := make([]PageText, len(ids))
	at := map[string]int{}
	for i, pid := range ids {
		out[i] = PageText{Number: i + 1, ID: pid}
		at[pid] = i
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT page_id,state,text,model,detail,updated_at FROM boox_page_index WHERE notebook_id=$1`, id)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var p PageText
		if e = rows.Scan(&p.ID, &p.State, &p.Text, &p.Model, &p.Detail, &p.RecognizedAt); e != nil {
			break
		}
		if i, ok := at[p.ID]; ok {
			p.Number = out[i].Number
			if p.State != "ready" {
				p.RecognizedAt = time.Time{}
			}
			out[i] = p
		}
	}
	re := rows.Err()
	rows.Close()
	if e != nil || re != nil {
		return nil, errors.Join(e, re)
	}
	byPage, _, e := s.notebookTags(ctx, id)
	if e != nil {
		return nil, e
	}
	for i := range out {
		out[i].Tags = byPage[out[i].ID]
	}
	rows, e = s.DB.QueryContext(ctx, `SELECT DISTINCT page_id FROM alexandria_note_action_item WHERE notebook_id=$1`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var pid string
		if e = rows.Scan(&pid); e != nil {
			return nil, e
		}
		if i, ok := at[pid]; ok {
			out[i].HasActions = true
		}
	}
	return out, rows.Err()
}

// NotebookText is a notebook's pages from..to (1-based, inclusive) with
// their recognized text; a text note's typed text comes whole in Typed.
type NotebookText struct {
	ID, Title string
	Total     int
	Typed     string
	Pages     []PageText
}

// PageTexts reads a notebook's text (ErrNotFound when it isn't one).
func (s Service) PageTexts(ctx context.Context, id string, from, to int) (NotebookText, error) {
	p, e := s.notebookPreview(ctx, id)
	if e != nil {
		return NotebookText{}, e
	}
	if p == nil {
		return NotebookText{}, ErrNotFound
	}
	out := NotebookText{ID: id, Title: p.Entry.Title, Total: p.Count}
	if p.TypedText {
		n, e := s.notebookSnapshot(ctx, id)
		if e != nil {
			return out, e
		}
		out.Typed = s.typedText(ctx, n)
		return out, nil
	}
	pages, e := s.pageStates(ctx, id)
	if e != nil {
		return out, e
	}
	for _, pg := range pages {
		if pg.Number >= from && (to <= 0 || pg.Number <= to) {
			out.Pages = append(out.Pages, pg)
		}
	}
	return out, nil
}

// PageImage draws page num (1-based) of a notebook as a JPEG at most
// maxEdge pixels on its longer side. It waits for the drawing slot the
// Library's thumbnails use rather than the Reader's, so an assistant can't
// crowd out someone reading.
func (s Service) PageImage(ctx context.Context, id string, num, maxEdge int) ([]byte, error) {
	n, e := s.notebookSnapshot(ctx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if n.Meta.ActiveScene == 1 {
		return nil, errors.New("this is a text note; read its text instead")
	}
	if num < 1 || num > len(booxpage.PageIDs(n.Meta.PageNameList)) {
		return nil, ErrNotFound
	}
	key := s.previewKey(n.Meta.UniqueID, n.Version, num)
	var img image.Image
	if p, ok := cachedPreview(key); ok {
		if img, e = png.Decode(bytes.NewReader(p.PNG)); e != nil {
			return nil, e
		}
	} else {
		wait, cancel := context.WithTimeout(ctx, thumbWait)
		defer cancel()
		select {
		case thumbSlots <- struct{}{}:
		case <-wait.Done():
			return nil, errPreviewBusy
		}
		p, full, e := s.drawPage(wait, n, num)
		<-thumbSlots
		if e != nil {
			return nil, e
		}
		if len(p.Warnings) == 0 {
			storePreview(key, p)
		}
		img = full
	}
	return FitJPEG(img, maxEdge)
}

// FitJPEG scales an image to at most maxEdge pixels on its longer side, on
// white (pages may be transparent), and encodes it as a JPEG.
func FitJPEG(img image.Image, maxEdge int) ([]byte, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("empty page image")
	}
	if maxEdge > 0 && max(w, h) > maxEdge {
		if w >= h {
			w, h = maxEdge, max(1, h*maxEdge/w)
		} else {
			w, h = max(1, w*maxEdge/h), maxEdge
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(out, out.Bounds(), img, b, draw.Over, nil)
	var buf bytes.Buffer
	if e := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 85}); e != nil {
		return nil, e
	}
	return buf.Bytes(), nil
}

// TagCount is a tag with how many live pages and notebooks carry it.
type TagCount struct {
	Tag              string
	Pages, Notebooks int
}

// Tags lists every BOOX tag, most used first.
func (s Service) Tags(ctx context.Context) ([]TagCount, error) {
	all, e := s.tagSummaries(ctx)
	out := make([]TagCount, 0, len(all))
	for _, t := range all {
		out = append(out, TagCount{Tag: t.Content, Pages: t.Pages, Notebooks: t.Notebooks})
	}
	return out, e
}

// TaggedPage is a page carrying a tag, with the start of its text.
type TaggedPage struct {
	NotebookID, Notebook, PageID, Snippet string
	Number                                int
}

// TaggedPages lists a tag's live pages (any case), with the tag as stored;
// ErrNotFound when no tag matches.
func (s Service) TaggedPages(ctx context.Context, tag string) (TagCount, []TaggedPage, error) {
	all, e := s.tagSummaries(ctx)
	if e != nil {
		return TagCount{}, nil, e
	}
	var found *tagSummary
	for i, t := range all {
		if strings.EqualFold(t.Content, strings.TrimSpace(tag)) {
			found = &all[i]
			break
		}
	}
	if found == nil {
		return TagCount{}, nil, ErrNotFound
	}
	pages, e := s.taggedPages(ctx, found.Content)
	out := make([]TaggedPage, 0, len(pages))
	for _, p := range pages {
		out = append(out, TaggedPage{NotebookID: p.NotebookID, Notebook: p.Title, PageID: p.PageID, Snippet: p.Snippet, Number: p.Number})
	}
	return TagCount{Tag: found.Content, Pages: found.Pages, Notebooks: found.Notebooks}, out, e
}

// Book is a BOOX book, with its highlights and bookmarks when read alone.
// Status is New, In Progress or Finished; Percent is reading progress.
type Book struct {
	// ID is the reading record; NativeID the book itself (downloads use it).
	NativeID                                             string
	ID, Title, Authors, Status, Percent, Shelf, FileName string
	Stored                                               bool
	CreatedAt, ModifiedAt, LastOpened                    int64
	InkPages                                             int
	Notes                                                []BookNote
}

// BookNote is a highlight (Kind "Highlight") or a bookmark.
type BookNote struct {
	Kind, Quote, Note, Page string
	CreatedAt               int64
}

// BookFilter selects books: stored here only or every one, on a shelf (ID,
// or path such as "Work / Papers"; "unfiled" for none), by title or author,
// by status (new, reading, finished), sorted by modified, created or name.
type BookFilter struct {
	StoredOnly                        bool
	Shelf, Query, Status, Sort, Order string
	Offset, Limit                     int
}

// Books lists BOOX books with the total.
func (s Service) Books(ctx context.Context, f BookFilter) ([]Book, int, error) {
	folders, e := s.readingFolders(ctx)
	if e != nil {
		return nil, 0, e
	}
	paths := map[string]string{}
	shelf := strings.TrimSpace(f.Shelf)
	for _, fo := range folders {
		paths[fo.ID] = fo.Path
		if shelf != "" && shelf != "unfiled" && (strings.EqualFold(fo.Path, shelf) || strings.EqualFold(fo.Name, shelf)) {
			shelf = fo.ID
		}
	}
	status := map[string]string{"": "", "new": "0", "reading": "1", "finished": "2"}
	st, ok := status[strings.ToLower(f.Status)]
	if !ok {
		return nil, 0, errors.New("status must be new, reading or finished")
	}
	view := "all"
	if f.StoredOnly {
		view = "stored"
	}
	sort := f.Sort
	if sort == "pages" {
		sort = ""
	}
	items, total, e := s.readingRows(ctx, readingListQuery{View: view, Folder: shelf, Query: f.Query, Status: st,
		Order: listview.State{Sort: sort, Order: f.Order}.NativeOrder(), Limit: f.Limit, Offset: f.Offset}, paths)
	if e != nil {
		return nil, 0, e
	}
	if e = s.bookShelves(ctx, items, paths); e != nil {
		return nil, 0, e
	}
	out := make([]Book, 0, len(items))
	for _, it := range items {
		out = append(out, bookOf(it))
	}
	return out, total, nil
}

func bookOf(it readingItem) Book {
	b := Book{NativeID: it.NativeID, ID: it.ID, Title: it.Title, Authors: it.Authors, Status: it.Status, Percent: it.Percent, Shelf: it.ShelfPath, Stored: it.Stored,
		CreatedAt: it.CreatedAt, ModifiedAt: it.ModifiedAt, LastOpened: it.LastAccess, FileName: it.FileName, InkPages: it.InkPages}
	for _, c := range it.Children {
		kind := c.Kind
		if kind == "Annotation" {
			kind = "Highlight"
		}
		b.Notes = append(b.Notes, BookNote{Kind: kind, Quote: c.Quote, Note: c.Note, Page: c.Page, CreatedAt: c.CreatedAt})
	}
	return b
}

// Book returns one book with its highlights and bookmarks (ErrNotFound when
// it isn't one).
func (s Service) Book(ctx context.Context, id string) (Book, error) {
	folders, e := s.readingFolders(ctx)
	if e != nil {
		return Book{}, e
	}
	paths := map[string]string{}
	for _, f := range folders {
		paths[f.ID] = f.Path
	}
	it, e := s.readingBook(ctx, id, paths)
	if e != nil {
		return Book{}, e
	}
	if it == nil {
		return Book{}, ErrNotFound
	}
	if lib, e := s.librarySnapshot(ctx, s.DB); e == nil {
		if b := lib.Books[it.NativeID]; b != nil {
			it.FileName = bookFileName(b)
			if isPDFBook(b) {
				if it.InkPages, e = s.readerInkPages(ctx, b.ID); e != nil {
					return Book{}, e
				}
			}
		}
	} else {
		return Book{}, e
	}
	return bookOf(*it), nil
}

// MinutesInfo is a notebook's Audio Note Minutes at a glance.
type MinutesInfo struct {
	NotebookID, Notebook, State, Topic, Detail string
	Requested, Updated                         time.Time
	Finished                                   *time.Time
}

// MinutesList lists live notebooks' minutes, newest first, optionally in
// one state (queued, processing, ready, failed, cancelled), with the total.
func (s Service) MinutesList(ctx context.Context, state string, offset, limit int) ([]MinutesInfo, int, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT m.notebook_id,coalesce(nullif(n.body->>'title',''),'Untitled'),m.state,m.topic,m.detail,m.requested_at,m.updated_at,m.finished_at,count(*) OVER()
 FROM alexandria_note_minutes m JOIN boox_search_notebooks n ON n.document_id=m.notebook_id
 WHERE m.source='boox' AND ($1='' OR m.state=$1) ORDER BY m.updated_at DESC,m.notebook_id LIMIT $2 OFFSET $3`, state, limit, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	var out []MinutesInfo
	total := 0
	for rows.Next() {
		var m MinutesInfo
		var finished sql.NullTime
		if e = rows.Scan(&m.NotebookID, &m.Notebook, &m.State, &m.Topic, &m.Detail, &m.Requested, &m.Updated, &finished, &total); e != nil {
			return nil, 0, e
		}
		if finished.Valid {
			m.Finished = &finished.Time
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// MinutesStale reports whether a notebook's ready minutes no longer match
// its recordings, recognized text, tags or the summary instructions.
func (s Service) MinutesStale(ctx context.Context, id string) (bool, error) {
	n, e := s.notebookSnapshot(ctx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if e != nil {
		return false, e
	}
	_, tags, e := s.notebookTags(ctx, id)
	if e != nil {
		return false, e
	}
	return s.minutesStale(ctx, n, tags)
}
