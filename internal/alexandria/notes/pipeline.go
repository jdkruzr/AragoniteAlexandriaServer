// Package notes runs synced notebook pages through render -> OCR ->
// page_text_from_server, note search and embeddings. Behavior follows
// UltraBridge's syncbridge (Apache-2.0): the OCR image omits text boxes,
// server text is OCR plus text-box text, and the search body adds the
// device's own recognized text.
//
// Differences, deliberately: the queue is durable (alexandria_page_dirty,
// written in the sync transaction), recognition is cached by the exact OCR
// input so an unchanged page is never recognized twice (an authoritative
// restore re-indexes without re-OCR), and the index, embeddings and authored
// page text commit together, only if the library generation is unchanged.
package notes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image/jpeg"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/embed"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/render"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/fnpath"
)

// DefaultPrompt matches UltraBridge's ForestNote OCR prompt.
const DefaultPrompt = "Transcribe all handwritten and printed text in this image. Output only the text."

// Source is the note_content source for synced notebook pages.
const Source = "forestnote"

// OCR recognizes one page image.
type OCR interface {
	Recognize(ctx context.Context, jpeg []byte, prompt string) (string, error)
	Model() string
}

// Pipeline processes dirty pages. Any collaborator may be nil: without OCR a
// page indexes its text boxes and device text only; without an embedder no
// vectors are written.
type Pipeline struct {
	OCR      OCR
	Prompt   string
	Embedder embed.Embedder
	// Debounce lets a burst of pen strokes settle before recognition.
	Debounce time.Duration
	// Lease bounds how long a crashed worker can hold a page.
	Lease  time.Duration
	Logger *slog.Logger
}

func (p Pipeline) logger() *slog.Logger {
	if p.Logger != nil {
		return p.Logger
	}
	return slog.Default()
}

// claim leases the oldest due page. ok=false means nothing is due.
func (p Pipeline) claim(ctx context.Context, db pg.DB) (page string, dirtied time.Time, generation string, attempts int, ok bool, err error) {
	lease := p.Lease
	if lease <= 0 {
		lease = 10 * time.Minute
	}
	err = db.QueryRowContext(ctx, `UPDATE alexandria_page_dirty d SET lease_until = now() + $1::interval
		WHERE d.page_id = (SELECT page_id FROM alexandria_page_dirty
			WHERE next_at <= now() AND dirtied_at <= clock_timestamp() - $2::interval
			AND (lease_until IS NULL OR lease_until < now())
			ORDER BY dirtied_at LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING d.page_id, d.dirtied_at, d.attempts`, interval(lease), interval(p.Debounce)).Scan(&page, &dirtied, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, "", 0, false, nil
	}
	if err != nil {
		return "", time.Time{}, "", 0, false, err
	}
	if err = db.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1`).Scan(&generation); err != nil {
		return "", time.Time{}, "", 0, false, err
	}
	return page, dirtied, generation, attempts, true, nil
}

func interval(d time.Duration) string { return fmt.Sprintf("%d milliseconds", d.Milliseconds()) }

// Step processes at most one due page. It reports whether it found one.
func (p Pipeline) Step(ctx context.Context, db pg.DB) (bool, error) {
	page, dirtied, generation, attempts, ok, err := p.claim(ctx, db)
	if err != nil || !ok {
		return false, err
	}
	if err := p.process(ctx, db, page, dirtied, generation); err != nil {
		// Back off (30 s doubling, at most an hour) and release the lease.
		delay := 30 * time.Second << min(attempts, 7)
		if delay > time.Hour {
			delay = time.Hour
		}
		if _, e := db.ExecContext(ctx, `UPDATE alexandria_page_dirty SET attempts=attempts+1, next_at=now()+$2::interval, lease_until=NULL
			WHERE page_id=$1 AND dirtied_at=$3`, page, interval(delay), dirtied); e != nil {
			return true, errors.Join(err, e)
		}
		p.logger().Warn("page pipeline deferred", "error", err.Error(), "attempts", attempts+1)
		return true, nil
	}
	return true, nil
}

type pageState struct {
	live       bool
	notebookID string
	width      int64
	height     int64
	strokes    []render.Stroke
	boxes      []string
	clientText string
	serverText sql.NullString // live server text, if any
	serverRow  bool           // a server text row exists (live or tombstoned)
}

func load(ctx context.Context, db pg.Querier, page string) (pageState, error) {
	var s pageState
	var pageDeleted, notebookDeleted sql.NullInt64
	var notebook sql.NullString
	var width, height sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT p.notebook_id, p.deleted_at, n.deleted_at, n.page_width, n.page_height
		FROM fn_page p LEFT JOIN fn_notebook n ON n.id = p.notebook_id WHERE p.id=$1`, page).
		Scan(&notebook, &pageDeleted, &notebookDeleted, &width, &height)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.notebookID = notebook.String
	s.live = notebook.Valid && !pageDeleted.Valid && !notebookDeleted.Valid
	// Legacy notebooks without exact geometry use UltraBridge's 3:4 default.
	s.width, s.height = 10000, 13333
	if width.Valid && height.Valid && width.Int64 > 0 && height.Int64 > 0 {
		s.width, s.height = width.Int64, height.Int64
	}
	if err := db.QueryRowContext(ctx, `SELECT text, deleted_at IS NULL FROM fn_page_text_from_server WHERE id=$1`, page).Scan(&s.serverText.String, &s.serverText.Valid); err == nil {
		s.serverRow = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return s, err
	}
	if !s.live {
		return s, nil
	}
	rows, err := db.QueryContext(ctx, `SELECT color, pen_width_min, pen_width_max, points, brush_kind, brush_version, brush_seed, point_dynamics, z
		FROM fn_stroke WHERE page_id=$1 AND deleted_at IS NULL ORDER BY z, id`, page)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var st render.Stroke
		var color, wmin, wmax, z sql.NullInt64
		if err := rows.Scan(&color, &wmin, &wmax, &st.Points, &st.BrushKind, &st.BrushVersion, &st.BrushSeed, &st.PointDynamics, &z); err != nil {
			rows.Close()
			return s, err
		}
		st.Color, st.PenWidthMin, st.PenWidthMax, st.Z = color.Int64, wmin.Int64, wmax.Int64, z.Int64
		s.strokes = append(s.strokes, st)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return s, err
	}
	rows, err = db.QueryContext(ctx, `SELECT COALESCE(text,'') FROM fn_text_box WHERE page_id=$1 AND deleted_at IS NULL ORDER BY z, y, x, id`, page)
	if err != nil {
		return s, err
	}
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			rows.Close()
			return s, err
		}
		if strings.TrimSpace(text) != "" {
			s.boxes = append(s.boxes, text)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return s, err
	}
	err = db.QueryRowContext(ctx, `SELECT COALESCE(text,'') FROM fn_page_text_from_client WHERE id=$1 AND deleted_at IS NULL`, page).Scan(&s.clientText)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return s, err
}

// inputHash identifies exactly what OCR sees: the page geometry and every
// stroke field the renderer reads, in paint order. Text boxes are not in the
// OCR image.
func inputHash(s pageState) string {
	h := sha256.New()
	num := func(v int64) { var b [8]byte; binary.BigEndian.PutUint64(b[:], uint64(v)); h.Write(b[:]) }
	bytesOf := func(b []byte) { num(int64(len(b))); h.Write(b) }
	h.Write([]byte("alexandria-page-ocr-v1"))
	num(s.width)
	num(s.height)
	num(int64(len(s.strokes)))
	for _, st := range s.strokes {
		num(st.Color)
		num(st.PenWidthMin)
		num(st.PenWidthMax)
		bytesOf(st.Points)
		bytesOf([]byte(st.BrushKind))
		num(st.BrushVersion)
		num(st.BrushSeed)
		bytesOf(st.PointDynamics)
		num(st.Z)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (p Pipeline) process(ctx context.Context, db pg.DB, page string, dirtied time.Time, generation string) error {
	s, err := load(ctx, db, page)
	if err != nil {
		return err
	}
	key := fnpath.Page(s.notebookID, page)
	if !s.live || (len(s.strokes) == 0 && len(s.boxes) == 0) {
		return finish(ctx, db, page, dirtied, generation, s, key, nil)
	}
	r := result{hash: inputHash(s)}
	var cached string
	err = db.QueryRowContext(ctx, `SELECT text, model FROM alexandria_page_ocr WHERE page_id=$1 AND input_hash=$2`, page, r.hash).Scan(&cached, &r.model)
	switch {
	case err == nil:
		r.ocr, r.cached = cached, true
	case !errors.Is(err, sql.ErrNoRows):
		return err
	case len(s.strokes) > 0 && p.OCR != nil:
		img, err := render.RenderPageSized(s.strokes, nil, s.width, s.height)
		if err != nil {
			return fmt.Errorf("render: %w", err)
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return fmt.Errorf("encode: %w", err)
		}
		prompt := p.Prompt
		if prompt == "" {
			prompt = DefaultPrompt
		}
		text, err := p.OCR.Recognize(ctx, buf.Bytes(), prompt)
		if err != nil {
			return fmt.Errorf("recognize: %w", err)
		}
		// PostgreSQL text cannot hold U+0000.
		r.ocr, r.model, r.recognized = strings.TrimSpace(strings.ReplaceAll(text, "\x00", "\uFFFD")), p.OCR.Model(), true
	}
	r.server = strings.TrimSpace(strings.Join(append([]string{r.ocr}, s.boxes...), "\n"))
	r.index = strings.TrimSpace(strings.Join([]string{r.server, s.clientText}, "\n"))
	if p.Embedder != nil && r.index != "" {
		// Vectors are computed before the transaction; a failure keeps the
		// previous vectors rather than failing the page.
		vectors, err := embedChunks(ctx, p.Embedder, r.index)
		if err != nil {
			p.logger().Warn("page embedding deferred", "error", err.Error())
		} else {
			r.vectors, r.embedModel = vectors, p.Embedder.Model()
		}
	}
	return finish(ctx, db, page, dirtied, generation, s, key, &r)
}

type result struct {
	hash, ocr, model   string
	cached, recognized bool
	server, index      string
	vectors            [][]float32
	embedModel         string
}

func embedChunks(ctx context.Context, e embed.Embedder, text string) ([][]float32, error) {
	var out [][]float32
	for _, chunk := range embed.ChunkText(text) {
		v, err := e.Embed(ctx, chunk)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// finish commits every derived effect of one page atomically, or nothing if a
// restore replaced the library meanwhile (the restore re-queued the page).
func finish(ctx context.Context, db pg.DB, page string, dirtied time.Time, generation string, s pageState, key string, r *result) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current string
	if err := tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&current); err != nil {
		return err
	}
	if current != generation {
		_, err := db.ExecContext(ctx, `UPDATE alexandria_page_dirty SET lease_until=NULL WHERE page_id=$1`, page)
		return err
	}
	// A page that moved notebooks leaves its old key behind.
	for _, q := range []string{`DELETE FROM alexandria_note_content WHERE note_key LIKE $1 AND note_key <> $2`,
		`DELETE FROM alexandria_embeddings WHERE note_key LIKE $1 AND note_key <> $2`} {
		if _, err := tx.ExecContext(ctx, q, fnpath.Scheme+"%/"+page, key); err != nil {
			return err
		}
	}
	var ops []relay.Op
	if r == nil {
		// Gone or empty: drop derived state and stop carrying stale text.
		for _, q := range []string{`DELETE FROM alexandria_note_content WHERE note_key=$1`, `DELETE FROM alexandria_embeddings WHERE note_key=$1`} {
			if _, err := tx.ExecContext(ctx, q, key); err != nil {
				return err
			}
		}
		if s.serverText.Valid {
			ops = append(ops, tombstone(page))
		}
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT INTO alexandria_note_content(note_key,page,body_text,source,model,indexed_at) VALUES($1,0,$2,$3,$4,now())
			ON CONFLICT (note_key,page) DO UPDATE SET body_text=EXCLUDED.body_text, source=EXCLUDED.source, model=EXCLUDED.model, indexed_at=now()`,
			key, r.index, Source, r.model); err != nil {
			return err
		}
		if r.index == "" {
			if _, err := tx.ExecContext(ctx, `DELETE FROM alexandria_embeddings WHERE note_key=$1`, key); err != nil {
				return err
			}
		} else if r.vectors != nil {
			if _, err := tx.ExecContext(ctx, `DELETE FROM alexandria_embeddings WHERE note_key=$1`, key); err != nil {
				return err
			}
			for i, v := range r.vectors {
				if _, err := tx.ExecContext(ctx, `INSERT INTO alexandria_embeddings(note_key,page,chunk,model,dimensions,embedding) VALUES($1,0,$2,$3,$4,$5::vector)`,
					key, i, r.embedModel, len(v), vectorLiteral(v)); err != nil {
					return err
				}
			}
		}
		if r.recognized {
			if _, err := tx.ExecContext(ctx, `INSERT INTO alexandria_page_ocr(page_id,input_hash,text,model) VALUES($1,$2,$3,$4)
				ON CONFLICT (page_id) DO UPDATE SET input_hash=EXCLUDED.input_hash, text=EXCLUDED.text, model=EXCLUDED.model, recognized_at=now()`,
				page, r.hash, r.ocr, r.model); err != nil {
				return err
			}
		}
		// Author only a real change, so reprocessing never churns the relay.
		switch {
		case r.server != "" && (!s.serverText.Valid || s.serverText.String != r.server):
			ops = append(ops, pageText(page, r.server, r.model))
		case r.server == "" && s.serverText.Valid:
			ops = append(ops, tombstone(page))
		}
	}
	if len(ops) > 0 {
		if _, err := relay.AuthorOpsTx(ctx, tx, ops); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM alexandria_page_dirty WHERE page_id=$1 AND dirtied_at=$2`, page, dirtied)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		// Edited meanwhile: keep the newer dirty mark, release our lease.
		if _, err := tx.ExecContext(ctx, `UPDATE alexandria_page_dirty SET lease_until=NULL WHERE page_id=$1`, page); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// pageText and tombstone build full-row page_text_from_server upserts, numbers
// as float64 like a decoded device op (UltraBridge syncstore/pagetext.go).
func pageText(page, text, model string) relay.Op {
	now := float64(time.Now().UnixMilli())
	var m any
	if model != "" {
		m = model
	}
	return relay.Op{Table: "page_text_from_server", PK: page, Cols: map[string]any{
		"text": text, "ocr_at": now, "model": m, "created_at": now, "deleted_at": nil}}
}

func tombstone(page string) relay.Op {
	now := float64(time.Now().UnixMilli())
	return relay.Op{Table: "page_text_from_server", PK: page, Cols: map[string]any{
		"text": "", "ocr_at": float64(0), "model": nil, "created_at": now, "deleted_at": now}}
}

// ReplaceDerived runs inside an authoritative restore, after the new rows are
// in place: drop derived notebook state and queue every page again. The OCR
// cache survives, so unchanged pages are re-indexed without recognition.
func ReplaceDerived(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{
		`DELETE FROM alexandria_note_content WHERE note_key LIKE 'forestnote://%'`,
		`DELETE FROM alexandria_embeddings WHERE note_key LIKE 'forestnote://%'`,
		`DELETE FROM alexandria_page_dirty`,
		`INSERT INTO alexandria_page_dirty(page_id) SELECT id FROM fn_page`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}

// vectorLiteral renders pgvector's text form, so no driver extension is needed.
func vectorLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(float64(f), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
