// Package relay is the server-side op log, deterministic LWW merge and
// bounded exchange for Alexandria shared-library sync. Selectively ported
// from UltraBridge (internal/syncstore, libraryhost exchange) under Apache-2.0.
//
// PostgreSQL adaptation: SQLite's single writer becomes the sync_seq row taken
// FOR UPDATE at the start of every mutating transaction, after the library
// generation row FOR SHARE (lock order: generation, then sync_seq). Writer ops
// keep UB's float64 column codec; reader rows keep their exact payloads.
package relay

import (
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/wire"
	"github.com/jdkruzr/rhizome/server-go/registry"
	rzsync "github.com/jdkruzr/rhizome/server-go/syncstore"
)

// Op is one full-row UPSERT on the wire. Its identity is (SiteID, OpSeq).
// WallTS is the HLC ordering key, serialized as op_ts.
type Op struct {
	Table  string         `json:"table"`
	PK     string         `json:"pk"`
	SiteID string         `json:"site_id"`
	OpSeq  int64          `json:"op_seq"`
	WallTS int64          `json:"op_ts"`
	Cols   map[string]any `json:"cols"`
}

// TablePK identifies a materialized row.
type TablePK struct {
	Table string
	PK    string
}

// RejectedOp identifies a permanently refused op.
type RejectedOp struct {
	SiteID string `json:"site_id"`
	OpSeq  int64  `json:"op_seq"`
	Reason string `json:"reason"`
}

// ForestNote's synced writer schema; the registry guarantees column order.
var knownCols = registry.ForestNote().KnownCols()

// Less compares (op_ts, op_seq, site_id) through Rhizome's rule, so ordering
// can never drift from the client's. The GREATER key wins a conflict.
func Less(a, b Op) bool {
	return rzsync.Less(
		rzsync.Op{SiteID: a.SiteID, OpSeq: a.OpSeq, OpTs: a.WallTS},
		rzsync.Op{SiteID: b.SiteID, OpSeq: b.OpSeq, OpTs: b.WallTS},
	)
}

// withV5Defaults completes an older writer row before validation and relay.
// Copy first so callers' maps are never mutated.
func withV5Defaults(op Op) Op {
	cols := make(map[string]any, len(op.Cols)+4)
	for k, v := range op.Cols {
		cols[k] = v
	}
	if op.Table == "notebook" {
		for _, c := range []string{"page_width", "page_height"} {
			if _, ok := cols[c]; !ok {
				cols[c] = nil
			}
		}
	}
	if op.Table == "stroke" {
		defaults := map[string]any{"brush_kind": "fountain", "brush_version": float64(1), "brush_seed": float64(0), "point_dynamics": nil}
		for c, v := range defaults {
			if _, ok := cols[c]; !ok {
				cols[c] = v
			}
		}
	}
	op.Cols = cols
	return op
}

// Normalize keeps only columns known for the op's table (unknown columns are
// dropped on materialize, never on relay).
func Normalize(op Op) Op {
	known := knownCols[op.Table]
	cols := make(map[string]any, len(known))
	for _, c := range known {
		if v, ok := op.Cols[c]; ok {
			cols[c] = v
		}
	}
	out := op
	out.Cols = cols
	return out
}

// validateOp returns "" if op is structurally acceptable, else a permanent
// rejection reason. Value types are checked when the winning op materializes.
func validateOp(op Op) string {
	known, ok := knownCols[op.Table]
	if !ok {
		return "unknown table"
	}
	if !wire.IsULID(op.PK) {
		return "pk is not a ULID"
	}
	if !wire.IsULID(op.SiteID) {
		return "site_id is not a ULID"
	}
	if op.OpSeq <= 0 {
		return "op_seq must be > 0"
	}
	for _, c := range known {
		if _, present := op.Cols[c]; !present {
			return "missing column: " + c
		}
	}
	return ""
}

// pgText makes a value storable in a PostgreSQL text column, which cannot hold
// U+0000 (SQLite can). Only server-side mirror copies are affected; the relay
// payload keeps the exact escaped value, so devices still converge byte-for-byte.
func pgText(s string) string { return strings.ReplaceAll(s, "\x00", "�") }
