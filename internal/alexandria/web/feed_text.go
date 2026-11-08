package web

import (
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Subscribed calendars often put HTML in event descriptions (paragraphs,
// lists, links), which escaped as text reads like source code. feedDetail
// turns it into readable text. The output is built from scratch: only
// line breaks, bullets, <strong>, <em> and checked links are ever written,
// and every piece of feed text is escaped, so a feed can't add markup.

// feedMarkup spots the tags these feeds actually use. Text without one is
// shown as written, so "doors open < 7pm" stays plain.
var feedMarkup = regexp.MustCompile(`(?i)<(/?(p|br|a|b|i|u|s|em|strong|ul|ol|li|div|span|font|table|thead|tbody|tr|td|th|col|colgroup|h[1-6]|html|body|head|section|article|blockquote|center|hr|wbr|img|pre|code|small|sup|sub|del|strike|dl|dt|dd|style|script)\b[^>]*|!--)>?`)

// feedEntity spots character references such as &amp; or &#8217;.
var feedEntity = regexp.MustCompile(`&(#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[a-zA-Z][a-zA-Z0-9]{1,31});`)

// feedDetailLimit bounds the work for one description.
const feedDetailLimit = 64 << 10

// feedDetail returns a subscribed event's description as safe HTML for the
// event pane, plus the same words as plain text for the calendar filter.
func feedDetail(s string) (template.HTML, string) {
	if len(s) > feedDetailLimit {
		s = s[:feedDetailLimit]
	}
	if !feedMarkup.MatchString(s) {
		if feedEntity.MatchString(s) {
			s = html.UnescapeString(s)
		}
		return template.HTML(html.EscapeString(s)), s
	}
	w := feedWriter{}
	w.read(s)
	return template.HTML(w.out.String()), strings.TrimSpace(w.plain.String())
}

// feedPlain is feedDetail's plain text alone.
func feedPlain(s string) string {
	_, plain := feedDetail(s)
	return plain
}

type feedWriter struct {
	out, plain strings.Builder
	pending    int         // line breaks owed before the next text
	started    bool        // some text has been written
	space      bool        // a space is owed before the next word
	open       []atom.Atom // inline tags written and not yet closed
	lists      []int       // list counters; -1 marks a bulleted list
	cells      int         // cells so far in the current table row
	skip       int         // depth inside elements whose text is dropped
}

func (w *feedWriter) read(s string) {
	z := xhtml.NewTokenizer(strings.NewReader(s))
	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			break // io.EOF: a string reader has no other errors
		}
		t := z.Token()
		switch tt {
		case xhtml.TextToken:
			if w.skip == 0 {
				w.text(t.Data)
			}
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			if dropped(t.DataAtom) {
				if tt == xhtml.StartTagToken {
					w.skip++
				}
				continue
			}
			if w.skip == 0 {
				w.start(t)
			}
		case xhtml.EndTagToken:
			if dropped(t.DataAtom) {
				if w.skip > 0 {
					w.skip--
				}
				continue
			}
			if w.skip == 0 {
				w.end(t.DataAtom)
			}
		}
	}
	w.closeAll()
}

// dropped elements contribute no text at all.
func dropped(a atom.Atom) bool {
	switch a {
	case atom.Script, atom.Style, atom.Head, atom.Title, atom.Noscript, atom.Template, atom.Iframe, atom.Object, atom.Svg, atom.Math, atom.Select, atom.Textarea:
		return true
	}
	return false
}

func (w *feedWriter) start(t xhtml.Token) {
	switch t.DataAtom {
	case atom.Br:
		w.br()
	case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Blockquote, atom.Table, atom.Pre, atom.Hr, atom.Dl:
		w.block(2)
	case atom.Div, atom.Section, atom.Article, atom.Header, atom.Footer, atom.Center, atom.Address, atom.Dt, atom.Dd, atom.Caption:
		w.block(1)
	case atom.Ul, atom.Ol:
		w.block(1)
		n := -1
		if t.DataAtom == atom.Ol {
			n = 0
		}
		w.lists = append(w.lists, n)
	case atom.Li:
		w.block(1)
		marker := "•"
		if k := len(w.lists) - 1; k >= 0 && w.lists[k] >= 0 {
			w.lists[k]++
			marker = strconv.Itoa(w.lists[k]) + "."
		}
		w.indent(len(w.lists))
		w.write(marker, marker)
		w.space = true
	case atom.Tr:
		w.block(1)
		w.cells = 0
	case atom.Td, atom.Th:
		if w.cells > 0 && w.started && w.pending == 0 {
			w.space = true
			w.write("·", "·")
			w.space = true
		}
		w.cells++
	case atom.B, atom.Strong:
		w.inline(atom.Strong, "<strong>")
	case atom.I, atom.Em:
		w.inline(atom.Em, "<em>")
	case atom.A:
		w.closeTag(atom.A)
		for _, a := range t.Attr {
			if a.Namespace == "" && strings.EqualFold(a.Key, "href") {
				if href, ok := feedLink(a.Val); ok {
					w.inline(atom.A, `<a href="`+html.EscapeString(href)+`" rel="noopener noreferrer nofollow">`)
				}
				break
			}
		}
	}
}

func (w *feedWriter) end(a atom.Atom) {
	switch a {
	case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Blockquote, atom.Table, atom.Pre, atom.Dl:
		w.block(2)
	case atom.Div, atom.Section, atom.Article, atom.Header, atom.Footer, atom.Center, atom.Address, atom.Dt, atom.Dd, atom.Caption, atom.Li, atom.Tr:
		w.block(1)
	case atom.Ul, atom.Ol:
		if len(w.lists) > 0 {
			w.lists = w.lists[:len(w.lists)-1]
		}
		w.block(1)
	case atom.B, atom.Strong:
		w.closeTag(atom.Strong)
	case atom.I, atom.Em:
		w.closeTag(atom.Em)
	case atom.A:
		w.closeTag(atom.A)
	}
}

// feedLink accepts web and mail links only, so javascript: and data: URLs
// never become links.
func feedLink(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Host == "" {
			return "", false
		}
	case "mailto":
		if u.Opaque == "" {
			return "", false
		}
	default:
		return "", false
	}
	return u.String(), true
}

// text writes feed text, collapsing HTML whitespace as a browser would.
func (w *feedWriter) text(s string) {
	lead := s != "" && isHTMLSpace(rune(s[0]))
	words := strings.FieldsFunc(s, isHTMLSpace)
	if len(words) == 0 {
		if lead {
			w.space = true
		}
		return
	}
	if lead {
		w.space = true
	}
	joined := strings.Join(words, " ")
	w.write(html.EscapeString(joined), joined)
	w.space = isHTMLSpace(rune(s[len(s)-1]))
}

func isHTMLSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' || r == ' '
}

// write emits text, first paying any owed line breaks or space.
func (w *feedWriter) write(markup, plain string) {
	w.flush()
	w.started = true
	w.out.WriteString(markup)
	w.plain.WriteString(plain)
}

// flush pays owed line breaks, or a space, once text has started.
func (w *feedWriter) flush() {
	if w.started && w.pending > 0 {
		n := min(w.pending, 2)
		w.out.WriteString(strings.Repeat("\n", n))
		w.plain.WriteString(strings.Repeat("\n", n))
	} else if w.started && w.space {
		w.out.WriteByte(' ')
		w.plain.WriteByte(' ')
	}
	w.pending, w.space = 0, false
}

// indent nests a list item under its parent item.
func (w *feedWriter) indent(depth int) {
	if depth > 1 {
		pad := strings.Repeat("  ", min(depth-1, 4))
		w.write(pad, pad)
	}
}

// block owes at least n line breaks; br adds one more.
func (w *feedWriter) block(n int) {
	if n > w.pending {
		w.pending = n
	}
	w.space = false
}

func (w *feedWriter) br() {
	w.pending++
	w.space = false
}

func (w *feedWriter) inline(a atom.Atom, tag string) {
	w.flush()
	w.open = append(w.open, a)
	w.out.WriteString(tag)
}

// closeTag closes the innermost open a, and any tags left open inside it,
// so the output stays well nested whatever order the feed used.
func (w *feedWriter) closeTag(a atom.Atom) {
	for i := len(w.open) - 1; i >= 0; i-- {
		if w.open[i] == a {
			for j := len(w.open) - 1; j >= i; j-- {
				w.out.WriteString("</" + w.open[j].String() + ">")
			}
			w.open = w.open[:i]
			return
		}
	}
}

func (w *feedWriter) closeAll() {
	for j := len(w.open) - 1; j >= 0; j-- {
		w.out.WriteString("</" + w.open[j].String() + ">")
	}
	w.open = nil
}
