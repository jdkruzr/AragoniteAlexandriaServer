// Selectively ported from UltraBridge under Apache-2.0.
package booxpage

import (
	"math"

	"github.com/fogleman/gg"
)

// tp transforms a point through mat, or returns it unchanged if mat is nil.
func tp(mat *booxMatrix, x, y float64) (float64, float64) {
	if mat == nil {
		return x, y
	}
	return mat.transformPoint(x, y)
}

func renderGeometric(dc *gg.Context, s *Shape) {
	if s.BoundingRect == nil {
		return // no geometry data
	}

	mat := parseMatrix(s.MatrixValues)

	r, g, b, a := decodeARGB(s.Color)
	dc.SetRGBA(r, g, b, a)
	dc.SetLineWidth(float64(s.Thickness))
	dc.SetLineCap(gg.LineCapRound)

	br := s.BoundingRect
	x := br.Left
	y := br.Top
	w := br.Right - br.Left
	h := br.Bottom - br.Top

	switch s.ShapeType {
	case 0: // Circle — transform center and radii endpoints
		cx, cy := tp(mat, x+w/2, y+h/2)
		// Approximate radius by transforming edge points
		rx1, _ := tp(mat, x+w, y+h/2)
		_, ry1 := tp(mat, x+w/2, y+h)
		dc.DrawEllipse(cx, cy, rx1-cx, ry1-cy)
	case 1: // Rectangle — transform corners
		x0, y0 := tp(mat, x, y)
		x1, y1 := tp(mat, x+w, y)
		x2, y2 := tp(mat, x+w, y+h)
		x3, y3 := tp(mat, x, y+h)
		dc.MoveTo(x0, y0)
		dc.LineTo(x1, y1)
		dc.LineTo(x2, y2)
		dc.LineTo(x3, y3)
		dc.ClosePath()
	case 7: // Line
		lx0, ly0 := tp(mat, br.Left, br.Top)
		lx1, ly1 := tp(mat, br.Right, br.Bottom)
		dc.DrawLine(lx0, ly0, lx1, ly1)
	case 8: // Triangle
		tx0, ty0 := tp(mat, x+w/2, y)
		tx1, ty1 := tp(mat, x, y+h)
		tx2, ty2 := tp(mat, x+w, y+h)
		dc.MoveTo(tx0, ty0)
		dc.LineTo(tx1, ty1)
		dc.LineTo(tx2, ty2)
		dc.ClosePath()
	case 28: // Arrow line
		ax0, ay0 := tp(mat, br.Left, br.Top)
		ax1, ay1 := tp(mat, br.Right, br.Bottom)
		dc.DrawLine(ax0, ay0, ax1, ay1)
		angle := math.Atan2(ay1-ay0, ax1-ax0)
		headLen := math.Min(15, w/4)
		dc.MoveTo(ax1, ay1)
		dc.LineTo(
			ax1-headLen*math.Cos(angle-math.Pi/6),
			ay1-headLen*math.Sin(angle-math.Pi/6),
		)
		dc.MoveTo(ax1, ay1)
		dc.LineTo(
			ax1-headLen*math.Cos(angle+math.Pi/6),
			ay1-headLen*math.Sin(angle+math.Pi/6),
		)
	default:
		// Fallback to bounding rect outline
		fx0, fy0 := tp(mat, x, y)
		fx1, fy1 := tp(mat, x+w, y)
		fx2, fy2 := tp(mat, x+w, y+h)
		fx3, fy3 := tp(mat, x, y+h)
		dc.MoveTo(fx0, fy0)
		dc.LineTo(fx1, fy1)
		dc.LineTo(fx2, fy2)
		dc.LineTo(fx3, fy3)
		dc.ClosePath()
	}

	// Fill if fillColor is non-zero, otherwise stroke.
	if s.FillColor != 0 {
		fr, fg, fb, fa := decodeARGB(s.FillColor)
		dc.SetRGBA(fr, fg, fb, fa)
		dc.FillPreserve()
		dc.SetRGBA(r, g, b, a)
	}
	dc.Stroke()
}
