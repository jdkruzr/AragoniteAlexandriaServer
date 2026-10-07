package notes

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image/jpeg"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
)

// Notebook browsing for the web UI, following UltraBridge's ForestNote views
// (internal/syncstore/inventory.go, internal/service/note.go,
// internal/forestpdf), Apache-2.0.

// Crumb is one ancestor folder, root first.
type Crumb struct{ ID, Name string }

// Entry is a folder or a notebook in a folder listing. Times are ms.
type Entry struct {
	IsFolder              bool
	ID, Name              string
	PageCount             int
	CreatedAt, ModifiedAt int64
	Status                string // notebooks: blank, partial or indexed
}

// notebookModified is the newest change to the notebook, its live pages or
// their live ink (HLC op_ts values are milliseconds).
const notebookModified = `GREATEST(n.lww_wall_ts,
	COALESCE((SELECT MAX(p.lww_wall_ts) FROM fn_page p WHERE p.notebook_id = n.id AND p.deleted_at IS NULL), 0),
	COALESCE((SELECT MAX(s.lww_wall_ts) FROM fn_stroke s JOIN fn_page p2 ON p2.id = s.page_id
		WHERE p2.notebook_id = n.id AND p2.deleted_at IS NULL AND s.deleted_at IS NULL), 0))`

// Folder lists one folder ("" = root): its breadcrumb, then folders and
// notebooks, each group sorted by sortField (name, created, modified, pages).
func Folder(ctx context.Context, db pg.Querier, folderID, sortField, order string) ([]Crumb, []Entry, error) {
	crumbs, err := folderPath(ctx, db, folderID)
	if err != nil {
		return nil, nil, err
	}
	var folders, notebooks []Entry
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(name,''), COALESCE(created_at,0), lww_wall_ts FROM fn_folder
		WHERE deleted_at IS NULL AND COALESCE(parent_folder_id,'') = $1`, folderID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		e := Entry{IsFolder: true}
		if err := rows.Scan(&e.ID, &e.Name, &e.CreatedAt, &e.ModifiedAt); err != nil {
			rows.Close()
			return nil, nil, err
		}
		folders = append(folders, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = db.QueryContext(ctx, `SELECT n.id, COALESCE(n.name,''), COALESCE(n.created_at,0), `+notebookModified+`,
		(SELECT count(*) FROM fn_page p WHERE p.notebook_id = n.id AND p.deleted_at IS NULL),
		(SELECT count(*) FROM fn_page p JOIN alexandria_note_content c ON c.note_key = 'forestnote://' || p.notebook_id || '/' || p.id
			WHERE p.notebook_id = n.id AND p.deleted_at IS NULL AND btrim(c.body_text) <> '')
		FROM fn_notebook n WHERE n.deleted_at IS NULL AND COALESCE(n.folder_id,'') = $1`, folderID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var e Entry
		var indexed int
		if err := rows.Scan(&e.ID, &e.Name, &e.CreatedAt, &e.ModifiedAt, &e.PageCount, &indexed); err != nil {
			rows.Close()
			return nil, nil, err
		}
		switch {
		case e.PageCount == 0:
			e.Status = "blank"
		case indexed >= e.PageCount:
			e.Status = "indexed"
		default:
			e.Status = "partial"
		}
		notebooks = append(notebooks, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	sortEntries(folders, sortField, order)
	sortEntries(notebooks, sortField, order)
	return crumbs, append(folders, notebooks...), nil
}

func sortEntries(entries []Entry, field, order string) {
	less := func(i, j int) bool {
		switch field {
		case "created":
			return entries[i].CreatedAt < entries[j].CreatedAt
		case "modified":
			return entries[i].ModifiedAt < entries[j].ModifiedAt
		case "pages":
			return entries[i].PageCount < entries[j].PageCount
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if order == "desc" {
			return less(j, i)
		}
		return less(i, j)
	})
}

// folderPath returns root→folder; a deleted ancestor truncates it, and a
// depth guard stops a corrupt parent cycle.
func folderPath(ctx context.Context, db pg.Querier, folderID string) ([]Crumb, error) {
	var chain []Crumb
	seen := map[string]bool{}
	for id := folderID; id != "" && !seen[id] && len(chain) < 64; {
		seen[id] = true
		var c Crumb
		var parent string
		err := db.QueryRowContext(ctx, `SELECT id, COALESCE(name,''), COALESCE(parent_folder_id,'') FROM fn_folder WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&c.ID, &c.Name, &parent)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, err
		}
		chain = append(chain, c)
		id = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

// NotebookFolder returns a live notebook's folder breadcrumb.
func NotebookFolder(ctx context.Context, db pg.Querier, notebookID string) ([]Crumb, error) {
	var folder string
	err := db.QueryRowContext(ctx, `SELECT COALESCE(folder_id,'') FROM fn_notebook WHERE id=$1 AND deleted_at IS NULL`, notebookID).Scan(&folder)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return folderPath(ctx, db, folder)
}

// DeleteNotebook tombstones a notebook and its live pages as server ops. They
// reach devices on their next sync (a later device edit wins by LWW), and the
// page pipeline drops each page's derived search state.
func DeleteNotebook(ctx context.Context, db pg.DB, notebookID string) error {
	now := float64(time.Now().UnixMilli())
	var name, folder sql.NullString
	var sortOrder, created, aspect, width, height sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT name, sort_order, created_at, folder_id, aspect_long_axis, page_width, page_height
		FROM fn_notebook WHERE id=$1 AND deleted_at IS NULL`, notebookID).Scan(&name, &sortOrder, &created, &folder, &aspect, &width, &height)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	num := func(n sql.NullInt64) any { return float64(n.Int64) }
	nullNum := func(n sql.NullInt64) any {
		if n.Valid {
			return float64(n.Int64)
		}
		return nil
	}
	nullStr := func(s sql.NullString) any {
		if s.Valid {
			return s.String
		}
		return nil
	}
	ops := []relay.Op{{Table: "notebook", PK: notebookID, Cols: map[string]any{
		"name": name.String, "sort_order": num(sortOrder), "created_at": num(created), "deleted_at": now,
		"folder_id": nullStr(folder), "aspect_long_axis": nullNum(aspect), "page_width": nullNum(width), "page_height": nullNum(height)}}}
	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(notebook_id,''), sort_order, created_at, template, template_pitch_mm
		FROM fn_page WHERE notebook_id=$1 AND deleted_at IS NULL`, notebookID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, nb string
		var ps, pc, pitch sql.NullInt64
		var template sql.NullString
		if err := rows.Scan(&id, &nb, &ps, &pc, &template, &pitch); err != nil {
			rows.Close()
			return err
		}
		ops = append(ops, relay.Op{Table: "page", PK: id, Cols: map[string]any{
			"notebook_id": nb, "sort_order": num(ps), "created_at": num(pc), "deleted_at": now,
			"template": nullStr(template), "template_pitch_mm": nullNum(pitch)}})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = (relay.Store{DB: db}).AuthorOps(ctx, ops)
	return err
}

// Reprocess forgets a notebook's cached recognition and queues its live pages,
// so the next worker steps recognize them again.
func Reprocess(ctx context.Context, db pg.DB, notebookID string) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM alexandria_page_ocr WHERE page_id IN (SELECT id FROM fn_page WHERE notebook_id=$1)`, notebookID); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO alexandria_page_dirty(page_id) SELECT id FROM fn_page WHERE notebook_id=$1 AND deleted_at IS NULL
		ON CONFLICT (page_id) DO UPDATE SET dirtied_at=clock_timestamp(), attempts=0, next_at=now()`, notebookID)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), tx.Commit()
}

// ExportPDF renders a live notebook's pages and assembles them into one PDF,
// one page per image at 72 DPI (UltraBridge forestpdf.AssemblePDF).
func ExportPDF(ctx context.Context, db pg.Querier, notebookID string, w io.Writer) (name string, err error) {
	name, pages, err := NotebookPages(ctx, db, notebookID)
	if err != nil {
		return "", err
	}
	if len(pages) == 0 {
		return "", fmt.Errorf("notebook has no pages to export")
	}
	pdf := fpdf.New("P", "pt", "A4", "")
	pdf.SetAutoPageBreak(false, 0)
	for i, p := range pages {
		image, err := PageImage(ctx, db, p.ID)
		if err != nil {
			return "", fmt.Errorf("render page %d: %w", i+1, err)
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(image))
		if err != nil {
			return "", err
		}
		wPt, hPt := float64(cfg.Width), float64(cfg.Height)
		pdf.AddPageFormat("P", fpdf.SizeType{Wd: wPt, Ht: hPt})
		ref := "p" + strconv.Itoa(i)
		opt := fpdf.ImageOptions{ImageType: "JPEG"}
		pdf.RegisterImageOptionsReader(ref, opt, bytes.NewReader(image))
		pdf.ImageOptions(ref, 0, 0, wPt, hPt, false, opt, 0, "")
	}
	if err := pdf.Output(w); err != nil {
		return "", err
	}
	return name, nil
}
