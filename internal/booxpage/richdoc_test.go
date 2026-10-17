package booxpage

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A synthetic editor document with every node type and mark seen in real
// text notes (counted by `boox-coverage -richtext`, 2026-10-16).
const syntheticRichDoc = `{"content":{"type":"doc","content":[
{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Synthetic planning notes"}]},
{"type":"paragraph","content":[{"type":"text","marks":[{"type":"bold"}],"text":"Summary:"},{"type":"text","text":" a paragraph long enough to wrap across more than one line of the page so wrapping is visible, with "},{"type":"text","marks":[{"type":"italic"}],"text":"italic"},{"type":"text","text":" and "},{"type":"text","marks":[{"type":"textStyle","attrs":{"fontSize":"18.0pt","fontColor":"#1a5fb4"}}],"text":"smaller blue"},{"type":"text","text":" words."}]},
{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Follow-ups"}]},
{"type":"bulletList","content":[
 {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"First synthetic bullet"}]}]},
 {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Second bullet"}]},{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Nested bullet"}]}]}]}]}]},
{"type":"taskList","content":[
 {"type":"taskItem","attrs":{"checked":true},"content":[{"type":"paragraph","content":[{"type":"text","text":"Done item"}]}]},
 {"type":"taskItem","attrs":{"checked":false},"content":[{"type":"paragraph","content":[{"type":"text","text":"Open item"}]}]}]},
{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Numbered"}]}]}]},
{"type":"paragraph","content":[]},
{"type":"onyxImg","attrs":{"width":553,"height":420,"src":"synthetic"}},
{"type":"attach","attrs":{"title":"synthetic.txt","format":"txt"}},
{"type":"mysteryBlock","content":[{"type":"text","text":"Unknown block text"}]}
]},"docVersion":2}`

func TestComposeRichTextDrawsTypedTextUnderInk(t *testing.T) {
	ink := image.NewRGBA(image.Rect(0, 0, 1404, 1872))
	draw.Draw(ink, ink.Bounds(), image.White, image.Point{}, draw.Src)
	for x := 100; x < 1300; x++ { // a pen stroke across the text
		ink.Set(x, 300, color.Black)
	}
	img, notices := ComposeRichText(ink, [][]byte{[]byte(syntheticRichDoc)}, nil)
	if img.Bounds().Dx() != 1404 || img.Bounds().Dy() < 1872 {
		t.Fatalf("size %v", img.Bounds())
	}
	if len(notices) != 3 || notices[0] != RichTextDrawnNotice || notices[1] != RichTextImageNotice {
		t.Fatalf("notices %q", notices)
	}
	if r, _, _, _ := img.At(700, 300).RGBA(); r != 0 {
		t.Fatal("ink stroke not kept over the text")
	}
	dark := 0
	for y := 0; y < 400; y++ {
		for x := 0; x < 1404; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r < 0x8000 && y != 300 {
				dark++
			}
		}
	}
	if dark < 2000 {
		t.Fatalf("typed text not drawn (%d dark pixels)", dark)
	}
	if dir := os.Getenv("BOOXPAGE_RENDER_DIR"); dir != "" {
		f, _ := os.Create(filepath.Join(dir, "richtext.png"))
		_ = png.Encode(f, img)
		f.Close()
	}
	// Nothing typed: the ink comes back untouched.
	if same, n := ComposeRichText(ink, [][]byte{[]byte(`{"content":{"type":"doc","content":[]}}`)}, nil); same != image.Image(ink) || n != nil {
		t.Fatal("empty document changed the page")
	}
}

// A typed-text image is the note's resource/data file named like its
// dataKey (or src), or a data: URI; it's drawn in place, keeping its shape.
func TestComposeRichTextDrawsImages(t *testing.T) {
	pic := image.NewRGBA(image.Rect(0, 0, 200, 100))
	draw.Draw(pic, pic.Bounds(), &image.Uniform{color.RGBA{220, 0, 0, 255}}, image.Point{}, draw.Src)
	var buf bytes.Buffer
	if e := png.Encode(&buf, pic); e != nil {
		t.Fatal(e)
	}
	load := func(k string) ([]byte, error) {
		if k == "resource/data/pic.png" {
			return buf.Bytes(), nil
		}
		return nil, errors.New("missing")
	}
	ink := image.NewRGBA(image.Rect(0, 0, 1000, 1400))
	draw.Draw(ink, ink.Bounds(), image.White, image.Point{}, draw.Src)
	red := func(img image.Image) int {
		n := 0
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				if r, g, _, _ := img.At(x, y).RGBA(); r > 0xc000 && g < 0x2000 {
					n++
				}
			}
		}
		return n
	}
	for name, attrs := range map[string]string{
		"file":     `"src":"/storage/emulated/0/note/pic.png","dataKey":"https://oss.example/a/pic.png"`,
		"data URI": `"src":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(buf.Bytes()) + `"`,
	} {
		doc := `{"content":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Above"}]},{"type":"onyxImg","attrs":{"width":553,"height":420,` + attrs + `}}]},"docVersion":2}`
		img, notices := ComposeRichText(ink, [][]byte{[]byte(doc)}, load)
		if slices.Contains(notices, RichTextImageNotice) {
			t.Fatalf("%s: image reported missing %q", name, notices)
		}
		// Drawn across the text width (1000 - 2×6% margins = 880) at 2:1.
		if n := red(img); n < 880*440*9/10 {
			t.Fatalf("%s: image not drawn (%d red pixels)", name, n)
		}
	}
	// A reference that resolves to nothing stays a placeholder.
	doc := `{"content":{"type":"doc","content":[{"type":"onyxImg","attrs":{"src":"/x/gone.webp","dataKey":"gone.webp"}}]},"docVersion":2}`
	if img, notices := ComposeRichText(ink, [][]byte{[]byte(doc)}, load); !slices.Contains(notices, RichTextImageNotice) || red(img) != 0 {
		t.Fatal("missing image not reported", notices)
	}
}
