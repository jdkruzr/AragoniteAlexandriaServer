// Selectively ported from UltraBridge under Apache-2.0.
package booxpage

import (
	"math"

	"github.com/fogleman/gg"
)

// Scribble shape types that contain stroke data (from BOOX_STROKE_FORMAT.md).
var scribbleTypes = map[int32]bool{
	2: true, 3: true, 4: true, 5: true, 15: true,
	21: true, 22: true, 47: true, 60: true, 61: true,
}

// Geometric shape types rendered from bounding rect.
var geometricTypes = map[int32]bool{
	0: true, 1: true, 7: true, 8: true, 28: true,
}

func renderShape(dc *gg.Context, s *Shape) {
	if s.ShapeType == 37 {
		renderFill(dc, s)
	} else if s.ShapeType == 40 {
		renderUniversal(dc, s)
	} else if scribbleTypes[s.ShapeType] {
		renderScribble(dc, s)
	} else if geometricTypes[s.ShapeType] {
		renderGeometric(dc, s)
	}
	// ShapePage classifies unsupported types; Preview separately composes images.
}

// renderScribble draws a pressure-sensitive stroke from point data.
func renderScribble(dc *gg.Context, s *Shape) {
	if len(s.Points) == 0 {
		return // AC2.7: skip shapes with empty/insufficient point data
	}

	mat := parseMatrix(s.MatrixValues)

	r, g, b, a := decodeARGB(s.Color)
	ps := getPenStyle(s.ShapeType)

	dc.SetRGBA(r, g, b, a*ps.AlphaMultiplier)
	dc.SetLineCap(gg.LineCapRound)
	dc.SetLineJoin(gg.LineJoinRound)

	if len(s.Points) == 1 {
		p := s.Points[0]
		x, y := float64(p.X), float64(p.Y)
		if mat != nil {
			x, y = mat.transformPoint(x, y)
		}
		dc.DrawCircle(x, y, pressureToWidth(float64(p.Pressure), float64(s.Thickness), ps)/2)
		dc.Fill()
		return
	}
	// Draw segment-by-segment with per-segment width from pressure.
	for i := 0; i < len(s.Points)-1; i++ {
		p0 := s.Points[i]
		p1 := s.Points[i+1]

		x0, y0 := float64(p0.X), float64(p0.Y)
		x1, y1 := float64(p1.X), float64(p1.Y)
		if mat != nil {
			x0, y0 = mat.transformPoint(x0, y0)
			x1, y1 = mat.transformPoint(x1, y1)
		}

		pressure := (float64(p0.Pressure) + float64(p1.Pressure)) / 2.0
		width := pressureToWidth(pressure, float64(s.Thickness), ps)

		dc.SetLineWidth(width)
		dc.MoveTo(x0, y0)
		dc.LineTo(x1, y1)
		dc.Stroke()
	}
}

// pressureToWidth maps pressure (0-4095 typical EMR range) to pixel width, modulated by pen type.
func pressureToWidth(pressure, thickness float64, ps penStyle) float64 {
	// Normalize pressure to 0-1 range.
	normalized := math.Max(0, math.Min(pressure/4095.0, 1.0))
	// Apply pen-type curve: different pens respond differently to pressure.
	curved := math.Pow(normalized, ps.PressureExponent)
	// Scale by base thickness and pen width range.
	width := ps.MinWidthFactor*thickness + curved*(ps.MaxWidthFactor-ps.MinWidthFactor)*thickness
	return math.Max(width, 0.5) // minimum visible width
}

// Native fills store pairs of opposite corners, not pressure-sensitive ink.
func renderFill(dc *gg.Context, s *Shape) {
	dc.Push()
	defer dc.Pop()
	dc.ClearPath()
	mat := parseMatrix(s.MatrixValues)
	for i := 0; i+1 < len(s.Points); i += 2 {
		a, b := s.Points[i], s.Points[i+1]
		for j, pt := range [][2]float64{{float64(a.X), float64(a.Y)}, {float64(b.X), float64(a.Y)}, {float64(b.X), float64(b.Y)}, {float64(a.X), float64(b.Y)}} {
			x, y := tp(mat, pt[0], pt[1])
			if j == 0 {
				dc.MoveTo(x, y)
			} else {
				dc.LineTo(x, y)
			}
		}
		dc.ClosePath()
	}
	r, g, b, a := decodeARGB(s.Color)
	dc.SetRGBA(r, g, b, a)
	dc.FillPreserve()
	dc.SetLineWidth(float64(s.Thickness))
	dc.Stroke()
}
