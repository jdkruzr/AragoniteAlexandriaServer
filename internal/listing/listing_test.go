package listing

import (
	"net/url"
	"strings"
	"testing"
)

func TestSortAndPaginationRetainScope(t *testing.T) {
	q := url.Values{"folder": {"f"}, "q": {"needle"}, "show": {"deleted"}}
	s := Parse(q)
	if s.Sort != "modified" || s.Order != "desc" {
		t.Fatal(s)
	}
	s.Finish(q, "/boox", 121)
	if s.Total != 121 || s.Pages != 3 || s.End != 60 || s.Previous != "" {
		t.Fatal(s)
	}
	u, _ := url.Parse(s.Next)
	if u.Query().Get("folder") != "f" || u.Query().Get("q") != "needle" || u.Query().Get("offset") != "60" {
		t.Fatal(u)
	}
	u, _ = url.Parse(s.SortURL("modified"))
	if u.Query().Get("order") != "asc" || u.Query().Get("offset") != "" {
		t.Fatal(u)
	}
	s = Parse(url.Values{"sort": {"updatedAt; DROP TABLE x"}, "order": {"desc;--"}})
	if !strings.Contains(s.NativeOrder(), "DESC NULLS LAST,document_id ASC") || strings.Contains(s.NativeOrder(), "DROP") {
		t.Fatal(s.NativeOrder())
	}
}
