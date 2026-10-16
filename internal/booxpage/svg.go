package booxpage

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/fogleman/gg"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

var svgNumber = regexp.MustCompile(`[-+]?(?:\d+\.?\d*|\.\d+)(?:[eE][-+]?\d+)?`)
var svgRule = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)

// Normalize a bounded, non-referencing SVG subset before the rasterizer. In
// particular, neither external resources nor recursive <use> graphs are allowed.
// Flatten class CSS because oksvg only reads style blocks inside defs.
func safeSVG(raw []byte) ([]byte, error) {
	if len(raw) > 1<<20 {
		return nil, fmt.Errorf("SVG exceeds preview limits")
	}
	d := xml.NewDecoder(bytes.NewReader(raw))
	var tokens []xml.Token
	classes := map[string]string{}
	// Clips that cover the whole canvas clip nothing; BOOX templates wrap
	// their drawing in one. Any other clip is refused below.
	noopClips := map[string]bool{}
	var canvasW, canvasH float64
	depth, count := 0, 0
	for {
		t, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		switch v := t.(type) {
		case xml.Directive:
			return nil, fmt.Errorf("SVG directives unsupported")
		case xml.StartElement:
			depth++
			count++
			if depth > 32 || count > 10000 {
				return nil, fmt.Errorf("SVG exceeds preview limits")
			}
			if v.Name.Local == "svg" && canvasW == 0 {
				canvasW, canvasH = svgCanvas(v)
			}
			if v.Name.Local == "clipPath" {
				id := ""
				for _, a := range v.Attr {
					if a.Name.Local == "id" {
						id = a.Value
					}
				}
				var clip struct {
					Inner []struct {
						XMLName   xml.Name
						X         string `xml:"x,attr"`
						Y         string `xml:"y,attr"`
						Width     string `xml:"width,attr"`
						Height    string `xml:"height,attr"`
						Transform string `xml:"transform,attr"`
					} `xml:",any"`
				}
				if d.DecodeElement(&clip, &v) != nil {
					return nil, fmt.Errorf("invalid SVG clip")
				}
				depth--
				if len(clip.Inner) == 1 && clip.Inner[0].XMLName.Local == "rect" && clip.Inner[0].Transform == "" && id != "" && canvasW > 0 {
					r := clip.Inner[0]
					x, y, w, h := svgFloat(r.X), svgFloat(r.Y), svgFloat(r.Width), svgFloat(r.Height)
					if x <= 0 && y <= 0 && x+w >= canvasW && y+h >= canvasH {
						noopClips[id] = true
						continue
					}
				}
				return nil, fmt.Errorf("SVG clipping unsupported")
			}
			switch v.Name.Local {
			case "svg", "g", "defs", "title", "desc", "line", "rect", "circle", "ellipse", "path", "polygon", "polyline", "style":
			default:
				return nil, fmt.Errorf("SVG element unsupported")
			}
			if v.Name.Local == "style" {
				var css string
				if d.DecodeElement(&css, &v) != nil {
					return nil, fmt.Errorf("invalid SVG style")
				}
				depth--
				matches := svgRule.FindAllStringSubmatch(css, -1)
				rest := svgRule.ReplaceAllString(css, "")
				if strings.TrimSpace(rest) != "" {
					return nil, fmt.Errorf("unsupported SVG CSS")
				}
				for _, m := range matches {
					for _, sel := range strings.Split(m[1], ",") {
						sel = strings.TrimSpace(sel)
						if !strings.HasPrefix(sel, ".") || strings.ContainsAny(sel, " >:+[") {
							return nil, fmt.Errorf("unsupported SVG selector")
						}
						classes[strings.TrimPrefix(sel, ".")] += m[2] + ";"
					}
				}
				continue
			}
			for _, a := range v.Attr {
				key := a.Name.Local
				if key == "clip-path" {
					if id, ok := strings.CutPrefix(strings.TrimSpace(a.Value), "url(#"); ok && noopClips[strings.TrimSuffix(id, ")")] {
						continue // dropped on output
					}
					return nil, fmt.Errorf("SVG clipping unsupported")
				}
				if key == "href" || strings.HasPrefix(strings.ToLower(key), "on") || strings.Contains(strings.ToLower(a.Value), "url(") {
					return nil, fmt.Errorf("SVG references unsupported")
				}
				if key == "id" || key == "class" || key == "xmlns" || key == "name" || key == "data-name" || key == "space" {
					continue
				}
				if strings.Contains(a.Value, "NaN") || strings.Contains(a.Value, "Inf") {
					return nil, fmt.Errorf("invalid SVG number")
				}
				for _, n := range svgNumber.FindAllString(a.Value, -1) {
					v, e := strconv.ParseFloat(n, 64)
					if e != nil || !finite(v) || v > 1e6 || v < -1e6 {
						return nil, fmt.Errorf("SVG coordinate exceeds limits")
					}
				}
			}
		case xml.EndElement:
			depth--
		}
		tokens = append(tokens, xml.CopyToken(t))
	}
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	for _, t := range tokens {
		if s, ok := t.(xml.StartElement); ok {
			var cls, style string
			attrs := []xml.Attr{}
			for _, a := range s.Attr {
				if a.Name.Local == "clip-path" {
					continue
				} else if a.Name.Local == "class" {
					cls = a.Value
				} else if a.Name.Local == "style" {
					style = a.Value
				} else {
					attrs = append(attrs, a)
				}
			}
			var combined string
			for _, c := range strings.Fields(cls) {
				v, exists := classes[c]
				if !exists {
					return nil, fmt.Errorf("undefined SVG class")
				}
				combined += v
			}
			combined += style
			if combined != "" {
				attrs = append(attrs, xml.Attr{Name: xml.Name{Local: "style"}, Value: combined})
			}
			s.Attr = attrs
			t = s
		}
		if e := enc.EncodeToken(t); e != nil {
			return nil, e
		}
	}
	if e := enc.Flush(); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}

// svgCanvas is the root's drawing area: its viewBox, else width and height.
func svgCanvas(root xml.StartElement) (float64, float64) {
	var w, h float64
	for _, a := range root.Attr {
		switch a.Name.Local {
		case "viewBox":
			if f := strings.Fields(strings.ReplaceAll(a.Value, ",", " ")); len(f) == 4 && svgFloat(f[0]) == 0 && svgFloat(f[1]) == 0 {
				return svgFloat(f[2]), svgFloat(f[3])
			}
		case "width":
			w = svgFloat(a.Value)
		case "height":
			h = svgFloat(a.Value)
		}
	}
	return w, h
}

func svgFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "px"), 64)
	if err != nil || !finite(v) {
		return 0
	}
	return v
}

func drawSVG(dc *gg.Context, b []byte) error {
	// Preserve the already-qualified simple ruled rasterization exactly.
	if drawLineSVG(dc, b) == nil {
		return nil
	}
	clean, e := safeSVG(b)
	if e != nil {
		return fmt.Errorf("SVG template is unsupported: %s", e)
	}
	icon, e := oksvg.ReadIconStream(bytes.NewReader(clean), oksvg.StrictErrorMode)
	if e != nil || icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 || !finite(icon.ViewBox.W) || !finite(icon.ViewBox.H) {
		return fmt.Errorf("SVG template cannot be rendered")
	}
	img := image.NewRGBA(image.Rect(0, 0, dc.Width(), dc.Height()))
	icon.SetTarget(0, 0, float64(dc.Width()), float64(dc.Height()))
	scan := rasterx.NewScannerGV(dc.Width(), dc.Height(), img, img.Bounds())
	icon.Draw(rasterx.NewDasher(dc.Width(), dc.Height(), scan), 1)
	dc.DrawImage(img, 0, 0)
	return nil
}
