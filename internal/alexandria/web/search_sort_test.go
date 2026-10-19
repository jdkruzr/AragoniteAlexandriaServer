package web

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/searchorder"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestSearchNativeDatesAndPageNavigation(t *testing.T) {
	env := testenv.Database(t)
	db := env.DB
	_, err := db.Exec(`INSERT INTO boox_projection(document_id,revision,domain,native_type,body)
 SELECT 'n'||i,'1-a','notebook','1',jsonb_build_object('uniqueId','n'||i,'title','Needle '||i,'type',1,'status',1,'createdAt',CASE WHEN i=2 THEN 'bad' WHEN i=3 THEN '0' WHEN i=4 THEN '-1' ELSE (1700000000000+i)::text END,'pageNameList','[]'::jsonb) FROM generate_series(1,110)i`)
	if err != nil {
		t.Fatal(err)
	}
	d := Deps{DB: db, Search: notes.Searcher{DB: db}, Status: Status{NativeBOOX: true}}
	rows, total, err := d.searchBOOX(context.Background(), "needle", searchorder.Order("title_asc"), 50)
	if err != nil || total != 110 || rows[0].Title != "Needle 51" || rows[49].Title != "Needle 100" {
		t.Fatal(total, err)
	}
	for _, order := range []searchorder.Order{"created_asc", "created_desc"} {
		rows, total, err = d.searchBOOX(context.Background(), "needle", order, 100)
		if err != nil || total != 110 || len(rows) != 10 {
			t.Fatal(total, err)
		}
		for _, r := range rows[7:] {
			if r.CreatedAt != 0 {
				t.Fatal("invalid native date did not sort last", r)
			}
		}
	}
	h := Handler(d)
	for _, lang := range []string{"en", "es"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/search?q=needle&mode=keyword&source=boox&sort=title_desc&titles_offset=50", nil)
		r.Header.Set("Accept-Language", lang)
		h.ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, `value="title_desc" selected`) || !strings.Contains(body, "Needle 60") || strings.Contains(body, ">Needle 61<") || !strings.Contains(body, "titles_offset=100") || !strings.Contains(body, "#titles-results") {
			t.Fatal(lang, w.Code, body)
		}
		if lang == "es" && !strings.Contains(body, "Título: Z–A") {
			t.Fatal("missing Spanish sort")
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/search?q=needle&sort=bad&titles_offset=999999", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `value="relevance" selected`) {
		t.Fatal(w.Code)
	}
	// Native deletion must immediately disappear from title ordering.
	if _, err = db.Exec(`UPDATE boox_projection SET body=body||'{"status":0}'::jsonb WHERE document_id='n1'`); err != nil {
		t.Fatal(err)
	}
	rows, total, err = d.searchBOOX(context.Background(), "needle", "title_asc", 0)
	if err != nil || total != 109 || rows[0].Title != fmt.Sprint("Needle ", 2) {
		t.Fatal(total, err)
	}
}
