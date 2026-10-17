package booxpage

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"math"
	"path"
	"strconv"
	"strings"
	"unicode"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	_ "golang.org/x/image/webp" // typed-text images are often WebP
)

var italicTextFont, _ = truetype.Parse(goitalic.TTF)
var boldItalicTextFont, _ = truetype.Parse(gobolditalic.TTF)

// RichTextDrawnNotice goes with a preview that draws a text notebook's typed text.
const RichTextDrawnNotice = "Typed text is drawn with a substitute font; the device's layout may differ."

// RichTextImageNotice is shown when a typed-text image couldn't be drawn.
const RichTextImageNotice = "Some images in typed text haven't arrived or can't be read; placeholders mark them."

// richImage finds and decodes a typed-text image: a data: URI, or the
// note's resource/data file named like its dataKey or src. Nil if it can't.
func richImage(b richBlock, load Load) image.Image {
	var raw []byte
	if data, ok := strings.CutPrefix(b.src, "data:"); ok {
		if _, enc, ok := strings.Cut(data, ";base64,"); ok && len(enc) <= 16<<20 {
			raw, _ = base64.StdEncoding.DecodeString(enc)
		}
	} else if load != nil {
		for _, ref := range []string{b.dataKey, b.src} {
			name := path.Base(strings.SplitN(ref, "?", 2)[0])
			if ref == "" || name == "." || name == "/" || strings.Contains(name, "..") {
				continue
			}
			if got, e := load("resource/data/" + name); e == nil {
				raw = got
				break
			}
		}
	}
	if len(raw) == 0 {
		return nil
	}
	c, _, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || c.Width <= 0 || c.Height <= 0 || int64(c.Width)*int64(c.Height) > 20e6 {
		return nil
	}
	img, _, e := image.Decode(bytes.NewReader(raw))
	if e != nil {
		return nil
	}
	return img
}

// A text notebook's typed page is a document from the BOOX editor (JSON,
// docVersion 2: doc → heading, paragraph, bulletList/orderedList/taskList →
// listItem/taskItem; onyxImg images; attach files; bold/italic/textStyle marks).
// richBlock is one laid-out line source: a block of styled runs with a marker.
type richBlock struct {
	kind    string // "heading", "paragraph", "image", "attach"
	level   int    // heading level
	indent  int    // list nesting
	marker  string // "•", "1.", "[ ]", "[x]" or ""
	runs    []richRun
	aspect  float64 // image height/width
	unknown bool
	// Images: the editor's src (a device path or data: URI) and dataKey
	// (its cloud name); the file is resource/data/<base name> in the note.
	src, dataKey string
	img          image.Image
}

type richRun struct {
	text                            string
	bold, italic, underline, strike bool
	scale                           float64 // font size relative to the body size
	color                           color.Color
	brk                             bool // a hard line break
}

type docNode struct {
	Type    string                     `json:"type"`
	Text    string                     `json:"text"`
	Attrs   map[string]json.RawMessage `json:"attrs"`
	Marks   []docNode                  `json:"marks"`
	Content []docNode                  `json:"content"`
}

// RichDocument parses an editor resource into blocks. HTML resources (older
// notes) become plain paragraphs.
func richDocument(raw []byte) []richBlock {
	var envelope struct {
		Content *docNode `json:"content"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Content == nil || envelope.Content.Type != "doc" {
		var out []richBlock
		for _, line := range strings.Split(plainHTML(string(raw)), "\n") {
			if strings.TrimSpace(line) != "" {
				out = append(out, richBlock{kind: "paragraph", runs: []richRun{{text: line, scale: 1}}})
			}
		}
		return out
	}
	var out []richBlock
	collectBlocks(*envelope.Content, 0, "", &out, 0)
	return out
}

func (n docNode) attr(name string) string {
	raw, ok := n.Attrs[name]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func collectBlocks(n docNode, indent int, marker string, out *[]richBlock, depth int) {
	if depth > 64 || len(*out) > 5000 {
		return
	}
	switch n.Type {
	case "doc", "blockquote":
		for _, c := range n.Content {
			collectBlocks(c, indent, "", out, depth+1)
		}
	case "heading", "paragraph", "codeBlock":
		b := richBlock{kind: "paragraph", indent: indent, marker: marker}
		if n.Type == "heading" {
			b.kind, b.level = "heading", 1
			if l, err := strconv.Atoi(n.attr("level")); err == nil && l > 0 {
				b.level = l
			}
		}
		b.runs = inlineRuns(n.Content)
		*out = append(*out, b)
	case "bulletList", "orderedList", "taskList":
		number := 1
		if s, err := strconv.Atoi(n.attr("start")); err == nil {
			number = s
		}
		for _, item := range n.Content {
			m := "•"
			switch {
			case n.Type == "orderedList":
				m = strconv.Itoa(number) + "."
				number++
			case item.Type == "taskItem" && item.attr("checked") == "true":
				m = "[x]"
			case item.Type == "taskItem":
				m = "[ ]"
			}
			collectBlocks(item, indent+1, m, out, depth+1)
		}
	case "listItem", "taskItem":
		// The marker goes on the item's first block; nested lists indent further.
		for i, c := range n.Content {
			if i > 0 {
				marker = ""
			}
			if c.Type == "bulletList" || c.Type == "orderedList" || c.Type == "taskList" {
				collectBlocks(c, indent, "", out, depth+1)
			} else {
				collectBlocks(c, indent, marker, out, depth+1)
			}
		}
	case "onyxImg", "image":
		w, _ := strconv.ParseFloat(n.attr("width"), 64)
		h, _ := strconv.ParseFloat(n.attr("height"), 64)
		aspect := 0.6
		if w > 0 && h > 0 {
			aspect = math.Min(math.Max(h/w, 0.1), 3)
		}
		*out = append(*out, richBlock{kind: "image", indent: indent, aspect: aspect, src: n.attr("src"), dataKey: n.attr("dataKey")})
	case "attach":
		title := n.attr("title")
		if title == "" || title == "null" {
			title = "file"
		}
		*out = append(*out, richBlock{kind: "attach", indent: indent, marker: marker, runs: []richRun{{text: "Attachment: " + title, italic: true, scale: 1}}})
	case "horizontalRule":
		*out = append(*out, richBlock{kind: "rule", indent: indent})
	default:
		// Unknown blocks keep their text so nothing typed goes missing.
		runs := inlineRuns(n.Content)
		if len(runs) > 0 {
			*out = append(*out, richBlock{kind: "paragraph", indent: indent, marker: marker, runs: runs, unknown: true})
		}
	}
}

func inlineRuns(nodes []docNode) []richRun {
	var runs []richRun
	for _, c := range nodes {
		switch c.Type {
		case "text":
			r := richRun{text: c.Text, scale: 1}
			for _, m := range c.Marks {
				switch m.Type {
				case "bold", "strong":
					r.bold = true
				case "italic", "em":
					r.italic = true
				case "underline":
					r.underline = true
				case "strike":
					r.strike = true
				case "textStyle":
					if pt := strings.TrimSuffix(strings.TrimSuffix(m.attr("fontSize"), "pt"), ".0"); pt != "" {
						if v, err := strconv.ParseFloat(pt, 64); err == nil && v > 0 {
							r.scale = math.Min(math.Max(v/24, 0.5), 3)
						}
					}
					r.color = cssColor(m.attr("fontColor"))
				}
			}
			runs = append(runs, r)
		case "hardBreak":
			runs = append(runs, richRun{brk: true, scale: 1})
		default:
			runs = append(runs, inlineRuns(c.Content)...)
		}
	}
	return runs
}

// cssColor reads #rgb, #rrggbb and rgb(r, g, b); anything else is black.
func cssColor(s string) color.Color {
	s = strings.TrimSpace(strings.ToLower(s))
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		if len(h) == 3 {
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		}
		if v, err := strconv.ParseUint(h, 16, 32); err == nil && len(h) == 6 {
			return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
		}
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		parts := strings.Split(s[4:len(s)-1], ",")
		if len(parts) == 3 {
			var c [3]uint8
			for i, p := range parts {
				v, err := strconv.Atoi(strings.TrimSpace(p))
				if err != nil || v < 0 || v > 255 {
					return nil
				}
				c[i] = uint8(v)
			}
			return color.RGBA{c[0], c[1], c[2], 255}
		}
	}
	return nil
}

// richLayout places blocks at a page width; it measures before drawing so the
// canvas can grow to fit the text.
type richLayout struct {
	width, body float64
	faces       map[string]font.Face
}

func newRichLayout(width float64) *richLayout {
	return &richLayout{width: width, body: math.Max(width/34, 12), faces: map[string]font.Face{}}
}

func (l *richLayout) face(bold, italic bool, size float64) font.Face {
	size = math.Round(size)
	key := fmt.Sprint(bold, italic, size)
	if f, ok := l.faces[key]; ok {
		return f
	}
	ttf := regularTextFont
	switch {
	case bold && italic:
		ttf = boldItalicTextFont
	case bold:
		ttf = boldTextFont
	case italic:
		ttf = italicTextFont
	}
	f := truetype.NewFace(ttf, &truetype.Options{Size: size})
	l.faces[key] = f
	return f
}

func (l *richLayout) margin() float64 { return math.Round(l.width * 0.06) }

// placed is a word (or marker) at a position, ready to draw.
type placed struct {
	text               string
	face               font.Face
	x, y, w            float64 // y is the baseline
	color              color.Color
	underline, strike  bool
	box                *[4]float64 // images and rules: x, y, w, h
	boxLabel, checkbox string
	img                image.Image // a drawn image fills box
}

// layout returns what to draw and the height used.
func (l *richLayout) layout(blocks []richBlock) ([]placed, float64) {
	var out []placed
	y := l.margin()
	right := l.width - l.margin()
	for _, b := range blocks {
		size := l.body
		bold := false
		gapBefore := 0.35 * l.body
		if b.kind == "heading" {
			bold = true
			size *= map[int]float64{1: 1.6, 2: 1.35, 3: 1.15}[min(b.level, 3)]
			if b.level > 3 {
				size = l.body * 1.05
			}
			gapBefore = 0.8 * l.body
		}
		left := l.margin() + float64(b.indent)*1.6*l.body
		y += gapBefore
		switch b.kind {
		case "image":
			w := right - left
			if b.img == nil {
				h := math.Min(w*b.aspect, 12*l.body)
				out = append(out, placed{box: &[4]float64{left, y, w, h}, boxLabel: "Image not shown"})
				y += h
				continue
			}
			// A drawn image keeps its proportions, at most 24 lines tall.
			h := w * b.aspect
			if limit := 24 * l.body; h > limit {
				h, w = limit, limit/b.aspect
			}
			out = append(out, placed{box: &[4]float64{left, y, w, h}, img: b.img})
			y += h + 0.4*l.body
			continue
		case "rule":
			out = append(out, placed{box: &[4]float64{left, y + l.body*0.5, right - left, 2}})
			y += l.body
			continue
		}
		if b.marker != "" {
			markerFace := l.face(false, false, size)
			mw := font.MeasureString(markerFace, b.marker).Ceil()
			ascent := float64(markerFace.Metrics().Ascent.Ceil())
			p := placed{text: b.marker, face: markerFace, x: left - float64(mw) - 0.4*l.body, y: y + ascent, w: float64(mw)}
			if b.marker == "[ ]" || b.marker == "[x]" {
				p.text, p.checkbox = "", b.marker
				p.x, p.w = left-1.3*l.body, 0.8*size
			}
			out = append(out, p)
		}
		lines := l.wrap(b.runs, size, bold, right-left)
		if len(lines) == 0 {
			lines = [][]placed{nil} // an empty paragraph still takes a line
		}
		for _, line := range lines {
			lineSize := size
			ascent := float64(l.face(bold, false, size).Metrics().Ascent.Ceil())
			for _, w := range line {
				if a := float64(w.face.Metrics().Ascent.Ceil()); a > ascent {
					ascent = a
				}
				if s := float64(w.face.Metrics().Height.Ceil()); s > lineSize {
					lineSize = s
				}
			}
			for _, w := range line {
				w.x += left
				w.y = y + ascent
				out = append(out, w)
			}
			y += lineSize * 1.45
		}
	}
	return out, y + l.margin()
}

// wrap breaks runs into lines of words that fit width (x relative to the block).
func (l *richLayout) wrap(runs []richRun, size float64, bold bool, width float64) [][]placed {
	var lines [][]placed
	var line []placed
	x := 0.0
	flush := func() {
		lines = append(lines, line)
		line, x = nil, 0
	}
	for _, r := range runs {
		if r.brk {
			flush()
			continue
		}
		face := l.face(bold || r.bold, r.italic, size*r.scale)
		for _, word := range splitWords(r.text) {
			w := float64(font.MeasureString(face, word).Ceil())
			if strings.TrimSpace(word) == "" {
				if x == 0 {
					continue // no leading space on a wrapped line
				}
				line = append(line, placed{text: word, face: face, x: x, w: w, color: r.color, underline: r.underline, strike: r.strike})
				x += w
				continue
			}
			if x > 0 && x+w > width {
				// Drop the trailing space before wrapping.
				if n := len(line); n > 0 && strings.TrimSpace(line[n-1].text) == "" {
					line = line[:n-1]
				}
				flush()
			}
			// A word longer than the line is broken by characters.
			for x == 0 && w > width && len([]rune(word)) > 1 {
				rs := []rune(word)
				cut := len(rs) - 1
				for cut > 1 && float64(font.MeasureString(face, string(rs[:cut])).Ceil()) > width {
					cut--
				}
				part := string(rs[:cut])
				line = append(line, placed{text: part, face: face, x: 0, w: float64(font.MeasureString(face, part).Ceil()), color: r.color, underline: r.underline, strike: r.strike})
				flush()
				word = string(rs[cut:])
				w = float64(font.MeasureString(face, word).Ceil())
			}
			line = append(line, placed{text: word, face: face, x: x, w: w, color: r.color, underline: r.underline, strike: r.strike})
			x += w
		}
	}
	if len(line) > 0 || len(lines) == 0 && len(runs) > 0 {
		flush()
	}
	return lines
}

// splitWords splits text into words and the spaces between them.
func splitWords(s string) []string {
	var out []string
	start := 0
	space := false
	for i, r := range s {
		isSpace := unicode.IsSpace(r)
		if i > 0 && isSpace != space {
			out = append(out, s[start:i])
			start = i
		}
		space = isSpace
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func (l *richLayout) draw(dc *gg.Context, items []placed) {
	for _, p := range items {
		if p.box != nil && p.img != nil {
			b := *p.box
			if dst, ok := dc.Image().(draw.Image); ok {
				r := image.Rect(int(b[0]), int(b[1]), int(math.Round(b[0]+b[2])), int(math.Round(b[1]+b[3])))
				draw.BiLinear.Scale(dst, r, p.img, p.img.Bounds(), draw.Over, nil)
			}
			continue
		}
		if p.box != nil {
			b := *p.box
			dc.SetRGB(0.6, 0.6, 0.6)
			if p.boxLabel == "" {
				dc.DrawRectangle(b[0], b[1], b[2], b[3])
				dc.Fill()
				continue
			}
			dc.SetLineWidth(2)
			dc.DrawRectangle(b[0], b[1], b[2], b[3])
			dc.Stroke()
			dc.SetFontFace(l.face(false, true, l.body))
			dc.DrawStringAnchored(p.boxLabel, b[0]+b[2]/2, b[1]+b[3]/2, 0.5, 0.35)
			continue
		}
		if p.checkbox != "" {
			s := p.w
			top := p.y - s
			dc.SetRGB(0, 0, 0)
			dc.SetLineWidth(math.Max(2, s/12))
			dc.DrawRectangle(p.x, top, s, s)
			dc.Stroke()
			if p.checkbox == "[x]" {
				dc.MoveTo(p.x+s*0.2, top+s*0.55)
				dc.LineTo(p.x+s*0.42, top+s*0.78)
				dc.LineTo(p.x+s*0.82, top+s*0.22)
				dc.Stroke()
			}
			continue
		}
		if p.color != nil {
			dc.SetColor(p.color)
		} else {
			dc.SetRGB(0, 0, 0)
		}
		dc.SetFontFace(p.face)
		dc.DrawString(p.text, p.x, p.y)
		size := float64(p.face.Metrics().Height.Ceil())
		if p.underline {
			dc.DrawRectangle(p.x, p.y+size*0.12, p.w, math.Max(1, size/16))
			dc.Fill()
		}
		if p.strike {
			dc.DrawRectangle(p.x, p.y-size*0.28, p.w, math.Max(1, size/16))
			dc.Fill()
		}
	}
}

// ComposeRichText draws a text notebook's typed documents (editor resources,
// in order) and lays the handwriting page's ink over them, as the device
// shows its handwriting layer above the text. The canvas grows downward to
// fit the text, within the preview size limit. It returns notices describing
// the approximation; resources that can't be read are reported, not fatal.
func ComposeRichText(ink image.Image, resources [][]byte, load Load) (image.Image, []string) {
	var blocks []richBlock
	for _, raw := range resources {
		blocks = append(blocks, richDocument(raw)...)
	}
	if len(blocks) == 0 {
		return ink, nil
	}
	missingImage := false
	for i := range blocks {
		if blocks[i].kind != "image" {
			continue
		}
		if img := richImage(blocks[i], load); img != nil {
			blocks[i].img = img
			c := img.Bounds()
			blocks[i].aspect = math.Min(math.Max(float64(c.Dy())/float64(c.Dx()), 0.1), 3)
		} else {
			missingImage = true
		}
	}
	bounds := ink.Bounds()
	w := bounds.Dx()
	l := newRichLayout(float64(w))
	items, needed := l.layout(blocks)
	h := max(bounds.Dy(), int(math.Ceil(needed)))
	notices := []string{RichTextDrawnNotice}
	if limit := int(20e6) / max(w, 1); h > limit || h > 32768 {
		h = min(limit, 32768)
		notices = append(notices, "Typed text is longer than the preview can show; the end is cut off.")
	}
	if missingImage {
		notices = append(notices, RichTextImageNotice)
	}
	for _, b := range blocks {
		if b.unknown {
			notices = append(notices, "Some typed-text formatting isn't reproduced; its text is shown plainly.")
			break
		}
	}
	dc := gg.NewContext(w, h)
	dc.SetRGB(1, 1, 1)
	dc.Clear()
	l.draw(dc, items)
	out := dc.Image().(*image.RGBA)
	// Darken: ink (on white paper) over text keeps the darker of the two.
	for y := 0; y < bounds.Dy() && y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := ink.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := out.PixOffset(x, y)
			out.Pix[i] = min(out.Pix[i], uint8(r>>8))
			out.Pix[i+1] = min(out.Pix[i+1], uint8(g>>8))
			out.Pix[i+2] = min(out.Pix[i+2], uint8(b>>8))
		}
	}
	return out, notices
}
