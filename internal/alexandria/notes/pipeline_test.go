package notes

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"hash/fnv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	ocrapi "github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/ocr"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

var ctx = context.Background()

const (
	nb   = "00000000000000000000000NB1"
	page = "00000000000000000000000PG1"
	ink  = "00000000000000000000000ST1"
	box  = "00000000000000000000000TB1"
)

type fakeOCR struct {
	calls atomic.Int32
	text  string
	err   error
	hook  func()
}

func (f *fakeOCR) Recognize(context.Context, []byte, string) (string, error) {
	f.calls.Add(1)
	if f.hook != nil {
		f.hook()
	}
	return f.text, f.err
}
func (f *fakeOCR) Model() string { return "fake-vision" }

// fakeEmbedder hashes words into 16 buckets: similar words, similar vectors.
type fakeEmbedder struct{}

func (fakeEmbedder) Model() string { return "fake-embed" }
func (fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, 16)
	for _, w := range strings.Fields(strings.ToLower(text)) {
		h := fnv.New32a()
		h.Write([]byte(w))
		v[h.Sum32()%16]++
	}
	v[0] += 0.001 // never the zero vector
	return v, nil
}

func setup(t *testing.T) *sql.DB {
	t.Helper()
	db := testenv.Database(t).DB
	if err := identity.EnsureSite(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := generation.Ensure(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db
}

func points(xs ...int32) string {
	b := make([]byte, 20*len(xs))
	for i, x := range xs {
		binary.LittleEndian.PutUint32(b[i*20:], uint32(x))
		binary.LittleEndian.PutUint32(b[i*20+4:], uint32(x))
		binary.LittleEndian.PutUint32(b[i*20+8:], 500)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func author(t *testing.T, db *sql.DB, ops ...relay.Op) {
	t.Helper()
	if _, err := (relay.Store{DB: db}).AuthorOps(ctx, ops); err != nil {
		t.Fatal(err)
	}
}

func notebookOp(name string, deleted any) relay.Op {
	return relay.Op{Table: "notebook", PK: nb, Cols: map[string]any{"name": name, "sort_order": float64(0), "created_at": float64(1),
		"deleted_at": deleted, "folder_id": nil, "aspect_long_axis": nil, "page_width": float64(10000), "page_height": float64(15000)}}
}
func pageOp(deleted any) relay.Op {
	return relay.Op{Table: "page", PK: page, Cols: map[string]any{"notebook_id": nb, "sort_order": float64(0), "created_at": float64(1),
		"deleted_at": deleted, "template": nil, "template_pitch_mm": nil}}
}
func strokeOp(pts string) relay.Op {
	return relay.Op{Table: "stroke", PK: ink, Cols: map[string]any{"page_id": page, "color": float64(4278190080), "pen_width_min": float64(10),
		"pen_width_max": float64(20), "points": pts, "z": float64(0), "created_at": float64(1), "deleted_at": nil}}
}
func boxOp(text string) relay.Op {
	return relay.Op{Table: "text_box", PK: box, Cols: map[string]any{"page_id": page, "x": float64(100), "y": float64(100), "width": float64(3000),
		"height": float64(800), "text": text, "font_name": "", "font_size": float64(300), "color": float64(4278190080), "weight": float64(400),
		"border_width": float64(0), "z": float64(1), "created_at": float64(1), "deleted_at": nil}}
}

func scalar(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	var v sql.NullString
	if err := db.QueryRow(q, args...).Scan(&v); err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatal(err)
	}
	return v.String
}

func step(t *testing.T, p Pipeline, db *sql.DB) bool {
	t.Helper()
	found, err := p.Step(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func TestPipelineRecognizesIndexesAndAuthorsOnce(t *testing.T) {
	db := setup(t)
	ocr := &fakeOCR{text: "  marmalade recipe  "}
	p := Pipeline{OCR: ocr}
	author(t, db, notebookOp("Kitchen", nil), pageOp(nil), strokeOp(points(100, 900, 1800)), boxOp("buy oranges"))
	if scalar(t, db, `SELECT count(*) FROM alexandria_page_dirty`) != "1" {
		t.Fatal("authored stroke did not queue its page")
	}
	if !step(t, p, db) || step(t, p, db) {
		t.Fatal("expected exactly one due page")
	}
	if ocr.calls.Load() != 1 {
		t.Fatal(ocr.calls.Load())
	}
	want := "marmalade recipe\nbuy oranges"
	if got := scalar(t, db, `SELECT body_text FROM alexandria_note_content WHERE note_key=$1`, "forestnote://"+nb+"/"+page); got != want {
		t.Fatalf("index %q", got)
	}
	if got := scalar(t, db, `SELECT text FROM fn_page_text_from_server WHERE id=$1 AND deleted_at IS NULL`, page); got != want {
		t.Fatalf("page text %q", got)
	}
	if scalar(t, db, `SELECT model FROM fn_page_text_from_server`) != "fake-vision" {
		t.Fatal("model not recorded")
	}
	ops := scalar(t, db, `SELECT count(*) FROM sync_ops WHERE table_name='page_text_from_server'`)
	// Reprocessing an unchanged page neither recognizes nor re-authors.
	exec(t, db, `INSERT INTO alexandria_page_dirty(page_id) VALUES($1)`, page)
	step(t, p, db)
	if ocr.calls.Load() != 1 || scalar(t, db, `SELECT count(*) FROM sync_ops WHERE table_name='page_text_from_server'`) != ops {
		t.Fatal("unchanged page churned OCR or relay")
	}
	// A text-box edit re-authors without re-recognizing; ink edits recognize.
	author(t, db, boxOp("buy lemons"))
	step(t, p, db)
	if ocr.calls.Load() != 1 || !strings.HasSuffix(scalar(t, db, `SELECT text FROM fn_page_text_from_server`), "buy lemons") {
		t.Fatal("box edit")
	}
	author(t, db, strokeOp(points(100, 900, 1800, 2500)))
	step(t, p, db)
	if ocr.calls.Load() != 2 {
		t.Fatal("ink edit not recognized")
	}
	// Deleting the page drops derived state and tombstones the text.
	author(t, db, pageOp(float64(5)))
	step(t, p, db)
	if scalar(t, db, `SELECT count(*) FROM alexandria_note_content`) != "0" || scalar(t, db, `SELECT count(*) FROM fn_page_text_from_server WHERE deleted_at IS NOT NULL`) != "1" {
		t.Fatal("deleted page left derived state")
	}
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func TestOCRFailureBacksOffAndKeepsThePage(t *testing.T) {
	db := setup(t)
	p := Pipeline{OCR: &fakeOCR{err: errors.New("gateway 503")}}
	author(t, db, notebookOp("N", nil), pageOp(nil), strokeOp(points(1, 2, 3)))
	if !step(t, p, db) {
		t.Fatal("no page")
	}
	if scalar(t, db, `SELECT attempts FROM alexandria_page_dirty`) != "1" || scalar(t, db, `SELECT next_at > now() AND lease_until IS NULL FROM alexandria_page_dirty`) != "true" {
		t.Fatal("failure not deferred")
	}
	if step(t, p, db) {
		t.Fatal("deferred page retried immediately")
	}
	if scalar(t, db, `SELECT count(*) FROM alexandria_note_content`) != "0" {
		t.Fatal("failed page indexed")
	}
}

func TestEditDuringRecognitionIsProcessedAgain(t *testing.T) {
	db := setup(t)
	ocr := &fakeOCR{text: "first"}
	ocr.hook = func() {
		ocr.hook = nil
		exec(t, db, `UPDATE alexandria_page_dirty SET dirtied_at=clock_timestamp()`)
	}
	p := Pipeline{OCR: ocr}
	author(t, db, notebookOp("N", nil), pageOp(nil), strokeOp(points(1, 2, 3)))
	step(t, p, db)
	if scalar(t, db, `SELECT count(*) FROM alexandria_page_dirty WHERE lease_until IS NULL`) != "1" {
		t.Fatal("newer dirty mark lost")
	}
	step(t, p, db)
	if scalar(t, db, `SELECT count(*) FROM alexandria_page_dirty`) != "0" {
		t.Fatal("page not finished")
	}
}

func TestRestoreBetweenClaimAndFinishWritesNothing(t *testing.T) {
	db := setup(t)
	ocr := &fakeOCR{text: "stale"}
	ocr.hook = func() { exec(t, db, `UPDATE sync_library_generation SET generation=$1`, strings.Repeat("d", 64)) }
	author(t, db, notebookOp("N", nil), pageOp(nil), strokeOp(points(1, 2, 3)))
	step(t, Pipeline{OCR: ocr}, db)
	if scalar(t, db, `SELECT count(*) FROM alexandria_note_content`) != "0" || scalar(t, db, `SELECT count(*) FROM fn_page_text_from_server`) != "0" {
		t.Fatal("stale generation wrote derived state")
	}
	if scalar(t, db, `SELECT count(*) FROM alexandria_page_dirty WHERE lease_until IS NULL`) != "1" {
		t.Fatal("lease not released")
	}
}

func TestRestoreReindexesFromCacheWithoutRecognition(t *testing.T) {
	db := setup(t)
	ocr := &fakeOCR{text: "cached words"}
	p := Pipeline{OCR: ocr}
	author(t, db, notebookOp("N", nil), pageOp(nil), strokeOp(points(1, 2, 3)))
	step(t, p, db)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceDerived(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if scalar(t, db, `SELECT count(*) FROM alexandria_note_content`) != "0" {
		t.Fatal("derived state survived restore")
	}
	step(t, p, db)
	if ocr.calls.Load() != 1 || scalar(t, db, `SELECT body_text FROM alexandria_note_content`) != "cached words" {
		t.Fatal("restore re-recognized or lost text")
	}
}

func TestDeviceExchangeQueuesItsPages(t *testing.T) {
	db := setup(t)
	// Device-pushed ops reach the queue through the same MarkDirty seam.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := relay.MarkDirty(ctx, tx, []relay.TablePK{{Table: "page", PK: page}, {Table: "page", PK: page}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if scalar(t, db, `SELECT count(*) FROM alexandria_page_dirty`) != "1" {
		t.Fatal("duplicate pages not coalesced")
	}
}

func TestHybridSearchFusesLexicalAndVectorHits(t *testing.T) {
	db := setup(t)
	p := Pipeline{OCR: &fakeOCR{text: "Orange marmalade needs bitter Seville oranges and patience"}, Embedder: fakeEmbedder{}}
	author(t, db, notebookOp("Kitchen", nil), pageOp(nil), strokeOp(points(1, 2, 3)))
	step(t, p, db)
	if scalar(t, db, `SELECT count(*) FROM alexandria_embeddings WHERE model='fake-embed' AND dimensions=16`) != "1" {
		t.Fatal("no embedding")
	}
	s := Searcher{DB: db, Embedder: fakeEmbedder{}}
	results, err := s.Search(ctx, "seville oranges", 10, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Lexical || results[0].NotebookName != "Kitchen" || results[0].PageNumber != 1 || !strings.Contains(results[0].Snippet, "Seville") {
		t.Fatalf("results %+v", results)
	}
	// A vector-only hit still finds the page when no words match.
	// "zebra" appears nowhere, so the AND/phrase query matches nothing lexically.
	results, err = s.Search(ctx, "bitter zebra", 10, true)
	if err != nil || len(results) != 1 || results[0].Lexical {
		t.Fatal(results, err)
	}
	if results, err = s.Search(ctx, "zebra crossing", 10, false); err != nil || len(results) != 0 {
		t.Fatal("keyword mode returned a vector hit", results, err)
	}
	// A deleted notebook's pages never surface.
	author(t, db, notebookOp("Kitchen", float64(9)))
	if results, err = s.Search(ctx, "seville", 10, true); err != nil || len(results) != 0 {
		t.Fatal("deleted notebook surfaced", results, err)
	}
}

func TestCancelledRecognitionCannotPublishOrReviveOnEdit(t *testing.T) {
	db := setup(t)
	author(t, db, notebookOp("N", nil), pageOp(nil), strokeOp(points(1, 2, 3)))
	id := scalar(t, db, `SELECT job_id::text FROM alexandria_page_dirty`)
	o := &fakeOCR{text: "late result", hook: func() {
		if _, e := (recognition.Store{DB: db}).Control(ctx, id, false, "cancel"); e != nil {
			t.Fatal(e)
		}
	}}
	step(t, Pipeline{OCR: o}, db)
	if scalar(t, db, `SELECT count(*) FROM alexandria_note_content`) != "0" || scalar(t, db, `SELECT count(*) FROM fn_page_text_from_server`) != "0" {
		t.Fatal("cancelled result published")
	}
	author(t, db, strokeOp(points(4, 5, 6)))
	if step(t, Pipeline{OCR: o}, db) {
		t.Fatal("source edit revived cancelled work")
	}
	if scalar(t, db, `SELECT state FROM alexandria_recognition_job WHERE id=$1`, id) != "cancelled" {
		t.Fatal("cancellation lost")
	}
}

func TestPermanentOCRFailureStopsRetrying(t *testing.T) {
	db := setup(t)
	author(t, db, notebookOp("N", nil), pageOp(nil), strokeOp(points(1, 2, 3)))
	o := &fakeOCR{err: &ocrapi.HTTPError{Status: 401}}
	step(t, Pipeline{OCR: o}, db)
	if scalar(t, db, `SELECT state FROM alexandria_page_dirty`) != "failed" {
		t.Fatal("permanent error was retried")
	}
	exec(t, db, `UPDATE alexandria_page_dirty SET next_at=now()`)
	if step(t, Pipeline{OCR: o}, db) {
		t.Fatal("failed job claimed")
	}
}
