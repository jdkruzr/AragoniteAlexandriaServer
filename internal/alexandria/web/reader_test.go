package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
)

// Search, tasks and MCP link to ?page=<id>; n= jumps by number. Both clamp
// to a real page so a stale link still opens the notebook.
func TestNotebookReaderSelectsPages(t *testing.T) {
	pages := []notes.Page{{ID: "a", Number: 1}, {ID: "b", Number: 2}, {ID: "c", Number: 3}}
	for _, c := range []struct {
		q          url.Values
		want       string
		prev, next bool
	}{
		{url.Values{}, "a", false, true},
		{url.Values{"page": {"b"}}, "b", true, true},
		{url.Values{"page": {"gone"}}, "a", false, true},
		{url.Values{"n": {"3"}}, "c", true, false},
		{url.Values{"n": {"99"}, "page": {"a"}}, "c", true, false},
		{url.Values{"n": {"-4"}}, "a", false, true},
	} {
		v := notebookReader("nb", "N", pages, nil, c.q, true)
		if v.Page.ID != c.want || (v.Previous != "") != c.prev || (v.Next != "") != c.next {
			t.Errorf("%v: got %s prev=%q next=%q", c.q, v.Page.ID, v.Previous, v.Next)
		}
	}
	v := notebookReader("nb", "N", pages, nil, url.Values{"page": {"b"}, "zoom": {"actual"}}, true)
	if !strings.HasSuffix(v.Next, "&page=c&zoom=actual") || v.Zoom != "actual" {
		t.Errorf("zoom not kept: %s", v.Next)
	}
	if empty := notebookReader("nb", "N", nil, nil, url.Values{"page": {"x"}}, true); empty.Count != 0 || empty.Page.ID != "" {
		t.Error("empty notebook selected a page")
	}
}

// The filmstrip marks the current page, keeps zoom in its links and knows
// which pages have text; the drawer link flips the remembered choice.
func TestNotebookReaderStripAndDrawer(t *testing.T) {
	pages := []notes.Page{{ID: "a", Number: 1, BodyText: "x"}, {ID: "b", Number: 2}}
	v := notebookReader("nb", "N", pages, []notes.Crumb{{ID: "f", Name: "Work"}}, url.Values{"page": {"b"}, "zoom": {"actual"}}, true)
	if len(v.Strip) != 2 || v.Strip[0].Current || !v.Strip[1].Current || !v.Strip[0].Text || v.Strip[1].Text {
		t.Fatalf("%+v", v.Strip)
	}
	if v.Strip[0].Href != "/files/forestnote?notebook=nb&page=a&zoom=actual" || v.Strip[0].Image != "/files/forestnote/thumb?page=a" {
		t.Fatalf("%+v", v.Strip[0])
	}
	if v.Folder != "Work" || v.Back != "/files/forestnote?selected=nb" || !strings.HasSuffix(v.DetailsLink, "&details=0") || v.GridLink != "/files/forestnote?notebook=nb&view=grid" {
		t.Fatalf("%+v", v)
	}
	if v = notebookReader("nb", "N", pages, nil, url.Values{}, false); !strings.HasSuffix(v.DetailsLink, "&details=1") {
		t.Fatal(v.DetailsLink)
	}
}
