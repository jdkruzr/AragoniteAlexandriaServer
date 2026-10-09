package booxpage

import (
	"bytes"
	"encoding/json"
	"github.com/go-pdf/fpdf"
	"image"
	"image/color"
	"image/png"
	"os/exec"
	"strings"
	"testing"

	"github.com/fogleman/gg"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/proto"
)

func TestSVGColorsClassesAndBlockedReferences(t *testing.T) {
	raw := []byte(`<svg viewBox="0 0 100 100"><style>.red,.blue{stroke-width:4;fill:none}.red{stroke:red}.blue{stroke:blue}</style><line class="red" x1="10" y1="20" x2="90" y2="20"/><circle class="blue" cx="50" cy="60" r="10"/></svg>`)
	dc := gg.NewContext(100, 100)
	dc.SetRGB(1, 1, 1)
	dc.Clear()
	if e := drawSVG(dc, raw); e != nil {
		t.Fatal(e)
	}
	r, g, b, _ := dc.Image().At(50, 20).RGBA()
	if r < 60000 || g > 1000 || b > 1000 {
		t.Fatal("class color lost")
	}
	for _, raw := range []string{`<!DOCTYPE svg><svg/>`, `<svg><use href="#a"/></svg>`, `<svg><image href="https://example.invalid"/></svg>`, `<svg><path fill="url(https://example.invalid)"/></svg>`, `<svg><path d="M 1e99 2"/></svg>`} {
		if _, e := safeSVG([]byte(raw)); e == nil {
			t.Fatal("accepted unsafe SVG", raw)
		}
	}
}
func TestLegacyBuiltInAndProceduralPaper(t *testing.T) {
	load := func(string) ([]byte, error) { return nil, bytes.ErrTooLarge }
	for _, layout := range []string{
		`{"properties":{"layoutType":"LayoutResVector","resourceAttr":{"resName":"com.onyx.android.note:drawable/ic_horizontal_line_24"}}}`,
		`{"properties":{"layoutType":"LayoutFittedGrid","pageMargins":{"spacing":20,"paddingLeft":10,"paddingRight":10,"paddingTop":10,"paddingBottom":10},"strokeAttr":{"color":-16777216,"width":2}}}`,
	} {
		dc := gg.NewContext(100, 100)
		dc.SetRGB(1, 1, 1)
		dc.Clear()
		if e := drawTemplate(dc, []byte(layout), load); e != nil {
			t.Fatal(e)
		}
		n := 0
		for y := 0; y < 100; y++ {
			for x := 0; x < 100; x++ {
				r, _, _, _ := dc.Image().At(x, y).RGBA()
				if r < 60000 {
					n++
				}
			}
		}
		if n < 100 {
			t.Fatal("paper was blank")
		}
	}
}
func TestAffineImageRotation(t *testing.T) {
	im := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			im.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, im)
	dc := gg.NewContext(100, 100)
	s := &Shape{BoundingRect: &Rect{Left: 0, Top: 0, Right: 20, Bottom: 10}, MatrixValues: []float64{0, -1, 50, 1, 0, 10, 0, 0, 1}}
	if e := drawImageShape(dc, s, func(string) ([]byte, error) { return b.Bytes(), nil }); e != nil {
		t.Fatal(e)
	}
	r, _, _, a := dc.Image().At(45, 20).RGBA()
	if r == 0 || a == 0 {
		t.Fatal("rotated image misplaced")
	}
	_, _, _, a = dc.Image().At(10, 5).RGBA()
	if a != 0 {
		t.Fatal("untransformed ghost")
	}
}
func TestVirtualPageIdentityAndPDFReference(t *testing.T) {
	list := &pb.VirtualPageList{Proto: []*pb.VirtualPage{{PageId: "other", ContentType: "pdf", ContentPageId: `{"pageIndex":8}`}, {PageId: "page", ContentType: "pdf", ContentRelativePath: "document/note/resource/content/test.pdf", ContentPageId: `{"pageIndex":2}`}}}
	raw, _ := proto.Marshal(list)
	b, e := backgroundFor(Metadata{UniqueID: "note"}, "page", []string{"virtual/page/pb/doc"}, func(string) ([]byte, error) { return raw, nil })
	if e != nil || b.Index != 2 || b.Path != "resource/content/test.pdf" {
		t.Fatalf("wrong reference: %+v %v", b, e)
	}
	list.Proto[1].ContentPageId = `{}`
	raw, _ = proto.Marshal(list)
	if _, e = backgroundFor(Metadata{}, "page", []string{"virtual/page/pb/doc"}, func(string) ([]byte, error) { return raw, nil }); e == nil {
		t.Fatal("missing PDF index accepted")
	}
}
func TestTemplateInvalidSpacingRefused(t *testing.T) {
	var raw map[string]any
	json.Unmarshal([]byte(`{"properties":{"layoutType":"LayoutFittedGrid","pageMargins":{"spacing":-1}}}`), &raw)
	b, _ := json.Marshal(raw)
	e := drawTemplate(gg.NewContext(100, 100), b, nil)
	if e == nil || !strings.Contains(e.Error(), "geometry") {
		t.Fatal(e)
	}
}

func TestPDFBackgroundSelectsPage(t *testing.T) {
	if _, e := exec.LookPath("pdftoppm"); e != nil {
		t.Skip("Poppler not installed")
	}
	pdf := fpdf.New("P", "pt", "A4", "")
	pdf.AddPage()
	pdf.SetFillColor(255, 0, 0)
	pdf.Rect(0, 0, 595, 842, "F")
	pdf.AddPage()
	pdf.SetFillColor(0, 0, 255)
	pdf.Rect(0, 0, 595, 842, "F")
	var b bytes.Buffer
	if e := pdf.Output(&b); e != nil {
		t.Fatal(e)
	}
	dc := gg.NewContext(100, 120)
	if e := drawPDF(dc, b.Bytes(), 1); e != nil {
		t.Fatal(e)
	}
	r, g, bl, _ := dc.Image().At(50, 60).RGBA()
	if r > 1000 || g > 1000 || bl < 60000 {
		t.Fatal("PDF page selection or placement incorrect")
	}
	if e := drawPDF(dc, b.Bytes(), 9); e == nil {
		t.Fatal("missing PDF page accepted")
	}
}
