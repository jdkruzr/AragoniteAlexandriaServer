package boox

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestPrefixBounds(t *testing.T) {
	prefixes := []string{"", "u/note/", "u/note/é", "u/note/界", "\uD7FF", string(utf8.MaxRune), "a" + string(utf8.MaxRune)}
	keys := append([]string{}, prefixes...)
	for _, p := range prefixes {
		keys = append(keys, p+"tail", p+"/", p+"0")
	}
	for _, p := range prefixes {
		upper, ok := prefixUpper(p)
		if ok && !utf8.ValidString(upper) {
			t.Fatal("invalid bound")
		}
		for _, key := range keys {
			got := key >= p && (!ok || key < upper)
			if got != strings.HasPrefix(key, p) {
				t.Fatalf("prefix=%q key=%q upper=%q: %v", p, key, upper, got)
			}
		}
	}
	if upper := directoryUpper("note/"); upper != "note0" {
		t.Fatal(upper)
	}
}

func TestPrefixPlansAndResults(t *testing.T) {
	db := testenv.Database(t).DB
	ctx := context.Background()
	// A transaction keeps temporary fixtures and plan modes on one session.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `CREATE TEMP TABLE survey_prefix(k text COLLATE "C" PRIMARY KEY) ON COMMIT DROP;
 INSERT INTO survey_prefix SELECT 'u/note/'||lpad(i::text,8,'0')||'/page' FROM generate_series(1,100000) i;
 INSERT INTO survey_prefix VALUES('u/note/é/page'),('u/note/界/page'),('other/note/é/page'); ANALYZE survey_prefix`); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"force_custom_plan", "force_generic_plan", "auto"} {
		if _, err = tx.ExecContext(ctx, "SET LOCAL plan_cache_mode="+mode); err != nil {
			t.Fatal(err)
		}
		for _, prefix := range []string{"", "u/", "u/note/00004999/", "u/note/é", "u/note/界", string(utf8.MaxRune)} {
			upper, bounded := prefixUpper(prefix)
			optimized := `SELECT k FROM survey_prefix WHERE k >= $1 COLLATE "C" AND starts_with(k,$1)`
			args := []any{prefix}
			if bounded {
				optimized += ` AND k < $2 COLLATE "C"`
				args = append(args, upper)
			}
			optimized += ` ORDER BY k LIMIT 20001`
			var results [2][]string
			for i, query := range []string{`SELECT k FROM survey_prefix WHERE starts_with(k,$1) ORDER BY k LIMIT 20001`, optimized} {
				stmt, e := tx.PrepareContext(ctx, query)
				if e != nil {
					t.Fatal(e)
				}
				values := args
				if i == 0 {
					values = args[:1]
				}
				for repeat := 0; repeat < 6; repeat++ {
					rows, e := stmt.QueryContext(ctx, values...)
					if e != nil {
						t.Fatal(e)
					}
					var keys []string
					for rows.Next() {
						var key string
						if e = rows.Scan(&key); e != nil {
							t.Fatal(e)
						}
						keys = append(keys, key)
					}
					e = rows.Err()
					rows.Close()
					if e != nil {
						t.Fatal(e)
					}
					results[i] = keys
				}
				stmt.Close()
			}
			if !reflect.DeepEqual(results[0], results[1]) {
				t.Fatalf("%s %q changed results", mode, prefix)
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `SET LOCAL plan_cache_mode=force_generic_plan;
 PREPARE prefix_plan(text,text) AS SELECT k FROM survey_prefix WHERE k >= $1 COLLATE "C" AND k < $2 COLLATE "C" ORDER BY k LIMIT 20001`); err != nil {
		t.Fatal(err)
	}
	defer tx.ExecContext(ctx, `DEALLOCATE prefix_plan`)
	rows, err := tx.QueryContext(ctx, `EXPLAIN EXECUTE prefix_plan('u/note/00004999/','u/note/000049990')`)
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&plan, line)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plan.String(), "Index") || strings.Contains(plan.String(), "Seq Scan") {
		t.Fatal(plan.String())
	}
}
