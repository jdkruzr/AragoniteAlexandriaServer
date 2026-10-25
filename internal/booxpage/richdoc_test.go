package booxpage

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func TestRichHTMLRendersTheDocument(t *testing.T) {
	out, notices := RichHTML([]byte(syntheticRichDoc), func(name string) string { return "/boox/resource?name=" + name })
	got := string(out)
	for _, want := range []string{
		"<h2>Synthetic planning notes</h2>", "<h3>Follow-ups</h3>",
		"<strong>Summary:</strong>", "<em>italic</em>",
		`<span style="font-size:0.75em;color:#1a5fb4">smaller blue</span>`,
		`<p class="rt-i1"><span class="rt-m">•</span> First synthetic bullet</p>`,
		`<p class="rt-i2"><span class="rt-m">•</span> Nested bullet</p>`,
		`<span class="rt-m">☑</span> Done item`, `<span class="rt-m">☐</span> Open item`,
		`<span class="rt-m">1.</span> Numbered`,
		"<p><br></p>", // an empty paragraph keeps its space
		`<figure><img src="/boox/resource?name=synthetic" alt=""></figure>`,
		"<em>Attachment: synthetic.txt</em>", "Unknown block text",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if len(notices) != 2 || notices[0] != RichTextShownNotice || notices[1] != RichTextFormattingNotice {
		t.Fatalf("notices %q", notices)
	}
	if dir := os.Getenv("BOOXPAGE_RENDER_DIR"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "richtext.html"), []byte(got), 0o600)
	}
	if out, n := RichHTML([]byte(`{"content":{"type":"doc","content":[]}}`), nil); out != "" || n != nil {
		t.Fatal("empty document produced output", out, n)
	}
}

// Nothing in a resource reaches the browser as markup: text, attribute
// values and image references are all escaped or rejected.
func TestRichHTMLEscapesEverything(t *testing.T) {
	doc := `{"content":{"type":"doc","content":[
{"type":"paragraph","content":[{"type":"text","text":"<script>alert(1)</script> & \"quotes\""}]},
{"type":"paragraph","content":[{"type":"text","marks":[{"type":"textStyle","attrs":{"fontColor":"red;background:url(x)","fontSize":"x\" onload=\"y"}}],"text":"styled"}]},
{"type":"onyxImg","attrs":{"src":"javascript:alert(1)","dataKey":"../../etc/passwd"}},
{"type":"onyxImg","attrs":{"src":"data:image/png;base64,AAAA"}}
]},"docVersion":2}`
	var asked []string
	out, _ := RichHTML([]byte(doc), func(name string) string { asked = append(asked, name); return "/r?n=" + name + `"x` })
	got := string(out)
	for _, bad := range []string{"<script", "url(", "onload", "background", `"x`} {
		if strings.Contains(got, bad) {
			t.Fatalf("%q leaked into\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "&lt;script&gt;alert(1)&lt;/script&gt; &amp; &#34;quotes&#34;") || !strings.Contains(got, "<p>styled</p>") {
		t.Fatalf("text not escaped as expected:\n%s", got)
	}
	// "../../etc/passwd" reduces to its base name, which is all a lookup can use;
	// the javascript: src is only a fallback, and the data: image has no file.
	if !slices.Equal(asked, []string{"passwd"}) || !strings.Contains(got, `/r?n=passwd&#34;x`) || !strings.Contains(got, `class="rt-missing"`) {
		t.Fatalf("image references %q\n%s", asked, got)
	}
	// Old HTML resources are reduced to text, never passed through.
	if out, _ := RichHTML([]byte(`<p>old <b>note</b><script>x()</script></p>`), nil); string(out) != "<p>old note</p>\n" {
		t.Fatalf("%q", out)
	}
}

func TestRichImageNames(t *testing.T) {
	doc := `{"content":{"type":"doc","content":[
{"type":"onyxImg","attrs":{"src":"/storage/emulated/0/note/pic.png","dataKey":"https://oss.example/a/pic.webp?x=1"}},
{"type":"onyxImg","attrs":{"src":"/storage/emulated/0/note/two.jpg"}},
{"type":"onyxImg","attrs":{"src":"data:image/png;base64,AAAA"}}]},"docVersion":2}`
	if got := RichImageNames([]byte(doc)); !slices.Equal(got, []string{"pic.webp", "two.jpg"}) {
		t.Fatal(got)
	}
}
