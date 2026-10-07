package render

import (
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// The device addresses fonts by a /system/fonts basename (e.g. "Roboto-Regular.ttf")
// that the server does not have. Rather than ship and manage the tablet's font
// files, we fall back to the bundled Go faces — enough for the OCR/search payoff
// and a faithful-enough page preview. Only the weight is honored (regular vs bold);
// the specific family is not. (Mirrors the client's FontCatalog.resolve fallback
// intent: an absent face must still render, never blank the box.)
var (
	regularFont = mustParseFont(goregular.TTF)
	boldFont    = mustParseFont(gobold.TTF)
)

func mustParseFont(b []byte) *truetype.Font {
	f, err := truetype.Parse(b)
	if err != nil {
		// The bundled fonts are compiled-in constants; a parse failure is a build
		// problem, not a runtime input, so panicking surfaces it immediately.
		panic("forestrender: parse bundled font: " + err.Error())
	}
	return f
}

// faceFor returns a font face at sizePx pixels, bold when weight >= 700 (the
// client's bold threshold). sizePx is the box's font_size, already in the 1:1
// virtual-unit→pixel space the renderer draws in.
func faceFor(weight, sizePx int64) font.Face {
	f := regularFont
	if weight >= 700 {
		f = boldFont
	}
	if sizePx < 1 {
		sizePx = 1
	}
	return truetype.NewFace(f, &truetype.Options{Size: float64(sizePx)})
}
