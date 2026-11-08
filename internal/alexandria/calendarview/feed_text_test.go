package calendarview

import (
	"strings"
	"testing"
)

func TestFeedDetail(t *testing.T) {
	for _, c := range []struct{ name, in, html, plain string }{
		{"plain text is left alone", "Doors open < 7pm & bring snacks\nSecond line", "Doors open &lt; 7pm &amp; bring snacks\nSecond line", "Doors open < 7pm & bring snacks\nSecond line"},
		{"entities alone are decoded", "Tea &amp; biscuits &#8212; free", "Tea &amp; biscuits — free", "Tea & biscuits — free"},
		{"list with a link, shaped like a makerspace feed",
			`Your help is crucial!<br><ul><li>Drop in for an hour, or the whole day!</li><li>Details on <a href="https://forum.example/t/47" target="_blank">the forum</a> </li></ul>`,
			"Your help is crucial!\n• Drop in for an hour, or the whole day!\n• Details on <a href=\"https://forum.example/t/47\" rel=\"noopener noreferrer nofollow\">the forum</a>",
			"Your help is crucial!\n• Drop in for an hour, or the whole day!\n• Details on the forum"},
		{"paragraphs, bold and nbsp, shaped like a party feed",
			"<p><b>Monthly meeting</b></p>\n<p>Join us&nbsp;at  the\n hall.</p><p></p><p><span>Questions? <a href='mailto:chair@example.org'>Email the chair</a></span></p>",
			"<strong>Monthly meeting</strong>\n\nJoin us at the hall.\n\nQuestions? <a href=\"mailto:chair@example.org\" rel=\"noopener noreferrer nofollow\">Email the chair</a>",
			"Monthly meeting\n\nJoin us at the hall.\n\nQuestions? Email the chair"},
		{"numbered and nested lists", "<ol><li>One<ul><li>sub</li></ul></li><li>Two</li></ol>", "1. One\n  • sub\n2. Two", "1. One\n  • sub\n2. Two"},
		{"table cells", "<table><tr><td>Date</td><td>Place</td></tr><tr><td>Mon</td><td>Hall</td></tr></table>after", "Date · Place\nMon · Hall\n\nafter", "Date · Place\nMon · Hall\n\nafter"},
		{"three breaks become one blank line", "a<br><br><br><br>b", "a\n\nb", "a\n\nb"},
		{"scripts, styles and comments are dropped", "<style>p{color:red}</style><p>Hi<script>alert(1)</script> there<!-- note --></p>", "Hi there", "Hi there"},
		{"unsafe links become text", `<a href="javascript:alert(1)">click</a> <a href=" data:text/html,x">data</a> <a href="//evil.example">rel</a> <a href="mailto:">empty</a>`, "click data rel empty", "click data rel empty"},
		{"markup in text and attributes stays text", `<p>&lt;script&gt;x&lt;/script&gt; <a href="https://ok.example/?a=1&amp;b=&quot;2&quot;" onclick="x()">ok</a></p>`, "&lt;script&gt;x&lt;/script&gt; <a href=\"https://ok.example/?a=1&amp;b=&#34;2&#34;\" rel=\"noopener noreferrer nofollow\">ok</a>", "<script>x</script> ok"},
		{"unclosed and misnested tags are closed", "<b>bold <i>both</b> tail <a href='https://a.example'>open", "<strong>bold <em>both</em></strong> tail <a href=\"https://a.example\" rel=\"noopener noreferrer nofollow\">open</a>", "bold both tail open"},
		{"nested links don't nest", "<a href='https://a.example'>one <a href='https://b.example'>two</a></a>", "<a href=\"https://a.example\" rel=\"noopener noreferrer nofollow\">one</a> <a href=\"https://b.example\" rel=\"noopener noreferrer nofollow\">two</a>", "one two"},
		{"a whole document wrapper", "<html><head><title>Event</title></head><body><h2>Title</h2>Body</body></html>", "Title\n\nBody", "Title\n\nBody"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, plain := FeedDetail(c.in)
			if string(got) != c.html || plain != c.plain {
				t.Fatalf("\nhtml  %q\nwant  %q\nplain %q\nwant  %q", got, c.html, plain, c.plain)
			}
		})
	}
}

// Whatever a feed sends, the only tags in the output are the ones the
// converter writes itself.
func TestFeedDetailWritesOnlyItsOwnTags(t *testing.T) {
	for _, in := range []string{
		`<img src=x onerror=alert(1)><p>a</p>`,
		`<p <a href="https://x.example">x</a>`,
		`<svg><a xlink:href="javascript:alert(1)"><p>y</p></a></svg>`,
		`<a href="https://x.example" title='"><script>alert(1)</script>'>t</a>`,
		`<iframe src="https://x.example"></iframe><textarea><b>z</b></textarea>`,
		`<b>` + strings.Repeat("<i>", 500) + "deep",
	} {
		got, _ := FeedDetail(in)
		rest := string(got)
		for _, allowed := range []string{"<strong>", "</strong>", "<em>", "</em>", "</a>"} {
			rest = strings.ReplaceAll(rest, allowed, "")
		}
		for {
			i := strings.Index(rest, "<a href=\"")
			if i < 0 {
				break
			}
			j := strings.Index(rest[i:], `" rel="noopener noreferrer nofollow">`)
			if j < 0 {
				t.Fatalf("malformed link in %q", got)
			}
			if strings.ContainsAny(rest[i+9:i+j], `<>"`) {
				t.Fatalf("unescaped href in %q", got)
			}
			rest = rest[:i] + rest[i+j+len(`" rel="noopener noreferrer nofollow">`):]
		}
		if strings.ContainsAny(rest, "<>") {
			t.Fatalf("%q produced foreign markup: %q", in, got)
		}
	}
}

func TestFeedDetailLimit(t *testing.T) {
	got, _ := FeedDetail("<p>" + strings.Repeat("x", feedDetailLimit*2))
	if len(got) > feedDetailLimit {
		t.Fatal("description not bounded", len(got))
	}
}
