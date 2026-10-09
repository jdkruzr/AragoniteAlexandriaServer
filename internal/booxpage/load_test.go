package booxpage

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/proto"
	"math"
	"testing"
)

func shapeZip(t *testing.T, shapes ...*pb.ShapeInfoProto) []byte {
	t.Helper()
	b, e := proto.Marshal(&pb.ShapeInfoProtoList{Proto: shapes})
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	f, e := z.Create("shapes")
	if e != nil {
		t.Fatal(e)
	}
	f.Write(b)
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}
func pointFixture(id string) []byte {
	b := make([]byte, 76)
	binary.BigEndian.PutUint16(b[2:4], 1)
	block := make([]byte, 36)
	for i := 0; i < 2; i++ {
		p := block[4+i*16:]
		binary.BigEndian.PutUint32(p, math.Float32bits(float32(10+20*i)))
		binary.BigEndian.PutUint32(p[4:], math.Float32bits(float32(10+20*i)))
		binary.BigEndian.PutUint16(p[10:], 2000)
	}
	b = append(b, block...)
	xref := len(b)
	entry := bytes.Repeat([]byte{' '}, 44)
	copy(entry, id)
	binary.BigEndian.PutUint32(entry[36:], 76)
	binary.BigEndian.PutUint32(entry[40:], uint32(len(block)))
	b = append(b, entry...)
	return binary.BigEndian.AppendUint32(b, uint32(xref))
}
func metaFixture() Metadata {
	m := Metadata{UniqueID: "note"}
	m.PageNameList = json.RawMessage(`{"pageNameList":["page"]}`)
	m.NotePageInfo.PageInfoMap = map[string]PageInfo{"page": {Width: 100, Height: 100, LayerList: []Layer{{ID: 0, Show: true}, {ID: 1, Show: false}}}}
	return m
}
func TestMergeRevisionsRemovalsLayersAndMissingPoints(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000001"
	base := &pb.ShapeInfoProto{UniqueId: id, ShapeType: 3, Thickness: 2, Color: -16777216, RevisionId: "r", UpdatedAt: 1}
	removed := proto.Clone(base).(*pb.ShapeInfoProto)
	removed.UpdatedAt = 2
	removed.ShapeStatus = 1
	other := proto.Clone(base).(*pb.ShapeInfoProto)
	other.UniqueId = "other"
	other.Zorder = 1
	assets := map[string][]byte{"shape/page#a.zip": shapeZip(t, base, other), "shape/page#b.zip": shapeZip(t, removed), "point/page#r#points": pointFixture(id)}
	load := func(k string) ([]byte, error) {
		b, ok := assets[k]
		if !ok {
			return nil, errors.New("missing")
		}
		return b, nil
	}
	p, w, e := ShapePage(metaFixture(), "page", []string{"shape/page#b.zip", "shape/page#a.zip"}, load)
	if e != nil || len(p.Shapes) != 0 || len(w) != 0 {
		t.Fatalf("erasure/layer: %v %v %v", p, w, e)
	}
	p, _, e = ShapePage(metaFixture(), "page", []string{"shape/page#a.zip"}, load)
	if e != nil || len(p.Shapes) != 1 || len(p.Shapes[0].Points) != 2 {
		t.Fatalf("point identity: %v %v", p, e)
	}
	delete(assets, "point/page#r#points")
	if _, _, e = ShapePage(metaFixture(), "page", []string{"shape/page#a.zip"}, load); e == nil {
		t.Fatal("missing points rendered as blank")
	}
}
func TestConflictingEqualTimestampRefused(t *testing.T) {
	a := &pb.ShapeInfoProto{UniqueId: "s", ShapeType: 6, Text: "one", UpdatedAt: 1}
	b := proto.Clone(a).(*pb.ShapeInfoProto)
	b.Text = "two"
	assets := map[string][]byte{"shape/page#a.zip": shapeZip(t, a), "shape/page#b.zip": shapeZip(t, b)}
	_, _, e := ShapePage(metaFixture(), "page", []string{"shape/page#a.zip", "shape/page#b.zip"}, func(k string) ([]byte, error) { return assets[k], nil })
	if e == nil {
		t.Fatal("ambiguous winner accepted")
	}
}
func TestMalformedPointBounds(t *testing.T) {
	b := pointFixture("shape")
	off := int(binary.BigEndian.Uint32(b[len(b)-4:]))
	binary.BigEndian.PutUint32(b[off+36:], 0)
	if _, e := parsePointFile(b); e == nil {
		t.Fatal("header overlap accepted")
	}
	b = pointFixture("shape")
	binary.BigEndian.PutUint32(b[80:], math.Float32bits(float32(math.NaN())))
	if _, e := parsePointFile(b); e == nil {
		t.Fatal("NaN accepted")
	}
}
func TestBlankAndUnsupportedAreDistinct(t *testing.T) {
	load := func(string) ([]byte, error) { return nil, errors.New("missing") }
	p, _, e := ShapePage(metaFixture(), "page", nil, load)
	if e != nil {
		t.Fatal(e)
	}
	_, warnings, e := Preview(p, load)
	if e != nil || len(warnings) == 0 {
		t.Fatal("missing template not reported")
	}
	p, _, e = ShapePage(metaFixture(), "page", nil, func(string) ([]byte, error) { return nil, nil })
	if e != nil {
		t.Fatal(e)
	}
	_, warnings, e = Preview(p, func(string) ([]byte, error) { return []byte(`{"properties":{"layoutType":"LayoutBlank"}}`), nil })
	if e != nil || len(warnings) != 0 {
		t.Fatalf("explicit blank template: %v %v", warnings, e)
	}
}
func TestTemplateExternalContentRejected(t *testing.T) {
	p := &Page{PageID: "p", Width: 100, Height: 100}
	_, w, e := Preview(p, func(k string) ([]byte, error) {
		if k == "template/json/p.template_json" {
			return []byte(`{"properties":{"imageAttr":{"relativePath":"evil.svg"}}}`), nil
		}
		return []byte(`<svg viewBox="0 0 100 100"><image href="https://attacker.invalid/a"/></svg>`), nil
	})
	if e != nil || len(w) == 0 {
		t.Fatal("unsupported external SVG not reported")
	}
}
func TestPageIDsFirmwareVariants(t *testing.T) {
	for _, v := range []string{`["a","b"]`, `{"pageNameList":["a","b"]}`, `"[\"a\",\"b\"]"`} {
		ids := PageIDs(json.RawMessage(v))
		if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
			t.Fatalf("%s: %v", v, ids)
		}
	}
}
