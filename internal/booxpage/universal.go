package booxpage

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/fogleman/gg"
)

type feature struct {
	Geometry struct {
		Type        string
		Coordinates json.RawMessage
		Features    []feature
	}
	Properties struct {
		WaveAttr   struct{ WavyLength, WavyPeak, WavyOffset float64 }
		SubType    string
		Radius     float64
		StrokeAttr struct {
			Color       int32
			Width       float64
			EnableColor bool
		}
		FillAttr struct {
			Color       int32
			EnableColor bool
		}
		LineStyle struct {
			Type              int
			DashLineIntervals []float64
			Phase             float64
		}
	}
}
type featurePath struct {
	Points           [][]float64
	Closed, Curve    bool
	Stroke, Fill     int32
	Width            float64
	Dash             []float64
	Phase            float64
	DoStroke, DoFill bool
}

func parseUniversal(raw string) ([]featurePath, error) {
	var extra struct{ FeatureCollection string }
	if len(raw) > 2<<20 || json.Unmarshal([]byte(raw), &extra) != nil {
		return nil, fmt.Errorf("Invalid universal shape.")
	}
	var fc struct{ Features []feature }
	if json.Unmarshal([]byte(extra.FeatureCollection), &fc) != nil || len(fc.Features) > 1000 {
		return nil, fmt.Errorf("Invalid universal shape features.")
	}
	if len(fc.Features) == 0 {
		return nil, fmt.Errorf("Universal shape has no readable features.")
	}
	budget := 100000
	return parseFeatures(fc.Features, 0, &budget)
}
func parseFeatures(features []feature, depth int, budget *int) ([]featurePath, error) {
	if depth > 8 || len(features) > 1000 {
		return nil, fmt.Errorf("Universal feature nesting exceeds limits.")
	}
	var out []featurePath
	count := 0
	for _, f := range features {
		p := f.Properties
		if p.Radius != 0 {
			return nil, fmt.Errorf("Rounded universal geometry is not rendered yet.")
		}
		path := featurePath{Stroke: p.StrokeAttr.Color, Fill: p.FillAttr.Color, Width: p.StrokeAttr.Width, DoStroke: p.StrokeAttr.EnableColor, DoFill: p.FillAttr.EnableColor, Dash: p.LineStyle.DashLineIntervals, Phase: p.LineStyle.Phase}
		for _, v := range append([]float64{path.Width, path.Phase}, path.Dash...) {
			if !finite(v) || v < 0 || v > 10000 {
				return nil, fmt.Errorf("Invalid universal line style.")
			}
		}
		if path.Phase != 0 {
			return nil, fmt.Errorf("Universal dash phase is not rendered yet.")
		}
		if len(path.Dash) > 32 {
			return nil, fmt.Errorf("Universal line style exceeds limits.")
		}
		for _, v := range path.Dash {
			if v == 0 {
				return nil, fmt.Errorf("Invalid universal dash.")
			}
		}
		if p.LineStyle.Type != 0 && p.LineStyle.Type != 1 {
			return nil, fmt.Errorf("Unsupported universal line style.")
		}

		if f.Geometry.Type == "FeatureCollection" && p.SubType == "Surface" {
			children, e := parseFeatures(f.Geometry.Features, depth+1, budget)
			if e != nil {
				return nil, e
			}
			out = append(out, children...)
			if path.DoFill {
				path.DoStroke = false
				path.Closed = true
				for _, child := range f.Geometry.Features {
					var pts [][]float64
					if json.Unmarshal(child.Geometry.Coordinates, &pts) != nil || len(pts) == 0 || len(pts[0]) != 2 {
						return nil, fmt.Errorf("Unsupported surface anchor geometry.")
					}
					path.Points = append(path.Points, pts[0])
				}
				out = append(out, path)
			}
			continue
		}
		var pts [][]float64
		var edges [][][]float64
		switch f.Geometry.Type {
		case "LineString", "MultiPoint", "DirectionLine", "BidirectionalLine":
			if json.Unmarshal(f.Geometry.Coordinates, &pts) != nil {
				return nil, fmt.Errorf("Invalid universal coordinates.")
			}
		case "Polygon", "MultiLineString":
			if json.Unmarshal(f.Geometry.Coordinates, &edges) != nil {
				return nil, fmt.Errorf("Invalid universal edges.")
			}
			for _, edge := range edges {
				pts = append(pts, edge...)
			}
		default:
			return nil, fmt.Errorf("Universal geometry %s is not rendered yet.", f.Geometry.Type)
		}
		for _, pt := range pts {
			if len(pt) != 2 || !finite(pt[0]) || !finite(pt[1]) || math.Abs(pt[0]) > 1e6 || math.Abs(pt[1]) > 1e6 {
				return nil, fmt.Errorf("Invalid universal point.")
			}
		}
		count += len(pts)
		if count > 10000 {
			return nil, fmt.Errorf("Universal shape exceeds limits.")
		}
		switch p.SubType {
		case "":
			if f.Geometry.Type == "MultiPoint" {
				continue
			} // Native move-only selection points have no visible path.
			path.Points = pts
			path.Closed = f.Geometry.Type == "Polygon"
			if f.Geometry.Type == "DirectionLine" || f.Geometry.Type == "BidirectionalLine" {
				if len(pts) != 2 {
					return nil, fmt.Errorf("Invalid arrow coordinates.")
				}
				head := func(a, b []float64) {
					dx, dy := b[0]-a[0], b[1]-a[1]
					l := math.Hypot(dx, dy)
					if l == 0 {
						return
					}
					ux, uy := dx/l, dy/l
					for _, sign := range []float64{-1, 1} {
						hp := path
						hp.Points = [][]float64{b, {b[0] - 20*ux - sign*14*uy, b[1] - 20*uy + sign*14*ux}}
						out = append(out, hp)
					}
				}
				head(pts[0], pts[1])
				if f.Geometry.Type == "BidirectionalLine" {
					head(pts[1], pts[0])
				}
			}

		case "Rectangle", "Oval", "Arc":
			need := 2
			if p.SubType == "Arc" {
				need = 3
			}
			if len(pts) != need {
				return nil, fmt.Errorf("Invalid universal bounds.")
			}
			x, y, x2, y2 := pts[0][0], pts[0][1], pts[1][0], pts[1][1]
			if x2 < x || y2 < y {
				return nil, fmt.Errorf("Invalid universal bounds.")
			}
			if p.SubType == "Rectangle" {
				path.Points = [][]float64{{x, y}, {x2, y}, {x2, y2}, {x, y2}}
				path.Closed = true
			} else {
				start, sweep := 0., 360.
				if p.SubType == "Arc" {
					start, sweep = pts[2][0], pts[2][1]
					if math.Abs(sweep) > 360 {
						return nil, fmt.Errorf("Unsupported arc sweep.")
					}
				}
				n := max(2, int(math.Ceil(math.Abs(sweep)/3)))
				for i := 0; i <= n; i++ {
					a := (start + sweep*float64(i)/float64(n)) * math.Pi / 180
					path.Points = append(path.Points, []float64{(x+x2)/2 + (x2-x)/2*math.Cos(a), (y+y2)/2 + (y2-y)/2*math.Sin(a)})
				}
				path.Closed = p.SubType == "Oval"
			}
		case "WaveLine":
			wave := p.WaveAttr
			if len(pts) < 2 || !finite(wave.WavyLength) || !finite(wave.WavyPeak) || !finite(wave.WavyOffset) || wave.WavyLength < 1 || math.Abs(wave.WavyPeak) > 10000 || math.Abs(wave.WavyOffset) > 10000 {
				return nil, fmt.Errorf("Invalid wave geometry.")
			}
			a, b := pts[0], pts[len(pts)-1]
			dx, dy := b[0]-a[0], b[1]-a[1]
			length := math.Hypot(dx, dy)
			if length == 0 {
				continue
			}
			steps := max(2, int(math.Ceil(length/wave.WavyLength))*180)
			if steps > 10000 {
				return nil, fmt.Errorf("Wave exceeds preview limits.")
			}
			ux, uy := dx/length, dy/length
			for i := 0; i <= steps; i++ {
				d := length * float64(i) / float64(steps)
				h := math.Sin(2*math.Pi*d/wave.WavyLength+wave.WavyOffset) * wave.WavyPeak
				path.Points = append(path.Points, []float64{a[0] + d*ux - h*uy, a[1] + d*uy + h*ux})
			}
		case "Curve":
			if len(pts) != 3 {
				return nil, fmt.Errorf("Invalid curve controls.")
			}
			path.Points = pts
			path.Curve = true
		default:
			return nil, fmt.Errorf("Universal %s geometry is not rendered yet.", p.SubType)
		}
		if f.Geometry.Type == "MultiLineString" {
			for _, edge := range edges {
				ep := path
				ep.Points = edge
				out = append(out, ep)
			}
		} else {
			out = append(out, path)
		}
	}
	for _, path := range out {
		*budget -= len(path.Points)
		if *budget < 0 {
			return nil, fmt.Errorf("Universal generated geometry exceeds limits.")
		}
	}
	return out, nil
}
func renderUniversal(dc *gg.Context, s *Shape) {
	mat := parseMatrix(s.MatrixValues)
	for _, p := range s.Features {
		if len(p.Points) == 0 {
			continue
		}
		dc.Push()
		dc.ClearPath()
		dc.SetDash(p.Dash...)
		// Nonzero dash phase is rejected at decode time.
		scale := 1.
		if len(s.MatrixValues) == 9 {
			m := s.MatrixValues
			scale = (math.Hypot(m[0], m[3]) + math.Hypot(m[1], m[4])) / 2
		}
		dc.SetLineWidth(p.Width * scale)
		x, y := tp(mat, p.Points[0][0], p.Points[0][1])
		dc.MoveTo(x, y)
		if p.Curve {
			x1, y1 := tp(mat, p.Points[1][0], p.Points[1][1])
			x2, y2 := tp(mat, p.Points[2][0], p.Points[2][1])
			dc.QuadraticTo(x1, y1, x2, y2)
		} else {
			for _, v := range p.Points[1:] {
				x, y := tp(mat, v[0], v[1])
				dc.LineTo(x, y)
			}
		}
		if p.Closed {
			dc.ClosePath()
		}
		if p.DoFill {
			r, g, b, a := decodeARGB(p.Fill)
			dc.SetRGBA(r, g, b, a)
			dc.FillPreserve()
		}
		if p.DoStroke {
			r, g, b, a := decodeARGB(p.Stroke)
			dc.SetRGBA(r, g, b, a)
			dc.Stroke()
		} else {
			dc.ClearPath()
		}
		dc.Pop()
	}
}
