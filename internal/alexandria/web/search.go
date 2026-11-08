package web

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/searchview"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
)

const searchPageSize = 20

// searchTypes are the Search tabs, in order.
var searchTypes = searchview.Types

// markPart is a run of result text, highlighted when it matched the query.
// The template escapes both kinds, so stored text can't add markup.
type markPart struct {
	Text string
	Hit  bool
}

// searchHit is one result row, as shown.
type searchHit struct {
	Label, Icon, URL string
	Title, Snippet   []markPart
	Meta             []string
	Tags             []string
	CreatedAt        int64
}

func (h searchHit) CreatedDate() string { return searchorder.Date(h.CreatedAt) }

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
			search := searchview.Searcher{DB: d.DB, Objects: d.Objects, Search: d.Search}
			lists, err := search.All(r.Context(), searchview.Query{Text: s.Query, Words: s.Mode == "keyword", Source: s.Source,
				NativeBOOX: d.Status.NativeBOOX, UntitledBook: l.T("Untitled book")})
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
			var shown searchview.List
			capped := false
			if s.Type == "all" {
				shown = searchview.Fuse(lists["notebooks"], lists["pages"], lists["books"], lists["tasks"])
				shown.Total = len(shown.Hits)
				capped = shown.Total < total
			} else {
				shown = lists[s.Type]
				capped = len(shown.Hits) < shown.Total
				shown.Total = len(shown.Hits)
			}
			searchview.Sort(shown.Hits, s.Order)
			offset := searchorder.OffsetOf(values.Get("offset"), searchPageSize)
			if offset >= shown.Total {
				offset = 0
			}
			page := shown.Hits[offset:min(offset+searchPageSize, shown.Total)]
			if err = search.Finish(r.Context(), s.Query, page); err != nil {
				d.fail(w, err)
				return
			}
			data["Tabs"], data["Hits"], data["Total"], data["Capped"] = tabs, d.showHits(r.Context(), s.Query, page, l), total, capped
			data["Paging"] = searchorder.NavigationOf(s.values(), "offset", "results", offset, shown.Total, searchPageSize)
		}
		d.render(w, r, "search", "Search", "search", data)
	})
}

// searchTypeLabels name the tabs; the template translates them.
var searchTypeLabels = map[string]string{"all": i18n.M("All"), "notebooks": i18n.M("Notebooks"), "pages": i18n.M("Pages"), "books": i18n.M("Books & highlights"), "tasks": i18n.M("Tasks")}

// showHits turns a page of matches into rows: labels, icons, highlighted
// titles and snippets, and the facts under each.
func (d Deps) showHits(ctx context.Context, query string, hits []searchview.Hit, l i18n.Localizer) []searchHit {
	// Page and reading rows mark every word of the query; the others, the
	// words they were matched by.
	all, terms := strings.Fields(strings.ToLower(query)), searchview.Terms(query)
	var loc *time.Location
	now := time.Now()
	out := make([]searchHit, 0, len(hits))
	for _, h := range hits {
		row := searchHit{URL: h.URL, CreatedAt: h.CreatedAt, Tags: h.Tags}
		source := "BOOX"
		if h.Source == "client" {
			source = "Client"
		}
		switch {
		case h.Live != nil && h.Reading != nil:
			label := map[searchview.Kind]string{searchview.KindHighlight: i18n.M("Highlight"), searchview.KindBookmark: i18n.M("Bookmark")}[h.Kind]
			if label == "" {
				label = i18n.M("Book")
			}
			row.Label, row.Icon, row.Meta = label, map[string]string{"Book": "book", "Highlight": "highlighter", "Bookmark": "bookmark-simple"}[label], []string{source}
			title := h.Title
			if label != "Book" && h.Reading.Page != "" {
				title = l.T("{{.Name}}, p. {{.Number}}", "Name", title, "Number", h.Reading.Page)
			}
			row.Title, row.Snippet = markTerms(title, all), parseMarks(h.Live.Snippet)
			if label == "Book" {
				row.Snippet = markTerms(h.Reading.Authors, all)
			}
		case h.Kind == searchview.KindMinutes:
			row.Label, row.Icon, row.Title, row.Meta = i18n.M("Minutes"), "microphone", markTerms(h.Live.NotebookName, all), []string{source, l.T("Audio Note Minutes")}
			row.Snippet = parseMarks(h.Live.Snippet)
		case h.Live != nil:
			title := h.Live.NotebookName
			if h.Live.PageNumber > 0 {
				title = l.T("{{.Name}}, p. {{.Number}}", "Name", title, "Number", h.Live.PageNumber)
			}
			how := l.T("recognized")
			if !h.Live.Lexical {
				how = l.T("found by meaning")
			}
			row.Label, row.Icon, row.Title, row.Meta = i18n.M("Page text"), "text-aa", markTerms(title, all), []string{source, how}
			row.Snippet = parseMarks(h.Live.Snippet)
		case h.Kind == searchview.KindNotebook:
			row.Label, row.Icon, row.Title, row.Meta = i18n.M("Notebook"), "notebook", markTerms(h.Title, terms), []string{source}
			if h.Typed {
				row.Meta = append(row.Meta, l.T("Text note"))
			}
			row.Meta = append(row.Meta, l.T("{{.Count}} pages", h.Pages))
		case h.Kind == searchview.KindBook:
			row.Label, row.Icon, row.Title, row.Snippet, row.Meta = i18n.M("Book"), "book", markTerms(h.Title, terms), markTerms(h.Authors, terms), []string{"Client", h.Format}
		case h.Kind == searchview.KindAnnotation:
			row.Label, row.Icon, row.Title, row.Snippet, row.Meta = i18n.M("Annotation"), "highlighter", markTerms(h.Title, terms), markTerms(h.Snippet, terms), []string{"Client"}
		case h.Kind == searchview.KindTask:
			if loc == nil {
				loc = settings.Zone(ctx, d.DB)
			}
			view := taskView{Sort: "due", Dir: "asc", ShowDone: h.Done}
			row.Label, row.Icon, row.URL = i18n.M("Task"), "check-square", view.Editing(h.ID)
			row.Title, row.Snippet = markTerms(h.Title, terms), markTerms(h.Snippet, terms)
			if h.Due > 0 {
				label, _ := dueLabel(l, time.UnixMilli(h.Due), now, loc)
				row.Meta = append(row.Meta, label)
			} else {
				row.Meta = append(row.Meta, l.T("No due date"))
			}
			if h.Done {
				row.Meta = append(row.Meta, l.T("Done"))
			}
		}
		out = append(out, row)
	}
	return out
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

// searchSorts are the sort menu's choices, in order.
var searchSorts = []struct{ Value, Label string }{
	{"relevance", i18n.M("Best match")}, {"title_asc", i18n.M("Title: A–Z")}, {"title_desc", i18n.M("Title: Z–A")},
	{"created_desc", i18n.M("Created: newest first")}, {"created_asc", i18n.M("Created: oldest first")},
}
