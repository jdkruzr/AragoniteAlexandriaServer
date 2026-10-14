package booxpage

import (
	"errors"
	"image"
	"testing"

	"github.com/fogleman/gg"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/encoding/protowire"
)

func TestParseResourcesReadsAudioRecords(t *testing.T) {
	var e []byte
	e = protowire.AppendTag(e, 1, protowire.BytesType)
	e = protowire.AppendString(e, "res")
	e = protowire.AppendTag(e, 3, protowire.VarintType)
	e = protowire.AppendVarint(e, 1791928241345)
	e = protowire.AppendTag(e, 5, protowire.BytesType)
	e = protowire.AppendString(e, "Recording1")
	e = protowire.AppendTag(e, 6, protowire.VarintType)
	e = protowire.AppendVarint(e, 185400)
	e = protowire.AppendTag(e, 7, protowire.VarintType)
	e = protowire.AppendVarint(e, ResourceAudio)
	e = protowire.AppendTag(e, 8, protowire.BytesType)
	e = protowire.AppendString(e, "/a.mp3")
	e = protowire.AppendTag(e, 10, protowire.Fixed32Type) // unknown wire types are skipped
	e = protowire.AppendFixed32(e, 7)
	var b []byte
	b = protowire.AppendTag(b, 2, protowire.VarintType)
	b = protowire.AppendVarint(b, 1)
	b = protowire.AppendTag(b, 1, protowire.BytesType)
	b = protowire.AppendBytes(b, e)
	list, err := ParseResources(b)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	r := list[0]
	if r.ID != "res" || r.Title != "Recording1" || r.DurationMS != 185400 || r.CreatedAt != 1791928241345 || r.Type != ResourceAudio || r.RelativePath != "/a.mp3" {
		t.Fatalf("%+v", r)
	}
	for _, bad := range [][]byte{{0x0a, 0x05, 0x01}, {0xff}, b[:len(b)-1]} {
		if _, err := ParseResources(bad); err == nil {
			t.Fatalf("accepted %x", bad)
		}
	}
}

func TestAudioShapeLoadsWithoutCoverageWarning(t *testing.T) {
	shape := &pb.ShapeInfoProto{UniqueId: "chip", ShapeType: shapeAudio, BoundingRect: `{"left":10,"top":10,"right":60,"bottom":30}`, MatrixValues: `{"values":[1,0,0,0,1,0,0,0,1]}`,
		Resource: `{"audioDuration":0,"relativePath":"/a.mp3","title":"Recording1","type":2,"uniqueId":"res"}`}
	assets := map[string][]byte{"shape/page#a.zip": shapeZip(t, shape)}
	load := func(k string) ([]byte, error) {
		if b, ok := assets[k]; ok {
			return b, nil
		}
		return nil, errors.New("missing")
	}
	p, warnings, err := ShapePage(metaFixture(), "page", []string{"shape/page#a.zip"}, load)
	if err != nil || len(warnings) != 0 || len(p.Audio) != 1 || len(p.Shapes) != 1 {
		t.Fatal(p, warnings, err)
	}
	if a := p.Audio[0]; a.Path != "resource/data/a.mp3" || a.Title != "Recording1" || a.ResourceID != "res" || a.ShapeID != "chip" {
		t.Fatalf("%+v", a)
	}
	dc := gg.NewContext(100, 100)
	renderShape(dc, p.Shapes[0])
	if img := dc.Image().(*image.RGBA); img.RGBAAt(35, 10).A == 0 || img.RGBAAt(18, 20).A == 0 {
		t.Fatal("speaker marker not drawn")
	}
	for _, bad := range []string{`{}`, `{"relativePath":"/../x.mp3"}`, `{"relativePath":"/a/b.mp3"}`, `not json`} {
		shape.Resource = bad
		assets["shape/page#a.zip"] = shapeZip(t, shape)
		p, warnings, err = ShapePage(metaFixture(), "page", []string{"shape/page#a.zip"}, load)
		if err != nil || len(warnings) != 1 || len(p.Audio) != 0 {
			t.Fatal(bad, p, warnings, err)
		}
	}
}
