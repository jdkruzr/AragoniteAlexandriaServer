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

func resourceList(records ...Resource) []byte {
	var b []byte
	for _, r := range records {
		var e []byte
		e = protowire.AppendTag(e, 1, protowire.BytesType)
		e = protowire.AppendString(e, r.ID)
		e = protowire.AppendTag(e, 3, protowire.VarintType)
		e = protowire.AppendVarint(e, uint64(r.CreatedAt))
		e = protowire.AppendTag(e, 4, protowire.VarintType)
		e = protowire.AppendVarint(e, uint64(r.UpdatedAt))
		e = protowire.AppendTag(e, 7, protowire.VarintType)
		e = protowire.AppendVarint(e, uint64(r.Type))
		e = protowire.AppendTag(e, 8, protowire.BytesType)
		e = protowire.AppendString(e, r.RelativePath)
		if r.Status != 0 {
			e = protowire.AppendTag(e, 13, protowire.VarintType)
			e = protowire.AppendVarint(e, uint64(r.Status))
		}
		b = protowire.AppendTag(b, 1, protowire.BytesType)
		b = protowire.AppendBytes(b, e)
	}
	return b
}

func TestCurrentResourcesReplaysDeltaLists(t *testing.T) {
	text := func(id string, created, updated int64, status int32) Resource {
		return Resource{ID: id, CreatedAt: created, UpdatedAt: updated, Type: ResourceRichText, RelativePath: "/" + id + ".html", Status: status}
	}
	photo := Resource{ID: "photo", CreatedAt: 5, UpdatedAt: 5, Type: 1, RelativePath: "/photo.png"}
	// The server writes the note; two devices each save it once (cursor moves
	// are enough). Each save restates the previous text as removed and adds a
	// new one. The photo only appears in the first list.
	lists := [][]byte{
		resourceList(text("a", 10, 10, 0), photo),
		resourceList(text("a", 10, 20, ResourceRemoved), text("b", 21, 21, 0)),
		resourceList(text("b", 21, 30, ResourceRemoved), text("c", 31, 31, 0)),
	}
	got, err := CurrentResources(lists)
	if err != nil || len(got) != 2 || got[0].ID != "photo" || got[1].ID != "c" || got[1].RelativePath != "/c.html" {
		t.Fatalf("%+v %v", got, err)
	}
	// Order of arrival doesn't matter; the newest record wins.
	if got, err := CurrentResources([][]byte{lists[2], lists[0], lists[1]}); err != nil || len(got) != 2 || got[1].ID != "c" {
		t.Fatalf("%+v %v", got, err)
	}
	// Identical restatements are fine; a different record at the same time is not.
	if _, err := CurrentResources([][]byte{lists[0], lists[0]}); err != nil {
		t.Fatal(err)
	}
	if _, err := CurrentResources([][]byte{lists[0], resourceList(text("a", 10, 10, ResourceRemoved))}); err == nil {
		t.Fatal("conflicting equal-time records accepted")
	}
	if got, err := CurrentResources(nil); err != nil || len(got) != 0 {
		t.Fatal(got, err)
	}
}
