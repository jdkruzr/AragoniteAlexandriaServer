package notes

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestSearchSortsBeforePaginationAndKeepsUnknownDatesLast(t *testing.T) {
	db := testenv.Database(t).DB
	ctx := context.Background()
	for _, sql := range []string{
		`INSERT INTO fn_notebook(id,name,created_at,lww_wall_ts,lww_op_seq,lww_site_id) SELECT 'n'||lpad(i::text,3,'0'),'Notebook '||(206-i),CASE WHEN i=1 THEN 0 WHEN i=2 THEN NULL ELSE 1700000000000+i END,1,1,'test' FROM generate_series(1,205)i`,
		`INSERT INTO fn_page(id,notebook_id,sort_order,created_at,lww_wall_ts,lww_op_seq,lww_site_id) SELECT 'p'||i,'n'||lpad(i::text,3,'0'),0,1,1,1,'test' FROM generate_series(1,205)i`,
		`INSERT INTO alexandria_note_content(note_key,page,body_text,source) SELECT 'forestnote://n'||lpad(i::text,3,'0')||'/p'||i,0,'needle text for the synthetic search fixture','forestnote' FROM generate_series(1,205)i`,
	} {
		if _, err := db.ExecContext(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	search := Searcher{DB: db}
	for _, order := range []searchorder.Order{"title_asc", "title_desc", "created_asc", "created_desc", searchorder.Relevance} {
		seen := map[string]bool{}
		var all []Result
		started := time.Now()
		for offset := 0; offset < 205; offset += 50 {
			results, total, err := search.SearchPage(ctx, "needle", 50, offset, false, "client", order)
			if err != nil || total != 205 || len(results) != min(50, 205-offset) {
				t.Fatal(order, offset, total, len(results), err)
			}
			for _, r := range results {
				if seen[r.NoteKey] {
					t.Fatal("duplicate across pages", r.NoteKey)
				}
				seen[r.NoteKey] = true
				all = append(all, r)
			}
		}
		t.Logf("%s: 205 results over five pages in %s", order, time.Since(started))
		if order == "title_asc" || order == "title_desc" {
			for i, r := range all {
				number := i + 1
				if order == "title_desc" {
					number = 205 - i
				}
				if r.NotebookName != fmt.Sprint("Notebook ", number) {
					t.Fatal(order, i, r.NotebookName)
				}
			}
		}
		if strings.HasPrefix(string(order), "created") {
			for i, r := range all[:203] {
				want := int64(1700000000003 + i)
				if order == "created_desc" {
					want = 1700000000205 - int64(i)
				}
				if r.CreatedAt != want {
					t.Fatal(order, i, r.CreatedAt, want)
				}
			}
			if all[203].CreatedAt != 0 || all[204].CreatedAt != 0 {
				t.Fatal("missing dates must be last", order)
			}
		}
	}
	// Old/out-of-range pages fall back without looping or returning an empty list.
	results, total, err := search.SearchPage(ctx, "needle", 50, 1000000, false, "client", "title_asc")
	if err != nil || total != 205 || results[0].NotebookName != "Notebook 1" {
		t.Fatal(total, err)
	}
	if _, err = db.Exec(`UPDATE fn_notebook SET deleted_at=1 WHERE id='n205'`); err != nil {
		t.Fatal(err)
	}
	results, total, err = search.SearchPage(ctx, "needle", 50, 0, false, "client", "title_asc")
	if err != nil || total != 204 || results[0].NotebookName != "Notebook 2" {
		t.Fatal(total, err)
	}
	// Same-title pages follow source page order, not lexical page IDs.
	if _, err = db.Exec(`INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) VALUES ('extra10','n204',10,1,1,'test'),('extra2','n204',2,1,1,'test');
 INSERT INTO alexandria_note_content(note_key,page,body_text,source) VALUES ('forestnote://n204/extra10',0,'needle','forestnote'),('forestnote://n204/extra2',0,'needle','forestnote')`); err != nil {
		t.Fatal(err)
	}
	results, _, err = search.SearchPage(ctx, "needle", 50, 0, false, "client", "title_asc")
	if err != nil || results[0].PageNumber != 1 || results[1].PageNumber != 2 || results[2].PageNumber != 3 || results[1].PageID != "extra2" {
		t.Fatal(results[:3], err)
	}
}
