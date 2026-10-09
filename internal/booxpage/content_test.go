package booxpage

import (
	"encoding/json"
	"github.com/fogleman/gg"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"strings"
	"testing"
)

func TestUniversalGeometryAndUnknownRefusal(t *testing.T) {
	geometry := map[string]any{"type": "FeatureCollection", "features": []any{map[string]any{"geometry": map[string]any{"type": "MultiPoint", "coordinates": [][]float64{{10, 10}, {40, 40}}}, "properties": map[string]any{"subType": "Rectangle", "strokeAttr": map[string]any{"enableColor": true, "color": -16777216, "width": 2}, "fillAttr": map[string]any{"enableColor": true, "color": -65536}}}}}
	b, _ := json.Marshal(geometry)
	extra, _ := json.Marshal(map[string]string{"featureCollection": string(b)})
	paths, e := parseUniversal(string(extra))
	if e != nil {
		t.Fatal(e)
	}
	dc := gg.NewContext(100, 100)
	renderUniversal(dc, &Shape{Features: paths, MatrixValues: []float64{1, 0, 30, 0, 1, 20, 0, 0, 1}})
	r, g, _, a := dc.Image().At(50, 40).RGBA()
	if r < 60000 || g > 1000 || a == 0 {
		t.Fatal("geometry misplaced or lost fill")
	}
	bad := strings.Replace(string(extra), "Rectangle", "AlienQuadrilateral", 1)
	if _, e = parseUniversal(bad); e == nil {
		t.Fatal("unknown geometry silently replaced")
	}
}
func TestTextExtractionAndPositioning(t *testing.T) {
	if got := plainHTML(`<p>Hello<br>world</p><script>private exploit</script><p>&amp; friends</p>`); strings.Contains(got, "exploit") || !strings.Contains(got, "& friends") {
		t.Fatal(got)
	}
	sp := &pb.ShapeInfoProto{UniqueId: "text", ShapeType: 16, Color: -16777216, Text: "Readable", BoundingRect: `{"left":10,"top":10,"right":90,"bottom":50}`, TextStyle: `{"textSize":16,"textSpacing":1}`}
	raw := shapeZip(t, sp)
	p, w, e := ShapePage(metaFixture(), "page", []string{"shape/page#a.zip"}, func(string) ([]byte, error) { return raw, nil })
	if e != nil || len(w) > 0 || len(p.Notices) != 1 || len(p.Texts) != 1 {
		t.Fatal(e, w, p)
	}
	dc := gg.NewContext(100, 100)
	if e = drawTextShape(dc, p.Shapes[0]); e != nil {
		t.Fatal(e)
	}
	n := 0
	for y := 10; y < 50; y++ {
		for x := 10; x < 90; x++ {
			_, _, _, a := dc.Image().At(x, y).RGBA()
			if a > 0 {
				n++
			}
		}
	}
	if n < 50 {
		t.Fatal("text blank")
	}
}
func TestNoInventedPolygonBoundingBox(t *testing.T) {
	sp := &pb.ShapeInfoProto{UniqueId: "poly", ShapeType: 17, BoundingRect: `{"left":1,"top":1,"right":90,"bottom":90}`}
	raw := shapeZip(t, sp)
	p, w, e := ShapePage(metaFixture(), "page", []string{"shape/page#a.zip"}, func(string) ([]byte, error) { return raw, nil })
	if e != nil || len(w) == 0 || len(p.Shapes) != 0 {
		t.Fatal("unsupported polygon became a box", e, w)
	}
}

func TestCrossPageReferenceAndDanglingTarget(t *testing.T) {
	m := metaFixture()
	m.NotePageInfo.PageInfoMap["source"] = PageInfo{Width: 100, Height: 100}
	src := &pb.ShapeInfoProto{UniqueId: "target", ShapeType: 1, Color: -16777216, Thickness: 2, BoundingRect: `{"left":1,"top":1,"right":10,"bottom":10}`, MatrixValues: `[2,0,0,0,2,0,0,0,1]`}
	ref := &pb.ShapeInfoProto{UniqueId: "ref", ShapeType: 2000, ConnectionBean: `{"shapeReferenceBean":{"pageId":"source","shapeId":"target","matrixValues":[1,0,30,0,1,20,0,0,1]}}`}
	a := map[string][]byte{"shape/source#a.zip": shapeZip(t, src), "shape/page#a.zip": shapeZip(t, ref)}
	p, w, e := ShapePage(m, "page", []string{"shape/source#a.zip", "shape/page#a.zip"}, func(k string) ([]byte, error) { return a[k], nil })
	if e != nil || len(w) > 0 || len(p.Shapes) != 1 {
		t.Fatal(e, w)
	}
	s := p.Shapes[0]
	if s.UniqueID != "ref" || s.MatrixValues[0] != 2 || s.MatrixValues[2] != 30 {
		t.Fatal("reference transform or identity lost")
	}
	delete(a, "shape/source#a.zip")
	p, w, e = ShapePage(m, "page", []string{"shape/page#a.zip"}, func(k string) ([]byte, error) { return a[k], nil })
	if e != nil || len(w) == 0 || len(p.Shapes) != 0 {
		t.Fatal("dangling reference silently accepted", e, w)
	}
}
