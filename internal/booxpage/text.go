package booxpage

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/net/html"
)

type textStyle struct {
	TextSize, TextSpacing                                        float64
	TextBold, TextItalic, TextUnderline                          bool
	AlignType, Orientation, TextBorder, PaddingStart, PaddingEnd int
}

var regularTextFont, _ = truetype.Parse(goregular.TTF)
var boldTextFont, _ = truetype.Parse(gobold.TTF)

// HTML is only parsed into text, never executed or delivered as trusted markup.
func plainHTML(raw string) string {
	t := html.NewTokenizer(strings.NewReader(raw))
	var out strings.Builder
	skip := 0
	for {
		typ := t.Next()
		if typ == html.ErrorToken {
			break
		}
		switch typ {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := t.TagName()
			n := string(name)
			if n == "script" || n == "style" {
				skip++
			}
			if skip == 0 && (n == "br" || n == "p" || n == "div" || n == "li") {
				out.WriteByte('\n')
			}
		case html.EndTagToken:
			name, _ := t.TagName()
			n := string(name)
			if (n == "script" || n == "style") && skip > 0 {
				skip--
			}
			if skip == 0 && (n == "p" || n == "div" || n == "li") {
				out.WriteByte('\n')
			}
		case html.TextToken:
			if skip == 0 {
				out.Write(t.Text())
			}
		}
	}
	return strings.TrimSpace(out.String())
}
func drawTextShape(dc *gg.Context, s *Shape) error {
	st := s.TextStyle
	b := s.BoundingRect
	if b == nil || b.Right <= b.Left || b.Bottom <= b.Top {
		return fmt.Errorf("Text placement unavailable.")
	}
	w, h := int(math.Ceil(b.Right-b.Left)), int(math.Ceil(b.Bottom-b.Top))
	if w > 8192 || h > 8192 || int64(w)*int64(h) > 20e6 {
		return fmt.Errorf("Text exceeds preview limits.")
	}
	f := regularTextFont
	if st.TextBold {
		f = boldTextFont
	}
	face := truetype.NewFace(f, &truetype.Options{Size: st.TextSize})
	defer face.Close()
	spacing := st.TextSpacing
	if spacing <= 0 {
		spacing = 1
	}
	lineHeight := float64(face.Metrics().Height) / 64 * spacing
	ascent := float64(face.Metrics().Ascent) / 64
	r, g, bl, a := decodeARGB(s.Color)
	if st.Orientation == 1 {
		return drawVerticalText(dc, s, face, lineHeight, ascent, w, h, func(c *gg.Context) { c.SetRGBA(r, g, bl, a) })
	}
	measure := gg.NewContext(1, 1)
	measure.SetFontFace(face)
	inner := float64(w - st.PaddingStart - st.PaddingEnd)
	if inner <= 0 {
		return fmt.Errorf("Text padding exceeds bounds.")
	}
	lines := measure.WordWrap(s.Text, inner)
	// The editor grows a text box to fit its text, so the stored bounds can be
	// shorter than the text. Extend the box downward rather than clipping.
	if need := int(math.Ceil(ascent + lineHeight*float64(max(len(lines)-1, 0)) + float64(face.Metrics().Descent)/64)); need > h && need <= 8192 && int64(w)*int64(need) <= 20e6 {
		h = need
	}
	box := gg.NewContext(w, h)
	box.SetFontFace(face)
	box.SetRGBA(r, g, bl, a)
	y := ascent
	for _, line := range lines {
		if y > float64(h)+st.TextSize {
			break
		}
		x := float64(st.PaddingStart)
		tw, _ := box.MeasureString(line)
		if st.AlignType == 1 {
			x += (inner - tw) / 2
		} else if st.AlignType == 2 {
			x += inner - tw
		}
		box.DrawString(line, x, y)
		if st.TextUnderline {
			box.SetLineWidth(math.Max(1, st.TextSize/20))
			box.DrawLine(x, y+2, x+tw, y+2)
			box.Stroke()
		}
		y += lineHeight
	}
	return placeImage(dc, grown(s, w, h, false), box.Image())
}

// drawVerticalText stacks upright characters top to bottom, starting a new
// column to the left when one is full, like the editor's vertical layout.
func drawVerticalText(dc *gg.Context, s *Shape, face font.Face, lineHeight, ascent float64, w, h int, color func(*gg.Context)) error {
	st := s.TextStyle
	column := float64(h - st.PaddingStart - st.PaddingEnd)
	if column < lineHeight {
		column = lineHeight
	}
	if !hasCJK(s.Text) {
		return drawSidewaysText(dc, s, face, lineHeight, ascent, column, w, h, color)
	}
	perColumn := max(int(column/lineHeight), 1)
	var columns [][]rune
	for _, para := range strings.Split(s.Text, "\n") {
		runes := []rune(para)
		if len(runes) == 0 {
			columns = append(columns, nil)
		}
		for len(runes) > 0 {
			n := min(perColumn, len(runes))
			columns = append(columns, runes[:n])
			runes = runes[n:]
		}
	}
	if need := int(math.Ceil(lineHeight * float64(len(columns)))); need > w && need <= 8192 && int64(need)*int64(h) <= 20e6 {
		w = need
	}
	box := gg.NewContext(w, h)
	box.SetFontFace(face)
	color(box)
	for i, col := range columns {
		cx := float64(w) - (float64(i)+0.5)*lineHeight
		y := float64(st.PaddingStart) + ascent
		for _, r := range col {
			if cx < -lineHeight || y > float64(h)+lineHeight {
				break
			}
			box.DrawStringAnchored(string(r), cx, y, 0.5, 0)
			y += lineHeight
		}
	}
	return placeImage(dc, grown(s, w, h, true), box.Image())
}

// drawSidewaysText lays out text without CJK characters as vertical layouts
// do: the line runs top to bottom, turned 90° clockwise, and further lines
// form columns to its left.
func drawSidewaysText(dc *gg.Context, s *Shape, face font.Face, lineHeight, ascent, column float64, w, h int, color func(*gg.Context)) error {
	st := s.TextStyle
	measure := gg.NewContext(1, 1)
	measure.SetFontFace(face)
	lines := measure.WordWrap(s.Text, column)
	if need := int(math.Ceil(lineHeight * float64(len(lines)))); need > w && need <= 8192 && int64(need)*int64(h) <= 20e6 {
		w = need
	}
	// A word longer than the column runs past it rather than being cut.
	for _, line := range lines {
		lw, _ := measure.MeasureString(line)
		if need := int(math.Ceil(lw)) + st.PaddingStart + st.PaddingEnd; need > h && need <= 8192 && int64(w)*int64(need) <= 20e6 {
			h = need
		}
	}
	box := gg.NewContext(w, h)
	box.SetFontFace(face)
	color(box)
	// Rotating 90° clockwise maps (x, y) to (w-y, x): the first line lands at
	// the right edge, reading downward.
	box.Translate(float64(w), 0)
	box.Rotate(math.Pi / 2)
	for i, line := range lines {
		box.DrawString(line, float64(st.PaddingStart), ascent+float64(i)*lineHeight)
	}
	return placeImage(dc, grown(s, w, h, true), box.Image())
}

func hasCJK(text string) bool {
	for _, r := range text {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

// grown returns the shape with its bounds widened to w×h pixels: leftward
// for vertical text, whose columns run right to left, otherwise downward.
func grown(s *Shape, w, h int, leftward bool) *Shape {
	b := *s.BoundingRect
	if leftward {
		b.Left = b.Right - float64(w)
	}
	b.Bottom = b.Top + float64(h)
	copy := *s
	copy.BoundingRect = &b
	return &copy
}

// RichTextNotebookWarning is the notebook-level notice for text notebooks.
// It describes the typed page, not their handwriting pages.
const RichTextNotebookWarning = "Rich-text notebook: available text is shown separately; native layout is not reproduced."

// RichText returns readable text from a text-notebook resource: the
// editor's JSON document (docVersion 2), or HTML from older notes.
func RichText(raw []byte) string {
	var envelope struct {
		Content *richNode `json:"content"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Content == nil || envelope.Content.Type != "doc" {
		return plainHTML(string(raw))
	}
	var out strings.Builder
	envelope.Content.write(&out, 0)
	lines := strings.Split(out.String(), "\n")
	kept := lines[:0]
	for _, l := range lines {
		if l = strings.TrimRight(l, " "); strings.TrimSpace(l) != "" {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "\n")
}

type richNode struct {
	Type    string     `json:"type"`
	Text    string     `json:"text"`
	Content []richNode `json:"content"`
}

func (n richNode) write(out *strings.Builder, depth int) {
	if depth > 64 {
		return
	}
	switch n.Type {
	case "text":
		out.WriteString(n.Text)
		return
	case "hardBreak":
		out.WriteByte('\n')
		return
	case "listItem":
		out.WriteString("• ")
	}
	for _, c := range n.Content {
		c.write(out, depth+1)
	}
	switch n.Type {
	case "paragraph", "heading", "codeBlock", "blockquote":
		out.WriteByte('\n')
	}
}
