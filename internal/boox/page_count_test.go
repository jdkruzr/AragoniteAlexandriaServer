package boox

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

func TestPageCountUsesSupportedNativeEncodings(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	for i, raw := range []string{`["a","b"]`, `{"pageNameList":["a","b"]}`, `"[\"a\",\"b\"]"`, `null`, `"bad"`, `[1]`} {
		var count int
		if err := s.DB.QueryRowContext(ctx, `SELECT jsonb_array_length(boox_search_page_ids($1::jsonb))`, raw).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != len(booxpage.PageIDs(json.RawMessage(raw))) {
			t.Fatalf("decoder mismatch for %s", raw)
		}
		body := fmt.Sprintf(`{"uniqueId":"note-%d","type":1,"status":1,"pageNameList":%s}`, i, raw)
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-a','notebook','1',$2,$3)`, fmt.Sprintf("note-%d", i), s.uid(), body); err != nil {
			t.Fatal(err)
		}
	}
	// Direct arrays count two; zero-page malformed records must sort last.
	rows, err := s.DB.QueryContext(ctx, `SELECT document_id FROM boox_projection ORDER BY `+(listview.State{Sort: "pages", Order: "desc"}).NativeOrder())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		if id != fmt.Sprintf("note-%d", i) {
			t.Fatal(id, i)
		}
		i++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if i != 6 {
		t.Fatal(i)
	}
}
