package booxpage

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/color"
	"math"

	"github.com/fogleman/gg"
)

// Pen colors note actions read, as the firmware stores them (ARGB).
const (
	InkRed  int32 = -40605    // 0xFFFF6163
	InkBlue int32 = -16733441 // 0xFF00AAFF
)

// inkMatches reports whether a shape is handwriting in the given pen color:
// a pen stroke (not a highlighter, text box or image) whose RGB matches
// exactly. Recovered strokes are left out because their color is a guess.
func inkMatches(s *Shape, argb int32) bool {
	return !s.Recovered && scribbleTypes[s.ShapeType] && s.ShapeType != shapeMarker &&
		s.ImagePath == "" && len(s.Points) > 0 && uint32(s.Color)&0xFFFFFF == uint32(argb)&0xFFFFFF
}

// InkShapes is the page's handwriting in one pen color, in drawing order.
func InkShapes(p *Page, argb int32) []*Shape {
	var out []*Shape
	for _, s := range p.Shapes {
		if inkMatches(s, argb) {
			out = append(out, s)
		}
	}
	return out
}

// InkHash identifies a set of strokes: it changes when one is added, erased
// or redrawn, and not when other ink on the page changes.
func InkHash(shapes []*Shape) string {
	h := sha256.New()
	h.Write([]byte("boox-ink-v1"))
	var b [16]byte
	for _, s := range shapes {
		h.Write([]byte("\x00" + s.UniqueID + "\x00" + s.RevisionID + "\x00"))
		for _, p := range s.Points {
			binary.LittleEndian.PutUint32(b[0:], math.Float32bits(p.X))
			binary.LittleEndian.PutUint32(b[4:], math.Float32bits(p.Y))
			h.Write(b[:8])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// InkImage draws only the given strokes, in black on white with no template,
// cropped to the ink with a margin. Nil when there is no ink.
func InkImage(p *Page, shapes []*Shape) image.Image {
	if len(shapes) == 0 || p.Width < 1 || p.Height < 1 {
		return nil
	}
	dc := gg.NewContext(int(p.Width), int(p.Height))
	dc.SetRGB(1, 1, 1)
	dc.Clear()
	for _, s := range shapes {
		c := *s
		c.Color = -16777216 // black reads best
		renderShape(dc, &c)
	}
	img := dc.Image()
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if r, g, bl, _ := img.At(x, y).RGBA(); r < 0xF000 || g < 0xF000 || bl < 0xF000 {
				minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < minX {
		return nil
	}
	const margin = 24
	crop := image.Rect(max(b.Min.X, minX-margin), max(b.Min.Y, minY-margin), min(b.Max.X, maxX+margin+1), min(b.Max.Y, maxY+margin+1))
	out := image.NewRGBA(image.Rect(0, 0, crop.Dx(), crop.Dy()))
	for y := 0; y < crop.Dy(); y++ {
		for x := 0; x < crop.Dx(); x++ {
			out.Set(x, y, color.RGBAModel.Convert(img.At(crop.Min.X+x, crop.Min.Y+y)))
		}
	}
	return out
}
