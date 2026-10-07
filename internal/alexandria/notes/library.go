package notes

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image/jpeg"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/render"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/fnpath"
)

// ErrNotFound reports a missing or deleted notebook, page or text box.
var ErrNotFound = errors.New("not found")

// Page is one live page of a notebook in display order (Number is 1-based).
type Page struct {
	ID       string
	Number   int
	Key      string
	BodyText string
	Model    string
}

// NotebookPages returns a live notebook's live pages, in display order, with
// their indexed text. The id tie-break keeps order stable on equal sort_order.
func NotebookPages(ctx context.Context, db pg.Querier, notebookID string) (name string, pages []Page, err error) {
	err = db.QueryRowContext(ctx, `SELECT COALESCE(name,'') FROM fn_notebook WHERE id=$1 AND deleted_at IS NULL`, notebookID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrNotFound
	}
	if err != nil {
		return "", nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT p.id, COALESCE(c.body_text,''), COALESCE(c.model,'') FROM fn_page p
		LEFT JOIN alexandria_note_content c ON c.note_key = 'forestnote://' || p.notebook_id || '/' || p.id AND c.page = 0
		WHERE p.notebook_id=$1 AND p.deleted_at IS NULL ORDER BY p.sort_order, p.id`, notebookID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Page
		if err := rows.Scan(&p.ID, &p.BodyText, &p.Model); err != nil {
			return "", nil, err
		}
		p.Number = len(pages) + 1
		p.Key = fnpath.Page(notebookID, p.ID)
		pages = append(pages, p)
	}
	return name, pages, rows.Err()
}

// TextBox is an editable text box on a live page.
type TextBox struct {
	ID, PageID, Text string
	Z                int64
}

// NotebookTextBoxes lists a notebook's live text boxes in page and paint order.
func NotebookTextBoxes(ctx context.Context, db pg.Querier, notebookID string) ([]TextBox, error) {
	rows, err := db.QueryContext(ctx, `SELECT t.id, t.page_id, COALESCE(t.text,''), COALESCE(t.z,0)
		FROM fn_text_box t JOIN fn_page p ON p.id = t.page_id
		WHERE p.notebook_id=$1 AND p.deleted_at IS NULL AND t.deleted_at IS NULL
		ORDER BY p.sort_order, p.id, t.z, t.id`, notebookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TextBox
	for rows.Next() {
		var b TextBox
		if err := rows.Scan(&b.ID, &b.PageID, &b.Text, &b.Z); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// EditTextBox re-authors one live text box with new text, every other column
// preserved, as a server op: it relays to devices, resolves by the same LWW
// rule as a device edit, and queues the page for re-indexing.
func EditTextBox(ctx context.Context, db pg.DB, boxID, text string) (pageID string, err error) {
	var fontName string
	var x, y, w, h, fontSize, color, weight, border, z, created sql.NullInt64
	var deleted sql.NullInt64
	err = db.QueryRowContext(ctx, `SELECT page_id, x, y, width, height, COALESCE(font_name,''), font_size, color, weight, border_width, z, created_at, deleted_at
		FROM fn_text_box WHERE id=$1`, boxID).Scan(&pageID, &x, &y, &w, &h, &fontName, &fontSize, &color, &weight, &border, &z, &created, &deleted)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && deleted.Valid) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	num := func(n sql.NullInt64) float64 { return float64(n.Int64) }
	op := relay.Op{Table: "text_box", PK: boxID, Cols: map[string]any{
		"page_id": pageID, "x": num(x), "y": num(y), "width": num(w), "height": num(h),
		"text": text, "font_name": fontName, "font_size": num(fontSize), "color": num(color),
		"weight": num(weight), "border_width": num(border), "z": num(z),
		"created_at": num(created), "deleted_at": nil,
	}}
	if _, err := (relay.Store{DB: db}).AuthorOps(ctx, []relay.Op{op}); err != nil {
		return "", fmt.Errorf("author text box edit: %w", err)
	}
	return pageID, nil
}

// PageImage renders a live page (ink and text boxes) as JPEG for viewing.
func PageImage(ctx context.Context, db pg.Querier, pageID string) ([]byte, error) {
	s, err := load(ctx, db, pageID)
	if err != nil {
		return nil, err
	}
	if !s.live {
		return nil, ErrNotFound
	}
	rows, err := db.QueryContext(ctx, `SELECT COALESCE(x,0), COALESCE(y,0), COALESCE(width,0), COALESCE(height,0), COALESCE(text,''), COALESCE(font_name,''),
		COALESCE(font_size,0), COALESCE(color,0), COALESCE(weight,400), COALESCE(border_width,0), COALESCE(z,0)
		FROM fn_text_box WHERE page_id=$1 AND deleted_at IS NULL ORDER BY z, id`, pageID)
	if err != nil {
		return nil, err
	}
	var boxes []render.TextBox
	for rows.Next() {
		var b render.TextBox
		if err := rows.Scan(&b.X, &b.Y, &b.Width, &b.Height, &b.Text, &b.FontName, &b.FontSize, &b.Color, &b.Weight, &b.BorderWidth, &b.Z); err != nil {
			rows.Close()
			return nil, err
		}
		boxes = append(boxes, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	img, err := render.RenderPageSized(s.strokes, boxes, s.width, s.height)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
