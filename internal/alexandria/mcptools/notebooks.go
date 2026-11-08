package mcptools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // Client page images
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/boox"
)

// imageEdge is the longer side of a page image sent to an assistant: large
// enough to read handwriting, small enough not to be scaled down again.
const imageEdge = 1568

// readPagesLimit and readTextLimit bound one read_pages call.
const (
	readPagesLimit = 50
	readTextLimit  = 256 << 10
)

type listNotebooksInput struct {
	Source         string `json:"source,omitempty" jsonschema:"boox or client; default boox when this library has BOOX"`
	Folder         string `json:"folder,omitempty" jsonschema:"a folder id from a previous listing; default every notebook, flat"`
	Query          string `json:"query,omitempty" jsonschema:"only notebooks whose title contains this"`
	Kind           string `json:"kind,omitempty" jsonschema:"BOOX only: all (default), handwritten, text, reading or other"`
	Sort           string `json:"sort,omitempty" jsonschema:"modified (default), created, name or pages"`
	Order          string `json:"order,omitempty" jsonschema:"desc (default, name: asc) or asc"`
	IncludeDeleted bool   `json:"deleted,omitempty" jsonschema:"BOOX only: list notebooks in the tablet's Trash instead"`
	Limit          int    `json:"limit,omitempty"`
	Cursor         string `json:"cursor,omitempty"`
}

type notebookRow struct {
	Notebook  string `json:"notebook,omitempty" jsonschema:"reference for get_notebook, read_pages and get_page_image"`
	Folder    string `json:"folder_id,omitempty" jsonschema:"for a folder: its id, to list it with folder"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	InFolder  string `json:"in_folder,omitempty"`
	Pages     int    `json:"pages,omitempty"`
	Notebooks int    `json:"notebooks,omitempty" jsonschema:"for a folder: its notebooks"`
	Created   string `json:"created,omitempty"`
	Modified  string `json:"modified,omitempty"`
	Deleted   bool   `json:"deleted,omitempty"`
	URL       string `json:"url"`
}

type listNotebooksOutput struct {
	Source     string        `json:"source"`
	Total      int           `json:"total"`
	Notebooks  []notebookRow `json:"notebooks"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

type notebookInput struct {
	Notebook string `json:"notebook" jsonschema:"boox:<id> or client:<id>"`
}

type pageRow struct {
	Page         int      `json:"page"`
	Recognition  string   `json:"recognition" jsonschema:"ready, queued, processing, failed, blocked, skipped, paused, cancelled, or none"`
	RecognizedAt string   `json:"recognized_at,omitempty"`
	Model        string   `json:"model,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	NoteActions  bool     `json:"note_actions,omitempty" jsonschema:"coloured ink on this page made tasks or events"`
	Text         string   `json:"text,omitempty"`
	Problem      string   `json:"problem,omitempty"`
}

type notebookOutput struct {
	Notebook   string    `json:"notebook"`
	Title      string    `json:"title"`
	Kind       string    `json:"kind"`
	Folder     string    `json:"folder,omitempty"`
	PageCount  int       `json:"page_count"`
	Recognized int       `json:"recognized" jsonschema:"pages with recognized text"`
	Typed      bool      `json:"typed,omitempty" jsonschema:"a text note: read_pages returns its typed text"`
	Created    string    `json:"created,omitempty"`
	Modified   string    `json:"modified,omitempty"`
	Deleted    bool      `json:"deleted,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	Recordings int       `json:"recordings,omitempty"`
	Minutes    string    `json:"minutes,omitempty" jsonschema:"Audio Note Minutes state; get_minutes reads them"`
	Pending    []string  `json:"pending,omitempty" jsonschema:"changes waiting for the tablet"`
	Pages      []pageRow `json:"pages"`
	URL        string    `json:"url"`
}

type readPagesInput struct {
	Notebook string `json:"notebook" jsonschema:"boox:<id> or client:<id>"`
	Pages    string `json:"pages,omitempty" jsonschema:"a page or range such as 3 or 3-7; default every page"`
}

type textBoxRow struct {
	ID   string `json:"id" jsonschema:"for edit_text_box"`
	Text string `json:"text"`
}

type readPage struct {
	pageRow
	TextBoxes []textBoxRow `json:"text_boxes,omitempty" jsonschema:"Alexandria Client text boxes on this page"`
}

type readPagesOutput struct {
	Notebook  string     `json:"notebook"`
	Title     string     `json:"title"`
	PageCount int        `json:"page_count"`
	Typed     string     `json:"typed_text,omitempty" jsonschema:"a text note's text"`
	Pages     []readPage `json:"pages"`
	Continue  int        `json:"continue_from,omitempty" jsonschema:"pages were left out to keep this reply small; ask again from this page"`
	URL       string     `json:"url"`
}

type pageImageInput struct {
	Notebook string `json:"notebook" jsonschema:"boox:<id> or client:<id>"`
	Page     int    `json:"page" jsonschema:"page number, from 1"`
}

type pageImageOutput struct {
	Notebook string `json:"notebook"`
	Page     int    `json:"page"`
	Bytes    int    `json:"bytes"`
	URL      string `json:"url"`
}

type editTextBoxInput struct {
	ID   string `json:"id" jsonschema:"a text box id from read_pages"`
	Text string `json:"text" jsonschema:"the box's new text"`
}

type editTextBoxOutput struct {
	ID string `json:"id"`
	OK bool   `json:"ok"`
}

// notebookPath is a notebook in the web Reader; BOOX pages are numbered,
// Client pages named by their ID.
func notebookPath(source, id string, page int, pageID string) string {
	if source == sourceBOOX {
		p := "/boox/notebook?id=" + url.QueryEscape(id)
		if page > 0 {
			p += "&page=" + strconv.Itoa(page)
		}
		return p
	}
	p := "/files/forestnote?notebook=" + url.QueryEscape(id)
	if pageID != "" {
		p += "&page=" + url.QueryEscape(pageID)
	}
	return p
}

// pageRange reads "3" or "3-7" ("" = every page).
func pageRange(v string) (from, to int, err error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 1, 0, nil
	}
	a, b, isRange := strings.Cut(v, "-")
	if from, err = strconv.Atoi(strings.TrimSpace(a)); err != nil || from < 1 {
		return 0, 0, errors.New("pages must be a page number or a range such as 3-7")
	}
	to = from
	if isRange {
		if to, err = strconv.Atoi(strings.TrimSpace(b)); err != nil || to < from {
			return 0, 0, errors.New("pages must be a page number or a range such as 3-7")
		}
	}
	return from, to, nil
}

func state(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

var clientSorts = map[string]string{"": "modified", "modified": "modified", "created": "created", "name": "name", "pages": "pages"}

func registerNotebooks(s *mcp.Server, d Deps) {
	add(s, d, tool{name: "list_notebooks", title: "List notebooks",
		description: "List notebooks from one source, BOOX or the Alexandria Client, newest first by default: every notebook flat, or one folder " +
			"(its folders first). Filter by title, and for BOOX by kind or the tablet's Trash."},
		func(ctx context.Context, in listNotebooksInput) (string, listNotebooksOutput, error) {
			source, err := d.source(in.Source)
			if err != nil {
				return "", listNotebooksOutput{}, err
			}
			p, err := pageOf(in.Limit, in.Cursor)
			if err != nil {
				return "", listNotebooksOutput{}, err
			}
			sort, ok := clientSorts[in.Sort]
			if !ok {
				return "", listNotebooksOutput{}, errors.New("sort must be modified, created, name or pages")
			}
			order := in.Order
			if order == "" {
				order = "desc"
				if sort == "name" {
					order = "asc"
				}
			}
			if order != "asc" && order != "desc" {
				return "", listNotebooksOutput{}, errors.New("order must be asc or desc")
			}
			loc := d.zone(ctx)
			out := listNotebooksOutput{Source: source, Notebooks: []notebookRow{}}
			if source == sourceBOOX {
				listSort := map[string]string{"modified": "", "created": "created", "name": "name", "pages": "pages"}[sort]
				rows, total, err := d.BOOX.Notebooks(ctx, boox.NotebookFilter{Folder: in.Folder, Query: strings.TrimSpace(in.Query), Kind: in.Kind,
					Sort: listSort, Order: order, Deleted: in.IncludeDeleted, Offset: p.offset, Limit: p.limit})
				if err != nil {
					return "", listNotebooksOutput{}, err
				}
				out.Total = total
				for _, n := range rows {
					r := notebookRow{Title: n.Title, Kind: n.Kind, InFolder: n.Folder, Pages: n.Pages, Notebooks: n.Notebooks,
						Created: ms(n.CreatedAt, loc), Modified: ms(n.ModifiedAt, loc), Deleted: n.Deleted}
					if n.IsFolder {
						r.Folder, r.Kind, r.URL = n.ID, "folder", d.link("/boox?folder="+url.QueryEscape(n.ID))
					} else {
						r.Notebook, r.URL = ref(sourceBOOX, n.ID), d.link(notebookPath(sourceBOOX, n.ID, 0, ""))
					}
					out.Notebooks = append(out.Notebooks, r)
				}
				out.NextCursor = p.next(len(rows), total)
			} else {
				if in.IncludeDeleted || (in.Kind != "" && in.Kind != "all") {
					return "", listNotebooksOutput{}, errors.New("kind and deleted apply to BOOX notebooks only")
				}
				var entries []notes.Entry
				if in.Folder == "" {
					entries, err = notes.AllNotebooks(ctx, d.DB, sort, order)
				} else {
					_, entries, err = notes.Folder(ctx, d.DB, in.Folder, sort, order)
				}
				if err != nil {
					return "", listNotebooksOutput{}, err
				}
				if q := strings.ToLower(strings.TrimSpace(in.Query)); q != "" {
					entries = slices.DeleteFunc(entries, func(e notes.Entry) bool { return !strings.Contains(strings.ToLower(e.Name), q) })
				}
				out.Total = len(entries)
				shown := window(entries, p)
				for _, e := range shown {
					r := notebookRow{Title: e.Name, Kind: "Notebook", InFolder: e.FolderName, Pages: e.PageCount, Notebooks: e.Notebooks,
						Created: ms(e.CreatedAt, loc), Modified: ms(e.ModifiedAt, loc)}
					if e.IsFolder {
						r.Folder, r.Kind, r.URL = e.ID, "folder", d.link("/files/forestnote?folder="+url.QueryEscape(e.ID))
					} else {
						r.Notebook, r.URL = ref(sourceClient, e.ID), d.link(notebookPath(sourceClient, e.ID, 0, ""))
					}
					out.Notebooks = append(out.Notebooks, r)
				}
				out.NextCursor = p.next(len(shown), len(entries))
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d in all (%s).\n", out.Total, source)
			for _, r := range out.Notebooks {
				if r.Folder != "" {
					fmt.Fprintf(&b, "- Folder %q (folder_id %s): %d notebooks\n", r.Title, r.Folder, r.Notebooks)
					continue
				}
				fmt.Fprintf(&b, "- %s (%s): %s, %d pages", r.Title, r.Notebook, r.Kind, r.Pages)
				if r.InFolder != "" {
					fmt.Fprintf(&b, ", in %s", r.InFolder)
				}
				fmt.Fprintf(&b, ", modified %s\n", r.Modified)
			}
			b.WriteString(more(out.NextCursor))
			return b.String(), out, nil
		})

	add(s, d, tool{name: "get_notebook", title: "Describe a notebook",
		description: "Describe one notebook: kind, folder, dates, and for each page its recognition state, tags and whether note actions made " +
			"tasks or events from it; for BOOX also recordings, Audio Note Minutes and changes waiting for the tablet. Use read_pages for the text."},
		func(ctx context.Context, in notebookInput) (string, notebookOutput, error) {
			source, id, err := parseRef(in.Notebook)
			if err != nil {
				return "", notebookOutput{}, err
			}
			loc := d.zone(ctx)
			out := notebookOutput{Notebook: ref(source, id), Pages: []pageRow{}, URL: d.link(notebookPath(source, id, 0, ""))}
			if source == sourceBOOX {
				if d.BOOX == nil {
					return "", notebookOutput{}, errNoBOOX
				}
				n, err := d.BOOX.Notebook(ctx, id)
				if errors.Is(err, boox.ErrNotFound) {
					return "", notebookOutput{}, fmt.Errorf("no BOOX notebook %s", in.Notebook)
				} else if err != nil {
					return "", notebookOutput{}, err
				}
				out.Title, out.Kind, out.Folder, out.PageCount, out.Recognized, out.Typed = n.Title, n.Kind, n.Folder, n.Pages, n.Recognized, n.Typed
				out.Created, out.Modified, out.Deleted, out.Tags, out.Recordings, out.Minutes, out.Pending = ms(n.CreatedAt, loc), ms(n.ModifiedAt, loc), n.Deleted, n.Tags, n.Recordings, n.Minutes, n.Pending
				if !n.Typed {
					for _, pg := range n.PageStates {
						out.Pages = append(out.Pages, booxPage(pg, loc))
					}
				}
			} else {
				e, err := notes.NotebookEntry(ctx, d.DB, id)
				if errors.Is(err, notes.ErrNotFound) {
					return "", notebookOutput{}, fmt.Errorf("no Alexandria Client notebook %s", in.Notebook)
				} else if err != nil {
					return "", notebookOutput{}, err
				}
				_, pages, err := notes.NotebookPages(ctx, d.DB, id)
				if err != nil {
					return "", notebookOutput{}, err
				}
				out.Title, out.Kind, out.Folder, out.PageCount, out.Recognized = e.Name, "Notebook", e.FolderName, len(pages), e.Indexed
				out.Created, out.Modified = ms(e.CreatedAt, loc), ms(e.ModifiedAt, loc)
				for _, pg := range pages {
					out.Pages = append(out.Pages, clientPage(pg))
				}
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s (%s)\n%s, %d pages, %d recognized", out.Title, out.Notebook, out.Kind, out.PageCount, out.Recognized)
			if out.Folder != "" {
				fmt.Fprintf(&b, ", in %s", out.Folder)
			}
			fmt.Fprintf(&b, "\nModified %s\n", out.Modified)
			if out.Deleted {
				b.WriteString("In the tablet's Trash.\n")
			}
			if len(out.Tags) > 0 {
				fmt.Fprintf(&b, "Tags: %s\n", strings.Join(out.Tags, ", "))
			}
			if out.Recordings > 0 || out.Minutes != "" {
				fmt.Fprintf(&b, "Recordings: %d; minutes: %s\n", out.Recordings, state(out.Minutes))
			}
			if out.Typed {
				b.WriteString("A text note: read_pages returns its typed text.\n")
			}
			for _, pg := range out.Pages {
				fmt.Fprintf(&b, "Page %d: %s", pg.Page, pg.Recognition)
				if len(pg.Tags) > 0 {
					fmt.Fprintf(&b, "; tags %s", strings.Join(pg.Tags, ", "))
				}
				if pg.NoteActions {
					b.WriteString("; note actions")
				}
				b.WriteString("\n")
			}
			return b.String(), out, nil
		})

	add(s, d, tool{name: "read_pages", title: "Read a notebook's pages",
		description: fmt.Sprintf("Read the recognized text of a notebook's pages (all, one, or a range such as 3-7), with each page's recognition state, "+
			"model and tags; a BOOX text note returns its typed text. Alexandria Client pages also list their text boxes, which edit_text_box changes. "+
			"At most %d pages per call; continue_from says where to pick up.", readPagesLimit)},
		func(ctx context.Context, in readPagesInput) (string, readPagesOutput, error) {
			source, id, err := parseRef(in.Notebook)
			if err != nil {
				return "", readPagesOutput{}, err
			}
			from, to, err := pageRange(in.Pages)
			if err != nil {
				return "", readPagesOutput{}, err
			}
			loc := d.zone(ctx)
			out := readPagesOutput{Notebook: ref(source, id), Pages: []readPage{}, URL: d.link(notebookPath(source, id, 0, ""))}
			var all []readPage
			if source == sourceBOOX {
				if d.BOOX == nil {
					return "", readPagesOutput{}, errNoBOOX
				}
				t, err := d.BOOX.PageTexts(ctx, id, from, to)
				if errors.Is(err, boox.ErrNotFound) {
					return "", readPagesOutput{}, fmt.Errorf("no BOOX notebook %s", in.Notebook)
				} else if err != nil {
					return "", readPagesOutput{}, err
				}
				out.Title, out.PageCount, out.Typed = t.Title, t.Total, t.Typed
				for _, pg := range t.Pages {
					all = append(all, readPage{pageRow: booxPage(pg, loc)})
				}
			} else {
				name, pages, err := notes.NotebookPages(ctx, d.DB, id)
				if errors.Is(err, notes.ErrNotFound) {
					return "", readPagesOutput{}, fmt.Errorf("no Alexandria Client notebook %s", in.Notebook)
				} else if err != nil {
					return "", readPagesOutput{}, err
				}
				boxes, err := notes.NotebookTextBoxes(ctx, d.DB, id)
				if err != nil {
					return "", readPagesOutput{}, err
				}
				byPage := map[string][]textBoxRow{}
				for _, tb := range boxes {
					byPage[tb.PageID] = append(byPage[tb.PageID], textBoxRow{ID: tb.ID, Text: tb.Text})
				}
				out.Title, out.PageCount = name, len(pages)
				for _, pg := range pages {
					if pg.Number >= from && (to == 0 || pg.Number <= to) {
						r := readPage{pageRow: clientPage(pg), TextBoxes: byPage[pg.ID]}
						r.Text = pg.BodyText
						all = append(all, r)
					}
				}
			}
			size := len(out.Typed)
			for i, pg := range all {
				size += len(pg.Text)
				for _, tb := range pg.TextBoxes {
					size += len(tb.Text)
				}
				if i == readPagesLimit || i > 0 && size > readTextLimit {
					out.Continue = pg.Page
					break
				}
				out.Pages = append(out.Pages, pg)
			}
			if from > 1 && len(all) == 0 && out.Typed == "" {
				return "", readPagesOutput{}, fmt.Errorf("%s has %d pages", in.Notebook, out.PageCount)
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s (%s), %d pages\n", out.Title, out.Notebook, out.PageCount)
			if out.Typed != "" {
				b.WriteString("\n" + out.Typed + "\n")
			}
			for _, pg := range out.Pages {
				fmt.Fprintf(&b, "\n--- Page %d (%s) ---\n", pg.Page, pg.Recognition)
				if len(pg.Tags) > 0 {
					fmt.Fprintf(&b, "Tags: %s\n", strings.Join(pg.Tags, ", "))
				}
				if pg.Text != "" {
					b.WriteString(pg.Text + "\n")
				} else if pg.Problem != "" {
					b.WriteString("(" + pg.Problem + ")\n")
				}
				for _, tb := range pg.TextBoxes {
					fmt.Fprintf(&b, "Text box %s: %s\n", tb.ID, tb.Text)
				}
			}
			if out.Continue > 0 {
				fmt.Fprintf(&b, "\nStopped to keep this short; ask for pages %d onward.\n", out.Continue)
			}
			return b.String(), out, nil
		})

	addPageImage(s, d)

	add(s, d, tool{name: "edit_text_box", title: "Edit a text box", write: true, idempotent: true,
		description: "Replace the text of an Alexandria Client text box (ids come from read_pages). The change reaches the owner's devices on " +
			"their next sync and is searchable again; a newer edit made on a device wins."},
		func(ctx context.Context, in editTextBoxInput) (string, editTextBoxOutput, error) {
			if in.ID == "" {
				return "", editTextBoxOutput{}, errors.New("id is required")
			}
			if _, err := notes.EditTextBox(ctx, d.DB, in.ID, in.Text); errors.Is(err, notes.ErrNotFound) {
				return "", editTextBoxOutput{}, fmt.Errorf("no text box %s (it may have been deleted)", in.ID)
			} else if err != nil {
				return "", editTextBoxOutput{}, err
			}
			return "Text box " + in.ID + " updated.\n", editTextBoxOutput{ID: in.ID, OK: true}, nil
		})
}

func booxPage(pg boox.PageText, loc *time.Location) pageRow {
	r := pageRow{Page: pg.Number, Recognition: state(pg.State), Model: pg.Model, Tags: pg.Tags, NoteActions: pg.HasActions, Text: pg.Text}
	if pg.State == "ready" {
		r.RecognizedAt = when(pg.RecognizedAt, loc)
	} else if pg.Detail != "" {
		r.Problem = pg.Detail
	}
	return r
}

func clientPage(pg notes.Page) pageRow {
	r := pageRow{Page: pg.Number, Recognition: "none", Model: pg.Model}
	if pg.BodyText != "" {
		r.Recognition = "ready"
	}
	return r
}

// addPageImage registers get_page_image, whose result is an image.
func addPageImage(s *mcp.Server, d Deps) {
	t := tool{name: "get_page_image", title: "Look at a page",
		description: fmt.Sprintf("Return a picture of one notebook page (at most %d pixels on its longer side), to read handwriting or see drawings "+
			"that recognition missed. BOOX text notes have no picture; read their text instead.", imageEdge)}
	mcp.AddTool(s, t.mcp(), func(ctx context.Context, _ *mcp.CallToolRequest, in pageImageInput) (*mcp.CallToolResult, pageImageOutput, error) {
		source, id, err := parseRef(in.Notebook)
		if err != nil {
			return nil, pageImageOutput{}, err
		}
		if in.Page < 1 {
			return nil, pageImageOutput{}, errors.New("page is a page number, from 1")
		}
		var jpeg []byte
		pageID := ""
		if source == sourceBOOX {
			if d.BOOX == nil {
				return nil, pageImageOutput{}, errNoBOOX
			}
			jpeg, err = d.BOOX.PageImage(ctx, id, in.Page, imageEdge)
			if errors.Is(err, boox.ErrNotFound) {
				return nil, pageImageOutput{}, fmt.Errorf("no page %d in BOOX notebook %s", in.Page, in.Notebook)
			}
		} else {
			var pages []notes.Page
			_, pages, err = notes.NotebookPages(ctx, d.DB, id)
			if errors.Is(err, notes.ErrNotFound) || err == nil && in.Page > len(pages) {
				return nil, pageImageOutput{}, fmt.Errorf("no page %d in Alexandria Client notebook %s", in.Page, in.Notebook)
			}
			if err == nil {
				var raw []byte
				pageID = pages[in.Page-1].ID
				if raw, err = notes.PageImage(ctx, d.DB, pageID); err == nil {
					var img image.Image
					if img, _, err = image.Decode(bytes.NewReader(raw)); err == nil {
						jpeg, err = boox.FitJPEG(img, imageEdge)
					}
				}
			}
		}
		if err != nil {
			return nil, pageImageOutput{}, err
		}
		out := pageImageOutput{Notebook: ref(source, id), Page: in.Page, Bytes: len(jpeg), URL: d.link(notebookPath(source, id, in.Page, pageID))}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: jpeg, MIMEType: "image/jpeg"},
			&mcp.TextContent{Text: fmt.Sprintf("Page %d of %s.", in.Page, out.Notebook)}}, StructuredContent: out}, out, nil
	})
}
