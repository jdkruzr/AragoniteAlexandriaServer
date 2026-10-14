package boox

import (
	"context"
	"fmt"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// CoveragePage explains why a page's preview is incomplete: notebook-level
// warnings, and each shape the renderer couldn't reproduce, by structure only.
type CoveragePage struct {
	Notebook, Title, PageID string
	Page                    int
	ActiveScene             int
	CanvasExpand            string
	IndexWarning, Error     string
	Warnings                []string
	ShapeTypes              map[string]int // rendered shapes by type
	Issues                  []booxpage.Issue
}

// BlockedCoverage reports every page whose recognition is blocked. It is
// read-only: it renders nothing to cache and changes no job.
func (s Service) BlockedCoverage(ctx context.Context) ([]CoveragePage, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT notebook_id,page_id FROM boox_page_index WHERE state='blocked' ORDER BY notebook_id,page_number LIMIT 500`)
	if err != nil {
		return nil, err
	}
	var pages [][2]string
	for rows.Next() {
		var p [2]string
		if err = rows.Scan(&p[0], &p[1]); err != nil {
			rows.Close()
			return nil, err
		}
		pages = append(pages, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	var out []CoveragePage
	for _, p := range pages {
		out = append(out, s.PageCoverage(ctx, p[0], p[1]))
	}
	return out, nil
}

func (s Service) PageCoverage(ctx context.Context, notebook, page string) CoveragePage {
	c := CoveragePage{Notebook: notebook, PageID: page, ShapeTypes: map[string]int{}}
	n, err := s.notebookSnapshot(ctx, notebook)
	if err != nil {
		c.Error = "notebook unavailable: " + err.Error()
		return c
	}
	c.Title, c.ActiveScene, c.IndexWarning, c.CanvasExpand = n.Meta.Title, n.Meta.ActiveScene, n.IndexWarning, n.Meta.NotePageInfo.CanvasExpandType
	for i, id := range booxpage.PageIDs(n.Meta.PageNameList) {
		if id == page {
			c.Page = i + 1
		}
	}
	p, warnings, err := booxpage.ShapePage(n.Meta, page, n.Keys, s.snapshotLoader(ctx, n))
	if err != nil {
		c.Error = err.Error()
		return c
	}
	c.Warnings, c.Issues = warnings, p.Issues
	for _, sh := range p.Shapes {
		c.ShapeTypes[fmt.Sprint(sh.ShapeType)]++
	}
	return c
}
