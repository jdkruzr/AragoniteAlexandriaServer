package relay

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Rhizome's shared conformance suite, run through the real PostgreSQL apply
// path (UltraBridge ran it through an in-memory merge only). The same JSON is
// run by the Kotlin client; neither side is the source of truth.
const vectorsDir = "../../../../rhizome/conformance/vectors"

type vector struct {
	Category      string                     `json:"category"`
	Name          string                     `json:"name"`
	Ops           []Op                       `json:"ops"`
	ExpectedState map[string][]expectedState `json:"expected_state"`
}
type expectedState struct {
	PK     string         `json:"pk"`
	SiteID string         `json:"site_id"`
	OpSeq  int64          `json:"op_seq"`
	OpTS   int64          `json:"op_ts"`
	Cols   map[string]any `json:"cols"`
}

func TestRhizomeMergeVectorsThroughPostgreSQL(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(vectorsDir, "*.vector.json"))
	if err != nil || len(paths) == 0 {
		if os.Getenv("ALEXANDRIA_REQUIRE_RHIZOME_VECTORS") == "1" {
			t.Fatalf("no Rhizome vectors at %s", vectorsDir)
		}
		t.Skip("sibling ../rhizome checkout not present")
	}
	ran := 0
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var v vector
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if v.Category != "merge" {
			continue
		}
		ran++
		for _, reverse := range []bool{false, true} {
			name := filepath.Base(p)
			if reverse {
				name += "/reversed"
			}
			t.Run(name, func(t *testing.T) {
				db := library(t)
				ops := append([]Op(nil), v.Ops...)
				if reverse {
					for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
						ops[i], ops[j] = ops[j], ops[i]
					}
				}
				// Older vectors predate later nullable columns: complete them
				// as an older client's row would arrive (absent = null).
				for i := range ops {
					ops[i] = withV5Defaults(ops[i])
					cols := map[string]any{}
					for k, val := range ops[i].Cols {
						cols[k] = val
					}
					for _, c := range knownCols[ops[i].Table] {
						if _, ok := cols[c]; !ok {
							cols[c] = nil
						}
					}
					ops[i].Cols = cols
				}
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if err := lockWriter(ctx, tx); err != nil {
					t.Fatal(err)
				}
				res, err := Store{DB: db}.applyBatchTx(ctx, tx, ops[0].SiteID, ops, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(res.Rejected) != 0 {
					t.Fatalf("rejected: %+v", res.Rejected)
				}
				for table, rows := range v.ExpectedState {
					if _, ok := mirrors[table]; !ok {
						t.Fatalf("unknown table %s", table)
					}
					got := map[string]expectedState{}
					q, err := tx.Query(`SELECT * FROM fn_` + table)
					if err != nil {
						t.Fatal(err)
					}
					names, _ := q.Columns()
					for q.Next() {
						values := make([]any, len(names))
						ptrs := make([]any, len(names))
						for i := range values {
							ptrs[i] = &values[i]
						}
						if err := q.Scan(ptrs...); err != nil {
							t.Fatal(err)
						}
						row := expectedState{Cols: map[string]any{}}
						for i, n := range names {
							switch n {
							case "id":
								row.PK = values[i].(string)
							case "lww_wall_ts":
								row.OpTS = values[i].(int64)
							case "lww_op_seq":
								row.OpSeq = values[i].(int64)
							case "lww_site_id":
								row.SiteID = values[i].(string)
							default:
								row.Cols[n] = jsonish(values[i])
							}
						}
						got[row.PK] = row
					}
					q.Close()
					if len(got) != len(rows) {
						t.Fatalf("%s: %d rows, want %d", table, len(got), len(rows))
					}
					for _, want := range rows {
						have, ok := got[want.PK]
						if !ok || have.SiteID != want.SiteID || have.OpSeq != want.OpSeq || have.OpTS != want.OpTS {
							t.Fatalf("%s/%s provenance: %+v want %+v", table, want.PK, have, want)
						}
						for k, wantValue := range want.Cols {
							if _, known := have.Cols[k]; !known {
								continue // unknown columns are ignored on materialize
							}
							if !reflect.DeepEqual(have.Cols[k], jsonish(wantValue)) {
								t.Fatalf("%s/%s.%s = %#v, want %#v", table, want.PK, k, have.Cols[k], wantValue)
							}
						}
					}
				}
			})
		}
	}
	if ran < 20 {
		t.Fatalf("only %d merge vectors ran", ran)
	}
}

// jsonish puts database and JSON values in one comparable shape.
func jsonish(v any) any {
	switch x := v.(type) {
	case int64:
		return float64(x)
	case []byte:
		return base64.StdEncoding.EncodeToString(x)
	case string:
		return strings.Clone(x)
	}
	return v
}
