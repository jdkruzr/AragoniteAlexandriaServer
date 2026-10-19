package readersearch

import (
	"context"
	"fmt"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestAnnotationOrderingPagesAndStaleFiltering(t *testing.T) {
	db := testenv.Database(t).DB
	for _, sql := range []string{
		`INSERT INTO fn_reader_annotation(id,book_id,initial_anchor_json,canvas_width,initial_height,creator_session_id,lww_op_ts,lww_op_seq,lww_site_id) SELECT 'a'||i,'b'||i,'{}',100,100,'s'||i,1,1,'test' FROM generate_series(1,125)i`,
		`INSERT INTO reader_search_documents(annotation_id,book_id,title,quote,recognized_text,anchor,input_hash,alternatives,revision) SELECT 'a'||i,'b'||i,'Book '||i,'needle','','{}','synthetic','[]',0 FROM generate_series(1,125)i`,
	} {
		if _, err := db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	for _, order := range []searchorder.Order{"title_asc", "title_desc", "created_asc", "created_desc"} {
		result, total, err := New(db).SearchPage(context.Background(), "needle", "", 50, 50, order)
		first, last := 51, 100
		if order == "title_desc" {
			first, last = 75, 26
		}
		if err != nil || total != 125 || len(result) != 50 || result[0].Title != fmt.Sprint("Book ", first) || result[49].Title != fmt.Sprint("Book ", last) {
			t.Fatal(order, total, result, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO reader_store_changes(table_name,pk) VALUES('reader_annotation','a1')`); err != nil {
		t.Fatal(err)
	}
	result, total, err := New(db).SearchPage(context.Background(), "needle", "", 50, 0, "title_asc")
	if err != nil || total != 124 || result[0].AnnotationID != "a2" {
		t.Fatal(total, err)
	}
}
