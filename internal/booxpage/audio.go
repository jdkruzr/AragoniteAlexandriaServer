package booxpage

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fogleman/gg"
	"google.golang.org/protobuf/encoding/protowire"
)

// Resource is one native resource record (resource/pb/<id>#<ts>), the
// firmware's ResourceDocProto list. Type 2 is an audio recording: CreatedAt is
// when recording started and DurationMS its length.
type Resource struct {
	ID, DocumentID, Title, RelativePath string
	CreatedAt, UpdatedAt, DurationMS    int64
	Type                                int32
}

// ResourceAudio is the native resource type for recordings.
const ResourceAudio = 2

// ParseResources decodes a resource record without a generated schema. Unknown
// fields are skipped so newer firmware does not break older servers.
func ParseResources(b []byte) ([]Resource, error) {
	if len(b) > 1<<20 {
		return nil, errors.New("resource record exceeds limit")
	}
	var out []Resource
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, errors.New("invalid resource record")
		}
		b = b[n:]
		if num == 1 && typ == protowire.BytesType {
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return nil, errors.New("invalid resource record")
			}
			r, err := parseResource(v)
			if err != nil {
				return nil, err
			}
			out = append(out, r)
			b = b[n:]
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, b)
		if n < 0 {
			return nil, errors.New("invalid resource record")
		}
		b = b[n:]
	}
	return out, nil
}

func parseResource(b []byte) (Resource, error) {
	var r Resource
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return r, errors.New("invalid resource entry")
		}
		b = b[n:]
		switch {
		case typ == protowire.BytesType && (num == 1 || num == 2 || num == 5 || num == 8):
			v, n := protowire.ConsumeString(b)
			if n < 0 {
				return r, errors.New("invalid resource entry")
			}
			switch num {
			case 1:
				r.ID = v
			case 2:
				r.DocumentID = v
			case 5:
				r.Title = v
			case 8:
				r.RelativePath = v
			}
			b = b[n:]
		case typ == protowire.VarintType && (num == 3 || num == 4 || num == 6 || num == 7):
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return r, errors.New("invalid resource entry")
			}
			switch num {
			case 3:
				r.CreatedAt = int64(v)
			case 4:
				r.UpdatedAt = int64(v)
			case 6:
				r.DurationMS = int64(v)
			case 7:
				r.Type = int32(v)
			}
			b = b[n:]
		default:
			n = protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return r, errors.New("invalid resource entry")
			}
			b = b[n:]
		}
	}
	if r.DurationMS < 0 || r.CreatedAt < 0 {
		return r, errors.New("invalid resource entry")
	}
	return r, nil
}

// AudioRef is a recording placed on a page (native shape type 23).
type AudioRef struct {
	ShapeID, ResourceID, Path, Title string
}

const shapeAudio = 23

// audioShape reads the recording a shape points at. Its geometry is the chip
// drawn on the page; the recording itself is resource/data/<relativePath>.
func audioShape(raw string) (AudioRef, error) {
	var ref struct {
		RelativePath, Title, UniqueID string
	}
	if json.Unmarshal([]byte(raw), &ref) != nil || ref.RelativePath == "" {
		return AudioRef{}, fmt.Errorf("recording reference unavailable")
	}
	name := strings.TrimPrefix(ref.RelativePath, "/")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return AudioRef{}, fmt.Errorf("recording reference unavailable")
	}
	return AudioRef{ResourceID: ref.UniqueID, Path: "resource/data/" + name, Title: ref.Title}, nil
}

// renderAudio draws a neutral speaker marker where the device shows its
// recording chip. Titles stay out of the image so recognition doesn't read them.
func renderAudio(dc *gg.Context, s *Shape) {
	if s.BoundingRect == nil {
		return
	}
	mat := parseMatrix(s.MatrixValues)
	br := s.BoundingRect
	x0, y0 := tp(mat, br.Left, br.Top)
	x1, y1 := tp(mat, br.Right, br.Bottom)
	w, h := x1-x0, y1-y0
	if w <= 0 || h <= 0 {
		return
	}
	dc.Push()
	defer dc.Pop()
	dc.SetRGBA(0.35, 0.35, 0.35, 1)
	dc.SetLineWidth(2)
	dc.DrawRoundedRectangle(x0, y0, w, h, h/4)
	dc.Stroke()
	// Speaker: a box and cone, then two arcs.
	u := h / 6
	cx, cy := x0+h/2, y0+h/2
	dc.MoveTo(cx-1.5*u, cy-0.7*u)
	dc.LineTo(cx-0.7*u, cy-0.7*u)
	dc.LineTo(cx+0.4*u, cy-1.6*u)
	dc.LineTo(cx+0.4*u, cy+1.6*u)
	dc.LineTo(cx-0.7*u, cy+0.7*u)
	dc.LineTo(cx-1.5*u, cy+0.7*u)
	dc.ClosePath()
	dc.Fill()
	for _, r := range []float64{1.2 * u, 2 * u} {
		dc.DrawArc(cx+0.4*u, cy, r, -0.8, 0.8)
		dc.Stroke()
	}
}
