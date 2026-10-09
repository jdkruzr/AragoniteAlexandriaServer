package books

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/jdkruzr/rhizome/server-go/assets"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/assetstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/reader"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/readersearch"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

// Ported from UltraBridge (alexandria-naming-and-books
// internal/service/library_test.go). The "bare notedb" case does not apply:
// migrations always create the tables.

const (
	libSite    = "0000000000000000000000000A"
	libBookA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	libBookB   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	libBookC   = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	libInline  = `{"version":1,"section":2,"start":10,"end":17,"quote":"passage","prefix":"","suffix":""}`
	libSticky  = `{"version":2,"presentation":"sticky","section":0,"start":3,"end":8,"quote":"hello","prefix":"","suffix":"","target":{"type":"text"}}`
	libDeleted = `{"version":1,"section":5,"start":0,"end":4,"quote":"gone","prefix":"","suffix":""}`
)

type libraryFixture struct {
	t       *testing.T
	db      *sql.DB
	objects blob.Store
	reader  reader.Store
	search  *readersearch.Store
	seq     int64
}

// newLibraryFixture builds a disposable PostgreSQL library and object prefix.
func newLibraryFixture(t *testing.T) *libraryFixture {
	t.Helper()
	lib := testenv.Database(t)
	if err := generation.Ensure(context.Background(), lib.DB); err != nil {
		t.Fatal(err)
	}
	return &libraryFixture{t: t, db: lib.DB, objects: testenv.Objects(t, lib.ID), reader: reader.Store{DB: lib.DB}, search: readersearch.New(lib.DB)}
}

// NewLibraryService is the fixture's constructor shape from UltraBridge.
func (f *libraryFixture) service() LibraryService { return NewLibraryService(f.db, f.objects) }

func (f *libraryFixture) put(table, id string, cols map[string]any) {
	f.t.Helper()
	f.seq++
	raw, err := json.Marshal(map[string]any{"table": table, "pk": id, "site_id": libSite, "op_seq": f.seq, "op_ts": 1_700_000_000_000 + f.seq, "cols": cols})
	if err != nil {
		f.t.Fatal(err)
	}
	p, err := reader.Prepare(libSite, [][]byte{raw})
	if err != nil {
		f.t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := f.db.Begin()
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err = tx.Exec(`SELECT last_seq FROM sync_seq WHERE id=1 FOR UPDATE`); err == nil {
		err = p.CommitTx(ctx, tx)
	}
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		f.t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err = f.reader.Drain(ctx, 0, 128); err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *libraryFixture) book(id, mediaType, metadata string) {
	f.put("reader_book", id, map[string]any{"asset_id": id, "byte_length": 2048, "media_type": mediaType, "metadata_json": metadata})
}

func (f *libraryFixture) annotation(book, id, anchor string, ink bool) {
	f.put("reader_annotation", id, map[string]any{"book_id": book, "initial_anchor_json": anchor, "canvas_width": 10000, "initial_height": 1000, "creator_session_id": "session-" + id})
	f.put("reader_edit_session", "session-"+id, map[string]any{"annotation_id": id, "kind": "interactive", "owner_site": libSite, "state": "open"})
	if !ink {
		return
	}
	points := make([]byte, 60)
	for i := 0; i < 3; i++ {
		binary.LittleEndian.PutUint32(points[i*20:], 1000+uint32(i)*2000)
		binary.LittleEndian.PutUint32(points[i*20+4:], 300+uint32(i)*200)
		binary.LittleEndian.PutUint32(points[i*20+8:], 800)
	}
	f.put("reader_stroke", "ink-"+id, map[string]any{"annotation_id": id, "session_id": "session-" + id, "paint_order": 1, "paint_site": libSite, "color": uint32(0xff000000), "pen_width_min": 40, "pen_width_max": 80, "brush_kind": "ballpoint", "brush_version": 1, "brush_seed": 7, "points": points, "point_dynamics": nil})
}

func (f *libraryFixture) recognition(id, text string) {
	f.t.Helper()
	p, err := f.reader.Projection(context.Background(), id, reader.DefaultLimits())
	if err != nil || p == nil || p.InputHash == nil {
		f.t.Fatalf("projection %s: %+v %v", id, p, err)
	}
	key, _ := contract.CompositeID(id, "client:"+libSite)
	f.put("reader_recognition", key, map[string]any{"annotation_id": id, "producer_id": "client:" + libSite, "input_hash": *p.InputHash, "engine": "mlkit", "model": "English", "language": "en", "status": "ready", "text": text})
}

// index hands every pending change to the search consumer and runs it dry,
// as the shared-library workers would in production.
func (f *libraryFixture) index() {
	f.t.Helper()
	ctx := context.Background()
	if _, err := f.search.Pump(ctx); err != nil {
		f.t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		more, err := f.search.Step(ctx, 8)
		if err != nil {
			f.t.Fatal(err)
		}
		if !more {
			return
		}
	}
	f.t.Fatal("search jobs did not finish")
}

func seedLibrary(t *testing.T) *libraryFixture {
	f := newLibraryFixture(t)
	f.book(libBookA, "application/epub+zip", `{"version":1,"title":"Metadata Title","creators":["Ursula K. Le Guin","Second Author"]}`)
	f.put("reader_book_title", libBookA, map[string]any{"title": "The Dispossessed"})
	f.book(libBookB, "application/pdf", `{"version":1,"title":"PDF Document","format":"PDF"}`)
	f.book(libBookC, "application/epub+zip", `{"version":1,"title":"Deleted Book"}`)
	f.put("reader_book_lifecycle", libBookC, map[string]any{"deleted": 1, "changed_at": 1})
	if _, err := f.db.Exec(`INSERT INTO rhizome_asset(asset_id,byte_length,chunk_bytes,state) VALUES($1,2048,262144,'ready')`, libBookA); err != nil {
		t.Fatal(err)
	}

	f.annotation(libBookA, "inline", libInline, true)
	f.recognition("inline", "anarres is free")
	f.annotation(libBookA, "deleted", libDeleted, true)
	f.put("reader_annotation_lifecycle", "deleted", map[string]any{"deleted": 1, "changed_at": 1})
	f.index()
	// Arrives after indexing: visible via projection status, not search text.
	f.annotation(libBookA, "sticky", libSticky, false)
	return f
}

func TestLibraryListBooks(t *testing.T) {
	f := seedLibrary(t)
	books, err := f.service().ListBooks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("books = %+v, want two live books", books)
	}
	// Sorted by title: "PDF Document" < "The Dispossessed".
	pdf, epub := books[0], books[1]
	if pdf.ID != libBookB || pdf.Format != "PDF" || pdf.FileState != "not uploaded" || pdf.AnnotationCount != 0 || pdf.Authors != "" {
		t.Errorf("pdf = %+v", pdf)
	}
	if epub.ID != libBookA || epub.Title != "The Dispossessed" || epub.Authors != "Ursula K. Le Guin, Second Author" ||
		epub.Format != "EPUB" || epub.ByteLength != 2048 || epub.FileState != "ready" || epub.AnnotationCount != 2 || epub.AddedAt <= 0 {
		t.Errorf("epub = %+v", epub)
	}
}

func TestLibraryGetBookAnnotations(t *testing.T) {
	f := seedLibrary(t)
	d, err := f.service().GetBook(context.Background(), libBookA)
	if err != nil {
		t.Fatal(err)
	}
	if d.Book.Title != "The Dispossessed" || d.Truncated {
		t.Fatalf("book = %+v truncated=%v", d.Book, d.Truncated)
	}
	if len(d.Annotations) != 2 {
		t.Fatalf("annotations = %+v, want sticky + inline (deleted hidden)", d.Annotations)
	}
	sticky, inline := d.Annotations[0], d.Annotations[1] // reading order: section 0 before section 2
	if sticky.ID != "sticky" || !sticky.Sticky || sticky.Target != "text" || sticky.Passage != "hello" ||
		sticky.Location != "Section 1" || sticky.InkStrokes != 0 || sticky.Status != "Waiting for the search index" || sticky.RecognizedText != "" {
		t.Errorf("sticky = %+v", sticky)
	}
	if inline.ID != "inline" || inline.Sticky || inline.Passage != "passage" || inline.Location != "Section 3" || !inline.Highlighted ||
		inline.RecognizedText != "anarres is free" || inline.TextSource != "Recognized (mlkit, English, en)" || inline.InkStrokes != 1 || inline.Status != "" {
		t.Errorf("inline = %+v", inline)
	}

	for _, id := range []string{libBookC, strings.Repeat("d", 64), "", strings.Repeat("e", 65)} {
		if _, err := f.service().GetBook(context.Background(), id); !errors.Is(err, ErrLibraryBookNotFound) {
			t.Errorf("GetBook(%q) err = %v, want ErrLibraryBookNotFound", id, err)
		}
	}
}

func TestLibraryPDFLocationAndCorrection(t *testing.T) {
	f := seedLibrary(t)
	f.annotation(libBookB, "pdfnote", libInline, true)
	f.recognition("pdfnote", "machine")
	p, _ := f.reader.Projection(context.Background(), "pdfnote", reader.DefaultLimits())
	key, _ := contract.CompositeID("pdfnote", "correction:"+libSite)
	f.put("reader_recognition", key, map[string]any{"annotation_id": "pdfnote", "producer_id": "correction:" + libSite, "input_hash": *p.InputHash, "engine": "manual-correction-v1", "model": nil, "language": nil, "status": "ready", "text": "human"})
	f.index()
	d, err := f.service().GetBook(context.Background(), libBookB)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Annotations) != 1 || d.Annotations[0].Location != "Page 3" || d.Annotations[0].RecognizedText != "human" || d.Annotations[0].TextSource != "Corrected by hand" {
		t.Fatalf("annotations = %+v", d.Annotations)
	}
}

func TestLibraryRenderAnnotationInk(t *testing.T) {
	f := seedLibrary(t)
	svc := f.service()
	b, err := svc.RenderAnnotationInk(context.Background(), "inline")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if w := img.Bounds().Dx(); w < 1 || w > 1200 {
		t.Errorf("ink width = %d, want bounded to 1200", w)
	}
	if _, err := svc.RenderAnnotationInk(context.Background(), "sticky"); !errors.Is(err, ErrLibraryNoInk) {
		t.Errorf("sticky ink err = %v, want ErrLibraryNoInk", err)
	}
	if _, err := svc.RenderAnnotationInk(context.Background(), "deleted"); !errors.Is(err, ErrLibraryNoInk) {
		t.Errorf("deleted ink err = %v, want ErrLibraryNoInk", err)
	}
	if _, err := svc.RenderAnnotationInk(context.Background(), "nope"); !errors.Is(err, ErrLibraryAnnotationNotFound) {
		t.Errorf("unknown ink err = %v, want ErrLibraryAnnotationNotFound", err)
	}
}

func TestLibraryReadOnlyAndEmptyLibrary(t *testing.T) {
	f := seedLibrary(t)
	count := func() (n int64) {
		f.db.QueryRow(`SELECT (SELECT count(*) FROM reader_store_incoming)+(SELECT count(*) FROM reader_store_changes)+(SELECT count(*) FROM sync_ops)+(SELECT count(*) FROM rhizome_asset)+(SELECT count(*) FROM rhizome_asset_chunk)+(SELECT COALESCE(SUM(generation),0) FROM rhizome_asset)`).Scan(&n)
		return n
	}
	before := count()
	svc := f.service()
	_, _ = svc.ListBooks(context.Background())
	_, _ = svc.GetBook(context.Background(), libBookA)
	_, _ = svc.RenderAnnotationInk(context.Background(), "inline")
	if file, err := svc.OpenBookFile(context.Background(), libBookA); err == nil {
		_, _ = io.ReadAll(file.Body)
		file.Body.Close()
	}
	if after := count(); after != before {
		t.Fatalf("read-only views wrote sync/journal rows: %d -> %d", before, after)
	}

}

func TestParseBookMetadata(t *testing.T) {
	for raw, want := range map[string]bookMetadata{
		`{"version":1,"title":"T","creators":["A","B"]}`: {title: "T", authors: []string{"A", "B"}},
		`{"version":1,"title":"T","creators":"Solo"}`:    {title: "T", authors: []string{"Solo"}},
		`{"version":2,"title":"Future"}`:                 {},
		`not json`:                                       {},
	} {
		got := parseBookMetadata(raw)
		if got.title != want.title || strings.Join(got.authors, "|") != strings.Join(want.authors, "|") {
			t.Errorf("parseBookMetadata(%s) = %+v, want %+v", raw, got, want)
		}
	}
}

// cancelledAnnotations adds one annotation whose creator session was cancelled
// with nothing else (hidden everywhere) and one whose creator was cancelled but
// a second live session added ink (still listed).
func (f *libraryFixture) cancelledAnnotations(book string) {
	f.put("reader_annotation", "cancelled", map[string]any{"book_id": book, "initial_anchor_json": libInline, "canvas_width": 10000, "initial_height": 1000, "creator_session_id": "session-cancelled"})
	f.put("reader_edit_session", "session-cancelled", map[string]any{"annotation_id": "cancelled", "kind": "interactive", "owner_site": libSite, "state": "cancelled"})

	f.put("reader_annotation", "revived", map[string]any{"book_id": book, "initial_anchor_json": libInline, "canvas_width": 10000, "initial_height": 1000, "creator_session_id": "session-revived"})
	f.put("reader_edit_session", "session-revived", map[string]any{"annotation_id": "revived", "kind": "interactive", "owner_site": libSite, "state": "cancelled"})
	f.put("reader_edit_session", "session-revived-2", map[string]any{"annotation_id": "revived", "kind": "interactive", "owner_site": libSite, "state": "finished"})
	points := make([]byte, 40)
	for i := 0; i < 2; i++ {
		binary.LittleEndian.PutUint32(points[i*20:], 1000+uint32(i)*500)
		binary.LittleEndian.PutUint32(points[i*20+4:], 400)
		binary.LittleEndian.PutUint32(points[i*20+8:], 600)
	}
	f.put("reader_stroke", "ink-revived", map[string]any{"annotation_id": "revived", "session_id": "session-revived-2", "paint_order": 1, "paint_site": libSite, "color": uint32(0xff000000), "pen_width_min": 40, "pen_width_max": 80, "brush_kind": "ballpoint", "brush_version": 1, "brush_seed": 7, "points": points, "point_dynamics": nil})
}

func TestLibraryCountMatchesDetailExcludingCancelled(t *testing.T) {
	f := seedLibrary(t)
	f.cancelledAnnotations(libBookA)
	svc := f.service()
	books, err := svc.ListBooks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range books {
		d, err := svc.GetBook(context.Background(), b.ID)
		if err != nil {
			t.Fatal(err)
		}
		if b.AnnotationCount != len(d.Annotations) {
			t.Errorf("%s: list count %d != detail annotations %d", b.Title, b.AnnotationCount, len(d.Annotations))
		}
		if b.ID != libBookA {
			continue
		}
		ids := map[string]bool{}
		for _, a := range d.Annotations {
			ids[a.ID] = true
		}
		if ids["cancelled"] || !ids["revived"] || !ids["inline"] || !ids["sticky"] || len(ids) != 3 {
			t.Errorf("book A annotations = %v, want inline, sticky and revived only", ids)
		}
	}
}

// uploadAsset stores content through the real asset store write path, as a
// device upload would, and returns its content-addressed ID.
func (f *libraryFixture) uploadAsset(content []byte) string {
	f.t.Helper()
	ctx := context.Background()
	id := assets.Digest(content)
	store := assetstore.Store{DB: f.db, Objects: f.objects}
	if _, _, err := store.Stage(ctx, assets.Descriptor{ID: id, ByteLength: int64(len(content)), ChunkBytes: assets.ChunkBytes}); err != nil {
		f.t.Fatal(err)
	}
	for i := 0; i*assets.ChunkBytes < len(content); i++ {
		chunk := content[i*assets.ChunkBytes : min(len(content), (i+1)*assets.ChunkBytes)]
		if err := store.WriteChunk(ctx, id, int64(i), chunk, assets.Digest(chunk)); err != nil {
			f.t.Fatal(err)
		}
	}
	if info, err := store.Complete(ctx, id); err != nil || info.State != "ready" {
		f.t.Fatalf("complete: %+v %v", info, err)
	}
	return id
}

func TestLibraryOpenBookFile(t *testing.T) {
	f := seedLibrary(t)
	content := bytes.Repeat([]byte("Le Guin/"), assets.ChunkBytes/4) // two chunks
	id := f.uploadAsset(content)
	f.put("reader_book", id, map[string]any{"asset_id": id, "byte_length": len(content), "media_type": "application/epub+zip", "metadata_json": `{"version":1,"title":"Lathe: of/Heaven?"}`})
	svc := f.service()

	file, err := svc.OpenBookFile(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file.Body)
	file.Body.Close()
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("read %d bytes, err %v; want %d identical bytes", len(got), err, len(content))
	}
	if file.Filename != "Lathe_ of_Heaven_.epub" || file.ContentType != "application/epub+zip" || file.Size != int64(len(content)) {
		t.Errorf("file = %+v", file)
	}

	// PDF book never uploaded; deleted book; unknown book.
	if _, err := svc.OpenBookFile(context.Background(), libBookB); !errors.Is(err, ErrLibraryFileUnavailable) {
		t.Errorf("unuploaded book err = %v", err)
	}
	for _, missing := range []string{libBookC, strings.Repeat("d", 64), ""} {
		if _, err := svc.OpenBookFile(context.Background(), missing); !errors.Is(err, ErrLibraryBookNotFound) {
			t.Errorf("OpenBookFile(%q) err = %v", missing, err)
		}
	}
	// Book A's asset row says ready but has no chunks: refused before any
	// byte is served (the object reader needs the full chunk list up front).
	if _, err := svc.OpenBookFile(context.Background(), libBookA); !errors.Is(err, ErrLibraryFileUnavailable) {
		t.Errorf("missing chunks opened: %v", err)
	}
}

func TestBookFilename(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"The Dispossessed", "application/epub+zip"}:  "The Dispossessed.epub",
		{"a/b\\c:d", "application/pdf"}:               "a_b_c_d.pdf",
		{"...", "application/x-mobipocket-ebook"}:     "book.mobi",
		{"line\nbreak", "application/epub+zip"}:       "line_break.epub",
		{strings.Repeat("é", 200), "application/pdf"}: strings.Repeat("é", 120) + ".pdf",
	} {
		if got := bookFilename(in[0], in[1]); got != want {
			t.Errorf("bookFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBookDatesIncludeTitleAndAnnotationChanges(t *testing.T) {
	f := newLibraryFixture(t)
	id := strings.Repeat("b", 64)
	f.book(id, "application/pdf", `{"version":1,"title":"Synthetic dates"}`)
	before, e := f.service().GetBook(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	f.put("reader_book_title", id, map[string]any{"title": "Renamed"})
	after, e := f.service().GetBook(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	if after.Book.AddedAt != before.Book.AddedAt || after.Book.ModifiedAt <= before.Book.ModifiedAt {
		t.Fatal("rename changed creation or failed to update modification")
	}
	f.annotation(id, "dated-annotation", libInline, false)
	final, e := f.service().GetBook(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	if final.Book.ModifiedAt <= after.Book.ModifiedAt {
		t.Fatal("annotation did not update book modification")
	}
}
