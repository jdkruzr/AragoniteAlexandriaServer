package boox

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"strings"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// The library tile BOOX shows for a notebook is the PNG stored beside it. The
// Notes app draws one when a note is closed on the device; a note written by
// the server has never been open there, so it ships its own: the title and as
// much of the opening text as fits, like a page seen from across the room.
const thumbWidth, thumbHeight = 412, 686

var thumbRegular, _ = truetype.Parse(goregular.TTF)
var thumbBold, _ = truetype.Parse(gobold.TTF)

func textThumbnail(title string, blocks []TextBlock) ([]byte, error) {
	dc := gg.NewContext(thumbWidth, thumbHeight)
	dc.SetRGB(1, 1, 1)
	dc.Clear()
	dc.SetRGB(0, 0, 0)
	const margin = 24.0
	width := thumbWidth - 2*margin
	y := margin
	// line draws wrapped text and reports whether there was room for all of it.
	line := func(text string, bold bool, size, after float64, maxLines int) bool {
		f := thumbRegular
		if bold {
			f = thumbBold
		}
		face := truetype.NewFace(f, &truetype.Options{Size: size})
		defer face.Close()
		dc.SetFontFace(face)
		height := size * 1.3
		lines := dc.WordWrap(strings.Join(strings.Fields(text), " "), width)
		for i, l := range lines {
			if y+height > thumbHeight-margin || (maxLines > 0 && i == maxLines) {
				return false
			}
			dc.DrawString(l, margin, y+size)
			y += height
		}
		y += after
		return true
	}
	if !line(title, true, 26, 14, 3) {
		return encodeThumb(dc)
	}
	for _, b := range blocks {
		text := strings.TrimSpace(b.Label + " " + b.Text)
		if text == "" {
			continue
		}
		ok := true
		switch b.Kind {
		case "heading":
			ok = line(text, true, 19, 6, 2)
		case "bullet":
			ok = line("• "+text, false, 16, 4, 0)
		default:
			ok = line(text, false, 16, 8, 0)
		}
		if !ok {
			break
		}
	}
	return encodeThumb(dc)
}

func encodeThumb(dc *gg.Context) ([]byte, error) {
	gray := image.NewGray(image.Rect(0, 0, thumbWidth, thumbHeight))
	draw.Draw(gray, gray.Bounds(), dc.Image(), image.Point{}, draw.Src)
	var out bytes.Buffer
	if e := png.Encode(&out, gray); e != nil {
		return nil, e
	}
	return out.Bytes(), nil
}
