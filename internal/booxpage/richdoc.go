package booxpage

import (
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"image/color"
	"math"
	"path"
	"strconv"
	"strings"
)

// RichTextShownNotice goes with a text notebook's typed text in the browser.
const RichTextShownNotice = "Typed text is shown with this browser's fonts; the device's layout may differ."

// RichTextFormattingNotice is shown when part of the typed text's formatting isn't reproduced.
const RichTextFormattingNotice = "Some typed-text formatting isn't reproduced; its text is shown plainly."

// listItem/taskItem; onyxImg images; attach files; bold/italic/textStyle marks).
// richBlock is one block of styled runs with a list marker.
type richBlock struct {
	kind    string // "heading", "paragraph", "image", "attach", "rule"
	level   int    // heading level
	indent  int    // list nesting
	marker  string // "•", "1.", "[ ]", "[x]" or ""
	runs    []richRun
	unknown bool
	// Images: the editor's src (a device path or data: URI) and dataKey
	// (its cloud name); the file is resource/data/<base name> in the note.
	src, dataKey string
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
		*out = append(*out, richBlock{kind: "image", indent: indent, src: n.attr("src"), dataKey: n.attr("dataKey")})
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

// RichHTML renders an editor resource for the browser. Nothing in the
// resource passes through as markup: block kinds and marks map to fixed tags,
// sizes and colours are generated from validated numbers, and all text is
// escaped. imageURL turns an image's resource/data name into a URL; an image
// without a usable name becomes a placeholder.
func RichHTML(raw []byte, imageURL func(name string) string) (template.HTML, []string) {
	var b strings.Builder
	unknown := false
	for _, k := range richDocument(raw) {
		unknown = unknown || k.unknown
		class := ""
		if k.indent > 0 {
			class = fmt.Sprintf(` class="rt-i%d"`, min(k.indent, 8))
		}
		switch k.kind {
		case "heading":
			// The page has its own title, so editor headings start one level down.
			tag := fmt.Sprintf("h%d", min(max(k.level, 1)+1, 6))
			fmt.Fprintf(&b, "<%s%s>%s</%s>\n", tag, class, richRunsHTML(k.runs), tag)
		case "image":
			if name := richImageName(k); name != "" && imageURL != nil {
				fmt.Fprintf(&b, "<figure%s><img src=\"%s\" alt=\"\"></figure>\n", class, html.EscapeString(imageURL(name)))
			} else {
				fmt.Fprintf(&b, "<p%s class=\"rt-missing\">[image]</p>\n", class)
			}
		case "rule":
			b.WriteString("<hr>\n")
		default: // paragraph, attach
			marker := ""
			if k.marker != "" {
				m := k.marker
				switch m {
				case "[ ]":
					m = "\u2610"
				case "[x]":
					m = "\u2611"
				}
				marker = `<span class="rt-m">` + html.EscapeString(m) + "</span> "
			}
			body := richRunsHTML(k.runs)
			if body == "" {
				body = "<br>"
			}
			fmt.Fprintf(&b, "<p%s>%s%s</p>\n", class, marker, body)
		}
	}
	if b.Len() == 0 {
		return "", nil
	}
	notices := []string{RichTextShownNotice}
	if unknown {
		notices = append(notices, RichTextFormattingNotice)
	}
	return template.HTML(b.String()), notices
}

func richRunsHTML(runs []richRun) string {
	var b strings.Builder
	for _, r := range runs {
		if r.brk {
			b.WriteString("<br>")
			continue
		}
		if r.text == "" {
			continue
		}
		text := html.EscapeString(r.text)
		var style []string
		if r.scale != 0 && r.scale != 1 {
			style = append(style, "font-size:"+strconv.FormatFloat(math.Round(r.scale*100)/100, 'f', -1, 64)+"em")
		}
		if c, ok := r.color.(color.RGBA); ok {
			style = append(style, fmt.Sprintf("color:#%02x%02x%02x", c.R, c.G, c.B))
		}
		for _, w := range []struct {
			on  bool
			tag string
		}{{r.strike, "s"}, {r.underline, "u"}, {r.italic, "em"}, {r.bold, "strong"}} {
			if w.on {
				text = "<" + w.tag + ">" + text + "</" + w.tag + ">"
			}
		}
		if len(style) > 0 {
			text = `<span style="` + strings.Join(style, ";") + `">` + text + "</span>"
		}
		b.WriteString(text)
	}
	return b.String()
}

// richImageName is the resource/data file an editor image refers to, by its
// cloud name or else its device path. Inline data: images have none.
func richImageName(b richBlock) string {
	for _, ref := range []string{b.dataKey, b.src} {
		if ref == "" || strings.HasPrefix(ref, "data:") {
			continue
		}
		name := path.Base(strings.SplitN(ref, "?", 2)[0])
		if name == "." || name == "/" || name == ".." || strings.ContainsAny(name, "/\\") {
			continue
		}
		return name
	}
	return ""
}

// RichImageNames lists the resource/data files an editor resource's images
// refer to, so only those can be served alongside it.
func RichImageNames(raw []byte) []string {
	var names []string
	for _, b := range richDocument(raw) {
		if b.kind == "image" {
			if name := richImageName(b); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}
