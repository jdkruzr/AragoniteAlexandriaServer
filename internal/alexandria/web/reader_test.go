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
		v := notebookReader("nb", "N", pages, nil, c.q)
		if v.Page.ID != c.want || (v.Previous != "") != c.prev || (v.Next != "") != c.next {
			t.Errorf("%v: got %s prev=%q next=%q", c.q, v.Page.ID, v.Previous, v.Next)
		}
	}
	v := notebookReader("nb", "N", pages, nil, url.Values{"page": {"b"}, "zoom": {"actual"}})
	if !strings.HasSuffix(v.Next, "&page=c&zoom=actual") || v.Zoom != "actual" {
		t.Errorf("zoom not kept: %s", v.Next)
	}
	if empty := notebookReader("nb", "N", nil, nil, url.Values{"page": {"x"}}); empty.Count != 0 || empty.Page.ID != "" {
		t.Error("empty notebook selected a page")
	}
}
