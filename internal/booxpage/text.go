package booxpage

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
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
	box := gg.NewContext(w, h)
	box.SetFontFace(face)
	r, g, bl, a := decodeARGB(s.Color)
	box.SetRGBA(r, g, bl, a)
	inner := float64(w - st.PaddingStart - st.PaddingEnd)
	if inner <= 0 {
		return fmt.Errorf("Text padding exceeds bounds.")
	}
	spacing := st.TextSpacing
	if spacing <= 0 {
		spacing = 1
	}
	y := float64(face.Metrics().Ascent) / 64
	for _, line := range box.WordWrap(s.Text, inner) {
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
		y += float64(face.Metrics().Height) / 64 * spacing
	}
	return placeImage(dc, s, box.Image())
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
