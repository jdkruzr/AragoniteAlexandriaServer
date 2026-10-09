package booxpage

import (
	"fmt"
	"github.com/fogleman/gg"
	"math"
	"strings"
)

// These are procedural equivalents of simple native stationery, not bundled
// firmware artwork. Unknown resource names remain explicit unsupported content.
func drawBuiltin(dc *gg.Context, name string) error {
	color := false
	if strings.HasPrefix(name, "cfa_") {
		color = true
		name = strings.TrimPrefix(name, "cfa_")
	}
	dc.Push()
	defer dc.Pop()
	dc.Scale(float64(dc.Width())/1650, float64(dc.Height())/2200)
	dc.SetRGB(.6, .6, .6)
	dc.SetLineWidth(2)
	line := func(x1, y1, x2, y2 float64) { dc.DrawLine(x1, y1, x2, y2); dc.Stroke() }
	var left, right, top, bottom, outerTop, outerBottom float64
	var intervals int
	switch name {
	case "new_scribble_back_ground_line_2_0":
		left, right, top, bottom, outerTop, outerBottom, intervals = 63.99, 1586.01, 81.03, 2119.01, 72.99, 2127.01, 20
	case "new_scribble_back_ground_line_1_6":
		left, right, top, bottom, outerTop, outerBottom, intervals = 63.99, 1586.01, 81.02, 2119.02, 72.98, 2127.02, 16
	case "new_scribble_back_ground_line":
		left, right, top, bottom, outerTop, outerBottom, intervals = 63.99, 1586.01, 81.01, 2119.01, 72.96, 2127.04, 12
	case "ic_horizontal_line_24":
		left, right, top, bottom, outerTop, outerBottom, intervals = 44.49, 1605.51, 39.17, 2160.83, 26.33, 2173.67, 24
	case "new_template_table_grid_v2":
		for x := 75.; x <= 1575; x += 100 {
			line(x, 50, x, 2150)
		}
		for y := 50.; y <= 2150; y += 100 {
			line(75, y, 1575, y)
		}
		return nil

	case "new_scribble_back_ground_grid_point", "new_scribble_back_ground_grid_point_v2":
		nx, ny := 25, 33
		left, right, top, bottom, radius := 66.98, 1583.02, 92., 2108., 2.99
		if strings.HasSuffix(name, "_v2") {
			nx, ny = 31, 42
			left, right, top, bottom, radius = 58.24, 1591.76, 52.09, 2147.91, 2
		}
		for y := 0; y < ny; y++ {
			for x := 0; x < nx; x++ {
				dc.DrawCircle(left+(right-left)*float64(x)/float64(nx-1), top+(bottom-top)*float64(y)/float64(ny-1), radius)
				dc.Fill()
			}
		}
		return nil
	case "new_template_table_grid":
		for i := 0; i <= 16; i++ {
			y := 83.99 + (2116-83.99)*float64(i)/16
			line(63.95, y, 1586.05, y)
		}
		for i := 0; i <= 12; i++ {
			x := 64.95 + (1585.05-64.95)*float64(i)/12
			line(x, 85.34, x, 2116)
		}
		return nil
	case "ic_to_do_list":
		dc.SetRGB(122./255, 122./255, 122./255)
		for i := 0; i <= 16; i++ {
			dc.DrawRectangle(44, 45.88+131.64*float64(i), 1562, 2)
			dc.Fill()
		}
		for i := 0; i < 16; i++ {
			dc.DrawRectangle(51.62, 60.8+131.64*float64(i), 103.79, 103.79)
			dc.Stroke()
		}
		return nil
	case "phone_template_grid":
		dc.Scale(1650./824, 2200./1648)
		dc.SetRGB(119./255, 119./255, 119./255)
		for x := 40.; x <= 784; x += 62 {
			dc.DrawRectangle(x, 40, 1, 1568)
			dc.Fill()
		}
		for i := 0; i < 26; i++ {
			y := 40 + math.Round(1567*float64(i)/25)
			dc.DrawRectangle(40, y, 744, 1)
			dc.Fill()
		}
		return nil
	default:
		return fmt.Errorf("This built-in template is not supported yet.")
	}
	if color {
		dc.SetRGB(1, 0, 0)
	}
	line(left, outerTop, right, outerTop)
	line(left, top, right, top)
	line(left, bottom, right, bottom)
	line(left, outerBottom, right, outerBottom)
	dc.SetRGB(.6, .6, .6)
	for i := 1; i < intervals; i++ {
		y := top + (bottom-top)*float64(i)/float64(intervals)
		line(left, y, right, y)
	}
	return nil
}
func drawProcedural(dc *gg.Context, kind string, spacing, left, top, right, bottom float64, color int32, width float64) error {
	if kind != "LayoutFittedHorizontalLine" && kind != "LayoutFittedVerticalLine" && kind != "LayoutFittedGrid" {
		return fmt.Errorf("This procedural template is not supported yet.")
	}
	for _, v := range []float64{spacing, left, top, right, bottom, width} {
		if !finite(v) || v < 0 || v > 1e5 {
			return fmt.Errorf("Invalid template geometry.")
		}
	}
	spacing = math.Max(1, spacing)
	width = math.Max(1, width)
	x2, y2 := float64(dc.Width())-right, float64(dc.Height())-bottom
	if x2 <= left || y2 <= top {
		return fmt.Errorf("Template margins exceed page dimensions.")
	}
	total := x2 - left
	if kind == "LayoutFittedHorizontalLine" {
		total = y2 - top
	}
	if n := math.Floor(total / spacing); n > 0 {
		spacing = total / n
	}
	if (x2-left+y2-top)/spacing > 10000 {
		return fmt.Errorf("Template exceeds preview limits.")
	}
	dc.Push()
	defer dc.Pop()
	r, g, b, a := decodeARGB(color)
	dc.SetRGBA(r, g, b, a)
	dc.SetLineWidth(width)
	line := func(x1, y1, x2, y2 float64) { dc.DrawLine(x1, y1, x2, y2); dc.Stroke() }
	if kind == "LayoutFittedGrid" && y2-top < x2-left {
		y2 -= math.Mod(y2-top, spacing)
	}
	if kind != "LayoutFittedHorizontalLine" {
		for x := left; x <= x2+1; x += spacing {
			line(x, top, x, y2)
		}
	}
	if kind != "LayoutFittedVerticalLine" {
		for y := top; y <= y2+1; y += spacing {
			line(left, y, x2, y)
		}
	}
	return nil
}
