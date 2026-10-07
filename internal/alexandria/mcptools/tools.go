// Package mcptools serves the Model Context Protocol tools at /mcp. Tool
// names, input schemas and result shapes are copied from UltraBridge
// (cmd/ultrabridge/mcptools.go, Apache-2.0) so an existing MCP client keeps
// working when it moves to this server. UltraBridge's tools called its own
// HTTP API over loopback; these call the library services in process.
//
// The server is stateless: each HTTP request builds a server bound to that
// request's admitted library connection.
package mcptools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/fnpath"
)

// Deps is one admitted request's library.
type Deps struct {
	DB     pg.DB
	Search notes.Searcher
	// PublicURL is the externally reachable base URL, for result links ("" = none).
	PublicURL string
}

var schemas = mcp.NewSchemaCache()

// Handler serves MCP over streamable HTTP for one admitted request.
func Handler(d Deps) http.Handler {
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return NewServer(d) },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
}

func NewServer(d Deps) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "aragonite-alexandria", Version: "1"}, &mcp.ServerOptions{SchemaCache: schemas})
	registerNoteTools(s, d)
	return s
}

type searchNotesInput struct {
	Query        string   `json:"query"`
	Source       string   `json:"source,omitempty"`
	Sources      []string `json:"sources,omitempty"`
	Folder       string   `json:"folder,omitempty"`
	Location     string   `json:"location,omitempty"`
	DeviceModel  string   `json:"device_model,omitempty"`
	CreatedFrom  string   `json:"created_from,omitempty"`
	CreatedTo    string   `json:"created_to,omitempty"`
	ModifiedFrom string   `json:"modified_from,omitempty"`
	ModifiedTo   string   `json:"modified_to,omitempty"`
	Sort         string   `json:"sort,omitempty"`
	Mode         string   `json:"mode,omitempty"`
	Limit        int      `json:"limit,omitempty"`
	// Deprecated aliases kept so older MCP clients keep working.
	Device   string `json:"device,omitempty"`
	DateFrom string `json:"date_from,omitempty"`
	DateTo   string `json:"date_to,omitempty"`
}

type getNotePagesInput struct {
	NotePath string `json:"note_path"`
}

type getNoteImageInput struct {
	NotePath string `json:"note_path"`
	Page     int    `json:"page"`
}

type listTextBoxesInput struct {
	NotebookID string `json:"notebook_id"`
}

type editTextBoxInput struct {
	BoxID string `json:"box_id"`
	Text  string `json:"text"`
}

type searchNotesOutput struct {
	Query   string            `json:"query"`
	Count   int               `json:"count"`
	Results []mcpSearchResult `json:"results"`
}

type mcpSearchResult struct {
	Path          string    `json:"path"`
	Page          int       `json:"page"`
	Title         string    `json:"title,omitempty"`
	Snippet       string    `json:"snippet"`
	Score         float64   `json:"score"`
	SourceType    string    `json:"source_type,omitempty"`
	DetailPath    string    `json:"detail_path,omitempty"`
	NativeURL     string    `json:"native_url,omitempty"`
	Folder        string    `json:"folder,omitempty"`
	DeviceModel   string    `json:"device_model,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	ModifiedAt    time.Time `json:"modified_at,omitempty"`
	DetailURL     string    `json:"detail_url"`
	ImageToolHint string    `json:"image_tool_hint"`
}

type notePagesOutput struct {
	NotePath string        `json:"note_path"`
	Count    int           `json:"count"`
	Pages    []mcpNotePage `json:"pages"`
}

type mcpNotePage struct {
	Page      int    `json:"page"`
	BodyText  string `json:"body_text"`
	TitleText string `json:"title_text,omitempty"`
	Keywords  string `json:"keywords,omitempty"`
	Source    string `json:"source,omitempty"`
}

type noteImageOutput struct {
	NotePath    string `json:"note_path"`
	Page        int    `json:"page"`
	ContentType string `json:"content_type"`
	Bytes       int    `json:"bytes"`
}

type textBoxesOutput struct {
	NotebookID string       `json:"notebook_id"`
	Count      int          `json:"count"`
	Boxes      []mcpTextBox `json:"boxes"`
}

type mcpTextBox struct {
	ID     string `json:"id"`
	PageID string `json:"page_id"`
	Text   string `json:"text"`
	Z      int64  `json:"z"`
}

type editTextBoxOutput struct {
	BoxID string `json:"box_id"`
	OK    bool   `json:"ok"`
}

// detailPath is the web page for a notebook page.
func detailPath(notebookID, pageID string) string {
	return "/files/forestnote?notebook=" + url.QueryEscape(notebookID) + "&page=" + url.QueryEscape(pageID)
}

func (d Deps) display(path string) string {
	if d.PublicURL == "" {
		return path
	}
	return strings.TrimRight(d.PublicURL, "/") + path
}

// resolve accepts a page path (forestnote://notebook/page) or a notebook
// path (forestnote://notebook). Page paths are UltraBridge's note paths.
func resolve(path string) (notebook, page string, ok bool) {
	if !fnpath.Is(path) {
		return "", "", false
	}
	rest := strings.TrimPrefix(path, fnpath.Scheme)
	notebook, page, _ = strings.Cut(rest, "/")
	return notebook, page, notebook != "" && !strings.Contains(page, "/")
}

func text(s string, out any) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}, StructuredContent: out}
}

func registerNoteTools(server *mcp.Server, d Deps) {
	mcp.AddTool[searchNotesInput, any](server, &mcp.Tool{
		Name:        "search_notes",
		Description: "Search synced handwritten notebook pages by keyword query (recognized handwriting, text boxes and device-recognized text). Optional: mode (hybrid/keyword) and limit. Other UltraBridge filters (source, folder, location, device_model, dates, sort) are accepted and ignored: this server holds Alexandria notebooks only.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchNotesInput) (*mcp.CallToolResult, any, error) {
		if in.Query == "" {
			return nil, nil, fmt.Errorf("query is required")
		}
		limit := in.Limit
		if limit <= 0 {
			limit = 10
		}
		results, err := d.Search.Search(ctx, in.Query, limit, in.Mode != "keyword")
		if err != nil {
			return nil, nil, fmt.Errorf("search failed: %w", err)
		}
		out := searchNotesOutput{Query: in.Query, Count: len(results), Results: []mcpSearchResult{}}
		var sb strings.Builder
		for i, r := range results {
			m := mcpSearchResult{Path: r.NoteKey, Page: 0, Title: fmt.Sprintf("%s, page %d", r.NotebookName, r.PageNumber), Snippet: r.Snippet,
				Score: r.Score, SourceType: notes.Source, DetailPath: detailPath(r.NotebookID, r.PageID)}
			m.DetailURL = d.display(m.DetailPath)
			m.ImageToolHint = fmt.Sprintf(`get_note_image {"note_path": %q, "page": 0}`, r.NoteKey)
			out.Results = append(out.Results, m)
			fmt.Fprintf(&sb, "--- Result %d ---\nNote: %s (page 0)\nTitle: %s\nSource type: %s\nURL: %s\nText:\n%s\n\n",
				i+1, m.Path, m.Title, m.SourceType, m.DetailURL, m.Snippet)
		}
		if len(results) == 0 {
			sb.WriteString("No results found.\n")
		}
		return text(sb.String(), out), out, nil
	})

	mcp.AddTool[getNotePagesInput, any](server, &mcp.Tool{
		Name:        "get_note_pages",
		Description: "Get all page text content for a specific note. Returns pages ordered by page number with body text and title. A notebook path (forestnote://{notebook_id}) returns every page; a page path returns that page.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getNotePagesInput) (*mcp.CallToolResult, any, error) {
		if in.NotePath == "" {
			return nil, nil, fmt.Errorf("note_path is required")
		}
		notebook, page, ok := resolve(in.NotePath)
		if !ok {
			return nil, nil, fmt.Errorf("note not found: %s", in.NotePath)
		}
		name, pages, err := notes.NotebookPages(ctx, d.DB, notebook)
		if errors.Is(err, notes.ErrNotFound) {
			return nil, nil, fmt.Errorf("note not found: %s", in.NotePath)
		}
		if err != nil {
			return nil, nil, err
		}
		out := notePagesOutput{NotePath: in.NotePath, Pages: []mcpNotePage{}}
		var sb strings.Builder
		for _, p := range pages {
			if page != "" && p.ID != page {
				continue
			}
			n := p.Number - 1
			if page != "" {
				n = 0 // a page path names a single-page note, as in UltraBridge
			}
			mp := mcpNotePage{Page: n, BodyText: p.BodyText, TitleText: fmt.Sprintf("%s, page %d", name, p.Number), Source: notes.Source}
			out.Pages = append(out.Pages, mp)
			fmt.Fprintf(&sb, "--- Page %d ---\nTitle: %s\nSource: %s\n%s\n\n", mp.Page, mp.TitleText, mp.Source, mp.BodyText)
		}
		if page != "" && len(out.Pages) == 0 {
			return nil, nil, fmt.Errorf("note not found: %s", in.NotePath)
		}
		out.Count = len(out.Pages)
		return text(sb.String(), out), out, nil
	})

	mcp.AddTool[getNoteImageInput, any](server, &mcp.Tool{
		Name:        "get_note_image",
		Description: "Get the rendered page image (JPEG) from a note. Returns the image for visual inspection of handwritten content.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getNoteImageInput) (*mcp.CallToolResult, any, error) {
		if in.NotePath == "" {
			return nil, nil, fmt.Errorf("note_path is required")
		}
		notebook, page, ok := resolve(in.NotePath)
		if ok && page == "" {
			// A notebook path plus a zero-based page index.
			_, pages, err := notes.NotebookPages(ctx, d.DB, notebook)
			if err == nil && in.Page >= 0 && in.Page < len(pages) {
				page = pages[in.Page].ID
			}
		}
		if !ok || page == "" {
			return nil, nil, fmt.Errorf("page image not found: %s page %d", in.NotePath, in.Page)
		}
		image, err := notes.PageImage(ctx, d.DB, page)
		if errors.Is(err, notes.ErrNotFound) {
			return nil, nil, fmt.Errorf("page image not found: %s page %d", in.NotePath, in.Page)
		}
		if err != nil {
			return nil, nil, err
		}
		out := noteImageOutput{NotePath: in.NotePath, Page: in.Page, ContentType: "image/jpeg", Bytes: len(image)}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.ImageContent{Data: image, MIMEType: "image/jpeg"}}, StructuredContent: out}, out, nil
	})

	mcp.AddTool[listTextBoxesInput, any](server, &mcp.Tool{
		Name:        "list_text_boxes",
		Description: "List the editable text boxes in a ForestNote notebook. Returns each box's id (needed by edit_text_box), the page it lives on, and its current text. The notebook_id is the first path segment of a forestnote://{notebook_id}/{page_id} note path.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listTextBoxesInput) (*mcp.CallToolResult, any, error) {
		if in.NotebookID == "" {
			return nil, nil, fmt.Errorf("notebook_id is required")
		}
		if _, _, err := notes.NotebookPages(ctx, d.DB, in.NotebookID); err != nil {
			if errors.Is(err, notes.ErrNotFound) {
				return nil, nil, fmt.Errorf("no ForestNote source, or notebook not found: %s", in.NotebookID)
			}
			return nil, nil, err
		}
		boxes, err := notes.NotebookTextBoxes(ctx, d.DB, in.NotebookID)
		if err != nil {
			return nil, nil, err
		}
		out := textBoxesOutput{NotebookID: in.NotebookID, Count: len(boxes), Boxes: []mcpTextBox{}}
		var sb strings.Builder
		for _, b := range boxes {
			out.Boxes = append(out.Boxes, mcpTextBox{ID: b.ID, PageID: b.PageID, Text: b.Text, Z: b.Z})
			fmt.Fprintf(&sb, "- id: %s (page %s)\n  text: %s\n", b.ID, b.PageID, b.Text)
		}
		if len(boxes) == 0 {
			sb.WriteString("No text boxes in this notebook.\n")
		}
		return text(sb.String(), out), out, nil
	})

	mcp.AddTool[editTextBoxInput, any](server, &mcp.Tool{
		Name:        "edit_text_box",
		Description: "Replace the text of a ForestNote text box (identified by box_id from list_text_boxes). The edit syncs to the user's devices on their next sync and is re-indexed for search. Last-writer-wins: a newer edit on the device can override this.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in editTextBoxInput) (*mcp.CallToolResult, any, error) {
		if in.BoxID == "" {
			return nil, nil, fmt.Errorf("box_id is required")
		}
		if _, err := notes.EditTextBox(ctx, d.DB, in.BoxID, in.Text); err != nil {
			if errors.Is(err, notes.ErrNotFound) {
				return nil, nil, fmt.Errorf("edit failed: text box not found or deleted: %s", in.BoxID)
			}
			return nil, nil, fmt.Errorf("edit failed: %w", err)
		}
		out := editTextBoxOutput{BoxID: in.BoxID, OK: true}
		return text(fmt.Sprintf("Text box %s updated.", in.BoxID), out), out, nil
	})
}
