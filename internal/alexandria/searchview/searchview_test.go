package searchview

import (
	"reflect"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
)

func TestFuseInterleavesByRank(t *testing.T) {
	hit := func(id string) Hit { return Hit{ID: id} }
	merged := Fuse(List{Hits: []Hit{hit("a1"), hit("a2"), hit("a3")}, Total: 7}, List{}, List{Hits: []Hit{hit("c1")}, Total: 1})
	var ids []string
	for _, h := range merged.Hits {
		ids = append(ids, h.ID)
	}
	if !reflect.DeepEqual(ids, []string{"a1", "c1", "a2", "a3"}) || merged.Total != 8 {
		t.Fatal(ids, merged.Total)
	}
}

func TestSortByTitleAndDate(t *testing.T) {
	hits := []Hit{{ID: "b", Title: "Beta", CreatedAt: 2}, {ID: "n", Title: "Alpha"}, {ID: "a", Title: "alpha 10", CreatedAt: 1}, {ID: "c", Title: "Alpha 9", CreatedAt: 3}}
	order := func() string {
		var ids []string
		for _, h := range hits {
			ids = append(ids, h.ID)
		}
		return strings.Join(ids, "")
	}
	Sort(hits, "title_asc")
	if got := order(); got != "ncab" {
		t.Fatal("title_asc", got)
	}
	Sort(hits, "created_desc")
	if got := order(); got != "cban" { // undated last
		t.Fatal("created_desc", got)
	}
	Sort(hits, "relevance")
	if got := order(); got != "cban" {
		t.Fatal("relevance must keep the order", got)
	}
}

func TestAroundAndPlainAndTerms(t *testing.T) {
	if got := Around("x "+strings.Repeat("filler ", 20)+"the budget line and "+strings.Repeat("more ", 20), []string{"budget"}, 30); !strings.HasPrefix(got, "…") || !strings.Contains(got, "budget") || !strings.HasSuffix(got, "…") {
		t.Fatal(got)
	}
	if Around("nothing here", []string{"budget"}, 30) != "" {
		t.Fatal("no match should give no snippet")
	}
	if got := Plain("a " + notes.MarkStart + "hit" + notes.MarkStop + " b"); got != "a hit b" {
		t.Fatal(got)
	}
	if got := Terms("One TWO " + strings.Repeat("x ", 30)); len(got) != MaxTerms || got[1] != "two" {
		t.Fatal(got)
	}
}
