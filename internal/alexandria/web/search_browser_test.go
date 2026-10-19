package web

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

type browserSearchEmbedder struct{}

func (browserSearchEmbedder) Model() string { return "synthetic" }
func (browserSearchEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}

func TestSearchSortingBrowserFixture(t *testing.T) {
	dir := os.Getenv("ALEXANDRIA_SEARCH_BROWSER")
	if dir == "" {
		t.Skip("browser driver not requested")
	}
	db := testenv.Database(t).DB
	for _, sql := range []string{
		`INSERT INTO fn_notebook(id,name,created_at,lww_wall_ts,lww_op_seq,lww_site_id) SELECT 'client'||i,'Client needle '||i,1700000000000+i*3600000,1,1,'test' FROM generate_series(1,120)i`,
		`INSERT INTO fn_page(id,notebook_id,sort_order,lww_wall_ts,lww_op_seq,lww_site_id) SELECT 'page'||i,'client'||i,0,1,1,'test' FROM generate_series(1,120)i`,
		`INSERT INTO alexandria_note_content(note_key,page,body_text,source) SELECT 'forestnote://client'||i||'/page'||i,0,'Needle text for a synthetic Client search result.','forestnote' FROM generate_series(1,120)i`,
		`INSERT INTO boox_projection(document_id,revision,domain,native_type,body) SELECT 'native'||i,'1-a','notebook','1',jsonb_build_object('uniqueId','native'||i,'type',1,'status',1,'title','Needle '||i,'createdAt',1701000000000+i*3600000,'pageNameList',jsonb_build_array('np'||i)) FROM generate_series(1,120)i`,
		`INSERT INTO boox_page_index(notebook_id,page_id,page_number,state,text,input_version) SELECT 'native'||i,'np'||i,1,'ready','Needle text for a synthetic BOOX search result.','synthetic' FROM generate_series(1,120)i`,
		`INSERT INTO fn_reader_annotation(id,book_id,initial_anchor_json,canvas_width,initial_height,creator_session_id,lww_op_ts,lww_op_seq,lww_site_id) SELECT 'a'||i,'b'||i,'{}',100,100,'s'||i,1,1,'test' FROM generate_series(1,120)i`,
		`INSERT INTO reader_search_documents(annotation_id,book_id,title,quote,recognized_text,anchor,input_hash,alternatives,revision) SELECT 'a'||i,'b'||i,'Book needle '||i,'needle','Synthetic annotation text','{}','synthetic','[]',0 FROM generate_series(1,120)i`,
	} {
		if _, err := db.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	d := Deps{DB: db, Search: notes.Searcher{DB: db, Embedder: browserSearchEmbedder{}}, Status: Status{NativeBOOX: true, Embedding: "synthetic"}}
	srv := httptest.NewServer(Handler(d))
	defer srv.Close()
	if err := os.WriteFile(filepath.Join(dir, "url"), []byte(srv.URL), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(12 * time.Minute)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "stop")); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("browser driver timeout")
}
