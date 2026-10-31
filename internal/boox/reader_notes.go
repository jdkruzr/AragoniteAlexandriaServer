package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// NeoReader keeps a book's handwriting beside it under reader/<book>/:
// shape/<pageKey>#<rev>#<ms>.zip archives and point/<pageKey>#<rev>#points
// files, with the page keys listed in the book's modeType 7 record. The
// tablet won't embed that ink into books it syncs (its export refuses any
// file under .ksync), so the server draws it onto a copy instead.

// readerInk is a book's live pen strokes, plus what was left out.
type readerInk struct {
	Strokes []*booxpage.ReaderShape
	// SideNotes are strokes on the reader's side-note pages, which have no
	// place on the PDF page. Other counts erasers, text and shapes, which
	// v1 doesn't draw. Missing strokes had no points.
	SideNotes, Other, Missing int
}

// exportSlots keeps annotated exports from running side by side.
var exportSlots = make(chan struct{}, 1)

const exportWait = 60 * time.Second

func isPDFBook(b *libraryBook) bool {
	return b.ContentType == "application/pdf" || strings.EqualFold(path.Ext(bookFileName(b)), ".pdf")
}

// readerInkPages counts the pages of a book with handwriting archives that
// the reading record still lists.
func (s Service) readerInkPages(ctx context.Context, book string) (int, error) {
	pages, mapped, err := s.readerNotePages(ctx, book)
	if err != nil {
		return 0, err
	}
	prefix := s.uid() + "/reader/" + book + "/shape/"
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT split_part(substr(l.native_key,length($1)+1),'#',1) FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key >= $1 COLLATE "C" AND l.native_key < $2 COLLATE "C" AND v.bytes>0 AND NOT v.deleted`, prefix, directoryUpper(prefix))
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return 0, err
		}
		if _, listed := pages[key]; listed || !mapped {
			n++
		}
	}
	return n, rows.Err()
}

// readerNotePages is the book's page-key map: each key's first sub-page is
// the ink on the page itself, later ones are side notes. ok is false when
// no record has arrived.
func (s Service) readerNotePages(ctx context.Context, book string) (pages map[string][]string, ok bool, err error) {
	var raw sql.NullString
	err = s.DB.QueryRowContext(ctx, `SELECT body->>'readerNotePageNameMap' FROM boox_projection WHERE native_uid=$1 AND domain='reading' AND body->>'modeType'='7' AND body->>'documentId'=$2 AND coalesce(body->>'status','0')='0' AND NOT body ? 'commitType' ORDER BY (CASE WHEN body->>'updatedAt' ~ '^[0-9]{1,15}$' THEN (body->>'updatedAt')::bigint ELSE 0 END) DESC, document_id LIMIT 1`, s.uid(), book).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !raw.Valid {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var m struct {
		Data map[string][]string `json:"data"`
	}
	if json.Unmarshal([]byte(raw.String), &m) != nil || m.Data == nil {
		return nil, false, nil
	}
	return m.Data, true, nil
}

// readerInk loads a book's handwriting: the newest record of each stroke,
// live ones only, on the pages the reading record still lists.
func (s Service) readerInk(ctx context.Context, book string) (readerInk, error) {
	var out readerInk
	pages, mapped, err := s.readerNotePages(ctx, book)
	if err != nil {
		return out, err
	}
	snap := notebookSnapshot{Assets: map[string]assetBinding{}}
	prefix := s.uid() + "/reader/" + book + "/"
	rows, err := s.DB.QueryContext(ctx, `SELECT l.native_key,v.sha256,v.bytes FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key >= $1 COLLATE "C" AND l.native_key < $2 COLLATE "C" AND v.bytes>0 AND NOT v.deleted AND (starts_with(l.native_key,$3) OR starts_with(l.native_key,$4)) ORDER BY l.native_key LIMIT 20001`, prefix, directoryUpper(prefix), prefix+"shape/", prefix+"point/")
	if err != nil {
		return out, err
	}
	archives := map[string][]string{}
	for rows.Next() {
		var v assetBinding
		if err = rows.Scan(&v.Key, &v.SHA, &v.Bytes); err != nil {
			rows.Close()
			return out, err
		}
		v.Key = strings.TrimPrefix(v.Key, prefix)
		snap.Assets[v.Key] = v
		if name, ok := strings.CutPrefix(v.Key, "shape/"); ok && strings.HasSuffix(name, ".zip") {
			key, _, _ := strings.Cut(name, "#")
			archives[key] = append(archives[key], v.Key)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(snap.Assets) > 20000 {
		return out, errors.New("book exceeds the handwriting manifest limit")
	}
	load := s.snapshotLoader(ctx, snap)
	for key, names := range archives {
		subPages, listed := pages[key]
		if mapped && !listed {
			continue // a page the reader no longer lists
		}
		var raw [][]byte
		for _, name := range names {
			b, err := load(name)
			if err != nil {
				return out, err
			}
			raw = append(raw, b)
		}
		shapes, err := booxpage.ReaderShapes(raw)
		if err != nil {
			return out, err
		}
		points := map[string]map[string][]booxpage.TinyPoint{}
		for _, sh := range shapes {
			if sh.Status != 0 {
				continue
			}
			if len(subPages) > 0 && sh.SubPage != subPages[0] {
				out.SideNotes++
				continue
			}
			if !sh.Pen() {
				out.Other++
				continue
			}
			p, seen := points[sh.RevisionID]
			if !seen {
				if b, err := load("point/" + key + "#" + sh.RevisionID + "#points"); err == nil {
					p, _ = booxpage.ParsePointFile(b)
				}
				points[sh.RevisionID] = p
			}
			if sh.Points = p[sh.UniqueID]; len(sh.Points) == 0 {
				out.Missing++
				continue
			}
			if sh.PageKey == "" {
				sh.PageKey = key
			}
			out.Strokes = append(out.Strokes, sh)
		}
	}
	return out, nil
}

// storedBook reads a stored book's verified bytes.
func (s Service) storedBook(ctx context.Context, b *libraryBook) ([]byte, error) {
	body, _, e := s.Objects.Get(ctx, "boox/bodies/"+b.SHA)
	if e != nil {
		return nil, e
	}
	defer body.Close()
	raw, e := io.ReadAll(io.LimitReader(body, maxBook+1))
	if e != nil || int64(len(raw)) != b.Size || hash(raw) != b.SHA {
		return nil, errors.New("book integrity check failed")
	}
	return raw, nil
}

func attachment(w http.ResponseWriter, name string) {
	ascii := strings.Map(func(c rune) rune {
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' {
			return '_'
		}
		return c
	}, name)
	w.Header().Set("Content-Disposition", `attachment; filename="`+ascii+`"; filename*=UTF-8''`+url.PathEscape(name))
}

// bookAnnotated serves a stored PDF with its handwriting drawn on.
func (s Service) bookAnnotated(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if e := s.ResolveIdentity(ctx); e != nil {
		s.failure(w, "bookAnnotated", e)
		return
	}
	v, e := s.librarySnapshot(ctx, s.DB)
	if e != nil {
		s.failure(w, "bookAnnotated", e)
		return
	}
	book := r.URL.Query().Get("book")
	b := v.Books[book]
	if b == nil || b.SHA == "" || !isPDFBook(b) {
		http.NotFound(w, r)
		return
	}
	wait, cancel := context.WithTimeout(ctx, exportWait)
	defer cancel()
	select {
	case exportSlots <- struct{}{}:
		defer func() { <-exportSlots }()
	case <-wait.Done():
		w.Header().Set("Retry-After", "10")
		http.Error(w, "Another annotated download is still being made. Try again in a moment.", http.StatusServiceUnavailable)
		return
	}
	ink, e := s.readerInk(ctx, b.ID)
	if e != nil {
		slog.Warn("annotated book unavailable", "book", b.ID, "error", e)
		http.Error(w, "The handwriting for this book is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	if len(ink.Strokes) == 0 {
		http.Error(w, "This book has no handwriting on its pages.", http.StatusNotFound)
		return
	}
	raw, e := s.storedBook(ctx, b)
	if e != nil {
		http.Error(w, "The book is temporarily unavailable.", http.StatusServiceUnavailable)
		return
	}
	out, note, e := annotatePDF(raw, ink.Strokes)
	if e != nil {
		slog.Warn("annotated book failed", "book", b.ID, "error", e)
		http.Error(w, "Alexandria couldn't draw on this PDF. The original is still available with Download.", http.StatusUnprocessableEntity)
		return
	}
	if note.Pages == 0 {
		http.Error(w, "This handwriting was made on a different copy of the PDF, so it has no pages to go on.", http.StatusConflict)
		return
	}
	slog.Info("annotated book", "book", b.ID, "pages", note.Pages, "strokes", note.Strokes, "unplaced", note.Unplaced, "side_notes", ink.SideNotes, "other", ink.Other, "missing", ink.Missing)
	name := bookFileName(b)
	attachment(w, strings.TrimSuffix(name, path.Ext(name))+" (annotated).pdf")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write(out)
}
