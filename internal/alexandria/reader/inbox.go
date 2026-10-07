// Package reader stores Alexandria reader rows: the receive inbox here, and
// (later) materialization into the reader mirror tables. Selectively ported
// from UltraBridge (internal/readerstore) under Apache-2.0.
package reader

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
)

const MaxRowBytes = 8 * 1024 * 1024
const MaxBatchBytes = 16 * 1024 * 1024

var ErrBudget = errors.New("reader storage budget exceeded; no partial result")
var ErrIdentityReused = errors.New("incoming operation identity reused")

var definitions = contract.Registry().ByName()

type preparedRow struct {
	op            contract.WireOp
	payload       []byte
	state, reason string
}

// Prepared owns canonical immutable payload copies, never the caller's slices/maps.
type Prepared struct{ rows []preparedRow }

// RelayEntry is immutable metadata for the host's existing relay transaction.
// Reason is nonempty only for a shape-invalid row: retain its receipt but do not
// relay it. Valid pending rows still need the dependency/ownership gate in Drain.
type RelayEntry struct {
	Table, PK, SiteID, Payload, Reason string
	OpSeq, OpTS                        int64
}

func (p *Prepared) RelayEntries() []RelayEntry {
	out := make([]RelayEntry, len(p.rows))
	for i, r := range p.rows {
		out[i] = RelayEntry{r.op.Table, r.op.PK, r.op.SiteID, string(r.payload), r.reason, r.op.OpSeq, r.op.OpTS}
	}
	return out
}

// Prepare runs decoding, canonicalization and full ink validation before the
// writer lock. verifiedSite must come from credential binding, not request.site_id.
// Invalid envelopes/identity/auth reject the batch; malformed domain rows with a
// usable identity are retained as quarantined diagnostic records.
func Prepare(verifiedSite string, rawOps [][]byte) (*Prepared, error) {
	if err := contract.RequireClientAuthor(contract.WireOp{SiteID: verifiedSite}, verifiedSite); err != nil {
		return nil, err
	}
	if len(rawOps) > 500 {
		return nil, ErrBudget
	}
	p := &Prepared{}
	size := 0
	canonicalSize := 0
	for _, raw := range rawOps {
		size += len(raw)
		if len(raw) > MaxRowBytes || size > MaxBatchBytes {
			return nil, ErrBudget
		}
		op, _, validation := contract.DecodeJSON(raw)
		if _, ok := definitions[op.Table]; !ok {
			return nil, fmt.Errorf("unknown reader table")
		}
		if err := contract.RequireClientAuthor(op, verifiedSite); err != nil {
			return nil, err
		}
		if op.OpSeq <= 0 || op.OpTS < 0 || len(op.PK) > 2048 {
			return nil, fmt.Errorf("invalid operation identity")
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		if len(object) != 6 {
			return nil, fmt.Errorf("invalid envelope")
		}
		for _, key := range []string{"table", "pk", "site_id", "op_ts", "op_seq", "cols"} {
			v, ok := object[key]
			if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
				return nil, fmt.Errorf("missing envelope field %s", key)
			}
		}
		// Canonicalize only the outer JSON; strings containing selectors remain exact.
		var value any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		canonical, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		// Invalid strings must not be silently repaired by Go JSON transcoding;
		// retain the original invalid payload for quarantine diagnostics/retries.
		if validation != nil {
			canonical = append([]byte(nil), raw...)
		}
		canonicalSize += len(canonical)
		if len(canonical) > MaxRowBytes || canonicalSize > MaxBatchBytes {
			return nil, ErrBudget
		}
		op.Cols = nil // Only envelope identity is needed after preparation.
		row := preparedRow{op: op, payload: canonical, state: "pending"}
		if validation != nil {
			row.state = "quarantined"
			row.reason = "Invalid reader row"
		}
		p.rows = append(p.rows, row)
	}
	return p, nil
}

// CommitTx participates in a HOST-OWNED transaction that already holds the
// relay writer lock. The caller MUST roll back on any error. It neither commits
// nor emits ACKs nor writes a second relay/clock/cursor.
func (p *Prepared) CommitTx(ctx context.Context, tx *sql.Tx) error {
	for _, r := range p.rows {
		var old []byte
		var oversized bool
		err := tx.QueryRowContext(ctx, `SELECT octet_length(payload) > $1, CASE WHEN octet_length(payload) <= $1 THEN payload END
			FROM reader_store_incoming WHERE site_id=$2 AND op_seq=$3`, MaxRowBytes, r.op.SiteID, r.op.OpSeq).Scan(&oversized, &old)
		if err == nil {
			if oversized {
				return ErrBudget
			}
			if !bytes.Equal(old, r.payload) {
				return ErrIdentityReused
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO reader_store_incoming(site_id,op_seq,payload,state,reason) VALUES($1,$2,$3,$4,$5)`,
			r.op.SiteID, r.op.OpSeq, r.payload, r.state, r.reason); err != nil {
			return err
		}
	}
	return nil
}
