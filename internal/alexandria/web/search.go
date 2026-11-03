package web

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/books"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/readersearch"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxvisibility"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
)

const (
	searchPageSize = 20
	// searchCap is how many of each kind's best matches a tab can list;
	// counts are always complete.
	searchCap = 500
	// searchTerms bounds the words a query is matched by.
	searchTerms = 16
)

// searchTypes are the Search tabs, in order.
var searchTypes = []string{"all", "notebooks", "pages", "books", "tasks"}

// markPart is a run of result text, highlighted when it matched the query.
// The template escapes both kinds, so stored text can't add markup.
type markPart struct {
	Text string
	Hit  bool
}

// searchHit is one result row.
type searchHit struct {
	Label, Icon, URL string
	Title, Snippet   []markPart
	Meta             []string
	Tags             []string
	CreatedAt        int64
	// title, position and key order hits for the title sorts.
	title    string
	position int64
	key      string
	// live is a page or BOOX reading match, finished once it is shown.
	live *notes.Result
	// reading describes a BOOX reading match.
	reading *readingMatch
}

func (h searchHit) CreatedDate() string { return searchorder.Date(h.CreatedAt) }

// searchList is one kind of match, best first, with its full count.
type searchList struct {
	Hits  []searchHit
	Total int
}

// searchTab is one of the type tabs.
type searchTab struct {
	Type, Label, URL string
	Count            int
	Current          bool
}

// searchState is the page's URL state.
type searchState struct {
	Query, Type, Mode, Source string
	Order                     searchorder.Order
}

func (s searchState) values() url.Values {
	v := url.Values{"q": {s.Query}}
	if s.Type != "all" {
		v.Set("type", s.Type)
	}
	if s.Mode != "" {
		v.Set("mode", s.Mode)
	}
	if s.Source != "" {
		v.Set("source", s.Source)
	}
	if s.Order != searchorder.Relevance {
		v.Set("sort", string(s.Order))
	}
	return v
}

// Link is the first page of results with one setting changed.
func (s searchState) Link(key, value string) string {
	switch key {
	case "type":
		s.Type = value
	case "mode":
		s.Mode = value
	case "source":
		s.Source = value
	}
	return "/search?" + s.values().Encode()
}

func (d Deps) searchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		values := r.URL.Query()
		s := searchState{Query: strings.TrimSpace(values.Get("q")), Type: values.Get("type"), Mode: values.Get("mode"), Source: values.Get("source"), Order: searchorder.Parse(values.Get("sort"))}
		if len(s.Query) > 256 || (s.Source != "" && s.Source != "client" && s.Source != "boox") || (s.Source == "boox" && !d.Status.NativeBOOX) {
			http.Error(w, "Invalid search query or source.", 400)
			return
		}
		if !slices.Contains(searchTypes, s.Type) {
			s.Type = "all"
		}
		if s.Mode != "keyword" || d.Status.Embedding == "" {
			s.Mode = ""
		}
		data := map[string]any{"State": s, "Semantic": d.Status.Embedding != "", "BOOX": d.Status.NativeBOOX, "Sorts": searchSorts,
			"DateSort": s.Order == "created_asc" || s.Order == "created_desc"}
		if s.Query != "" {
			l := d.Localizer(r)
			lists, err := d.searchAll(r.Context(), s, l)
			if err != nil {
				d.fail(w, err)
				return
			}
			total := 0
			for _, t := range searchTypes[1:] {
				total += lists[t].Total
			}
			var tabs []searchTab
			for _, t := range searchTypes {
				count := total
				if t != "all" {
					count = lists[t].Total
				}
				tabs = append(tabs, searchTab{Type: t, Label: searchTypeLabels[t], URL: s.Link("type", t), Count: count, Current: t == s.Type})
			}
			var shown searchList
			capped := false
			if s.Type == "all" {
				parts := []searchList{lists["notebooks"], lists["pages"], lists["books"], lists["tasks"]}
				shown = fuse(parts...)
				shown.Total = len(shown.Hits)
				capped = shown.Total < total
			} else {
				shown = lists[s.Type]
				capped = len(shown.Hits) < shown.Total
				shown.Total = len(shown.Hits)
			}
			sortHits(shown.Hits, s.Order)
			offset := searchorder.OffsetOf(values.Get("offset"), searchPageSize)
			if offset >= shown.Total {
				offset = 0
			}
			page := shown.Hits[offset:min(offset+searchPageSize, shown.Total)]
			if err = d.finishHits(r.Context(), s.Query, page, l); err != nil {
				d.fail(w, err)
				return
			}
			data["Tabs"], data["Hits"], data["Total"], data["Capped"] = tabs, page, total, capped
			data["Paging"] = searchorder.NavigationOf(s.values(), "offset", "results", offset, shown.Total, searchPageSize)
		}
		d.render(w, r, "search", "Search", "search", data)
	})
}

// searchTypeLabels name the tabs; the template translates them.
var searchTypeLabels = map[string]string{"all": i18n.M("All"), "notebooks": i18n.M("Notebooks"), "pages": i18n.M("Pages"), "books": i18n.M("Books & highlights"), "tasks": i18n.M("Tasks")}

// searchAll ranks every kind of match, each tab's sources fused into one list.
func (d Deps) searchAll(ctx context.Context, s searchState, l i18n.Localizer) (map[string]searchList, error) {
	terms := strings.Fields(strings.ToLower(s.Query))
	if len(terms) > searchTerms {
		terms = terms[:searchTerms]
	}
	scope := s.Source
	if !d.Status.NativeBOOX {
		scope = "client"
	}
	ranking, err := d.Search.Rank(ctx, s.Query, searchCap, s.Mode != "keyword", scope)
	if err != nil {
		return nil, err
	}
	pages := searchList{Total: ranking.PagesTotal}
	for i := range ranking.Pages {
		r := &ranking.Pages[i]
		pages.Hits = append(pages.Hits, searchHit{URL: r.URL, CreatedAt: r.CreatedAt, title: r.NotebookName, position: r.Position, key: r.NoteKey, live: r})
	}
	reading, err := d.readingHits(ctx, ranking, terms)
	if err != nil {
		return nil, err
	}
	var notebooks []searchList
	if scope != "client" {
		list, err := d.searchBOOXNotebooks(ctx, terms, l)
		if err != nil {
			return nil, err
		}
		notebooks = append(notebooks, list)
	}
	parts := []searchList{reading}
	if scope != "boox" {
		list, err := d.searchClientNotebooks(ctx, terms, l)
		if err != nil {
			return nil, err
		}
		notebooks = append(notebooks, list)
		if parts, err = d.searchClientBooks(ctx, s.Query, terms, parts, l); err != nil {
			return nil, err
		}
	}
	// Tasks aren't in a source's library, so only the chosen source narrows them.
	tasks, err := d.searchTasks(ctx, terms, s.Source, l)
	if err != nil {
		return nil, err
	}
	return map[string]searchList{"notebooks": fuse(notebooks...), "pages": pages, "books": fuse(parts...), "tasks": tasks}, nil
}

// fuse merges ranked lists by reciprocal rank (k = 60). Each match is in one
// list, so this interleaves them: every list's first match, then every
// list's second, earlier lists first.
func fuse(lists ...searchList) searchList {
	type scored struct {
		hit   searchHit
		score float64
		list  int
	}
	var all []scored
	out := searchList{}
	for i, list := range lists {
		out.Total += list.Total
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

// sortHits applies a title or date order; relevance keeps the ranking.
func sortHits(hits []searchHit, order searchorder.Order) {
	byTitle := func(a, b searchHit) int {
		if c := searchorder.Natural(a.title, b.title); c != 0 {
			return c
		}
		if c := cmp.Compare(a.position, b.position); c != 0 {
			return c
		}
		return strings.Compare(a.key, b.key)
	}
	// Missing dates sort last either way, as in SQL.
	dated := func(a, b searchHit, desc bool) int {
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
		slices.SortStableFunc(hits, func(a, b searchHit) int { return byTitle(b, a) })
	case "created_asc":
		slices.SortStableFunc(hits, func(a, b searchHit) int { return dated(a, b, false) })
	case "created_desc":
		slices.SortStableFunc(hits, func(a, b searchHit) int { return dated(a, b, true) })
	}
}

// readingMatch is what a BOOX reading record adds to its search match.
type readingMatch struct {
	Kind, Page, Authors, Book, BookID string
}

// readingHits turns BOOX reading matches into Books & highlights rows. A
// highlight or bookmark takes its book's title and opens that book.
func (d Deps) readingHits(ctx context.Context, ranking notes.Ranking, terms []string) (searchList, error) {
	out := searchList{Total: ranking.ReadingTotal}
	if len(ranking.Reading) == 0 {
		return out, nil
	}
	ids := make([]string, len(ranking.Reading))
	for i, r := range ranking.Reading {
		ids[i] = r.NotebookID
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT p.document_id,coalesce(p.body->>'modeType',''),coalesce(p.body->>'pageNumber',''),
 CASE jsonb_typeof(p.body->'authors') WHEN 'array' THEN (SELECT string_agg(a,', ') FROM jsonb_array_elements_text(p.body->'authors') a) ELSE coalesce(p.body->>'authors','') END,
 coalesce(b.document_id,''),coalesce(nullif(b.body->>'title',''),b.body->>'name','')
 FROM boox_projection p LEFT JOIN boox_projection b ON p.body->>'modeType' IN ('1','2') AND b.domain='reading' AND b.document_id=p.native_uid||'#'||(p.body->>'documentId')
 WHERE p.document_id=ANY($1)`, ids)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	found := map[string]*readingMatch{}
	for rows.Next() {
		var id, mode string
		m := &readingMatch{}
		if err = rows.Scan(&id, &mode, &m.Page, &m.Authors, &m.BookID, &m.Book); err != nil {
			return out, err
		}
		m.Kind = map[string]string{"1": i18n.M("Highlight"), "2": i18n.M("Bookmark")}[mode]
		if m.Kind == "" {
			m.Kind, m.BookID = i18n.M("Book"), id
		}
		found[id] = m
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	for i := range ranking.Reading {
		r := &ranking.Reading[i]
		m := found[r.NotebookID]
		if m == nil {
			m = &readingMatch{Kind: "Book", BookID: r.NotebookID}
		}
		title := r.NotebookName
		if m.Kind != "Book" && m.Book != "" {
			title = m.Book
		}
		h := searchHit{URL: "/boox/reading?selected=" + url.QueryEscape(m.BookID), CreatedAt: r.CreatedAt, title: title, key: r.NoteKey, live: r, reading: m}
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

// searchBOOXNotebooks matches BOOX notebook titles containing every term.
func (d Deps) searchBOOXNotebooks(ctx context.Context, terms []string, l i18n.Localizer) (searchList, error) {
	out := searchList{}
	title := `coalesce(p.body->>'title','')`
	rows, err := d.DB.QueryContext(ctx, `SELECT p.document_id,coalesce(nullif(p.body->>'title',''),'Untitled'),coalesce(`+searchorder.NativeCreatedSQL("p")+`,0),
 coalesce(p.body->>'activeScene','')='1',jsonb_array_length(boox_search_page_ids(p.body->'pageNameList')),count(*) OVER()
 FROM boox_projection p WHERE `+booxvisibility.Notebook("p")+` AND `+termsSQL(title)+`
 ORDER BY `+titleRank(title)+`,p.document_id LIMIT $2`, terms, searchCap)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var h searchHit
		var typed bool
		var pages int
		if err = rows.Scan(&h.key, &h.title, &h.CreatedAt, &typed, &pages, &out.Total); err != nil {
			return out, err
		}
		h.Label, h.Icon, h.URL = i18n.M("Notebook"), "notebook", "/boox/notebook?id="+url.QueryEscape(h.key)
		h.Title = markTerms(h.title, terms)
		h.Meta = []string{"BOOX"}
		if typed {
			h.Meta = append(h.Meta, l.T("Text note"))
		}
		h.Meta = append(h.Meta, l.T("{{.Count}} pages", pages))
		out.Hits = append(out.Hits, h)
	}
	return out, rows.Err()
}

// searchClientNotebooks matches Client notebook names containing every term.
func (d Deps) searchClientNotebooks(ctx context.Context, terms []string, l i18n.Localizer) (searchList, error) {
	out := searchList{}
	title := `coalesce(n.name,'')`
	rows, err := d.DB.QueryContext(ctx, `SELECT n.id,coalesce(nullif(n.name,''),'Untitled'),CASE WHEN n.created_at>0 THEN n.created_at ELSE 0 END,
 (SELECT count(*) FROM fn_page p WHERE p.notebook_id=n.id AND p.deleted_at IS NULL),count(*) OVER()
 FROM fn_notebook n WHERE n.deleted_at IS NULL AND `+termsSQL(title)+`
 ORDER BY `+titleRank(title)+`,n.id LIMIT $2`, terms, searchCap)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var h searchHit
		var pages int
		if err = rows.Scan(&h.key, &h.title, &h.CreatedAt, &pages, &out.Total); err != nil {
			return out, err
		}
		h.Label, h.Icon, h.URL = i18n.M("Notebook"), "notebook", "/files/forestnote?notebook="+url.QueryEscape(h.key)
		h.Title = markTerms(h.title, terms)
		h.Meta = []string{"Client", l.T("{{.Count}} pages", pages)}
		out.Hits = append(out.Hits, h)
	}
	return out, rows.Err()
}

// searchClientBooks adds Client books whose title or author has every term,
// then Client book annotations, to parts.
func (d Deps) searchClientBooks(ctx context.Context, query string, terms []string, parts []searchList, l i18n.Localizer) ([]searchList, error) {
	all, err := books.NewLibraryService(d.DB, d.Objects).ListBooks(ctx)
	if err != nil {
		return nil, err
	}
	list := searchList{}
	for _, b := range all {
		text := strings.ToLower(b.Title + " " + b.Authors)
		if !slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(text, t) }) {
			list.Hits = append(list.Hits, searchHit{Label: i18n.M("Book"), Icon: "book", URL: "/files/forestnote/books?selected=" + url.QueryEscape(b.ID),
				Title: markTerms(b.Title, terms), Snippet: markTerms(b.Authors, terms), Meta: []string{"Client", b.Format},
				CreatedAt: b.AddedAt, title: b.Title, key: b.ID})
		}
	}
	// Exact titles, then titles that start with the query, then the rest
	// alphabetically.
	q := strings.Join(terms, " ")
	slices.SortStableFunc(list.Hits, func(a, b searchHit) int {
		rank := func(h searchHit) int {
			t := strings.ToLower(h.title)
			switch {
			case t == q:
				return 0
			case strings.HasPrefix(t, q):
				return 1
			}
			return 2
		}
		if c := cmp.Compare(rank(a), rank(b)); c != 0 {
			return c
		}
		return searchorder.Natural(a.title, b.title)
	})
	list.Total = len(list.Hits)
	list.Hits = list.Hits[:min(len(list.Hits), searchCap)]
	parts = append(parts, list)
	// Annotation search takes at most 16 words.
	if len(strings.Fields(query)) > searchTerms {
		return parts, nil
	}
	store := readersearch.New(d.DB)
	notes := searchList{}
	for offset := 0; offset < searchCap; offset += 100 {
		found, total, err := store.SearchPage(ctx, query, "", 100, offset, searchorder.Relevance)
		if err != nil {
			return nil, err
		}
		notes.Total = total
		for _, a := range found {
			title := a.Title
			if title == "" {
				title = l.T("Untitled book")
			}
			notes.Hits = append(notes.Hits, searchHit{Label: i18n.M("Annotation"), Icon: "highlighter", URL: "/files/forestnote/books?selected=" + url.QueryEscape(a.BookID),
				Title: markTerms(title, terms), Snippet: markTerms(a.Snippet, terms), Meta: []string{"Client"}, title: title, key: a.AnnotationID})
		}
		if offset+100 >= total {
			break
		}
	}
	return append(parts, notes), nil
}

// searchTasks matches tasks whose title or details have every term, titles
// first. A task belongs to BOOX or the Client when a note action or Client
// page made it.
func (d Deps) searchTasks(ctx context.Context, terms []string, scope string, l i18n.Localizer) (searchList, error) {
	out := searchList{}
	// Before the query: a request's connection can't run another while rows are open.
	loc, now := settings.Zone(ctx, d.DB), time.Now()
	text := `coalesce(t.title,'')||' '||coalesce(t.detail,'')`
	rows, err := d.DB.QueryContext(ctx, `SELECT t.task_id,coalesce(t.title,''),coalesce(t.detail,''),t.status IN ('completed','cancelled'),t.due_time,t.created_at,count(*) OVER()
 FROM alexandria_tasks t WHERE NOT t.deleted AND `+termsSQL(text)+`
 AND ($2='' OR $2='client' AND t.forestnote_notebook_id IS NOT NULL
  OR $2='boox' AND EXISTS(SELECT 1 FROM alexandria_note_action_item i WHERE i.kind='task' AND i.item_id=t.task_id))
 ORDER BY `+termsSQL(`coalesce(t.title,'')`)+` DESC,
  ts_rank(setweight(to_tsvector('simple',coalesce(t.title,'')),'A')||setweight(to_tsvector('simple',coalesce(t.detail,'')),'C'),plainto_tsquery('simple',array_to_string($1::text[],' '))) DESC,
  t.updated_at DESC,t.task_id LIMIT $3`, terms, scope, searchCap)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var h searchHit
		var detail string
		var done bool
		var due int64
		if err = rows.Scan(&h.key, &h.title, &detail, &done, &due, &h.CreatedAt, &out.Total); err != nil {
			return out, err
		}
		view := taskView{Sort: "due", Dir: "asc", ShowDone: done}
		h.Label, h.Icon, h.URL = i18n.M("Task"), "check-square", view.Editing(h.key)
		h.Title = markTerms(h.title, terms)
		h.Snippet = markTerms(around(detail, terms, 200), terms)
		if due > 0 {
			label, _ := dueLabel(l, time.UnixMilli(due), now, loc)
			h.Meta = append(h.Meta, label)
		} else {
			h.Meta = append(h.Meta, l.T("No due date"))
		}
		if done {
			h.Meta = append(h.Meta, l.T("Done"))
		}
		out.Hits = append(out.Hits, h)
	}
	return out, rows.Err()
}

// finishHits fills in the shown page and reading matches: their snippets,
// page numbers and tags come from one query each, for these rows only.
func (d Deps) finishHits(ctx context.Context, query string, hits []searchHit, l i18n.Localizer) error {
	var live []notes.Result
	for _, h := range hits {
		if h.live != nil {
			live = append(live, *h.live)
		}
	}
	if err := d.Search.Snippets(ctx, query, live); err != nil {
		return err
	}
	terms := strings.Fields(strings.ToLower(query))
	i := 0
	for n := range hits {
		h := &hits[n]
		if h.live == nil {
			continue
		}
		r := live[i]
		i++
		h.Snippet = parseMarks(r.Snippet)
		h.Tags = r.Tags
		source := "BOOX"
		if r.Source == "client" {
			source = "Client"
		}
		switch {
		case h.reading != nil:
			m := h.reading
			h.Label, h.Icon, h.Meta = m.Kind, map[string]string{"Book": "book", "Highlight": "highlighter", "Bookmark": "bookmark-simple"}[m.Kind], []string{source}
			title := h.title
			if m.Kind != "Book" && m.Page != "" {
				title = l.T("{{.Name}}, p. {{.Number}}", "Name", title, "Number", m.Page)
			}
			h.Title = markTerms(title, terms)
			if m.Kind == "Book" {
				h.Snippet = markTerms(m.Authors, terms)
			}
		case r.Kind == "Minutes":
			h.Label, h.Icon, h.Title, h.Meta = i18n.M("Minutes"), "microphone", markTerms(r.NotebookName, terms), []string{source, l.T("Audio Note Minutes")}
		default:
			title := r.NotebookName
			if r.PageNumber > 0 {
				title = l.T("{{.Name}}, p. {{.Number}}", "Name", title, "Number", r.PageNumber)
			}
			how := l.T("recognized")
			if !r.Lexical {
				how = l.T("found by meaning")
			}
			h.Label, h.Icon, h.Title, h.Meta = i18n.M("Page text"), "text-aa", markTerms(title, terms), []string{source, how}
		}
	}
	return nil
}

// parseMarks splits a snippet at the markers notes.Snippets placed. A
// marker without its partner ends or starts the highlight at the edge.
func parseMarks(s string) []markPart {
	var out []markPart
	hit := false
	for s != "" {
		next := notes.MarkStop
		if !hit {
			next = notes.MarkStart
		}
		n := strings.Index(s, next)
		if n < 0 {
			n = len(s)
		}
		if n > 0 {
			out = append(out, markPart{Text: strings.NewReplacer(notes.MarkStart, "", notes.MarkStop, "").Replace(s[:n]), Hit: hit})
		}
		s = s[min(len(s), n+len(next)):]
		hit = !hit
	}
	return out
}

// markTerms highlights every case-insensitive occurrence of the terms.
func markTerms(s string, terms []string) []markPart {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	words := make([][]rune, 0, len(terms))
	for _, t := range terms {
		if t != "" {
			words = append(words, []rune(t))
		}
	}
	// Longer terms first, so "garden" wins over "gar".
	slices.SortStableFunc(words, func(a, b []rune) int { return cmp.Compare(len(b), len(a)) })
	var out []markPart
	add := func(text string, hit bool) {
		if n := len(out); n > 0 && out[n-1].Hit == hit {
			out[n-1].Text += text
			return
		}
		out = append(out, markPart{Text: text, Hit: hit})
	}
	for i := 0; i < len(runes); {
		matched := 0
		for _, w := range words {
			if i+len(w) <= len(runes) && strings.EqualFold(string(runes[i:i+len(w)]), string(w)) {
				matched = len(w)
				break
			}
		}
		if matched > 0 {
			add(string(runes[i:i+matched]), true)
			i += matched
			continue
		}
		add(string(runes[i]), false)
		i++
	}
	return out
}

// around is at most limit characters of s, starting a little before the
// first term it contains; "" when it contains none.
func around(s string, terms []string, limit int) string {
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

// searchSorts are the sort menu's choices, in order.
var searchSorts = []struct{ Value, Label string }{
	{"relevance", i18n.M("Best match")}, {"title_asc", i18n.M("Title: A–Z")}, {"title_desc", i18n.M("Title: Z–A")},
	{"created_desc", i18n.M("Created: newest first")}, {"created_asc", i18n.M("Created: oldest first")},
}
