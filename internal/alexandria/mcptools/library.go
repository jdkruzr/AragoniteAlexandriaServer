package mcptools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/books"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/boox"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
)

type emptyInput struct{}

type tagRow struct {
	Tag       string `json:"tag"`
	Pages     int    `json:"pages"`
	Notebooks int    `json:"notebooks"`
	URL       string `json:"url"`
}

type listTagsOutput struct {
	Tags []tagRow `json:"tags"`
}

type tagInput struct {
	Tag string `json:"tag" jsonschema:"a tag, in any case"`
}

type taggedPageRow struct {
	Notebook string `json:"notebook"`
	Title    string `json:"title"`
	Page     int    `json:"page"`
	Snippet  string `json:"snippet,omitempty"`
	URL      string `json:"url"`
}

type tagOutput struct {
	tagRow
	PagesWithTag []taggedPageRow `json:"tagged_pages"`
}

type listBooksInput struct {
	Source string `json:"source,omitempty" jsonschema:"boox or client; default boox when this library has BOOX"`
	Stored bool   `json:"stored_only,omitempty" jsonschema:"BOOX only: books whose file is stored here, not reading records alone"`
	Shelf  string `json:"shelf,omitempty" jsonschema:"BOOX only: a shelf name or path such as Work / Papers, or unfiled"`
	Status string `json:"status,omitempty" jsonschema:"BOOX only: new, reading or finished"`
	Query  string `json:"query,omitempty" jsonschema:"title or author contains this"`
	Sort   string `json:"sort,omitempty" jsonschema:"modified (default), created or title"`
	Order  string `json:"order,omitempty" jsonschema:"desc (default, title: asc) or asc"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type bookRow struct {
	Book        string `json:"book" jsonschema:"reference for get_book"`
	Title       string `json:"title"`
	Authors     string `json:"authors,omitempty"`
	Status      string `json:"status,omitempty"`
	Progress    string `json:"progress,omitempty"`
	Shelf       string `json:"shelf,omitempty"`
	Format      string `json:"format,omitempty"`
	Stored      bool   `json:"stored" jsonschema:"the book's file is on this server"`
	Created     string `json:"created,omitempty"`
	Modified    string `json:"modified,omitempty"`
	Opened      string `json:"last_opened,omitempty"`
	Annotations int    `json:"annotations,omitempty"`
	URL         string `json:"url"`
}

type listBooksOutput struct {
	Source     string    `json:"source"`
	Total      int       `json:"total"`
	Books      []bookRow `json:"books"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type bookInput struct {
	Book string `json:"book" jsonschema:"boox:<id> or client:<id>"`
}

type bookNote struct {
	Kind     string `json:"kind" jsonschema:"highlight, bookmark, annotation or sticky note"`
	Location string `json:"location,omitempty"`
	Quote    string `json:"quote,omitempty"`
	Note     string `json:"note,omitempty" jsonschema:"the reader's note, or text recognized from handwriting"`
	Created  string `json:"created,omitempty"`
}

type bookOutput struct {
	bookRow
	FileName       string     `json:"file_name,omitempty"`
	InkPages       int        `json:"handwritten_pages,omitempty" jsonschema:"BOOX PDFs: pages with handwriting"`
	AnnotatedPDF   string     `json:"annotated_pdf,omitempty" jsonschema:"download link for the PDF with its handwriting drawn in"`
	Notes          []bookNote `json:"notes"`
	NotesTruncated bool       `json:"notes_truncated,omitempty"`
}

type listMinutesInput struct {
	State  string `json:"state,omitempty" jsonschema:"ready, queued, processing, failed or cancelled; default all"`
	Limit  int    `json:"limit,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

type minutesRow struct {
	Notebook string `json:"notebook"`
	Title    string `json:"title"`
	State    string `json:"state"`
	Topic    string `json:"topic,omitempty"`
	Problem  string `json:"problem,omitempty"`
	Updated  string `json:"updated"`
	Written  string `json:"written,omitempty"`
	URL      string `json:"url"`
}

type listMinutesOutput struct {
	Total      int          `json:"total"`
	Minutes    []minutesRow `json:"minutes"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

type getMinutesInput struct {
	Notebook    string `json:"notebook" jsonschema:"boox:<id>"`
	Transcripts *bool  `json:"include_transcripts,omitempty" jsonschema:"include recording transcripts (default true)"`
}

type transcriptRow struct {
	Title      string `json:"title"`
	RecordedAt string `json:"recorded_at,omitempty"`
	Seconds    int64  `json:"seconds"`
	Language   string `json:"language,omitempty"`
	State      string `json:"state"`
	Text       string `json:"text,omitempty"`
}

type getMinutesOutput struct {
	Notebook   string           `json:"notebook"`
	Title      string           `json:"title"`
	State      string           `json:"state"`
	Problem    string           `json:"problem,omitempty"`
	OutOfDate  bool             `json:"out_of_date,omitempty" jsonschema:"recordings, page text or instructions changed since these were written"`
	Topic      string           `json:"topic"`
	Keywords   []string         `json:"keywords"`
	KeyPoints  []minutes.Point  `json:"key_points" jsonschema:"each marked as from the recording, the handwritten notes or both"`
	Conclusion string           `json:"conclusion"`
	Model      string           `json:"model,omitempty"`
	Written    string           `json:"written,omitempty"`
	Metadata   minutes.Metadata `json:"metadata"`
	Recordings []transcriptRow  `json:"recordings"`
	URL        string           `json:"url"`
}

type recognitionInput struct {
	Source   string `json:"source,omitempty" jsonschema:"all (default), boox or client"`
	Notebook string `json:"notebook,omitempty" jsonschema:"only this notebook"`
}

type recognitionGroup struct {
	Notebook  string `json:"notebook"`
	Title     string `json:"title"`
	State     string `json:"state" jsonschema:"running, waiting or paused"`
	Done      int    `json:"done"`
	Total     int    `json:"total"`
	Automatic bool   `json:"automatic,omitempty"`
	Updated   string `json:"updated"`
	URL       string `json:"url"`
}

type recognitionPage struct {
	Notebook string `json:"notebook"`
	Title    string `json:"title"`
	Page     int    `json:"page"`
	State    string `json:"state" jsonschema:"failed, blocked or skipped"`
	Problem  string `json:"problem,omitempty"`
	Attempts int    `json:"attempts"`
	Updated  string `json:"updated"`
	URL      string `json:"url"`
}

type recognitionOutput struct {
	Active    int                `json:"active" jsonschema:"notebooks being recognized"`
	Failed    int                `json:"failed"`
	Blocked   int                `json:"blocked"`
	Skipped   int                `json:"skipped"`
	Working   []recognitionGroup `json:"working"`
	Attention []recognitionPage  `json:"needs_attention"`
	URL       string             `json:"url"`
}

func tagPath(tag string) string { return "/boox/tags?selected=" + url.QueryEscape(tag) }

func registerLibrary(s *mcp.Server, d Deps) {
	if d.BOOX != nil {
		registerBOOXOnly(s, d)
	}
	add(s, d, tool{name: "list_books", title: "List books",
		description: "List books from one source. BOOX: every book on the tablets (or stored_only, with its file here), by shelf, reading status, " +
			"title or author, with progress. Alexandria Client: synced books with format and annotation count."},
		func(ctx context.Context, in listBooksInput) (string, listBooksOutput, error) {
			source, err := d.source(in.Source)
			if err != nil {
				return "", listBooksOutput{}, err
			}
			p, err := pageOf(in.Limit, in.Cursor)
			if err != nil {
				return "", listBooksOutput{}, err
			}
			sort := in.Sort
			if sort == "" {
				sort = "modified"
			}
			if sort != "modified" && sort != "created" && sort != "title" {
				return "", listBooksOutput{}, errors.New("sort must be modified, created or title")
			}
			order := in.Order
			if order == "" {
				order = map[bool]string{true: "asc", false: "desc"}[sort == "title"]
			}
			if order != "asc" && order != "desc" {
				return "", listBooksOutput{}, errors.New("order must be asc or desc")
			}
			loc := d.zone(ctx)
			out := listBooksOutput{Source: source, Books: []bookRow{}}
			if source == sourceBOOX {
				listSort := map[string]string{"modified": "", "created": "created", "title": "name"}[sort]
				list, total, err := d.BOOX.Books(ctx, boox.BookFilter{StoredOnly: in.Stored, Shelf: in.Shelf, Query: strings.TrimSpace(in.Query),
					Status: in.Status, Sort: listSort, Order: order, Offset: p.offset, Limit: p.limit})
				if err != nil {
					return "", listBooksOutput{}, err
				}
				out.Total = total
				for _, b := range list {
					out.Books = append(out.Books, d.booxBook(b, loc))
				}
				out.NextCursor = p.next(len(list), total)
			} else {
				if in.Stored || in.Shelf != "" || in.Status != "" {
					return "", listBooksOutput{}, errors.New("stored_only, shelf and status apply to BOOX books only")
				}
				all, err := books.NewLibraryService(d.DB, d.Objects).ListBooks(ctx)
				if err != nil {
					return "", listBooksOutput{}, err
				}
				if q := strings.ToLower(strings.TrimSpace(in.Query)); q != "" {
					all = slices.DeleteFunc(all, func(b books.LibraryBook) bool {
						return !strings.Contains(strings.ToLower(b.Title+" "+b.Authors), q)
					})
				}
				slices.SortStableFunc(all, func(a, b books.LibraryBook) int {
					c := 0
					switch sort {
					case "title":
						c = searchorder.Natural(a.Title, b.Title)
					case "created":
						c = compareInt(a.AddedAt, b.AddedAt)
					default:
						c = compareInt(a.ModifiedAt, b.ModifiedAt)
					}
					if order == "desc" {
						c = -c
					}
					return c
				})
				out.Total = len(all)
				shown := window(all, p)
				for _, b := range shown {
					out.Books = append(out.Books, d.clientBook(b, loc))
				}
				out.NextCursor = p.next(len(shown), len(all))
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d books in all (%s).\n", out.Total, source)
			for _, r := range out.Books {
				fmt.Fprintf(&b, "- %s", r.Title)
				if r.Authors != "" {
					fmt.Fprintf(&b, " by %s", r.Authors)
				}
				fmt.Fprintf(&b, " (%s)", r.Book)
				for _, f := range []string{r.Status, r.Progress, r.Shelf, r.Format} {
					if f != "" {
						b.WriteString(", " + f)
					}
				}
				b.WriteString("\n")
			}
			b.WriteString(more(out.NextCursor))
			return b.String(), out, nil
		})

	add(s, d, tool{name: "get_book", title: "Open a book",
		description: "Describe one book with its highlights and bookmarks (BOOX) or annotations (Alexandria Client: the passage, the note, " +
			"and text recognized from handwriting). Stored BOOX PDFs with handwriting link to a download with the handwriting drawn in."},
		func(ctx context.Context, in bookInput) (string, bookOutput, error) {
			source, id, err := parseRef(in.Book)
			if err != nil {
				return "", bookOutput{}, err
			}
			loc := d.zone(ctx)
			out := bookOutput{Notes: []bookNote{}}
			if source == sourceBOOX {
				if d.BOOX == nil {
					return "", bookOutput{}, errNoBOOX
				}
				bk, err := d.BOOX.Book(ctx, id)
				if errors.Is(err, boox.ErrNotFound) {
					return "", bookOutput{}, fmt.Errorf("no BOOX book %s", in.Book)
				} else if err != nil {
					return "", bookOutput{}, err
				}
				out.bookRow, out.FileName, out.InkPages = d.booxBook(bk, loc), bk.FileName, bk.InkPages
				if bk.InkPages > 0 {
					out.AnnotatedPDF = d.link("/boox/books/annotated.pdf?book=" + url.QueryEscape(bk.NativeID))
				}
				for _, n := range bk.Notes {
					note := bookNote{Kind: strings.ToLower(n.Kind), Quote: n.Quote, Note: n.Note, Created: ms(n.CreatedAt, loc)}
					if n.Page != "" {
						note.Location = "page " + n.Page
					}
					out.Notes = append(out.Notes, note)
				}
			} else {
				detail, err := books.NewLibraryService(d.DB, d.Objects).GetBook(ctx, id)
				if err != nil {
					return "", bookOutput{}, fmt.Errorf("no Alexandria Client book %s", in.Book)
				}
				out.bookRow, out.NotesTruncated = d.clientBook(detail.Book, loc), detail.Truncated
				for _, a := range detail.Annotations {
					kind := "annotation"
					if a.Sticky {
						kind = "sticky note"
					}
					if a.Highlighted {
						kind = "highlight"
					}
					out.Notes = append(out.Notes, bookNote{Kind: kind, Location: a.Location, Quote: a.Passage, Note: a.RecognizedText, Created: ms(a.CreatedAt, loc)})
				}
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s", out.Title)
			if out.Authors != "" {
				fmt.Fprintf(&b, " by %s", out.Authors)
			}
			fmt.Fprintf(&b, " (%s)\n", out.Book)
			for _, f := range [][2]string{{"Status", out.Status}, {"Progress", out.Progress}, {"Shelf", out.Shelf}, {"Format", out.Format}, {"Annotated PDF", out.AnnotatedPDF}} {
				if f[1] != "" {
					fmt.Fprintf(&b, "%s: %s\n", f[0], f[1])
				}
			}
			for _, n := range out.Notes {
				fmt.Fprintf(&b, "\n[%s", n.Kind)
				if n.Location != "" {
					fmt.Fprintf(&b, ", %s", n.Location)
				}
				b.WriteString("]")
				if n.Quote != "" {
					fmt.Fprintf(&b, " %q", n.Quote)
				}
				if n.Note != "" {
					fmt.Fprintf(&b, "\n  Note: %s", n.Note)
				}
				b.WriteString("\n")
			}
			return b.String(), out, nil
		})

	add(s, d, tool{name: "recognition_status", title: "Recognition progress",
		description: "How handwriting recognition is going: notebooks being recognized (pages done of total), and pages that failed, were blocked " +
			"or were skipped, with the reason. Narrow it to a source or one notebook."},
		func(ctx context.Context, in recognitionInput) (string, recognitionOutput, error) {
			source := in.Source
			if source == "all" {
				source = ""
			}
			notebook := ""
			if in.Notebook != "" {
				var err error
				if source, notebook, err = parseRef(in.Notebook); err != nil {
					return "", recognitionOutput{}, err
				}
			}
			store := recognition.Store{DB: d.DB}
			loc := d.zone(ctx)
			tally, err := store.Tally(ctx, source)
			if err != nil {
				return "", recognitionOutput{}, err
			}
			out := recognitionOutput{Active: tally.Active, Failed: tally.Failed, Blocked: tally.Blocked, Skipped: tally.Skipped,
				Working: []recognitionGroup{}, Attention: []recognitionPage{}, URL: d.link("/jobs")}
			groups, _, err := store.Groups(ctx, source, true, 0, 500)
			if err != nil {
				return "", recognitionOutput{}, err
			}
			for _, g := range groups {
				if notebook != "" && g.Notebook != notebook {
					continue
				}
				out.Working = append(out.Working, recognitionGroup{Notebook: ref(g.Source, g.Notebook), Title: g.Title, State: strings.ToLower(g.State()),
					Done: g.Done, Total: g.Total, Automatic: g.Automatic, Updated: when(g.Updated, loc), URL: d.link(g.URL())})
			}
			pages, _, err := store.Pages(ctx, source, []string{"failed", "blocked", "skipped"}, 0, 500)
			if err != nil {
				return "", recognitionOutput{}, err
			}
			for _, j := range pages {
				if notebook != "" && j.Notebook != notebook {
					continue
				}
				out.Attention = append(out.Attention, recognitionPage{Notebook: ref(j.Source, j.Notebook), Title: j.Title, Page: j.Number, State: j.State,
					Problem: j.Detail, Attempts: j.Attempts, Updated: when(j.Updated, loc), URL: d.link(j.URL())})
			}
			out.Working, out.Attention = out.Working[:min(len(out.Working), 50)], out.Attention[:min(len(out.Attention), 50)]
			var b strings.Builder
			fmt.Fprintf(&b, "%d notebooks being recognized; %d pages failed, %d blocked, %d skipped.\n", out.Active, out.Failed, out.Blocked, out.Skipped)
			for _, g := range out.Working {
				fmt.Fprintf(&b, "- %s (%s): %s, %d of %d pages\n", g.Title, g.Notebook, g.State, g.Done, g.Total)
			}
			for _, p := range out.Attention {
				fmt.Fprintf(&b, "- %s page %d (%s): %s %s\n", p.Title, p.Page, p.Notebook, p.State, clip(p.Problem, 200))
			}
			return b.String(), out, nil
		})
}

func compareInt(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func (d Deps) booxBook(b boox.Book, loc *time.Location) bookRow {
	return bookRow{Book: ref(sourceBOOX, b.ID), Title: b.Title, Authors: b.Authors, Status: b.Status, Progress: b.Percent, Shelf: b.Shelf, Stored: b.Stored,
		Created: ms(b.CreatedAt, loc), Modified: ms(b.ModifiedAt, loc), Opened: ms(b.LastOpened, loc), Annotations: len(b.Notes),
		URL: d.link("/boox/reading?selected=" + url.QueryEscape(b.ID))}
}

func (d Deps) clientBook(b books.LibraryBook, loc *time.Location) bookRow {
	return bookRow{Book: ref(sourceClient, b.ID), Title: b.Title, Authors: b.Authors, Format: b.Format, Stored: b.FileState == "ready",
		Created: ms(b.AddedAt, loc), Modified: ms(b.ModifiedAt, loc), Annotations: b.AnnotationCount,
		URL: d.link("/files/forestnote/books?selected=" + url.QueryEscape(b.ID))}
}

// registerBOOXOnly adds the tools for what only BOOX has: tags and minutes.
func registerBOOXOnly(s *mcp.Server, d Deps) {
	add(s, d, tool{name: "list_tags", title: "List BOOX tags",
		description: "List the tags put on BOOX pages with the lasso's Tag action, most used first, with how many pages and notebooks carry each. " +
			"Searching for a tag also finds its pages."},
		func(ctx context.Context, _ emptyInput) (string, listTagsOutput, error) {
			tags, err := d.BOOX.Tags(ctx)
			if err != nil {
				return "", listTagsOutput{}, err
			}
			out := listTagsOutput{Tags: []tagRow{}}
			var b strings.Builder
			fmt.Fprintf(&b, "%d tags.\n", len(tags))
			for _, t := range tags {
				out.Tags = append(out.Tags, tagRow{Tag: t.Tag, Pages: t.Pages, Notebooks: t.Notebooks, URL: d.link(tagPath(t.Tag))})
				fmt.Fprintf(&b, "- %s: %d pages in %d notebooks\n", t.Tag, t.Pages, t.Notebooks)
			}
			return b.String(), out, nil
		})

	add(s, d, tool{name: "get_tag", title: "Pages with a BOOX tag",
		description: "List the BOOX pages carrying a tag, by notebook and page, with the start of each page's recognized text."},
		func(ctx context.Context, in tagInput) (string, tagOutput, error) {
			t, pages, err := d.BOOX.TaggedPages(ctx, in.Tag)
			if errors.Is(err, boox.ErrNotFound) {
				return "", tagOutput{}, fmt.Errorf("no tag %q; list_tags shows them all", in.Tag)
			} else if err != nil {
				return "", tagOutput{}, err
			}
			out := tagOutput{tagRow: tagRow{Tag: t.Tag, Pages: t.Pages, Notebooks: t.Notebooks, URL: d.link(tagPath(t.Tag))}, PagesWithTag: []taggedPageRow{}}
			var b strings.Builder
			fmt.Fprintf(&b, "%s: %d pages in %d notebooks\n", t.Tag, t.Pages, t.Notebooks)
			for _, p := range pages {
				out.PagesWithTag = append(out.PagesWithTag, taggedPageRow{Notebook: ref(sourceBOOX, p.NotebookID), Title: p.Notebook, Page: p.Number, Snippet: p.Snippet,
					URL: d.link(notebookPath(sourceBOOX, p.NotebookID, p.Number, ""))})
				fmt.Fprintf(&b, "- %s, page %d (%s): %s\n", p.Notebook, p.Number, ref(sourceBOOX, p.NotebookID), clip(p.Snippet, 160))
			}
			return b.String(), out, nil
		})

	add(s, d, tool{name: "list_minutes", title: "List Audio Note Minutes",
		description: "List BOOX notebooks that have Audio Note Minutes (summaries of their recordings and handwriting), newest first, with each " +
			"one's state and topic. get_minutes reads them."},
		func(ctx context.Context, in listMinutesInput) (string, listMinutesOutput, error) {
			switch in.State {
			case "", "ready", "queued", "processing", "failed", "cancelled":
			default:
				return "", listMinutesOutput{}, errors.New("state must be ready, queued, processing, failed or cancelled")
			}
			p, err := pageOf(in.Limit, in.Cursor)
			if err != nil {
				return "", listMinutesOutput{}, err
			}
			loc := d.zone(ctx)
			list, total, err := d.BOOX.MinutesList(ctx, in.State, p.offset, p.limit)
			if err != nil {
				return "", listMinutesOutput{}, err
			}
			out := listMinutesOutput{Total: total, Minutes: []minutesRow{}, NextCursor: p.next(len(list), total)}
			var b strings.Builder
			fmt.Fprintf(&b, "%d notebooks with minutes.\n", total)
			for _, m := range list {
				r := minutesRow{Notebook: ref(sourceBOOX, m.NotebookID), Title: m.Notebook, State: m.State, Topic: m.Topic, Updated: when(m.Updated, loc),
					URL: d.link("/boox/minutes?id=" + url.QueryEscape(m.NotebookID))}
				if m.Finished != nil {
					r.Written = when(*m.Finished, loc)
				}
				if m.State == "failed" {
					r.Problem = m.Detail
				}
				out.Minutes = append(out.Minutes, r)
				fmt.Fprintf(&b, "- %s (%s): %s", r.Title, r.Notebook, r.State)
				if r.Topic != "" {
					fmt.Fprintf(&b, ", %q", r.Topic)
				}
				b.WriteString("\n")
			}
			b.WriteString(more(out.NextCursor))
			return b.String(), out, nil
		})

	add(s, d, tool{name: "get_minutes", title: "Read Audio Note Minutes",
		description: "Read a BOOX notebook's Audio Note Minutes: topic, keywords, key points (each marked as from the recording, the handwritten " +
			"notes or both), conclusion, whether they're out of date, details of the recordings, and their transcripts unless include_transcripts is false."},
		func(ctx context.Context, in getMinutesInput) (string, getMinutesOutput, error) {
			source, id, err := parseRef(in.Notebook)
			if err != nil {
				return "", getMinutesOutput{}, err
			}
			if source != sourceBOOX {
				return "", getMinutesOutput{}, errors.New("only BOOX notebooks have Audio Note Minutes")
			}
			var live bool
			if err := d.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_search_notebooks WHERE document_id=$1)`, id).Scan(&live); err != nil {
				return "", getMinutesOutput{}, err
			}
			m, ok, err := minutes.Load(ctx, d.DB, "boox", id)
			if err != nil {
				return "", getMinutesOutput{}, err
			}
			if !live || !ok {
				return "", getMinutesOutput{}, fmt.Errorf("%s has no minutes; list_minutes shows the notebooks that do", in.Notebook)
			}
			loc := d.zone(ctx)
			out := getMinutesOutput{Notebook: ref(sourceBOOX, id), Title: m.Metadata.Title, State: m.State, Topic: m.Topic, Keywords: m.Keywords,
				KeyPoints: m.KeyPoints, Conclusion: m.Conclusion, Model: m.Model, Metadata: m.Metadata, Recordings: []transcriptRow{},
				URL: d.link("/boox/minutes?id=" + url.QueryEscape(id))}
			if out.Keywords == nil {
				out.Keywords = []string{}
			}
			if out.KeyPoints == nil {
				out.KeyPoints = []minutes.Point{}
			}
			if out.Metadata.Recordings == nil {
				out.Metadata.Recordings = []minutes.RecordingSummary{}
			}
			if m.State == "failed" {
				out.Problem = m.Detail
			}
			if m.FinishedAt.Valid {
				out.Written = when(m.FinishedAt.Time, loc)
			}
			if m.State == "ready" {
				if out.OutOfDate, err = d.BOOX.MinutesStale(ctx, id); err != nil && !errors.Is(err, boox.ErrNotFound) {
					return "", getMinutesOutput{}, err
				}
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Minutes for %s (%s): %s\n", out.Title, out.Notebook, out.State)
			if out.OutOfDate {
				b.WriteString("Out of date: the notebook changed since these were written.\n")
			}
			if out.Topic == "" {
				fmt.Fprintf(&b, "Not written yet. %s\n", out.Problem)
			} else {
				fmt.Fprintf(&b, "Topic: %s\nKeywords: %s\nKey points:\n", out.Topic, strings.Join(out.Keywords, ", "))
				for i, p := range out.KeyPoints {
					fmt.Fprintf(&b, "%d. %s [%s]\n", i+1, p.Text, p.From)
				}
				fmt.Fprintf(&b, "Conclusion: %s\n", out.Conclusion)
			}
			for _, r := range m.Recordings {
				t := transcriptRow{Title: r.Title, RecordedAt: when(r.RecordedAt, loc), Seconds: r.DurationMS / 1000, Language: r.Language, State: r.State}
				fmt.Fprintf(&b, "\nRecording %q, %s, %d s\n", r.Title, t.RecordedAt, t.Seconds)
				if in.Transcripts == nil || *in.Transcripts {
					t.Text = r.Text
					b.WriteString(r.Text + "\n")
				}
				out.Recordings = append(out.Recordings, t)
			}
			return b.String(), out, nil
		})
}
