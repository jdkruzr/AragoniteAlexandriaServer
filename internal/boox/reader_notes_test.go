package boox

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"net/url"
	"strings"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
	"google.golang.org/protobuf/encoding/protowire"
)

// inkablePDF is a two-page landscape PDF and its pages' NeoReader names.
func inkablePDF(t *testing.T) ([]byte, []string) {
	t.Helper()
	f := fpdf.New("L", "pt", "Letter", "")
	f.SetFont("Helvetica", "", 14)
	for _, text := range []string{"Synthetic page one", "Synthetic page two"} {
		f.AddPage()
		f.Text(72, 72, text)
	}
	var b bytes.Buffer
	if e := f.Output(&b); e != nil {
		t.Fatal(e)
	}
	ctx, e := api.ReadContext(bytes.NewReader(b.Bytes()), model.NewDefaultConfiguration())
	if e != nil {
		t.Fatal(e)
	}
	must(t, ctx.EnsurePageCount())
	var refs []string
	for i := 1; i <= ctx.PageCount; i++ {
		_, ref, _, e := ctx.PageDict(i, false)
		must(t, e)
		refs = append(refs, booxpage.ReaderPDFReference(ref.ObjectNumber.Value(), ref.GenerationNumber.Value()))
	}
	return b.Bytes(), refs
}

type testStroke struct {
	ID, SubPage, Revision, Reference string
	Type, Status                     int32
	Color                            uint32
	Points                           [][2]float32
}

// readerArchive writes strokes the way NeoReader does: a zip holding one
// ShapeInfoProtoList in the reader's field numbering.
func readerArchive(t *testing.T, name string, strokes []testStroke) []byte {
	t.Helper()
	var list []byte
	for _, s := range strokes {
		var m []byte
		str := func(n protowire.Number, v string) {
			m = protowire.AppendTag(m, n, protowire.BytesType)
			m = protowire.AppendString(m, v)
		}
		num := func(n protowire.Number, v uint64) {
			m = protowire.AppendTag(m, n, protowire.VarintType)
			m = protowire.AppendVarint(m, v)
		}
		str(1, s.ID)
		num(3, 1760000000000)
		num(4, 1760000000000)
		str(5, s.SubPage)
		num(9, uint64(int64(int32(s.Color))))
		m = protowire.AppendTag(m, 12, protowire.Fixed32Type)
		m = protowire.AppendFixed32(m, math.Float32bits(2))
		num(15, uint64(s.Type))
		str(25, s.Revision)
		if s.Status != 0 {
			num(27, uint64(s.Status))
		}
		str(33, `{"pageReferenceId":"`+s.Reference+`"}`)
		list = protowire.AppendTag(list, 1, protowire.BytesType)
		list = protowire.AppendBytes(list, m)
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, e := z.Create(name)
	must(t, e)
	_, e = w.Write(list)
	must(t, e)
	must(t, z.Close())
	return b.Bytes()
}

// readerPoints writes a V1 point file: header, one block per stroke, the
// xref, and the xref's offset.
func readerPoints(key string, strokes []testStroke) []byte {
	out := make([]byte, 76)
	copy(out[4:], key)
	type entry struct {
		id          string
		offset, len int
	}
	var xref []entry
	for _, s := range strokes {
		at := len(out)
		out = append(out, 0, 0, 0, 0)
		for _, p := range s.Points {
			out = binary.BigEndian.AppendUint32(out, math.Float32bits(p[0]))
			out = binary.BigEndian.AppendUint32(out, math.Float32bits(p[1]))
			out = binary.BigEndian.AppendUint16(out, 0)
			out = binary.BigEndian.AppendUint16(out, 2048)
			out = binary.BigEndian.AppendUint32(out, 0)
		}
		xref = append(xref, entry{s.ID, at, len(out) - at})
	}
	xrefAt := len(out)
	for _, x := range xref {
		id := make([]byte, 36)
		copy(id, x.id)
		out = append(out, id...)
		out = binary.BigEndian.AppendUint32(out, uint32(x.offset))
		out = binary.BigEndian.AppendUint32(out, uint32(x.len))
	}
	return binary.BigEndian.AppendUint32(out, uint32(xrefAt))
}

// inkedPages reports which pages of a PDF paint Alexandria's ink form.
func inkedPages(t *testing.T, raw []byte) []bool {
	t.Helper()
	ctx, e := api.ReadContext(bytes.NewReader(raw), model.NewDefaultConfiguration())
	must(t, e)
	must(t, ctx.EnsurePageCount())
	var out []bool
	for i := 1; i <= ctx.PageCount; i++ {
		d, _, _, e := ctx.PageDict(i, false)
		must(t, e)
		inked := false
		if o, ok := d.Find("Resources"); ok {
			res, e := ctx.DereferenceDict(o)
			must(t, e)
			if o, ok := res.Find("XObject"); ok {
				xo, e := ctx.DereferenceDict(o)
				must(t, e)
				_, inked = xo.Find("AlxInk")
			}
		}
		out = append(out, inked)
	}
	return out
}

func TestAnnotatePDF(t *testing.T) {
	raw, refs := inkablePDF(t)
	line := []booxpage.TinyPoint{{X: 100, Y: 100, Pressure: 2048}, {X: 300, Y: 120, Pressure: 4000}, {X: 500, Y: 140, Pressure: 4000}}
	strokes := []*booxpage.ReaderShape{
		{UniqueID: "a", ShapeType: 5, Color: -16777216, Thickness: 2, PageReference: refs[1], Points: line},
		{UniqueID: "b", ShapeType: 15, Color: int32(-256), Thickness: 20, PageKey: booxpage.ReaderPageKey(refs[1]), Points: line},
		{UniqueID: "c", ShapeType: 5, Thickness: 2, PageReference: "pdf-9999-0", Points: line},
	}
	out, note, e := annotatePDF(raw, strokes)
	if e != nil {
		t.Fatal(e)
	}
	if note.Pages != 1 || note.Strokes != 2 || note.Unplaced != 1 {
		t.Fatalf("%+v", note)
	}
	if got := inkedPages(t, out); len(got) != 2 || got[0] || !got[1] {
		t.Fatal("inked pages", got)
	}
	// The page's own content is kept, wrapped, ahead of the ink.
	ctx, e := api.ReadContext(bytes.NewReader(out), model.NewDefaultConfiguration())
	must(t, e)
	must(t, ctx.EnsurePageCount())
	d, _, _, e := ctx.PageDict(2, false)
	must(t, e)
	contents, _ := d.Find("Contents")
	if a, ok := contents.(types.Array); !ok || len(a) != 3 {
		t.Fatal("contents", contents)
	}
	if _, _, e := annotatePDF([]byte("not a pdf"), strokes); e == nil {
		t.Fatal("garbage annotated")
	}
}

func TestBookAnnotatedDownload(t *testing.T) {
	s, g := libraryFixture(t)
	ctx := context.Background()
	pdf, refs := inkablePDF(t)
	libraryRecord(s, g, 4, "b1", map[string]any{"name": "Synthetic.pdf", "title": "Synthetic", "type": "pdf", "extraAttributes": "{}"})
	libraryStoredBook(t, s, g, "e1", "Plain.epub")
	bind := func(key string, body []byte, ct string) {
		t.Helper()
		tx, e := s.DB.BeginTx(ctx, nil)
		must(t, e)
		defer tx.Rollback()
		a, e := s.stagePublicationAsset(ctx, body, ct)
		must(t, e)
		must(t, s.bindPublicationAsset(ctx, tx, s.uid()+"/reader/b1/"+key, a))
		must(t, tx.Commit())
	}
	bind("book/b1.pdf", pdf, "application/pdf")
	two, one := booxpage.ReaderPageKey(refs[1]), booxpage.ReaderPageKey(refs[0])
	line := [][2]float32{{100, 100}, {300, 120}, {500, 140}}
	live := []testStroke{
		{ID: "ink", SubPage: "main", Revision: "r1", Reference: refs[1], Type: 5, Color: 0xff000000, Points: line},
		{ID: "gone", SubPage: "main", Revision: "r1", Reference: refs[1], Type: 5, Status: 1, Points: line},
		{ID: "side", SubPage: "aside", Revision: "r1", Reference: refs[1], Type: 5, Points: line},
		{ID: "rub", SubPage: "main", Revision: "r1", Reference: refs[1], Type: 14, Points: line},
	}
	bind("shape/"+two+"#doc1#1760000000000.zip", readerArchive(t, two+"#doc1#1760000000000", live), "application/zip")
	bind("point/"+two+"#r1#points", readerPoints(two, live), "application/octet-stream")
	// Page one's archive is stale: the reading record no longer lists it.
	stale := []testStroke{{ID: "old", SubPage: "x", Revision: "r2", Reference: refs[0], Type: 5, Points: line}}
	bind("shape/"+one+"#doc2#1760000000000.zip", readerArchive(t, one+"#doc2#1760000000000", stale), "application/zip")
	bind("point/"+one+"#r2#points", readerPoints(one, stale), "application/octet-stream")
	pages, _ := json.Marshal(map[string]any{"data": map[string][]string{two: {"main", "aside"}}})
	libraryRecord(s, g, 7, "notes1", map[string]any{"documentId": "b1", "readerNotePageNameMap": string(pages)})
	managementObserve(t, s)

	page := bookWeb(t, s, "GET", "/boox/reading?view=stored&selected="+url.QueryEscape(s.uid()+"#b1"), nil).Body.String()
	if !strings.Contains(page, "/boox/books/annotated.pdf?book=b1") || !strings.Contains(page, "Handwriting on 1 page.") {
		t.Fatal("preview lacks the annotated download", page)
	}
	w := bookWeb(t, s, "GET", "/boox/books/annotated.pdf?book=b1", nil)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/pdf" || !strings.Contains(w.Header().Get("Content-Disposition"), "Synthetic (annotated).pdf") {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	if got := inkedPages(t, w.Body.Bytes()); len(got) != 2 || got[0] || !got[1] {
		t.Fatal("inked pages", got)
	}
	ink, e := s.readerInk(ctx, "b1")
	must(t, e)
	if len(ink.Strokes) != 1 || ink.Strokes[0].UniqueID != "ink" || ink.SideNotes != 1 || ink.Other != 1 {
		t.Fatalf("%+v", ink)
	}
	if w := bookWeb(t, s, "GET", "/boox/books/annotated.pdf?book=e1", nil); w.Code != 404 {
		t.Fatal("non-PDF book", w.Code)
	}
	if page := bookWeb(t, s, "GET", "/boox/reading?view=stored&selected="+url.QueryEscape(s.uid()+"#e1"), nil).Body.String(); strings.Contains(page, "annotated.pdf") {
		t.Fatal("EPUB offered an annotated download")
	}
}
