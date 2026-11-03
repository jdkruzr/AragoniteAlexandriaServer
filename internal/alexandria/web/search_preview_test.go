package web

import (
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
)

// previewHit is a synthetic Search result, highlighted as the handler would.
type previewHit struct {
	label, icon, title, text string
	meta                     []string
	tags                     []string
}

// searchPreview is the Search page data for synthetic results, as the
// handler would build it. Previews and documentation screenshots share it.
func searchPreview(query, kind string, counts map[string]int, hits []previewHit, extra map[string]any) map[string]any {
	s := searchState{Query: query, Type: kind, Order: searchorder.Relevance}
	terms := strings.Fields(strings.ToLower(query))
	total := 0
	for _, t := range searchTypes[1:] {
		total += counts[t]
	}
	var tabs []searchTab
	for _, t := range searchTypes {
		count := total
		if t != "all" {
			count = counts[t]
		}
		tabs = append(tabs, searchTab{Type: t, Label: searchTypeLabels[t], URL: s.Link("type", t), Count: count, Current: t == kind})
	}
	var shown []searchHit
	for _, h := range hits {
		shown = append(shown, searchHit{Label: h.label, Icon: h.icon, URL: "#", Title: markTerms(h.title, terms), Snippet: markTerms(h.text, terms), Meta: h.meta, Tags: h.tags})
	}
	listed := counts[kind]
	if kind == "all" {
		listed = total
	}
	data := map[string]any{"State": s, "Semantic": true, "BOOX": true, "Sorts": searchSorts, "Tabs": tabs, "Hits": shown, "Total": total,
		"Paging": searchorder.NavigationOf(s.values(), "offset", "results", 0, listed, searchPageSize)}
	for k, v := range extra {
		data[k] = v
	}
	return data
}
