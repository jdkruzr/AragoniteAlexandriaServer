package booxpage

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/proto"
)

// Load reads only immutable, verified objects selected by the caller's snapshot.
// Absence is an error, never proof that the native page was blank.
type Load func(string) ([]byte, error)
type Layer struct {
	ID   int32 `json:"id"`
	Show bool  `json:"show"`
}
type PageInfo struct {
	Width, Height float64
	LayerList     []Layer
}
type Metadata struct {
	VirtualPages         map[string]*pb.VirtualPage `json:"-"`
	RemovePageList       json.RawMessage
	Encryption           struct{ EncryptionType int }
	UniqueID             string `json:"uniqueId"`
	Title                string
	ParentUniqueID       string
	Type, Status         int
	ActiveScene          int
	PageNameList         json.RawMessage
	RichTextPageNameList json.RawMessage
	NotePageInfo         struct {
		PageInfoMap      map[string]PageInfo
		DefaultPageRect  Rect
		CanvasExpandType string
	}
}

func PageIDs(raw json.RawMessage) []string {
	var ids []string
	if json.Unmarshal(raw, &ids) == nil {
		return ids
	}
	var wrapped struct{ PageNameList []string }
	if json.Unmarshal(raw, &wrapped) == nil {
		return wrapped.PageNameList
	}
	var text string
	if json.Unmarshal(raw, &text) == nil && len(text) > 0 && text[0] != '"' {
		return PageIDs(json.RawMessage(text))
	}
	return nil
}
func ShapePage(meta Metadata, id string, keys []string, load Load) (*Page, []string, error) {
	return shapePage(meta, id, keys, load, map[string]bool{})
}
func shapePage(meta Metadata, id string, keys []string, load Load, visiting map[string]bool) (*Page, []string, error) {
	if visiting[id] || len(visiting) >= 4 {
		return nil, nil, fmt.Errorf("Cyclic or excessive page references.")
	}
	visiting[id] = true
	defer delete(visiting, id)

	info := meta.NotePageInfo.PageInfoMap[id]
	w, h := info.Width, info.Height
	if w <= 0 || h <= 0 {
		w = meta.NotePageInfo.DefaultPageRect.Right - meta.NotePageInfo.DefaultPageRect.Left
		h = meta.NotePageInfo.DefaultPageRect.Bottom - meta.NotePageInfo.DefaultPageRect.Top
	}
	if !finite(w) || !finite(h) || w < 1 || h < 1 || w > 8192 || h > 8192 || w*h > 20e6 {
		return nil, nil, fmt.Errorf("page dimensions unavailable or too large")
	}
	p := &Page{PageID: id, Width: w, Height: h}
	var warnings []string
	bg, bgErr := backgroundFor(meta, id, keys, load)
	p.Background = bg
	if bgErr != nil {
		warnings = append(warnings, bgErr.Error())
	}
	if meta.Encryption.EncryptionType != 0 {
		return nil, nil, fmt.Errorf("encrypted notebook preview is not supported")
	}
	if meta.ActiveScene < 0 || meta.ActiveScene > 4 {
		return nil, nil, fmt.Errorf("this notebook uses an unsupported native layout")
	}
	if meta.ActiveScene == 1 {
		warnings = append(warnings, "Rich-text notebook: available text is shown separately; native layout is not reproduced.")
	}
	if meta.ActiveScene == 2 {
		warnings = append(warnings, "Meeting notebook: audio and transcript layout are not reproduced.")
	}
	if meta.ActiveScene == 4 {
		warnings = append(warnings, "Draft notebook layout has not been qualified.")
	}
	if meta.NotePageInfo.CanvasExpandType != "" && meta.NotePageInfo.CanvasExpandType != "DEFAULT" {
		warnings = append(warnings, "Expanded canvas layout has not been qualified; this preview may be cropped.")
	}
	winners := map[string]*pb.ShapeInfoProto{}
	sort.Strings(keys)
	count := 0
	decodedBytes := 0
	for _, key := range keys {
		if !strings.HasPrefix(key, "shape/"+id+"#") || !strings.HasSuffix(key, ".zip") {
			continue
		}
		raw, err := load(key)
		if err != nil {
			return nil, nil, fmt.Errorf("shape archive unavailable or failed verification")
		}
		z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid shape archive")
		}
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			if f.UncompressedSize64 > 16<<20 {
				return nil, nil, fmt.Errorf("shape archive exceeds preview limit")
			}
			r, e := f.Open()
			if e != nil {
				return nil, nil, e
			}
			b, e := io.ReadAll(io.LimitReader(r, (16<<20)+1))
			r.Close()
			if e != nil || len(b) > 16<<20 {
				return nil, nil, fmt.Errorf("unreadable shape archive")
			}
			decodedBytes += len(b)
			if decodedBytes > 128<<20 {
				return nil, nil, fmt.Errorf("expanded shape data exceeds preview limit")
			}
			var list pb.ShapeInfoProtoList
			if proto.Unmarshal(b, &list) != nil {
				return nil, nil, fmt.Errorf("invalid shape metadata")
			}
			for _, s := range list.Proto {
				count++
				if count > 50000 {
					return nil, nil, fmt.Errorf("page exceeds preview shape limit")
				}
				if s.UniqueId == "" {
					return nil, nil, fmt.Errorf("shape identity missing")
				}
				old := winners[s.UniqueId]
				if old == nil || s.UpdatedAt > old.UpdatedAt {
					winners[s.UniqueId] = s
				} else if s.UpdatedAt == old.UpdatedAt && !proto.Equal(s, old) {
					return nil, nil, fmt.Errorf("ambiguous shape versions; preview withheld")
				}
			}
		}
	}
	layerOrder := map[int32]int{}
	hidden := map[int32]bool{}
	for i, l := range info.LayerList {
		layerOrder[l.ID] = i
		hidden[l.ID] = !l.Show
	}
	points := map[string]map[string][]TinyPoint{}
	totalPoints := 0
	ordered := make([]*pb.ShapeInfoProto, 0, len(winners))
	for _, sp := range winners {
		ordered = append(ordered, sp)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.CreatedAt != b.CreatedAt {
			return a.CreatedAt < b.CreatedAt
		}
		return a.UniqueId < b.UniqueId
	})
	for _, sp := range ordered {
		if sp.ShapeStatus != 0 || hidden[sp.Zorder] {
			continue
		}

		if sp.OptionsRepo != "" {
			var options struct{ Repo map[string]json.RawMessage }
			if json.Unmarshal([]byte(sp.OptionsRepo), &options) != nil {
				return nil, nil, fmt.Errorf("invalid shape options")
			}
			visible := true
			transparent := false
			if raw, ok := options.Repo["KEY_IS_VISIBLE"]; ok {
				if json.Unmarshal(raw, &visible) != nil {
					return nil, nil, fmt.Errorf("unsupported visibility option")
				}
			}
			if raw, ok := options.Repo["KEY_IS_TRANSPARENT"]; ok {
				if json.Unmarshal(raw, &transparent) != nil {
					return nil, nil, fmt.Errorf("unsupported transparency option")
				}
			}
			if !visible {
				continue
			}
			if transparent {
				warnings = append(warnings, "Transparent erasure compositing is not rendered yet.")
				continue
			}
		}
		if len(info.LayerList) > 0 {
			if _, ok := layerOrder[sp.Zorder]; !ok {
				warnings = append(warnings, "A shape references an unknown layer.")
			}
		}
		s := &Shape{UniqueID: sp.UniqueId, ShapeType: sp.ShapeType, Color: sp.Color, FillColor: sp.FillColor, Thickness: sp.Thickness, ZOrder: sp.Zorder, Text: sp.Text, RevisionID: sp.RevisionId}
		if sp.BoundingRect != "" {
			if json.Unmarshal([]byte(sp.BoundingRect), &s.BoundingRect) != nil {
				return nil, nil, fmt.Errorf("invalid shape bounds")
			}
		}
		if s.BoundingRect != nil {
			b := s.BoundingRect
			for _, v := range []float64{b.Left, b.Top, b.Right, b.Bottom} {
				if !finite(v) || math.Abs(v) > 1e6 {
					return nil, nil, fmt.Errorf("invalid shape bounds")
				}
			}
		}
		if sp.MatrixValues != "" {
			if json.Unmarshal([]byte(sp.MatrixValues), &s.MatrixValues) != nil {
				var wrapped struct{ Values []float64 }
				if json.Unmarshal([]byte(sp.MatrixValues), &wrapped) != nil {
					return nil, nil, fmt.Errorf("unsupported shape transform")
				}
				s.MatrixValues = wrapped.Values
			}
			if len(s.MatrixValues) != 9 {
				return nil, nil, fmt.Errorf("unsupported shape transform")
			}
			for _, v := range s.MatrixValues {
				if !finite(v) || math.Abs(v) > 1e7 {
					return nil, nil, fmt.Errorf("invalid shape transform")
				}
			}
			if s.MatrixValues[6] != 0 || s.MatrixValues[7] != 0 || s.MatrixValues[8] != 1 {
				return nil, nil, fmt.Errorf("perspective transform unsupported")
			}
		}
		if !finite(float64(s.Thickness)) || s.Thickness < 0 || s.Thickness > 10000 {
			return nil, nil, fmt.Errorf("invalid pen width")
		}
		if scribbleTypes[s.ShapeType] || s.ShapeType == 37 {
			if len(sp.PointList) > 0 {
				if (len(sp.PointList)-4)%16 != 0 {
					return nil, nil, fmt.Errorf("invalid inline points")
				}
				s.Points = decodeTinyPoints(sp.PointList)
			} else {
				if s.RevisionID == "" {
					return nil, nil, fmt.Errorf("stroke point reference missing")
				}
				key := "point/" + id + "#" + s.RevisionID + "#points"
				pt, ok := points[key]
				if !ok {
					b, e := load(key)
					if e != nil {
						return nil, nil, fmt.Errorf("stroke points have not arrived or failed verification")
					}
					pt, e = parsePointFile(b)
					if e != nil {
						return nil, nil, e
					}
					points[key] = pt
				}
				var exists bool
				s.Points, exists = pt[s.UniqueID]
				if !exists {
					return nil, nil, fmt.Errorf("stroke is missing from its point file")
				}
			}
			if s.ShapeType == 37 && len(s.Points)%2 != 0 {
				return nil, nil, fmt.Errorf("invalid fill rectangle points")
			}
			totalPoints += len(s.Points)
			if totalPoints > 500000 {
				return nil, nil, fmt.Errorf("page exceeds preview point limit")
			}
			if len(s.Points) == 0 {
				return nil, nil, fmt.Errorf("stroke contains no points")
			}
			for _, pt := range s.Points {
				if !finite(float64(pt.X)) || !finite(float64(pt.Y)) || math.Abs(float64(pt.X)) > 1e7 || math.Abs(float64(pt.Y)) > 1e7 {
					return nil, nil, fmt.Errorf("invalid point coordinate")
				}
			}
		} else if s.ShapeType == 2000 {
			var ref struct {
				ShapeReferenceBean struct {
					PageID, ShapeID string
					MatrixValues    []float64
				}
			}
			if json.Unmarshal([]byte(sp.ConnectionBean), &ref) != nil || ref.ShapeReferenceBean.PageID == "" || ref.ShapeReferenceBean.ShapeID == "" {
				warnings = append(warnings, "Reference shape target is unavailable.")
				continue
			}
			r := ref.ShapeReferenceBean
			var target *Shape
			if r.PageID == id {
				for _, candidate := range p.Shapes {
					if candidate.UniqueID == r.ShapeID {
						target = candidate
						break
					}
				}
			} else {
				page, _, err := shapePage(meta, r.PageID, keys, load, visiting)
				if err == nil {
					for _, candidate := range page.Shapes {
						if candidate.UniqueID == r.ShapeID {
							target = candidate
							break
						}
					}
				}
			}
			if target == nil {
				warnings = append(warnings, "Reference shape target could not be decoded.")
				continue
			}
			combined, err := composeAffine(r.MatrixValues, target.MatrixValues)
			if err != nil {
				warnings = append(warnings, err.Error())
				continue
			}
			copy := *target
			copy.UniqueID = s.UniqueID
			copy.ZOrder = s.ZOrder
			copy.MatrixValues = combined
			s = &copy
			totalPoints += len(s.Points)
			if totalPoints > 500000 {
				return nil, nil, fmt.Errorf("page exceeds preview point limit")
			}
			if s.Text != "" {
				p.Texts = append(p.Texts, s.Text)
			}
		} else if s.ShapeType == 6 || s.ShapeType == 16 {
			if len(s.Text) > 65536 || len(sp.RichText) > 1<<20 {
				return nil, nil, fmt.Errorf("text exceeds preview limit")
			}
			if s.Text == "" && sp.RichText != "" {
				s.Text = plainHTML(sp.RichText)
			}
			if s.Text != "" {
				p.Texts = append(p.Texts, s.Text)
			}
			if json.Unmarshal([]byte(sp.TextStyle), &s.TextStyle) != nil || !finite(s.TextStyle.TextSize) || s.TextStyle.TextSize <= 0 || s.TextStyle.TextSize > 1000 || !finite(s.TextStyle.TextSpacing) || s.TextStyle.TextSpacing < 0 || s.TextStyle.TextSpacing > 100 || s.TextStyle.Orientation != 0 || s.TextStyle.TextBorder != 0 || s.TextStyle.PaddingStart < 0 || s.TextStyle.PaddingEnd < 0 {
				warnings = append(warnings, "Text styling is unsupported; readable text is shown separately.")
				continue
			}
			p.Notices = append(p.Notices, "Text uses a substitute font; native typography and rich spans may differ.")
		} else if s.ShapeType == 29 {
			var extra struct{ TextContent string }
			_ = json.Unmarshal([]byte(sp.Extra), &extra)
			text := extra.TextContent
			if text == "" {
				var ref struct{ RelativePath string }
				if json.Unmarshal([]byte(sp.Resource), &ref) == nil && ref.RelativePath != "" {
					if b, e := load("resource/data/" + strings.TrimPrefix(ref.RelativePath, "/")); e == nil && len(b) <= 1<<20 {
						text = plainHTML(string(b))
					}
				}
			}
			if text != "" {
				p.Texts = append(p.Texts, text)
			}
			warnings = append(warnings, "Rich-text layout is not reproduced; available text is shown separately.")
			continue
		} else if s.ShapeType == 40 {
			features, err := parseUniversal(sp.Extra)
			if err != nil {
				warnings = append(warnings, err.Error())
				continue
			}
			s.Features = features
		} else if s.ShapeType == 19 {
			var ref struct{ RelativePath string }
			if json.Unmarshal([]byte(sp.Resource), &ref) != nil || ref.RelativePath == "" {
				return nil, nil, fmt.Errorf("image reference unavailable")
			}
			s.ImagePath = "resource/data/" + strings.TrimPrefix(ref.RelativePath, "/")
		} else if !geometricTypes[s.ShapeType] {
			if (s.ShapeType == 6 || s.ShapeType == 16 || s.ShapeType == 29) && s.Text != "" {
				p.Texts = append(p.Texts, s.Text)
			}
			warnings = append(warnings, fmt.Sprintf("Native shape type %d is not rendered yet.", s.ShapeType))
			continue
		}
		p.Shapes = append(p.Shapes, s)
	}
	sort.Slice(p.Shapes, func(i, j int) bool {
		a, b := p.Shapes[i], p.Shapes[j]
		if layerOrder[a.ZOrder] != layerOrder[b.ZOrder] {
			return layerOrder[a.ZOrder] < layerOrder[b.ZOrder]
		}
		aa, bb := winners[a.UniqueID], winners[b.UniqueID]
		if aa.CreatedAt != bb.CreatedAt {
			return aa.CreatedAt < bb.CreatedAt
		}
		return a.UniqueID < b.UniqueID
	})
	textBytes := 0
	for _, text := range p.Texts {
		textBytes += len(text)
	}
	if textBytes > 1<<20 {
		return nil, nil, fmt.Errorf("page text exceeds preview limit")
	}
	sort.Strings(p.Notices)
	p.Notices = compact(p.Notices)
	sort.Strings(warnings)
	warnings = compact(warnings)
	return p, warnings, nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func compact(s []string) []string {
	var out []string
	for _, v := range s {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}
