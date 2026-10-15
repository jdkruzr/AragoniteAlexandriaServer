package boox

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

// A server-written note's tile shows its words, not a blank page, and stays
// within the tile even when the text runs long.
func TestTextThumbnailShowsTheText(t *testing.T) {
	blocks := []TextBlock{{Kind: "heading", Text: "Summary"}, {Kind: "paragraph", Text: "A synthetic meeting about synthetic things."}}
	for i := 0; i < 80; i++ {
		blocks = append(blocks, TextBlock{Kind: "bullet", Label: "Point:", Text: "a fairly long synthetic bullet that wraps across more than one line of the tile"})
	}
	raw, e := textThumbnail("20261015 Synthetic Test_Minutes", blocks)
	if e != nil {
		t.Fatal(e)
	}
	img, e := png.Decode(bytes.NewReader(raw))
	if e != nil {
		t.Fatal(e)
	}
	if b := img.Bounds(); b.Dx() != thumbWidth || b.Dy() != thumbHeight {
		t.Fatalf("bounds %v", b)
	}
	ink, bottom := 0, 0
	for y := 0; y < thumbHeight; y++ {
		for x := 0; x < thumbWidth; x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r < 0x8000 {
				ink++
				bottom = y
			}
		}
	}
	if ink < 2000 {
		t.Errorf("only %d dark pixels; the tile is nearly blank", ink)
	}
	if bottom >= thumbHeight-20 {
		t.Errorf("text reaches row %d, into the bottom margin", bottom)
	}
	if out := os.Getenv("ALEXANDRIA_THUMB_OUT"); out != "" {
		_ = os.WriteFile(out, raw, 0o644)
	}
}
