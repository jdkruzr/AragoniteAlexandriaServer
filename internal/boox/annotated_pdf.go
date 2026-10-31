package boox

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// pdfcpu otherwise writes a configuration directory under $HOME.
func init() { model.ConfigPath = "disable" }

// annotation is what annotatePDF drew, for the page and the logs.
type annotation struct {
	Pages, Strokes int
	// Unplaced strokes name a PDF page this file doesn't have: the ink was
	// drawn on a different copy of the book.
	Unplaced int
}

var errNoPDFPages = errors.New("pdf has no pages")

// annotatePDF draws NeoReader strokes onto the PDF pages they were written
// on. Each inked page gets one form XObject painted after its own content,
// which is left as it was, so text stays selectable and ink stays vector.
func annotatePDF(raw []byte, strokes []*booxpage.ReaderShape) ([]byte, annotation, error) {
	var note annotation
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	ctx, err := api.ReadContext(bytes.NewReader(raw), conf)
	if err != nil {
		return nil, note, err
	}
	if err = ctx.EnsurePageCount(); err != nil {
		return nil, note, err
	}
	if ctx.PageCount < 1 {
		return nil, note, errNoPDFPages
	}
	bounds, err := ctx.PageBoundaries(nil)
	if err != nil {
		return nil, note, err
	}
	// NeoReader names a page by its object reference, or by its index when
	// it has none; the page key is a hash of that name.
	byRef, byKey := map[string]int{}, map[string]int{}
	for i := 1; i <= ctx.PageCount; i++ {
		_, ref, _, err := ctx.PageDict(i, false)
		if err != nil {
			return nil, note, err
		}
		for _, name := range []string{booxpage.ReaderPDFReference(ref.ObjectNumber.Value(), ref.GenerationNumber.Value()), "pdf-invalid-" + strconv.Itoa(i-1)} {
			if _, taken := byRef[name]; !taken {
				byRef[name] = i
				byKey[booxpage.ReaderPageKey(name)] = i
			}
		}
	}
	pages := map[int][]*booxpage.ReaderShape{}
	for _, s := range strokes {
		page, ok := byRef[s.PageReference]
		if !ok && s.PageReference == "" {
			page, ok = byKey[s.PageKey]
		}
		if !ok {
			note.Unplaced++
			continue
		}
		pages[page] = append(pages[page], s)
	}
	for page := 1; page <= ctx.PageCount; page++ {
		list := pages[page]
		if len(list) == 0 {
			continue
		}
		booxpage.SortReaderShapes(list)
		if err = overlayPage(ctx, page, bounds[page-1], list); err != nil {
			return nil, note, fmt.Errorf("page %d: %w", page, err)
		}
		note.Pages++
		note.Strokes += len(list)
	}
	var out bytes.Buffer
	if err = api.WriteContext(ctx, &out); err != nil {
		return nil, note, err
	}
	return out.Bytes(), note, nil
}

func overlayPage(ctx *model.Context, page int, pb model.PageBoundaries, list []*booxpage.ReaderShape) error {
	d, _, inherited, err := ctx.PageDict(page, false)
	if err != nil {
		return err
	}
	crop := pb.CropBox()
	if crop == nil {
		crop = pb.MediaBox()
	}
	if crop == nil {
		return errors.New("page has no media box")
	}
	// Points are in the page as NeoReader shows it: the crop box after
	// rotation, top-left origin, y down. The form's matrix maps that back to
	// unrotated PDF space.
	w, h := crop.Width(), crop.Height()
	var m []float64
	switch ((pb.Rot % 360) + 360) % 360 {
	case 90:
		w, h = h, w
		m = []float64{0, 1, 1, 0, crop.LL.X, crop.LL.Y}
	case 180:
		m = []float64{-1, 0, 0, 1, crop.UR.X, crop.LL.Y}
	case 270:
		w, h = h, w
		m = []float64{0, -1, -1, 0, crop.UR.X, crop.UR.Y}
	default:
		m = []float64{1, 0, 0, -1, crop.LL.X, crop.UR.Y}
	}
	form, err := ctx.XRefTable.NewStreamDictForBuf(inkContent(list))
	if err != nil {
		return err
	}
	form.InsertName("Type", "XObject")
	form.InsertName("Subtype", "Form")
	form.Insert("BBox", types.NewNumberArray(0, 0, w, h))
	form.Insert("Matrix", types.NewNumberArray(m...))
	form.Insert("Resources", types.Dict{"ExtGState": types.Dict{"AlxMarker": types.Dict{"Type": types.Name("ExtGState"), "BM": types.Name("Darken")}}})
	if err = form.Encode(); err != nil {
		return err
	}
	formRef, err := ctx.IndRefForNewObject(*form)
	if err != nil {
		return err
	}

	// Name the form in the page's resources. Pages often share one resource
	// dictionary, so the page gets its own copy rather than changing others.
	var res types.Dict
	if o, found := d.Find("Resources"); found && o != nil {
		if res, err = ctx.DereferenceDict(o); err != nil {
			return err
		}
	}
	if res == nil && inherited != nil {
		res = inherited.Resources
	}
	if res == nil {
		res = types.Dict{}
	} else {
		res = res.Clone().(types.Dict)
	}
	d.Update("Resources", res)
	xobjects := types.Dict{}
	if o, found := res.Find("XObject"); found && o != nil {
		shared, err := ctx.DereferenceDict(o)
		if err != nil {
			return err
		}
		if shared != nil {
			xobjects = shared.Clone().(types.Dict)
		}
	}
	res.Update("XObject", xobjects)
	name := "AlxInk"
	for n := 1; ; n++ {
		if _, taken := xobjects.Find(name); !taken {
			break
		}
		name = "AlxInk" + strconv.Itoa(n)
	}
	xobjects.Update(name, *formRef)

	// Wrap the page's own content in q…Q so whatever state it leaves can't
	// move the ink, then paint the form.
	open, err := ctx.XRefTable.StreamDictIndRef([]byte("q\n"))
	if err != nil {
		return err
	}
	paint, err := ctx.XRefTable.StreamDictIndRef([]byte("\nQ q /" + name + " Do Q\n"))
	if err != nil {
		return err
	}
	contents := types.Array{*open}
	if o, found := d.Find("Contents"); found && o != nil {
		if ref, ok := o.(types.IndirectRef); ok {
			if inner, err := ctx.Dereference(ref); err == nil {
				if a, ok := inner.(types.Array); ok {
					o = a
				}
			}
		}
		if a, ok := o.(types.Array); ok {
			contents = append(contents, a...)
		} else {
			contents = append(contents, o)
		}
	}
	d.Update("Contents", append(contents, *paint))
	return nil
}

// inkContent is a form's drawing operators: round-capped polylines whose
// width follows pressure, split only where the width changes.
func inkContent(list []*booxpage.ReaderShape) []byte {
	var b strings.Builder
	b.WriteString("1 J 1 j\n")
	for _, s := range list {
		if len(s.Points) == 0 {
			continue
		}
		c := uint32(s.Color)
		b.WriteString("q ")
		if s.Marker() {
			b.WriteString("/AlxMarker gs ")
		}
		fmt.Fprintf(&b, "%s %s %s RG\n", num(float64(c>>16&0xff)/255), num(float64(c>>8&0xff)/255), num(float64(c&0xff)/255))
		widths := booxpage.StrokeWidths(s.ShapeType, s.Thickness, s.Points)
		pt := func(i int) string {
			return num(float64(s.Points[i].X)) + " " + num(float64(s.Points[i].Y))
		}
		if len(s.Points) == 1 {
			fmt.Fprintf(&b, "%s w %s m %s l S\n", num(widths[0]), pt(0), pt(0))
			b.WriteString("Q\n")
			continue
		}
		for i := 0; i < len(widths); {
			j := i
			for j+1 < len(widths) && math.Abs(widths[j+1]-widths[i]) < 0.05 {
				j++
			}
			fmt.Fprintf(&b, "%s w %s m", num(widths[i]), pt(i))
			for k := i + 1; k <= j+1; k++ {
				b.WriteString(" " + pt(k) + " l")
			}
			b.WriteString(" S\n")
			i = j + 1
		}
		b.WriteString("Q\n")
	}
	return []byte(b.String())
}

func num(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}
