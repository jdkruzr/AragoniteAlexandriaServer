// Package books is the read-only Books view of an Alexandria library: books,
// their annotations with recognized or corrected text, annotation ink, and
// the original book file. Ported from UltraBridge (alexandria-naming-and-books
// internal/service/library.go) under Apache-2.0; PostgreSQL adaptation: the
// tables always exist (migrations) and book bytes come from object storage.
package books

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"image/png"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/jdkruzr/rhizome/server-go/assets"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/assetstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/reader"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/readersearch"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/render"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

// Read-only Alexandria library views (books and their annotations) over the
// shared-library reader mirror (fn_reader_* tables written by readerstore) and
// the derived annotation search documents (readersearch). Nothing here authors
// sync rows, touches clocks or schedules work; it only reads.

var (
	// ErrLibraryBookNotFound is returned for an unknown or deleted book.
	ErrLibraryBookNotFound = errors.New("library book not found")
	// ErrLibraryAnnotationNotFound is returned for an unknown annotation.
	ErrLibraryAnnotationNotFound = errors.New("library annotation not found")
	// ErrLibraryNoInk is returned when an annotation has no visible ink to render.
	ErrLibraryNoInk = errors.New("annotation has no visible ink")
	// ErrLibraryFileUnavailable is returned when a live book's bytes have not
	// (fully, verifiably) reached the asset store.
	ErrLibraryFileUnavailable = errors.New("book file not uploaded")
)

const (
	// maxLibraryAnnotations bounds one book page; the detail reports truncation.
	maxLibraryAnnotations = 500
	// maxLibraryProjections bounds per-request projections for annotations
	// that have no current search document (each loads that annotation's ink).
	maxLibraryProjections = 50
	// Ink thumbnails are bounded for display, not OCR.
	libraryInkMaxWidth  = 1200.0
	libraryInkMaxHeight = 4000.0
)

// LibraryBook is one synced Alexandria book. FileState describes whether the
// book's bytes have reached the server's asset store ("ready", "uploading",
// "invalid" or "not uploaded"); book metadata syncs independently of the bytes.
type LibraryBook struct {
	ID              string
	Title           string
	Authors         string
	Format          string // EPUB, MOBI or PDF
	MediaType       string
	ByteLength      int64
	FileState       string
	AnnotationCount int
	ModifiedAt      int64
	AddedAt         int64 // ms (HLC op_ts of the book row)
}

// LibraryAnnotation is one annotation in a book. Text fields are raw client
// data; the web layer escapes them.
type LibraryAnnotation struct {
	ID             string
	Sticky         bool   // a sticky note rather than an inline annotation
	Target         string // sticky target: text, image, section or page
	Section        int64  // 0-based spine section (PDF: page index)
	Start          int64
	Location       string // human label, e.g. "Section 3" or "Page 12"
	Passage        string // anchored book text (the selector quote)
	Highlighted    bool
	RecognizedText string
	TextSource     string // e.g. "Corrected by hand" or "Recognized (engine, model)"
	InkStrokes     int    // stored stroke rows (before erase); >0 means ink may render
	Status         string // empty when current; otherwise a short explanation
	CreatedAt      int64  // ms
}

// LibraryBookDetail is a book plus its annotations in reading order.
type LibraryBookDetail struct {
	Book        LibraryBook
	Annotations []LibraryAnnotation
	Truncated   bool
}

// LibraryService is the read-only Books surface.
type LibraryService interface {
	ListBooks(ctx context.Context) ([]LibraryBook, error)
	GetBook(ctx context.Context, bookID string) (LibraryBookDetail, error)
	// RenderAnnotationInk returns a PNG of the annotation's visible ink.
	RenderAnnotationInk(ctx context.Context, annotationID string) ([]byte, error)
	// OpenBookFile streams a live book's original bytes from the asset store.
	OpenBookFile(ctx context.Context, bookID string) (LibraryBookFile, error)
}

// LibraryBookFile is an open, read-only stream of a book's bytes. The caller
// must Close Body. Reads verify every chunk digest and, at EOF, the whole-file
// SHA-256 against the content-addressed book ID; a mismatch surfaces as a read
// error rather than silently serving corrupt bytes.
type LibraryBookFile struct {
	Body        io.ReadCloser
	Filename    string
	ContentType string
	Size        int64
}

type libraryService struct {
	db      pg.DB
	objects blob.Store
	reader  reader.Store
	search  *readersearch.Store
}

// NewLibraryService reads one admitted library.
func NewLibraryService(db pg.DB, objects blob.Store) LibraryService {
	return &libraryService{db: db, objects: objects, reader: reader.Store{DB: db}, search: readersearch.New(db)}
}

// libraryListed is the single membership rule for both the book list's count
// and the book page, so the two always agree. An annotation is listed unless it
// is deleted or cancelled. Cancelled mirrors the projection reducer: its creator
// session exists but is cancelled, and no other non-cancelled session of the
// annotation contributed ink, values or erase claims. A missing creator session
// (still syncing) is listed, as the page then shows it as waiting.
const libraryListed = `NOT EXISTS (SELECT 1 FROM fn_reader_annotation_lifecycle l WHERE l.id=a.id AND l.deleted=1)
	 AND NOT (
	   EXISTS (SELECT 1 FROM fn_reader_edit_session c WHERE c.id=a.creator_session_id AND c.annotation_id=a.id AND c.state='cancelled')
	   AND NOT EXISTS (SELECT 1 FROM fn_reader_edit_session o WHERE o.annotation_id=a.id AND o.id<>a.creator_session_id AND o.state<>'cancelled' AND (
	     EXISTS (SELECT 1 FROM fn_reader_stroke x WHERE x.session_id=o.id)
	     OR EXISTS (SELECT 1 FROM fn_reader_annotation_value x WHERE x.session_id=o.id)
	     OR EXISTS (SELECT 1 FROM fn_reader_erase_claim x WHERE x.session_id=o.id))))`

func (s *libraryService) books(ctx context.Context, bookID string) ([]LibraryBook, error) {
	assetState := `COALESCE((SELECT state FROM rhizome_asset WHERE asset_id=b.asset_id),'')`
	q := `SELECT b.id, b.media_type, b.byte_length, b.metadata_json, COALESCE(t.title,''), b.lww_op_ts,
 GREATEST(b.lww_op_ts,COALESCE(t.lww_op_ts,0),
 COALESCE((SELECT MAX(a.lww_op_ts) FROM fn_reader_annotation a WHERE a.book_id=b.id),0),
 COALESCE((SELECT MAX(v.lww_op_ts) FROM fn_reader_annotation_value v JOIN fn_reader_edit_session es ON es.id=v.session_id JOIN fn_reader_annotation a ON a.id=es.annotation_id WHERE a.book_id=b.id),0),
 COALESCE((SELECT MAX(v.lww_op_ts) FROM fn_reader_annotation_lifecycle v JOIN fn_reader_annotation a ON a.id=v.id WHERE a.book_id=b.id),0),
 COALESCE((SELECT MAX(v.lww_op_ts) FROM fn_reader_stroke v JOIN fn_reader_annotation a ON a.id=v.annotation_id WHERE a.book_id=b.id),0),
 COALESCE((SELECT MAX(v.lww_op_ts) FROM fn_reader_erase_claim v JOIN fn_reader_edit_session es ON es.id=v.session_id JOIN fn_reader_annotation a ON a.id=es.annotation_id WHERE a.book_id=b.id),0)),
	  (SELECT count(*) FROM fn_reader_annotation a WHERE a.book_id=b.id AND ` + libraryListed + `),
	  ` + assetState + `
	 FROM fn_reader_book b LEFT JOIN fn_reader_book_title t ON t.id=b.id
	 WHERE NOT EXISTS (SELECT 1 FROM fn_reader_book_lifecycle bl WHERE bl.id=b.id AND bl.deleted=1)`
	var args []any
	if bookID != "" {
		q += ` AND b.id=$1`
		args = append(args, bookID)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryBook
	for rows.Next() {
		var b LibraryBook
		var metadata, title, state string
		if err = rows.Scan(&b.ID, &b.MediaType, &b.ByteLength, &metadata, &title, &b.AddedAt, &b.ModifiedAt, &b.AnnotationCount, &state); err != nil {
			return nil, err
		}
		meta := parseBookMetadata(metadata)
		b.Title = strings.TrimSpace(title)
		if b.Title == "" {
			b.Title = meta.title
		}
		if b.Title == "" {
			b.Title = "Untitled book"
		}
		b.Authors = strings.Join(meta.authors, ", ")
		b.Format = bookFormat(b.MediaType)
		b.FileState = assetStateLabel(state)
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListBooks returns live (non-deleted) books sorted by title.
func (s *libraryService) ListBooks(ctx context.Context) ([]LibraryBook, error) {
	out, err := s.books(ctx, "")
	if err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Title), strings.ToLower(out[j].Title)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// GetBook returns one live book and its visible annotations in reading order.
func (s *libraryService) GetBook(ctx context.Context, bookID string) (LibraryBookDetail, error) {
	var d LibraryBookDetail
	if bookID == "" || len(bookID) > 64 {
		return d, ErrLibraryBookNotFound
	}
	found, err := s.books(ctx, bookID)
	if err != nil {
		return d, err
	}
	if len(found) == 0 {
		return d, ErrLibraryBookNotFound
	}
	d.Book = found[0]

	docs, err := s.search.BookDocuments(ctx, bookID)
	if err != nil {
		return d, err
	}

	type row struct {
		id, anchor string
		created    int64
		strokes    int
	}
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.initial_anchor_json, a.lww_op_ts,
	  (SELECT count(*) FROM fn_reader_stroke s WHERE s.annotation_id=a.id)
	 FROM fn_reader_annotation a WHERE a.book_id=$1 AND `+libraryListed+` ORDER BY a.lww_op_ts, a.id LIMIT $2`,
		bookID, maxLibraryAnnotations+1)
	if err != nil {
		return d, err
	}
	var list []row
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.id, &r.anchor, &r.created, &r.strokes); err != nil {
			rows.Close()
			return d, err
		}
		list = append(list, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return d, err
	}
	if len(list) > maxLibraryAnnotations {
		list, d.Truncated = list[:maxLibraryAnnotations], true
	}

	projected := 0
	for _, r := range list {
		a := LibraryAnnotation{ID: r.id, CreatedAt: r.created, InkStrokes: r.strokes}
		anchor := r.anchor
		if doc, ok := docs[r.id]; ok {
			anchor = doc.Anchor
			a.Highlighted = doc.Quote != ""
			a.RecognizedText = doc.RecognizedText
			a.TextSource = recognitionSource(doc.Alternatives)
		} else if projected < maxLibraryProjections {
			projected++
			p, err := s.reader.Projection(ctx, r.id, reader.DefaultLimits())
			switch {
			case errors.Is(err, reader.ErrBudget):
				a.Status = "Too large to inspect here"
			case err != nil:
				return d, err
			case p == nil:
				continue // vanished between queries
			default:
				// Membership is decided by libraryListed (shared with the list
				// count), so a projection disagreeing in an edge case is labelled
				// rather than silently dropped.
				switch p.Status {
				case contract.Deleted:
					a.Status = "Deleted"
				case contract.Cancelled:
					a.Status = "Cancelled"
				case contract.Ready:
					a.Status = "Waiting for the search index"
				case contract.Pending:
					a.Status = "Waiting for related rows to sync"
				case contract.Unsupported:
					a.Status = "Uses a format this server does not support yet"
				default:
					a.Status = "Invalid annotation data"
				}
				if p.Anchor != nil {
					anchor = *p.Anchor
				}
				a.Highlighted = p.HighlightPresent != nil && *p.HighlightPresent && p.Visible
			}
		} else {
			a.Status = "Not indexed yet"
		}
		applyAnchor(&a, anchor)
		unit := "Section"
		if d.Book.Format == "PDF" {
			unit = "Page"
		}
		a.Location = fmt.Sprintf("%s %d", unit, a.Section+1)
		d.Annotations = append(d.Annotations, a)
	}
	sort.SliceStable(d.Annotations, func(i, j int) bool {
		x, y := d.Annotations[i], d.Annotations[j]
		if x.Section != y.Section {
			return x.Section < y.Section
		}
		if x.Start != y.Start {
			return x.Start < y.Start
		}
		return x.CreatedAt < y.CreatedAt
	})
	return d, nil
}

// RenderAnnotationInk renders the annotation's surviving (non-erased) strokes
// on its own canvas, scaled down for display.
func (s *libraryService) RenderAnnotationInk(ctx context.Context, annotationID string) ([]byte, error) {
	if annotationID == "" || len(annotationID) > 2048 {
		return nil, ErrLibraryAnnotationNotFound
	}
	p, err := s.reader.Projection(ctx, annotationID, reader.DefaultLimits())
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, ErrLibraryAnnotationNotFound
	}
	if !p.Visible || len(p.Strokes) == 0 || p.EffectiveHeight == nil || *p.EffectiveHeight <= 0 || p.CanvasWidth <= 0 {
		return nil, ErrLibraryNoInk
	}
	strokes := make([]render.Stroke, 0, len(p.Strokes))
	for i, r := range p.Strokes {
		st := render.Stroke{Z: int64(i)}
		st.Color, _ = r.Columns["color"].(int64)
		st.PenWidthMin, _ = r.Columns["pen_width_min"].(int64)
		st.PenWidthMax, _ = r.Columns["pen_width_max"].(int64)
		st.Points, _ = r.Columns["points"].([]byte)
		st.BrushKind, _ = r.Columns["brush_kind"].(string)
		st.BrushVersion, _ = r.Columns["brush_version"].(int64)
		st.BrushSeed, _ = r.Columns["brush_seed"].(int64)
		st.PointDynamics, _ = r.Columns["point_dynamics"].([]byte)
		strokes = append(strokes, st)
	}
	width, height := p.CanvasWidth, *p.EffectiveHeight
	scale := math.Min(1, libraryInkMaxWidth/float64(width))
	if float64(height)*scale > libraryInkMaxHeight {
		scale = libraryInkMaxHeight / float64(height)
	}
	img, err := render.RenderRegionAtScale(strokes, width, height, scale)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode annotation ink: %w", err)
	}
	return buf.Bytes(), nil
}

type bookMetadata struct {
	title   string
	authors []string
}

// parseBookMetadata reads the client's versioned metadata_json (EPUB/MOBI
// "creators" may be an array or a single string). Unknown versions yield
// nothing rather than guessing.
func parseBookMetadata(raw string) bookMetadata {
	var m struct {
		Version  int             `json:"version"`
		Title    json.RawMessage `json:"title"`
		Creators json.RawMessage `json:"creators"`
	}
	var out bookMetadata
	if json.Unmarshal([]byte(raw), &m) != nil || m.Version != 1 {
		return out
	}
	strs := func(r json.RawMessage) []string {
		var many []string
		if json.Unmarshal(r, &many) == nil {
			return many
		}
		var one string
		if json.Unmarshal(r, &one) == nil && one != "" {
			return []string{one}
		}
		return nil
	}
	if t := strs(m.Title); len(t) > 0 {
		out.title = strings.TrimSpace(t[0])
	}
	for _, a := range strs(m.Creators) {
		if a = strings.TrimSpace(a); a != "" {
			out.authors = append(out.authors, a)
		}
	}
	return out
}

func bookFormat(mediaType string) string {
	switch mediaType {
	case "application/epub+zip":
		return "EPUB"
	case "application/x-mobipocket-ebook":
		return "MOBI"
	case "application/pdf":
		return "PDF"
	}
	return mediaType
}

func assetStateLabel(state string) string {
	switch state {
	case "ready":
		return "ready"
	case "staging", "verifying":
		return "uploading"
	case "invalid":
		return "invalid"
	}
	return "not uploaded"
}

// applyAnchor fills location, passage and sticky presentation from an anchor
// selector (v1 inline or v2 sticky). Malformed anchors leave fields empty.
func applyAnchor(a *LibraryAnnotation, raw string) {
	var anchor struct {
		Version      int    `json:"version"`
		Presentation string `json:"presentation"`
		Section      int64  `json:"section"`
		Start        int64  `json:"start"`
		Quote        string `json:"quote"`
		Target       struct {
			Type string `json:"type"`
		} `json:"target"`
	}
	if json.Unmarshal([]byte(raw), &anchor) != nil {
		return
	}
	a.Section, a.Start, a.Passage = anchor.Section, anchor.Start, anchor.Quote
	if anchor.Version == 2 && anchor.Presentation == "sticky" {
		a.Sticky, a.Target = true, anchor.Target.Type
	}
}

func recognitionSource(alts []readersearch.Alternative) string {
	if len(alts) == 0 {
		return ""
	}
	first := alts[0]
	if strings.HasPrefix(first.Producer, "correction:") {
		return "Corrected by hand"
	}
	parts := []string{first.Engine}
	for _, v := range []any{first.Model, first.Language} {
		if s, ok := v.(string); ok && s != "" {
			parts = append(parts, s)
		}
	}
	return "Recognized (" + strings.Join(parts, ", ") + ")"
}

// OpenBookFile returns a read-only stream of a live book's original bytes. Only
// a verified ("ready") asset is served; anything else is ErrLibraryFileUnavailable.
func (s *libraryService) OpenBookFile(ctx context.Context, bookID string) (LibraryBookFile, error) {
	var f LibraryBookFile
	if bookID == "" || len(bookID) > 64 {
		return f, ErrLibraryBookNotFound
	}
	found, err := s.books(ctx, bookID)
	if err != nil {
		return f, err
	}
	if len(found) == 0 {
		return f, ErrLibraryBookNotFound
	}
	b := found[0]
	if b.FileState != "ready" {
		return f, ErrLibraryFileUnavailable
	}
	var assetID string
	if err = s.db.QueryRowContext(ctx, `SELECT asset_id FROM fn_reader_book WHERE id=$1`, bookID).Scan(&assetID); err != nil {
		return f, err
	}
	info, digests, err := (assetstore.Store{DB: s.db, Objects: s.objects}).ReadyChunks(ctx, assetID)
	if err != nil || info.ByteLength != b.ByteLength {
		return f, ErrLibraryFileUnavailable
	}
	// Reads come from objects alone, so the stream outlives no DB handle.
	f.Body = &assetReader{source: assetstore.NewReader(ctx, s.objects, info, digests), id: assetID, info: info, hash: sha256.New()}
	f.Size = info.ByteLength
	f.ContentType = b.MediaType
	f.Filename = bookFilename(b.Title, b.MediaType)
	return f, nil
}

// assetReader streams the book through verified chunk objects and checks the
// whole-file SHA-256 at EOF; it never writes.
type assetReader struct {
	source *assetstore.Reader
	id     string
	info   assets.Info
	off    int64
	hash   hash.Hash
	err    error
}

func (r *assetReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	if r.off >= r.info.ByteLength {
		if hex.EncodeToString(r.hash.Sum(nil)) != r.id {
			r.err = fmt.Errorf("book file digest mismatch")
		} else {
			r.err = io.EOF
		}
		return 0, r.err
	}
	if rest := r.info.ByteLength - r.off; int64(len(p)) > rest {
		p = p[:rest]
	}
	n, err := r.source.ReadAt(p, r.off)
	r.hash.Write(p[:n])
	r.off += int64(n)
	if err != nil && err != io.EOF {
		r.err = err
		return n, err
	}
	return n, nil
}

func (r *assetReader) Close() error { r.err = io.ErrClosedPipe; return nil }

// bookFilename builds a safe download name from the display title plus the
// format's extension.
func bookFilename(title, mediaType string) string {
	ext := map[string]string{"application/epub+zip": ".epub", "application/x-mobipocket-ebook": ".mobi", "application/pdf": ".pdf"}[mediaType]
	var b strings.Builder
	for _, r := range title {
		switch {
		case r < 0x20 || r == 0x7f || strings.ContainsRune(`/\:*?"<>|`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name := strings.Trim(strings.TrimSpace(b.String()), ".")
	if name == "" {
		name = "book"
	}
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	return name + ext
}
