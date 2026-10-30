package boox

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"
)

// BOOX tags are the firmware's TagModel rows (migration 0030, view boox_tag):
// one row per tag per page, made with the lasso's "Tag" action. Alexandria
// reads them; it never writes tags.

// noteTag reports whether a NOTE_TREE record is a tag (modeType 1, no type).
func noteTag(b map[string]any, uid string) bool {
	content, ok := b["content"].(string)
	id, _ := b["_id"].(string)
	_, typed := b["type"]
	return ok && len(content) <= 1024 && !typed && fmt.Sprint(b["modeType"]) == "1" && b["dbId"] == uid+"-NOTE_TREE" && b["user"] == uid && id != "" && b["uniqueId"] == id && b["commitType"] == nil
}

// notebookTags returns a live notebook's tags by page ID, and all of them.
func (s Service) notebookTags(ctx context.Context, id string) (map[string][]string, []string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT page_id,content FROM boox_tag WHERE notebook_id=$1 ORDER BY content`, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	byPage, seen := map[string][]string{}, map[string]bool{}
	var all []string
	for rows.Next() {
		var page, content string
		if err := rows.Scan(&page, &content); err != nil {
			return nil, nil, err
		}
		byPage[page] = append(byPage[page], content)
		if !seen[content] {
			seen[content] = true
			all = append(all, content)
		}
	}
	return byPage, all, rows.Err()
}

// libraryTags returns the most-used tags across live notebooks, then tags
// that aren't on any page (made in the tag manager), up to limit.
func (s Service) libraryTags(ctx context.Context, limit int) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.content FROM boox_tag t LEFT JOIN boox_search_notebooks n ON n.document_id=t.notebook_id
	 GROUP BY t.content ORDER BY count(DISTINCT (t.notebook_id,t.page_id)) FILTER (WHERE n.document_id IS NOT NULL) DESC,lower(t.content) LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type tagSummary struct {
	Content          string
	Pages, Notebooks int
}

type taggedPage struct {
	NotebookID, Title, PageID, Snippet string
	Number                             int
}

type tagsView struct {
	// Tag is the selected tag, as it is written on the pages.
	Tag   string
	Tags  []tagSummary
	Pages []taggedPage
	// Summary counts the selected tag's pages and notebooks.
	Summary *tagSummary
}

var tagsTemplate = template.Must(template.New("tags.html").Funcs(viewFuncs()).ParseFS(templateFS, "templates/tags.html"))

// tagsPage lists every tag, with the selected one's pages beside the list
// (?selected=, or ?tag= from older links).
func (s Service) tagsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := tagsView{Tag: strings.TrimSpace(q.Get("selected"))}
	if d.Tag == "" {
		d.Tag = strings.TrimSpace(q.Get("tag"))
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT t.content,count(DISTINCT (t.notebook_id,t.page_id)) FILTER (WHERE n.document_id IS NOT NULL),count(DISTINCT t.notebook_id) FILTER (WHERE n.document_id IS NOT NULL)
	 FROM boox_tag t LEFT JOIN boox_search_notebooks n ON n.document_id=t.notebook_id GROUP BY t.content ORDER BY 2 DESC,lower(t.content)`)
	if err != nil {
		s.failure(w, "tagsPage", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var t tagSummary
		if err := rows.Scan(&t.Content, &t.Pages, &t.Notebooks); err != nil {
			s.failure(w, "tagsPage", err)
			return
		}
		d.Tags = append(d.Tags, t)
	}
	if err := rows.Err(); err != nil {
		s.failure(w, "tagsPage", err)
		return
	}
	rows.Close()
	if d.Tag != "" {
		// Tags match without regard to case, like the device's tag search.
		for i, t := range d.Tags {
			if strings.EqualFold(t.Content, d.Tag) {
				d.Tag, d.Summary = t.Content, &d.Tags[i]
			}
		}
		if d.Pages, err = s.taggedPages(r.Context(), d.Tag); err != nil {
			s.failure(w, "tagsPage", err)
			return
		}
	}
	s.renderPage(w, r, tagsTemplate, "BOOX Tags", d)
}

// taggedPages lists the live pages carrying a tag, by notebook then page.
func (s Service) taggedPages(ctx context.Context, tag string) ([]taggedPage, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT n.document_id,coalesce(n.body->>'title','Untitled'),t.page_id,ids.ord,coalesce(p.text,'')
	 FROM boox_tag t JOIN boox_search_notebooks n ON n.document_id=t.notebook_id
	 CROSS JOIN LATERAL (SELECT min(o)::integer AS ord FROM jsonb_array_elements_text(boox_search_page_ids(n.body->'pageNameList')) WITH ORDINALITY l(id,o) WHERE l.id=t.page_id) ids
	 LEFT JOIN boox_page_index p ON p.notebook_id=t.notebook_id AND p.page_id=t.page_id
	 WHERE lower(t.content)=lower($1) AND ids.ord IS NOT NULL`, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []taggedPage
	for rows.Next() {
		var p taggedPage
		var text string
		if err := rows.Scan(&p.NotebookID, &p.Title, &p.PageID, &p.Number, &text); err != nil {
			return nil, err
		}
		p.Snippet = snippet(text, 200)
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Title != b.Title {
			return strings.ToLower(a.Title) < strings.ToLower(b.Title)
		}
		return a.Number < b.Number
	})
	return out, rows.Err()
}

func snippet(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "…"
}
