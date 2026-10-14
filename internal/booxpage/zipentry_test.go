package booxpage

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"testing"

	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
)

// shiftedDescriptor rebuilds a one-entry archive the way some older firmware
// wrote it: the central directory is right, but three stray bytes sit
// between the compressed data and the data descriptor.
func shiftedDescriptor(t *testing.T, raw []byte) []byte {
	t.Helper()
	at := bytes.Index(raw, []byte("PK\x07\x08"))
	if at < 0 {
		t.Fatal("no data descriptor")
	}
	out := append(append(append([]byte{}, raw[:at]...), 0x38, 0xd5, 0x32), raw[at:]...)
	end := bytes.LastIndex(out, []byte("PK\x05\x06"))
	binary.LittleEndian.PutUint32(out[end+16:], binary.LittleEndian.Uint32(out[end+16:])+3)
	return out
}

func TestShiftedDataDescriptorIsReadWhenCentralDirectoryVerifies(t *testing.T) {
	box := &pb.ShapeInfoProto{UniqueId: "box", ShapeType: 1, Color: -16777216, Thickness: 2, BoundingRect: `{"left":10,"top":10,"right":40,"bottom":40}`, CreatedAt: 1}
	good := shapeZip(t, box)
	odd := shiftedDescriptor(t, good)
	if z, e := zip.NewReader(bytes.NewReader(odd), int64(len(odd))); e != nil {
		t.Fatal(e)
	} else if r, e := z.File[0].Open(); e == nil {
		if _, e = readAllFrom(r); e == nil {
			t.Fatal("fixture does not reproduce the checksum error")
		}
	}
	p, w, e := ShapePage(metaFixture(), "page", []string{"shape/page#a#1.zip"}, func(string) ([]byte, error) { return odd, nil })
	if e != nil || len(w) != 0 || len(p.Shapes) != 1 {
		t.Fatal(e, w)
	}
	// Damaged compressed data still fails: the central CRC must match.
	bad := append([]byte{}, odd...)
	bad[40] ^= 0xff
	if _, _, e := ShapePage(metaFixture(), "page", []string{"shape/page#a#1.zip"}, func(string) ([]byte, error) { return bad, nil }); e == nil {
		t.Fatal("damaged archive accepted")
	}
}

func readAllFrom(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var b bytes.Buffer
	_, e := b.ReadFrom(r)
	return b.Bytes(), e
}
