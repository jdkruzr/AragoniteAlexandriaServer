package booxpage

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/proto"
	"image/png"
	"math"
	"strings"
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

func TestLegacyWrappedMatrixRendersSameAsArray(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000001"
	base := &pb.ShapeInfoProto{UniqueId: id, ShapeType: 3, Thickness: 2, Color: -16777216, RevisionId: "r", MatrixValues: `[1,0,8,0,1,12,0,0,1]`}
	render := func(matrix string) []byte {
		t.Helper()
		base.MatrixValues = matrix
		z := shapeZip(t, base)
		load := func(k string) ([]byte, error) {
			switch k {
			case "shape/page#a.zip":
				return z, nil
			case "point/page#r#points":
				return pointFixture(id), nil
			default:
				return []byte(`{"properties":{"layoutType":"LayoutBlank"}}`), nil
			}
		}
		p, _, e := ShapePage(metaFixture(), "page", []string{"shape/page#a.zip"}, load)
		if e != nil {
			t.Fatal(e)
		}
		img, _, e := Preview(p, load)
		if e != nil {
			t.Fatal(e)
		}
		var b bytes.Buffer
		if e = png.Encode(&b, img); e != nil {
			t.Fatal(e)
		}
		return b.Bytes()
	}
	a := render(base.MatrixValues)
	b := render(`{"empty":false,"values":[1,0,8,0,1,12,0,0,1]}`)
	if !bytes.Equal(a, b) {
		t.Fatal("firmware transform representation changed geometry")
	}
}

func TestIssuesNameTheShapeWithoutContent(t *testing.T) {
	ok := &pb.ShapeInfoProto{UniqueId: "audio", ShapeType: shapeAudio, BoundingRect: `{"left":10,"top":10,"right":60,"bottom":30}`, Resource: `{"relativePath":"/a.mp3"}`, CreatedAt: 1}
	odd := &pb.ShapeInfoProto{UniqueId: "odd", ShapeType: 77, Text: "private words", BoundingRect: `{"left":0,"top":0,"right":40,"bottom":10}`, Extra: `{"mystery":{"depth":1},"plain":2}`, CreatedAt: 2}
	assets := map[string][]byte{"shape/page#a.zip": shapeZip(t, ok, odd)}
	load := func(k string) ([]byte, error) {
		if b, found := assets[k]; found {
			return b, nil
		}
		return nil, errors.New("missing")
	}
	p, w, e := ShapePage(metaFixture(), "page", []string{"shape/page#a.zip"}, load)
	if e != nil || len(w) != 1 || len(p.Issues) != 1 {
		t.Fatal(p.Issues, w, e)
	}
	i := p.Issues[0]
	if i.ShapeID != "odd" || i.ShapeType != 77 || i.Warning != w[0] || i.Summary.Width != 40 || i.Summary.TextRunes != 13 ||
		strings.Join(i.Summary.ExtraKeys, ",") != "mystery,mystery.depth,plain" || strings.Join(i.Summary.Fields, ",") != "boundingRect,extra,text" {
		t.Fatalf("%+v", i)
	}
	if b, _ := json.Marshal(p.Issues); bytes.Contains(b, []byte("private")) {
		t.Fatal("issue leaked shape text")
	}
}

func TestRegularPolygonFromPenPoints(t *testing.T) {
	id := "00000000-0000-0000-0000-000000000024"
	sp := &pb.ShapeInfoProto{UniqueId: id, ShapeType: 24, Thickness: 2, Color: -16777216, RevisionId: "r", BoundingRect: `{"left":0,"top":0,"right":40,"bottom":40}`}
	assets := map[string][]byte{"shape/page#a.zip": shapeZip(t, sp), "point/page#r#points": pointFixture(id)}
	load := func(k string) ([]byte, error) {
		if b, ok := assets[k]; ok {
			return b, nil
		}
		return nil, errors.New("missing")
	}
	p, w, e := ShapePage(metaFixture(), "page", []string{"shape/page#a.zip"}, load)
	if e != nil || len(w) != 0 || len(p.Shapes) != 1 || len(p.Shapes[0].Points) != 2 {
		t.Fatal(p, w, e)
	}
	// Down (10,10), up (30,30): centre (10,20), radius √500, first vertex straight up.
	v := regularPolygon(p.Shapes[0].Points, 5)
	r := math.Sqrt(500)
	if len(v) != 5 || math.Abs(v[0][0]-10) > 1e-4 || math.Abs(v[0][1]-(20-r)) > 1e-4 || math.Abs(v[1][0]-(10+r*math.Sin(2*math.Pi/5))) > 1e-4 {
		t.Fatal(v)
	}
	if len(regularPolygon(p.Shapes[0].Points[:1], 5)) != 0 {
		t.Fatal("one point made a polygon")
	}
}

func TestDraftPagesGrowToFitInkAndBookmarksAreSkipped(t *testing.T) {
	m := metaFixture()
	m.ActiveScene = 4
	low := &pb.ShapeInfoProto{UniqueId: "box", ShapeType: 1, Color: -16777216, Thickness: 2, BoundingRect: `{"left":10,"top":120,"right":40,"bottom":160}`, CreatedAt: 1}
	mark := &pb.ShapeInfoProto{UniqueId: "mark", ShapeType: shapeBookmark, Extra: `{"textContent":"Todo"}`, BoundingRect: `{"left":0,"top":0,"right":10,"bottom":10}`, CreatedAt: 2}
	raw := shapeZip(t, low, mark)
	p, w, e := ShapePage(m, "page", []string{"shape/page#a.zip"}, func(string) ([]byte, error) { return raw, nil })
	if e != nil || len(w) != 0 || p.Height != 180 || p.Width != 100 || len(p.Shapes) != 1 || len(p.Texts) != 1 || p.Texts[0] != "Todo" {
		t.Fatal(e, w, p.Width, p.Height, p.Texts)
	}
	img, _, e := Preview(p, func(string) ([]byte, error) { return nil, errors.New("no template") })
	if e != nil || img.Bounds().Dy() != 180 {
		t.Fatal("preview cropped the draft page", e)
	}
}

func TestMeetingNotebookPagesAreNotBlocked(t *testing.T) {
	m := metaFixture()
	m.ActiveScene = 2
	p, w, e := ShapePage(m, "page", nil, func(string) ([]byte, error) { return nil, errors.New("missing") })
	if e != nil || len(w) != 0 || len(p.Notices) != 1 {
		t.Fatal(e, w, p.Notices)
	}
}

func TestZeroFilledSessionsAreRecoveredFromOrphanedPoints(t *testing.T) {
	const a, b, lost, late = "00000000-0000-0000-0000-00000000000a", "00000000-0000-0000-0000-00000000000b", "00000000-0000-0000-0000-00000000000c", "00000000-0000-0000-0000-00000000000d"
	// Two good sessions: a black pen, then a red marker just before the gap.
	// A shape created after the gap must not be chosen as "the pen before".
	black := &pb.ShapeInfoProto{UniqueId: a, ShapeType: 2, Color: -16777216, Thickness: 3, RevisionId: "ra", CreatedAt: 100}
	red := &pb.ShapeInfoProto{UniqueId: b, ShapeType: 5, Color: -65536, Thickness: 7, RevisionId: "rb", CreatedAt: 200}
	after := &pb.ShapeInfoProto{UniqueId: late, ShapeType: 2, Color: -16776961, Thickness: 1, RevisionId: "rd", CreatedAt: 900}
	blobs := map[string][]byte{
		"shape/page#s1#100.zip": shapeZip(t, black),
		"shape/page#s2#200.zip": shapeZip(t, red),
		"shape/page#s3#300.zip": make([]byte, 4875),
		"shape/page#s4#900.zip": shapeZip(t, after),
		"point/page#ra#points":  pointFixture(a),
		"point/page#rb#points":  pointFixture(b),
		"point/page#rc#points":  pointFixture(lost),
		"point/page#rd#points":  pointFixture(late),
	}
	keys := make([]string, 0, len(blobs))
	for k := range blobs {
		keys = append(keys, k)
	}
	load := func(k string) ([]byte, error) { return blobs[k], nil }
	p, w, e := ShapePage(metaFixture(), "page", keys, load)
	if e != nil || len(w) != 0 || len(p.Shapes) != 4 || len(p.Notices) != 1 || !strings.Contains(p.Notices[0], "recovered from point data") {
		t.Fatal(e, w, len(p.Shapes), p.Notices)
	}
	var got *Shape
	for _, s := range p.Shapes {
		if s.UniqueID == lost {
			got = s
		}
	}
	if got == nil || got.ShapeType != 5 || got.Color != -65536 || got.Thickness != 7 || got.RevisionID != "rc" || len(got.Points) != 2 {
		t.Fatalf("recovered stroke %+v", got)
	}
	if _, _, e := Preview(p, func(string) ([]byte, error) { return nil, errors.New("no template") }); e != nil {
		t.Fatal(e)
	}

	// Without an empty archive, an unreferenced point file is not drawn:
	// that is how erased ink looks too.
	blobs["shape/page#s3#300.zip"] = shapeZip(t, &pb.ShapeInfoProto{UniqueId: lost, ShapeType: 2, RevisionId: "rc", ShapeStatus: 1, CreatedAt: 300})
	p, _, e = ShapePage(metaFixture(), "page", keys, load)
	if e != nil || len(p.Shapes) != 3 || len(p.Notices) != 0 {
		t.Fatal(e, len(p.Shapes), p.Notices)
	}
	keysNoEmpty := []string{"shape/page#s1#100.zip", "shape/page#s2#200.zip", "point/page#ra#points", "point/page#rb#points", "point/page#rc#points"}
	p, _, e = ShapePage(metaFixture(), "page", keysNoEmpty, load)
	if e != nil || len(p.Shapes) != 2 {
		t.Fatal(e, len(p.Shapes))
	}
}

func TestZeroFilledShapeArchiveWithoutPointsKeepsANotice(t *testing.T) {
	box := &pb.ShapeInfoProto{UniqueId: "box", ShapeType: 1, Color: -16777216, Thickness: 2, BoundingRect: `{"left":10,"top":10,"right":40,"bottom":40}`, CreatedAt: 1}
	blobs := map[string][]byte{"shape/page#a#1.zip": shapeZip(t, box), "shape/page#b#2.zip": make([]byte, 4875), "shape/page#c#3.zip": make([]byte, 1962)}
	load := func(k string) ([]byte, error) { return blobs[k], nil }
	p, w, e := ShapePage(metaFixture(), "page", []string{"shape/page#a#1.zip", "shape/page#b#2.zip", "shape/page#c#3.zip"}, load)
	if e != nil || len(w) != 0 || len(p.Shapes) != 1 || len(p.Notices) != 1 || !strings.HasPrefix(p.Notices[0], "2 pen sessions") {
		t.Fatal(e, w, p.Notices)
	}
	blobs["shape/page#b#2.zip"] = []byte("\x00\x01not a zip")
	if _, _, e := ShapePage(metaFixture(), "page", []string{"shape/page#a#1.zip", "shape/page#b#2.zip"}, load); e == nil {
		t.Fatal("garbage archive accepted")
	}
}
