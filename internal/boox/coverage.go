package boox

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"image/png"

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
	Width, Height           float64    // page size
	InkBounds               [4]float64 // left, top, right, bottom of rendered shapes' boxes
	Background              *booxpage.Background
	BackgroundLive          bool   // the background file is held by the server
	BackgroundHead          string // first bytes of an unreadable template, hex
	Warnings                []string
	DrawWarnings            []string       // template, background and drawing
	ShapeTypes              map[string]int // rendered shapes by type
	Issues                  []booxpage.Issue
	PNG                     []byte `json:"-"` // the preview, when requested
}

// BlockedCoverage reports every page whose recognition is blocked or failed. It is
// read-only: it renders nothing to cache and changes no job.
func (s Service) BlockedCoverage(ctx context.Context, withPNG bool) ([]CoveragePage, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT notebook_id,page_id FROM boox_page_index WHERE state IN ('blocked','failed') ORDER BY notebook_id,page_number LIMIT 500`)
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
		out = append(out, s.PageCoverage(ctx, p[0], p[1], withPNG))
	}
	return out, nil
}

func (s Service) PageCoverage(ctx context.Context, notebook, page string, withPNG bool) CoveragePage {
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
	c.Warnings, c.Issues, c.Width, c.Height, c.Background = warnings, p.Issues, p.Width, p.Height, p.Background
	if p.Background != nil {
		_, c.BackgroundLive = n.Assets[p.Background.Path]
		// Templates are stock layouts, not page content.
		if c.BackgroundLive && p.Background.Kind == "geo_layout" {
			if raw, err := s.snapshotLoader(ctx, n)(p.Background.Path); err == nil && (len(raw) == 0 || raw[0] != '{') {
				c.BackgroundHead = hex.EncodeToString(raw[:min(len(raw), 64)])
			}
		}
	}
	img, tw, err := booxpage.Preview(p, s.snapshotLoader(ctx, n))
	if err != nil {
		c.Error = "drawing: " + err.Error()
	} else {
		c.DrawWarnings = tw
		if withPNG {
			var b bytes.Buffer
			if png.Encode(&b, img) == nil {
				c.PNG = b.Bytes()
			}
		}
	}
	first := true
	for _, sh := range p.Shapes {
		c.ShapeTypes[fmt.Sprint(sh.ShapeType)]++
		if r := sh.BoundingRect; r != nil {
			if first {
				c.InkBounds, first = [4]float64{r.Left, r.Top, r.Right, r.Bottom}, false
			}
			c.InkBounds = [4]float64{min(c.InkBounds[0], r.Left), min(c.InkBounds[1], r.Top), max(c.InkBounds[2], r.Right), max(c.InkBounds[3], r.Bottom)}
		}
	}
	return c
}
