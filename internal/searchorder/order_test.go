package searchorder

import (
	"context"
	"net/url"
	"reflect"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestNaturalNamesAndUntrustedNavigation(t *testing.T) {
	db := testenv.Database(t).DB
	names := []string{"Note 10", "Note 02", "note 2", "Note 1", "Note 000", "Note 99999999999999999999999999999999999999999999999999", "Note 100000000000000000000000000000000000000000000000000", "Árbol 10", "Árbol 2"}
	rows, err := db.QueryContext(context.Background(), `SELECT title FROM unnest($1::text[]) AS names(title) ORDER BY `+Order("title_asc").SQL("title", "NULL::bigint", "title", "title ASC"), names)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got = append(got, name)
	}
	rows.Close()
	want := []string{names[4], names[3], names[1], names[2], names[0], names[5], names[6], names[8], names[7]}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if Parse("name;DROP TABLE x") != Relevance || Offset("-50") != 0 || Offset("1000001") != 0 || Offset("99") != 50 {
		t.Fatal("invalid navigation accepted")
	}
	p := Navigation(url.Values{"q": {"needle & hay"}, "sort": {"title_desc"}, "mode": {"keyword"}, "source": {"boox"}, "titles_offset": {"50"}}, "pages_offset", "pages-results", 50, 205)
	u, err := url.Parse(p.Next)
	if err != nil || u.Fragment != "pages-results" || u.Query().Get("pages_offset") != "100" || u.Query().Get("titles_offset") != "50" || u.Query().Get("q") != "needle & hay" || u.Query().Get("sort") != "title_desc" || p.Start != 51 || p.End != 100 || p.Pages != 5 {
		t.Fatal(p, err)
	}
}
