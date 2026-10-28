package recognition

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

// The header badge counts the pages the Jobs list shows as needing the owner:
// current failed and blocked jobs, never cleared or superseded attempts.
func TestAttentionCountsCurrentFailedAndBlockedPages(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	add := func(source, state string, cleaned, superseded bool) {
		t.Helper()
		var sup any
		if superseded {
			sup = uuid.New()
		}
		if _, e := db.ExecContext(ctx, `INSERT INTO alexandria_recognition_job(id,source,notebook_id,page_id,state,cleaned_at,superseded_by) VALUES($1,$2,'synthetic',$3,$4,CASE WHEN $5 THEN now() END,$6)`, uuid.New(), source, uuid.NewString(), state, cleaned, sup); e != nil {
			t.Fatal(e)
		}
	}
	s := Store{DB: db}
	if n, e := s.Attention(ctx); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	add("boox", "failed", false, false)
	add("client", "blocked", false, false)
	add("boox", "failed", true, false)  // cleared from the list
	add("boox", "blocked", false, true) // a newer attempt replaced it
	add("boox", "ready", false, false)
	add("client", "queued", false, false)
	n, e := s.Attention(ctx)
	if e != nil || n != 2 {
		t.Fatal(n, e)
	}
	v, e := s.List(ctx, Filter{})
	if e != nil || v.Counts["failed"]+v.Counts["blocked"] != n {
		t.Fatal("badge disagrees with the Jobs list", v.Counts, e)
	}

	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SET LOCAL enable_seqscan=off`); e != nil {
		t.Fatal(e)
	}
	rows, e := tx.QueryContext(ctx, `EXPLAIN SELECT count(*) FROM alexandria_recognition_job WHERE state IN ('failed','blocked') AND cleaned_at IS NULL AND superseded_by IS NULL`)
	if e != nil {
		t.Fatal(e)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		_ = rows.Scan(&line)
		plan.WriteString(line + "\n")
	}
	rows.Close()
	if !strings.Contains(plan.String(), "alexandria_recognition_job_attention") {
		t.Fatalf("the count can't use its partial index:\n%s", plan.String())
	}
}
