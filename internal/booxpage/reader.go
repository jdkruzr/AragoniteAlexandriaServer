package booxpage

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// ReaderShape is one stroke NeoReader drew on a book page. The reader keeps
// these in its own ShapeInfoProto (ReaderShapeDocProto), whose field numbers
// differ from the Notes one, so they are decoded here by number.
type ReaderShape struct {
	UniqueID, SubPage, RevisionID, PageReference, PageKey string
	CreatedAt, UpdatedAt                                  int64
	Color                                                 int32 // ARGB
	Thickness                                             float32
	ShapeType, Status                                     int32
	Points                                                []TinyPoint
}

// Erasers and other non-pen records are counted by callers, not drawn.
func (s ReaderShape) Pen() bool { return scribbleTypes[s.ShapeType] }

// Marker reports a highlighter stroke, which the firmware blends with darken.
func (s ReaderShape) Marker() bool { return s.ShapeType == shapeMarker }

// ReaderShapes merges a book page's shape archives the way NeoReader does:
// the newest record per stroke wins. Removed strokes are kept so callers can
// tell them apart from missing ones.
func ReaderShapes(archives [][]byte) (map[string]*ReaderShape, error) {
	winners := map[string]*ReaderShape{}
	decoded := 0
	for _, raw := range archives {
		if len(raw) > 0 && len(bytes.Trim(raw, "\x00")) == 0 {
			continue // a zeroed pen session; the rest of the page is still good
		}
		z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, errors.New("invalid reader shape archive")
		}
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			b, err := readEntry(f, 16<<20)
			if err != nil {
				return nil, errors.New("unreadable reader shape archive")
			}
			if decoded += len(b); decoded > 128<<20 {
				return nil, errors.New("expanded reader shapes exceed the limit")
			}
			list, err := readerShapeList(b)
			if err != nil {
				return nil, err
			}
			for _, s := range list {
				if s.UniqueID == "" {
					return nil, errors.New("reader shape identity missing")
				}
				old := winners[s.UniqueID]
				if old == nil && len(winners) >= 50000 {
					return nil, errors.New("page exceeds the reader shape limit")
				}
				if old == nil || s.UpdatedAt >= old.UpdatedAt {
					winners[s.UniqueID] = s
				}
			}
		}
	}
	return winners, nil
}

func readerShapeList(b []byte) ([]*ReaderShape, error) {
	var out []*ReaderShape
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, errors.New("invalid reader shape list")
		}
		b = b[n:]
		if num == 1 && typ == protowire.BytesType {
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, errors.New("invalid reader shape list")
			}
			s, err := readerShape(v)
			if err != nil {
				return nil, err
			}
			out = append(out, s)
			b = b[n:]
			continue
		}
		if n = protowire.ConsumeFieldValue(num, typ, b); n < 0 {
			return nil, errors.New("invalid reader shape list")
		}
		b = b[n:]
	}
	return out, nil
}

// Field numbers from ReaderShapeDocProto.ShapeInfoProto.
func readerShape(b []byte) (*ReaderShape, error) {
	s := &ReaderShape{}
	var v2, v3 string
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, errors.New("invalid reader shape")
		}
		b = b[n:]
		var (
			u   uint64
			str string
		)
		switch typ {
		case protowire.VarintType:
			u, n = protowire.ConsumeVarint(b)
		case protowire.Fixed32Type:
			var f uint32
			f, n = protowire.ConsumeFixed32(b)
			u = uint64(f)
		case protowire.BytesType:
			var v []byte
			v, n = protowire.ConsumeBytes(b)
			str = string(v)
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
		}
		if n < 0 {
			return nil, errors.New("invalid reader shape")
		}
		b = b[n:]
		switch num {
		case 1:
			s.UniqueID = str
		case 3:
			s.CreatedAt = int64(u)
		case 4:
			s.UpdatedAt = int64(u)
		case 5:
			s.SubPage = str
		case 9:
			s.Color = int32(u)
		case 12:
			if typ == protowire.Fixed32Type {
				s.Thickness = math.Float32frombits(uint32(u))
			}
		case 15:
			s.ShapeType = int32(u)
		case 25:
			s.RevisionID = str
		case 27:
			s.Status = int32(u)
		case 31:
			v2 = str
		case 33:
			v3 = str
		case 34:
			s.PageKey = str
		}
	}
	for _, position := range []string{v3, v2} {
		var p struct {
			PageReferenceID string `json:"pageReferenceId"`
		}
		if position != "" && json.Unmarshal([]byte(position), &p) == nil && p.PageReferenceID != "" {
			s.PageReference = p.PageReferenceID
			break
		}
	}
	return s, nil
}

// ParsePointFile reads a V1 point file into each stroke's points.
func ParsePointFile(data []byte) (map[string][]TinyPoint, error) { return parsePointFile(data) }

// ReaderPageKey is NeoReader's name for a PDF page's handwriting: the Java
// String.hashCode of its position JSON.
func ReaderPageKey(reference string) string {
	var h int32
	for _, c := range utf16Units(`{"pageReferenceId":"` + reference + `"}`) {
		h = 31*h + int32(c)
	}
	return strconv.Itoa(int(h))
}

func utf16Units(s string) []uint16 {
	var out []uint16
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xd800+(r>>10)), uint16(0xdc00+(r&0x3ff)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

// ReaderPDFReference is the page reference NeoReader gives a PDF page object.
func ReaderPDFReference(object, generation int) string {
	return fmt.Sprintf("pdf-%d-%d", object, generation)
}

// StrokeWidths gives each segment of a pen stroke its pressure width, the
// same model the notebook renderer uses. A single point gets one width.
func StrokeWidths(shapeType int32, thickness float32, points []TinyPoint) []float64 {
	ps := getPenStyle(shapeType)
	if len(points) == 1 {
		return []float64{pressureToWidth(float64(points[0].Pressure), float64(thickness), ps)}
	}
	out := make([]float64, 0, len(points))
	for i := 0; i+1 < len(points); i++ {
		out = append(out, pressureToWidth((float64(points[i].Pressure)+float64(points[i+1].Pressure))/2, float64(thickness), ps))
	}
	return out
}

// SortReaderShapes orders strokes as they were drawn.
func SortReaderShapes(list []*ReaderShape) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].CreatedAt != list[j].CreatedAt {
			return list[i].CreatedAt < list[j].CreatedAt
		}
		return strings.Compare(list[i].UniqueID, list[j].UniqueID) < 0
	})
}
