// Package searchview ranks everything Search finds, by type: notebooks
// (titles), pages (page text, BOOX reading text and minutes), books and
// highlights (BOOX reading records, Client books and annotations) and tasks.
// It returns plain data; the web page and MCP each present it their way.
package searchview

import (
	"cmp"
	"context"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/books"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/readersearch"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxvisibility"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
)

const (
	// Cap is how many of each kind's best matches a type can list; counts
	// are always complete.
	Cap = 500
	// MaxTerms bounds the words a query is matched by.
	MaxTerms = 16
)

// Types are the result types, in order; "all" fuses the others.
var Types = []string{"all", "notebooks", "pages", "books", "tasks"}

// Kind is what a hit is.
type Kind string

const (
	KindPage       Kind = "page"       // recognized or typed page text
	KindMinutes    Kind = "minutes"    // Audio Note Minutes
	KindNotebook   Kind = "notebook"   // a notebook title
	KindBook       Kind = "book"       // a BOOX reading record or Client book
	KindHighlight  Kind = "highlight"  // a BOOX highlight
	KindBookmark   Kind = "bookmark"   // a BOOX bookmark
	KindAnnotation Kind = "annotation" // a Client book annotation
	KindTask       Kind = "task"
)

// Reading is what a BOOX reading record adds to its match: a highlight or
// bookmark names its book.
type Reading struct {
	Page, Authors, Book, BookID string
}

// Hit is one match. Fields beyond Kind, Source, ID, Title and URL are set
// when they apply. Page and reading hits get their snippet, tags and page
// number from Finish.
type Hit struct {
	Kind   Kind
	Source string // "boox" or "client"; "" for a task
	ID     string // note key, notebook, book, annotation or task ID
	Title  string // notebook, book or task title
	// URL is the site-relative web page; "" for a task (the web links its
	// own editor).
	URL       string
	Snippet   string // plain text; Finish's page snippets carry notes.MarkStart/MarkStop
	Tags      []string
	CreatedAt int64 // ms; 0 when unknown

	Live    *notes.Result // page and BOOX reading matches
	Reading *Reading      // BOOX reading matches

	Pages   int    // notebooks: page count
	Typed   bool   // BOOX notebooks: a text note
	Authors string // Client books
	Format  string // Client books
	BookID  string // Client books and annotations
	Detail  string // tasks
	Done    bool   // tasks: completed or cancelled
	Due     int64  // tasks: due time in ms, 0 for none

	position int64
}

// List is one type's matches, best first, with its full count.
type List struct {
	Hits  []Hit
	Total int
	// Meaning is how many of Hits were found by meaning alone: the nearest
	// pages to the query, not pages that have its words.
	Meaning int
}

// Query is a search.
type Query struct {
	Text string
	// Words searches words only; otherwise meaning too, when configured.
	Words bool
	// Source is "", "boox" or "client".
	Source string
	// NativeBOOX is whether this library has BOOX; without it only Client
	// content is searched.
	NativeBOOX bool
	// UntitledBook names an annotation's book that has no title.
	UntitledBook string
}

// Searcher searches one library.
type Searcher struct {
	DB      pg.DB
	Objects blob.Store
	Search  notes.Searcher
}

// Terms are a query's lowercase words, at most MaxTerms.
func Terms(query string) []string {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) > MaxTerms {
		terms = terms[:MaxTerms]
	}
	return terms
}

// All ranks every kind of match, each type's sources fused into one list.
func (s Searcher) All(ctx context.Context, q Query) (map[string]List, error) {
	terms := Terms(q.Text)
	scope := q.Source
	if !q.NativeBOOX {
		scope = "client"
	}
	ranking, err := s.Search.Rank(ctx, q.Text, Cap, !q.Words, scope)
	if err != nil {
		return nil, err
	}
	pages := List{Total: ranking.PagesTotal}
	for i := range ranking.Pages {
		r := &ranking.Pages[i]
		kind := KindPage
		if r.Kind == "Minutes" {
			kind = KindMinutes
		}
		pages.Hits = append(pages.Hits, Hit{Kind: kind, Source: r.Source, ID: r.NoteKey, Title: r.NotebookName, URL: r.URL, CreatedAt: r.CreatedAt, position: r.Position, Live: r})
	}
	if scope != "client" {
		if err = s.dropMinutesCopies(ctx, &pages); err != nil {
			return nil, err
		}
	}
	pages.Meaning = meaningOnly(pages.Hits)
	reading, err := s.readingHits(ctx, ranking)
	if err != nil {
		return nil, err
	}
	reading.Meaning = meaningOnly(reading.Hits)
	var notebooks []List
	if scope != "client" {
		list, err := s.booxNotebooks(ctx, terms)
		if err != nil {
			return nil, err
		}
		notebooks = append(notebooks, list)
	}
	parts := []List{reading}
	if scope != "boox" {
		list, err := s.clientNotebooks(ctx, terms)
		if err != nil {
			return nil, err
		}
		notebooks = append(notebooks, list)
		if parts, err = s.clientBooks(ctx, q, terms, parts); err != nil {
			return nil, err
		}
	}
	// Tasks aren't in a source's library, so only the chosen source narrows them.
	tasks, err := s.tasks(ctx, terms, q.Source)
	if err != nil {
		return nil, err
	}
	return map[string]List{"notebooks": Fuse(notebooks...), "pages": pages, "books": Fuse(parts...), "tasks": tasks}, nil
}

// meaningOnly counts the hits found by meaning alone.
func meaningOnly(hits []Hit) int {
	n := 0
	for _, h := range hits {
		if h.Live != nil && !h.Live.Lexical {
			n++
		}
	}
	return n
}

// dropMinutesCopies leaves out the pages of a "_Minutes" note sent to BOOX
// when the minutes it carries are among the matches: it says the same thing.
func (s Searcher) dropMinutesCopies(ctx context.Context, pages *List) error {
	minutes := map[string]bool{}
	for _, h := range pages.Hits {
		if h.Kind == KindMinutes {
			minutes[h.Live.NotebookID] = true
		}
	}
	if len(minutes) == 0 {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT document_id,payload->>'minutesOf' FROM boox_operation
	 WHERE kind='create_text_note' AND coalesce(payload->>'minutesOf','')<>''`)
	if err != nil {
		return err
	}
	defer rows.Close()
	copyOf := map[string]string{}
	for rows.Next() {
		var note, of string
		if err = rows.Scan(&note, &of); err != nil {
			return err
		}
		copyOf[note] = of
	}
	if err = rows.Err(); err != nil {
		return err
	}
	before := len(pages.Hits)
	pages.Hits = slices.DeleteFunc(pages.Hits, func(h Hit) bool {
		return h.Kind == KindPage && h.Source == "boox" && minutes[copyOf[h.Live.NotebookID]]
	})
	pages.Total -= before - len(pages.Hits)
	return nil
}

// Fuse merges ranked lists by reciprocal rank (k = 60). Each match is in one
// list, so this interleaves them: every list's first match, then every
// list's second, earlier lists first.
func Fuse(lists ...List) List {
	type scored struct {
		hit   Hit
		score float64
		list  int
	}
	var all []scored
	out := List{}
	for i, list := range lists {
		out.Total += list.Total
		out.Meaning += list.Meaning
		for rank, hit := range list.Hits {
			all = append(all, scored{hit, 1 / (60 + float64(rank+1)), i})
		}
	}
	slices.SortStableFunc(all, func(a, b scored) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		return cmp.Compare(a.list, b.list)
	})
	for _, s := range all {
		out.Hits = append(out.Hits, s.hit)
	}
	return out
}

// Sort applies a title or date order; relevance keeps the ranking.
func Sort(hits []Hit, order searchorder.Order) {
	byTitle := func(a, b Hit) int {
		if c := searchorder.Natural(a.Title, b.Title); c != 0 {
			return c
		}
		if c := cmp.Compare(a.position, b.position); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	}
	// Missing dates sort last either way, as in SQL.
	dated := func(a, b Hit, desc bool) int {
		if (a.CreatedAt == 0) != (b.CreatedAt == 0) {
			if a.CreatedAt == 0 {
				return 1
			}
			return -1
		}
		c := cmp.Compare(a.CreatedAt, b.CreatedAt)
		if desc {
			c = -c
		}
		if c != 0 {
			return c
		}
		return byTitle(a, b)
	}
	switch order {
	case "title_asc":
		slices.SortStableFunc(hits, byTitle)
	case "title_desc":
		slices.SortStableFunc(hits, func(a, b Hit) int { return byTitle(b, a) })
	case "created_asc":
		slices.SortStableFunc(hits, func(a, b Hit) int { return dated(a, b, false) })
	case "created_desc":
		slices.SortStableFunc(hits, func(a, b Hit) int { return dated(a, b, true) })
	}
}

// Finish fills in the snippets, tags and page numbers of the page and
// reading matches among hits, with one query for these rows only.
func (s Searcher) Finish(ctx context.Context, query string, hits []Hit) error {
	var live []notes.Result
	for _, h := range hits {
		if h.Live != nil {
			live = append(live, *h.Live)
		}
	}
	if err := s.Search.Snippets(ctx, query, live); err != nil {
		return err
	}
	i := 0
	for n := range hits {
		h := &hits[n]
		if h.Live == nil {
			continue
		}
		r := live[i]
		i++
		h.Live = &r
		h.Snippet, h.Tags = r.Snippet, r.Tags
		if h.Kind == KindBook && h.Reading != nil {
			h.Snippet = h.Reading.Authors
		}
	}
	return nil
}

// readingHits turns BOOX reading matches into books and highlights. A
// highlight or bookmark takes its book's title and opens that book.
func (s Searcher) readingHits(ctx context.Context, ranking notes.Ranking) (List, error) {
	out := List{Total: ranking.ReadingTotal}
	if len(ranking.Reading) == 0 {
		return out, nil
	}
	ids := make([]string, len(ranking.Reading))
	for i, r := range ranking.Reading {
		ids[i] = r.NotebookID
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT p.document_id,coalesce(p.body->>'modeType',''),coalesce(p.body->>'pageNumber',''),
 CASE jsonb_typeof(p.body->'authors') WHEN 'array' THEN (SELECT string_agg(a,', ') FROM jsonb_array_elements_text(p.body->'authors') a) ELSE coalesce(p.body->>'authors','') END,
 coalesce(b.document_id,''),coalesce(nullif(b.body->>'title',''),b.body->>'name','')
 FROM boox_projection p LEFT JOIN boox_projection b ON p.body->>'modeType' IN ('1','2') AND b.domain='reading' AND b.document_id=p.native_uid||'#'||(p.body->>'documentId')
 WHERE p.document_id=ANY($1)`, ids)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	type match struct {
		kind Kind
		m    Reading
	}
	found := map[string]match{}
	for rows.Next() {
		var id, mode string
		var m Reading
		if err = rows.Scan(&id, &mode, &m.Page, &m.Authors, &m.BookID, &m.Book); err != nil {
			return out, err
		}
		kind := map[string]Kind{"1": KindHighlight, "2": KindBookmark}[mode]
		if kind == "" {
			kind, m.BookID = KindBook, id
		}
		found[id] = match{kind, m}
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	for i := range ranking.Reading {
		r := &ranking.Reading[i]
		f, ok := found[r.NotebookID]
		if !ok {
			f = match{KindBook, Reading{BookID: r.NotebookID}}
		}
		m := f.m
		title := r.NotebookName
		if f.kind != KindBook && m.Book != "" {
			title = m.Book
		}
		h := Hit{Kind: f.kind, Source: r.Source, ID: r.NoteKey, Title: title, URL: "/boox/reading?selected=" + url.QueryEscape(m.BookID), CreatedAt: r.CreatedAt, Live: r, Reading: &m}
		if m.BookID == "" {
			h.URL = r.URL // a highlight whose book isn't here: its own record
		}
		out.Hits = append(out.Hits, h)
	}
	return out, nil
}

// termsSQL is true when every one of $1's lowercase terms is in the text.
func termsSQL(text string) string {
	return `NOT EXISTS(SELECT 1 FROM unnest($1::text[]) AS term(t) WHERE strpos(lower(` + text + `),term.t)=0)`
}

// titleRank puts exact titles first, then titles that start with the query.
func titleRank(title string) string {
	return `lower(` + title + `)=array_to_string($1::text[],' ') DESC,starts_with(lower(` + title + `),$1[1]) DESC,length(` + title + `)`
}

// booxNotebooks matches BOOX notebook titles containing every term.
func (s Searcher) booxNotebooks(ctx context.Context, terms []string) (List, error) {
	out := List{}
	title := `coalesce(p.body->>'title','')`
	rows, err := s.DB.QueryContext(ctx, `SELECT p.document_id,coalesce(nullif(p.body->>'title',''),'Untitled'),coalesce(`+searchorder.NativeCreatedSQL("p")+`,0),
 coalesce(p.body->>'activeScene','')='1',jsonb_array_length(boox_search_page_ids(p.body->'pageNameList')),count(*) OVER()
 FROM boox_projection p WHERE `+booxvisibility.Notebook("p")+` AND `+termsSQL(title)+`
 ORDER BY `+titleRank(title)+`,p.document_id LIMIT $2`, terms, Cap)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		h := Hit{Kind: KindNotebook, Source: "boox"}
		if err = rows.Scan(&h.ID, &h.Title, &h.CreatedAt, &h.Typed, &h.Pages, &out.Total); err != nil {
			return out, err
		}
		h.URL = "/boox/notebook?id=" + url.QueryEscape(h.ID)
		out.Hits = append(out.Hits, h)
	}
	return out, rows.Err()
}

// clientNotebooks matches Client notebook names containing every term.
func (s Searcher) clientNotebooks(ctx context.Context, terms []string) (List, error) {
	out := List{}
	title := `coalesce(n.name,'')`
	rows, err := s.DB.QueryContext(ctx, `SELECT n.id,coalesce(nullif(n.name,''),'Untitled'),CASE WHEN n.created_at>0 THEN n.created_at ELSE 0 END,
 (SELECT count(*) FROM fn_page p WHERE p.notebook_id=n.id AND p.deleted_at IS NULL),count(*) OVER()
 FROM fn_notebook n WHERE n.deleted_at IS NULL AND `+termsSQL(title)+`
 ORDER BY `+titleRank(title)+`,n.id LIMIT $2`, terms, Cap)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		h := Hit{Kind: KindNotebook, Source: "client"}
		if err = rows.Scan(&h.ID, &h.Title, &h.CreatedAt, &h.Pages, &out.Total); err != nil {
			return out, err
		}
		h.URL = "/files/forestnote?notebook=" + url.QueryEscape(h.ID)
		out.Hits = append(out.Hits, h)
	}
	return out, rows.Err()
}

// clientBooks adds Client books whose title or author has every term, then
// Client book annotations, to parts.
func (s Searcher) clientBooks(ctx context.Context, q Query, terms []string, parts []List) ([]List, error) {
	all, err := books.NewLibraryService(s.DB, s.Objects).ListBooks(ctx)
	if err != nil {
		return nil, err
	}
	list := List{}
	for _, b := range all {
		text := strings.ToLower(b.Title + " " + b.Authors)
		if !slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(text, t) }) {
			list.Hits = append(list.Hits, Hit{Kind: KindBook, Source: "client", ID: b.ID, Title: b.Title, URL: "/files/forestnote/books?selected=" + url.QueryEscape(b.ID),
				Snippet: b.Authors, Authors: b.Authors, Format: b.Format, BookID: b.ID, CreatedAt: b.AddedAt})
		}
	}
	// Exact titles, then titles that start with the query, then the rest
	// alphabetically.
	joined := strings.Join(terms, " ")
	slices.SortStableFunc(list.Hits, func(a, b Hit) int {
		rank := func(h Hit) int {
			t := strings.ToLower(h.Title)
			switch {
			case t == joined:
				return 0
			case strings.HasPrefix(t, joined):
				return 1
			}
			return 2
		}
		if c := cmp.Compare(rank(a), rank(b)); c != 0 {
			return c
		}
		return searchorder.Natural(a.Title, b.Title)
	})
	list.Total = len(list.Hits)
	list.Hits = list.Hits[:min(len(list.Hits), Cap)]
	parts = append(parts, list)
	// Annotation search takes at most 16 words.
	if len(strings.Fields(q.Text)) > MaxTerms {
		return parts, nil
	}
	store := readersearch.New(s.DB)
	annotations := List{}
	for offset := 0; offset < Cap; offset += 100 {
		found, total, err := store.SearchPage(ctx, q.Text, "", 100, offset, searchorder.Relevance)
		if err != nil {
			return nil, err
		}
		annotations.Total = total
		for _, a := range found {
			title := a.Title
			if title == "" {
				title = q.UntitledBook
			}
			annotations.Hits = append(annotations.Hits, Hit{Kind: KindAnnotation, Source: "client", ID: a.AnnotationID, Title: title,
				URL: "/files/forestnote/books?selected=" + url.QueryEscape(a.BookID), Snippet: a.Snippet, BookID: a.BookID})
		}
		if offset+100 >= total {
			break
		}
	}
	return append(parts, annotations), nil
}

// tasks matches tasks whose title or details have every term, titles first.
// A task belongs to BOOX or the Client when a note action or Client page
// made it.
func (s Searcher) tasks(ctx context.Context, terms []string, scope string) (List, error) {
	out := List{}
	text := `coalesce(t.title,'')||' '||coalesce(t.detail,'')`
	rows, err := s.DB.QueryContext(ctx, `SELECT t.task_id,coalesce(t.title,''),coalesce(t.detail,''),t.status IN ('completed','cancelled'),t.due_time,t.created_at,count(*) OVER()
 FROM alexandria_tasks t WHERE NOT t.deleted AND `+termsSQL(text)+`
 AND ($2='' OR $2='client' AND t.forestnote_notebook_id IS NOT NULL
  OR $2='boox' AND EXISTS(SELECT 1 FROM alexandria_note_action_item i WHERE i.kind='task' AND i.item_id=t.task_id))
 ORDER BY `+termsSQL(`coalesce(t.title,'')`)+` DESC,
  ts_rank(setweight(to_tsvector('simple',coalesce(t.title,'')),'A')||setweight(to_tsvector('simple',coalesce(t.detail,'')),'C'),plainto_tsquery('simple',array_to_string($1::text[],' '))) DESC,
  t.updated_at DESC,t.task_id LIMIT $3`, terms, scope, Cap)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		h := Hit{Kind: KindTask}
		if err = rows.Scan(&h.ID, &h.Title, &h.Detail, &h.Done, &h.Due, &h.CreatedAt, &out.Total); err != nil {
			return out, err
		}
		h.Snippet = Around(h.Detail, terms, 200)
		out.Hits = append(out.Hits, h)
	}
	return out, rows.Err()
}

// Around is at most limit characters of s, starting a little before the
// first term it contains; "" when it contains none.
func Around(s string, terms []string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	lower := strings.ToLower(s)
	first := -1
	for _, t := range terms {
		if n := strings.Index(lower, t); n >= 0 && (first < 0 || n < first) {
			first = n
		}
	}
	if first < 0 {
		return ""
	}
	runes := []rune(s)
	start := max(0, utf8.RuneCountInString(lower[:first])-min(40, limit/3))
	prefix := ""
	if start > 0 {
		prefix = "…"
	}
	if len(runes)-start <= limit {
		return prefix + string(runes[start:])
	}
	return prefix + string(runes[start:start+limit]) + "…"
}

// Plain removes the highlight markers Finish's page snippets carry.
func Plain(snippet string) string {
	return strings.NewReplacer(notes.MarkStart, "", notes.MarkStop, "").Replace(snippet)
}
