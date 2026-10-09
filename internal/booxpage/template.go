package booxpage

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"strconv"
	"strings"

	"github.com/fogleman/gg"
	"golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

// Preview always reports renderer limitations. Templates are rasterized here;
// untrusted SVG never reaches the browser and cannot reference external URLs.
func Preview(p *Page, load Load) (image.Image, []string, error) {
	dc := gg.NewContext(int(p.Width), int(p.Height))
	dc.SetRGB(1, 1, 1)
	dc.Clear()
	var warnings []string
	if err := drawBackground(dc, p, load); err != nil {
		warnings = append(warnings, err.Error())
	}
	for _, s := range p.Shapes {
		if s.ShapeType == 6 || s.ShapeType == 16 {
			if e := drawTextShape(dc, s); e != nil {
				warnings = append(warnings, e.Error())
			}
		} else if s.ImagePath != "" {
			if e := drawImageShape(dc, s, load); e != nil {
				warnings = append(warnings, e.Error())
			}
		} else {
			renderShape(dc, s)
		}
	}
	return dc.Image(), warnings, nil
}
func drawTemplate(dc *gg.Context, raw []byte, load Load) error {
	var t struct {
		DisplayFillColor int32
		Properties       struct {
			LayoutType                  string
			UseFixedRatio, UnderContent bool
			PageMargins                 struct{ Degree, PaddingBottom, PaddingLeft, PaddingRight, PaddingTop, Spacing float64 }
			ResourceAttr                struct{ ResName, AssetsResName string }
			StrokeAttr                  struct {
				Color int32
				Width float64
			}
			ImageAttr struct{ RelativePath string }
		}
	}
	if json.Unmarshal(raw, &t) != nil {
		return fmt.Errorf("Template metadata is unreadable.")
	}
	m := t.Properties.PageMargins
	if m.Degree != 0 || t.Properties.UseFixedRatio {
		return fmt.Errorf("This template layout is not supported yet.")
	}
	if t.DisplayFillColor != 0 {
		r, g, b, a := decodeARGB(t.DisplayFillColor)
		dc.SetRGBA(r, g, b, a)
		dc.DrawRectangle(0, 0, float64(dc.Width()), float64(dc.Height()))
		dc.Fill()
	}
	path := t.Properties.ImageAttr.RelativePath
	if path == "" {
		if t.Properties.LayoutType == "LayoutResVector" {
			name := t.Properties.ResourceAttr.ResName
			if t.Properties.ResourceAttr.AssetsResName != "" {
				name = t.Properties.ResourceAttr.AssetsResName
			}
			name = name[strings.LastIndex(name, "/")+1:]
			if name == "" {
				return nil
			} // Native empty resource selection is plain paper.
			if b, e := load("note/templateRes/" + name + ".svg"); e == nil {
				return drawSVG(dc, b)
			}
			return drawBuiltin(dc, name)
		}
		if strings.HasPrefix(t.Properties.LayoutType, "LayoutFitted") {
			return drawProcedural(dc, t.Properties.LayoutType, m.Spacing, m.PaddingLeft, m.PaddingTop, m.PaddingRight, m.PaddingBottom, t.Properties.StrokeAttr.Color, t.Properties.StrokeAttr.Width)
		}
		if t.Properties.LayoutType != "" && t.Properties.LayoutType != "LayoutBlank" {
			return fmt.Errorf("This procedural template is not supported yet.")
		}
		return nil
	}
	if m.PaddingLeft != 0 || m.PaddingRight != 0 || m.PaddingTop != 0 || m.PaddingBottom != 0 {
		return fmt.Errorf("This image template margin layout is not supported yet.")
	}
	b, e := load(path)
	if e != nil {
		return fmt.Errorf("The template image has not arrived or failed verification.")
	}
	if strings.HasSuffix(strings.ToLower(path), ".svg") {
		return drawSVG(dc, b)
	}
	config, _, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 20e6 {
		return fmt.Errorf("Template image format or size is unsupported.")
	}
	img, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return fmt.Errorf("Template image is unreadable.")
	}
	dc.Push()
	defer dc.Pop()
	dc.Scale(float64(dc.Width())/float64(config.Width), float64(dc.Height())/float64(config.Height))
	dc.DrawImage(img, 0, 0)
	return nil
}

// The first qualified vector subset is Onyx's plain ruled SVG. Reject other
// constructs explicitly instead of drawing a deceptively blank background.
func drawLineSVG(dc *gg.Context, b []byte) error {
	type line struct{ x1, y1, x2, y2 float64 }
	var lines []line
	var width, height float64
	styleOK := false
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		token, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fmt.Errorf("Template SVG is unreadable.")
		}
		switch t := token.(type) {
		case xml.Directive:
			return fmt.Errorf("Template SVG directives are unsupported.")
		case xml.StartElement:
			attrs := map[string]string{}
			for _, a := range t.Attr {
				attrs[a.Name.Local] = a.Value
			}
			switch t.Name.Local {
			case "svg":
				v := strings.Fields(attrs["viewBox"])
				if len(v) != 4 || v[0] != "0" || v[1] != "0" {
					return fmt.Errorf("Template SVG viewport is unsupported.")
				}
				width, _ = strconv.ParseFloat(v[2], 64)
				height, _ = strconv.ParseFloat(v[3], 64)
			case "defs", "title":
			case "style":
				var css string
				if d.DecodeElement(&css, &t) != nil {
					return fmt.Errorf("Template style is unreadable.")
				}
				css = strings.Join(strings.Fields(css), "")
				styleOK = css == ".cls-1{fill:none;stroke:#999;stroke-miterlimit:10;stroke-width:2px;}"
			case "line":
				if attrs["class"] != "cls-1" || len(attrs) != 5 {
					return fmt.Errorf("Template line style is unsupported.")
				}
				coords := []float64{}
				for _, key := range []string{"x1", "y1", "x2", "y2"} {
					v, e := strconv.ParseFloat(attrs[key], 64)
					if e != nil || !finite(v) || v < 0 || v > 1e6 {
						return fmt.Errorf("Template line coordinate is unsupported.")
					}
					coords = append(coords, v)
				}
				lines = append(lines, line{coords[0], coords[1], coords[2], coords[3]})
				if len(lines) > 10000 {
					return fmt.Errorf("Template exceeds preview limits.")
				}
			default:
				return fmt.Errorf("This SVG template is not supported yet.")
			}
		}
	}
	if !styleOK || !finite(width) || !finite(height) || width <= 0 || height <= 0 {
		return fmt.Errorf("This SVG template style is not supported yet.")
	}
	dc.Push()
	defer dc.Pop()
	dc.Scale(float64(dc.Width())/width, float64(dc.Height())/height)
	dc.SetRGB(.6, .6, .6)
	dc.SetLineWidth(2)
	for _, l := range lines {
		dc.DrawLine(l.x1, l.y1, l.x2, l.y2)
		dc.Stroke()
	}
	return nil
}

func drawImageShape(dc *gg.Context, s *Shape, load Load) error {
	if s.BoundingRect == nil {
		return fmt.Errorf("Image placement is unavailable.")
	}
	b := s.BoundingRect
	if b.Right <= b.Left || b.Bottom <= b.Top {
		return fmt.Errorf("Image bounds are invalid.")
	}
	raw, e := load(s.ImagePath)
	if e != nil {
		return fmt.Errorf("An image has not arrived or failed verification.")
	}
	c, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || c.Width <= 0 || c.Height <= 0 || int64(c.Width)*int64(c.Height) > 20e6 {
		return fmt.Errorf("Image format or size is unsupported.")
	}
	img, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return fmt.Errorf("Image is unreadable.")
	}
	return placeImage(dc, s, img)
}
func placeImage(dc *gg.Context, s *Shape, img image.Image) error {
	b := s.BoundingRect
	c := img.Bounds()
	dc.Push()
	defer dc.Pop()
	sx, sy := (b.Right-b.Left)/float64(c.Dx()), (b.Bottom-b.Top)/float64(c.Dy())
	mat := f64.Aff3{sx, 0, b.Left, 0, sy, b.Top}
	if len(s.MatrixValues) > 0 {
		m := s.MatrixValues
		if len(m) != 9 {
			return fmt.Errorf("Invalid image transform.")
		}
		mat = f64.Aff3{m[0] * sx, m[1] * sy, m[0]*b.Left + m[1]*b.Top + m[2], m[3] * sx, m[4] * sy, m[3]*b.Left + m[4]*b.Top + m[5]}
	}
	dst, ok := dc.Image().(draw.Image)
	if !ok {
		return fmt.Errorf("Image compositor unavailable.")
	}
	draw.BiLinear.Transform(dst, mat, img, img.Bounds(), draw.Over, nil)
	return nil
}
