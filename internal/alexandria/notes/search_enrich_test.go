package notes

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

type countedSearchDB struct {
	pg.DB
	queries int
}

func (d *countedSearchDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	d.queries++
	return d.DB.QueryContext(ctx, q, args...)
}

func TestSearchEnrichmentBatchesSelectedPages(t *testing.T) {
	db := testenv.Database(t).DB
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id)
 SELECT 'page-'||i,'book',i,1,1,'fixture' FROM generate_series(1,100) i`); err != nil {
		t.Fatal(err)
	}
	for i, tag := range []string{"Beta", "Alpha", "Alpha"} {
		id := fmt.Sprintf("tag-%d", i)
		body := fmt.Sprintf(`{"uniqueId":%q,"modeType":"1","documentId":"book","pageUniqueId":"page-2","content":%q}`, id, tag)
		if _, err := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-a','notebook','','fixture',$2)`, id, body); err != nil {
			t.Fatal(err)
		}
	}
	counted := &countedSearchDB{DB: db}
	s := Searcher{DB: counted}
	for _, size := range []int{0, 1, 25, 100} {
		results := make([]Result, size)
		for i := range results {
			results[i] = Result{Source: "client", PageID: fmt.Sprintf("page-%d", size-i), NoteKey: fmt.Sprint(i)}
		}
		counted.queries = 0
		if err := s.enrich(ctx, results); err != nil {
			t.Fatal(err)
		}
		want := 1
		if size == 0 {
			want = 0
		}
		if counted.queries != want {
			t.Fatal("query count", counted.queries, want)
		}
		for i, r := range results {
			if r.PageNumber != size-i || r.NoteKey != fmt.Sprint(i) {
				t.Fatal("enrichment changed order or ordinal", r)
			}
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE fn_page SET deleted_at=1 WHERE id='page-1';UPDATE fn_page SET sort_order=0 WHERE id='page-100'`); err != nil {
		t.Fatal(err)
	}
	results := []Result{{Source: "client", PageID: "page-100"}, {Source: "client", PageID: "missing"}, {Source: "boox", Kind: "Notebook", NotebookID: "book", PageID: "page-2"}, {Source: "boox", Kind: "Reading", Tags: []string{"keep"}}}
	counted.queries = 0
	if err := s.enrich(ctx, results); err != nil {
		t.Fatal(err)
	}
	if counted.queries != 2 || results[0].PageNumber != 1 || results[1].PageNumber != 0 || !reflect.DeepEqual(results[2].Tags, []string{"Alpha", "Beta"}) || !reflect.DeepEqual(results[3].Tags, []string{"keep"}) {
		t.Fatal(counted.queries, results)
	}
}
