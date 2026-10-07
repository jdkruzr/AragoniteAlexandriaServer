package relay

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/reader"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/wire"
	"github.com/jdkruzr/rhizome/server-go/bounded"
)

// MaxDeviceNameLen caps the stored device label in runes. Over-long names are
// truncated, never rejected: a cosmetic label must not brick sync.
const MaxDeviceNameLen = 128

var readerTables = contract.Registry().ByName()

// SchemaHash is the only hash this relay accepts: ForestNote writer plus reader.
var SchemaHash = contract.CandidateCombined().SchemaHash()

// Exchange applies one bounded push and returns one bounded pull page. It
// commits ONLY after the exact encoded response is known to fit, so a refused
// response never commits an ACK, receipt, HLC, mirror or log change.
// verifiedSite comes from the device credential, never the request body, and
// ctx must carry the request's generation admission.
func (s Store) Exchange(ctx context.Context, req bounded.Request, limits bounded.Limits, verifiedSite string) ([]byte, []TablePK, error) {
	if req.ProtocolVersion != 1 || req.SchemaHash != SchemaHash {
		return nil, nil, bounded.Fail(409, "reader_candidate_schema_required")
	}
	if !wire.IsULID(verifiedSite) || req.SiteID != verifiedSite {
		return nil, nil, bounded.Fail(403, "site_binding_mismatch")
	}
	if req.Cursor < 0 {
		return nil, nil, bounded.Fail(400, "bad_cursor")
	}
	if err := limits.Validate(); err != nil {
		return nil, nil, err
	}
	if err := req.ValidateRows(bounded.Defaults()); err != nil {
		return nil, nil, err
	}
	name := []rune(strings.TrimSpace(req.DeviceName))
	if len(name) > MaxDeviceNameLen {
		name = name[:MaxDeviceNameLen]
	}
	deviceName := pgText(string(name))

	var readerRaw [][]byte
	var writerOps []Op
	var entries []reader.RelayEntry
	total := 0
	for _, raw := range req.Ops {
		total += len(raw)
		if total > reader.MaxBatchBytes {
			return nil, nil, bounded.Fail(413, "request_body_too_large")
		}
		var identity contract.WireOp
		if err := json.Unmarshal(raw, &identity); err != nil || identity.OpSeq <= 0 || identity.OpTS < 0 {
			return nil, nil, bounded.Fail(400, "invalid_op")
		}
		if identity.SiteID != verifiedSite {
			return nil, nil, bounded.Fail(403, "site_binding_mismatch")
		}
		if _, ok := readerTables[identity.Table]; ok {
			readerRaw = append(readerRaw, raw)
			continue
		}
		var op Op
		if err := json.Unmarshal(raw, &op); err != nil {
			return nil, nil, bounded.Fail(400, "invalid_op")
		}
		op = withV5Defaults(op)
		payload, err := json.Marshal(op)
		if err != nil {
			return nil, nil, err
		}
		writerOps = append(writerOps, op)
		entries = append(entries, reader.RelayEntry{Table: op.Table, PK: op.PK, SiteID: op.SiteID, OpSeq: op.OpSeq, OpTS: op.WallTS, Payload: string(payload)})
	}
	p, err := reader.Prepare(verifiedSite, readerRaw)
	if err != nil {
		if errors.Is(err, reader.ErrBudget) {
			return nil, nil, bounded.Fail(413, "reader_budget_exceeded")
		}
		return nil, nil, bounded.Fail(400, "invalid_reader_envelope")
	}
	readerEntries := p.RelayEntries()
	entries = append(entries, readerEntries...)
	// Canonical identity comparisons are prepared before acquiring the writer.
	seen := map[int64]string{}
	for _, e := range entries {
		payload, err := canonicalPayload(e.Payload)
		if err != nil {
			return nil, nil, bounded.Fail(400, "invalid_op")
		}
		if old, ok := seen[e.OpSeq]; ok && old != payload {
			return nil, nil, bounded.Fail(409, "operation_identity_reused")
		}
		seen[e.OpSeq] = payload
	}

	receipt := func(ctx context.Context, tx *sql.Tx, site string, seq int64) (bool, error) {
		var found int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM reader_store_incoming WHERE site_id=$1 AND op_seq=$2`, site, seq).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return err == nil, err
	}
	extra := &batchExtension{
		preservePayload: func(table string) bool { _, ok := readerTables[table]; return ok },
		receipt:         receipt,
	}
	extra.stage = func(ctx context.Context, tx *sql.Tx, now int64) (int64, []RejectedOp, error) {
		// Check collisions across BOTH halves before generic writer dedup can hide
		// one. Never materialize an oversized historical relay payload.
		for _, e := range entries {
			var old sql.NullString
			var oversized bool
			err := tx.QueryRowContext(ctx, `SELECT octet_length(payload) > $1, CASE WHEN octet_length(payload) <= $1 THEN payload END
				FROM sync_ops WHERE site_id=$2 AND op_seq=$3`, reader.MaxRowBytes, e.SiteID, e.OpSeq).Scan(&oversized, &old)
			if err == nil {
				if oversized {
					return 0, nil, bounded.Fail(413, "historical_identity_too_large")
				}
				// Both paths persist deterministic payloads: canonical JSON for
				// readers, the Op codec for writers.
				if old.String != e.Payload {
					return 0, nil, bounded.Fail(409, "operation_identity_reused")
				}
			} else if !errors.Is(err, sql.ErrNoRows) {
				return 0, nil, err
			}
			if _, isReader := readerTables[e.Table]; !isReader {
				found, err := receipt(ctx, tx, e.SiteID, e.OpSeq)
				if err != nil {
					return 0, nil, err
				}
				if found {
					return 0, nil, bounded.Fail(409, "operation_identity_reused")
				}
			}
		}
		if err := p.CommitTx(ctx, tx); err != nil {
			if errors.Is(err, reader.ErrIdentityReused) {
				return 0, nil, bounded.Fail(409, "operation_identity_reused")
			}
			if errors.Is(err, reader.ErrBudget) {
				return 0, nil, bounded.Fail(413, "historical_identity_too_large")
			}
			return 0, nil, err
		}
		var maxTS int64
		var rejected []RejectedOp
		for _, e := range readerEntries {
			if e.OpTS > maxTS {
				maxTS = e.OpTS
			}
			if e.Reason != "" {
				rejected = append(rejected, RejectedOp{SiteID: e.SiteID, OpSeq: e.OpSeq, Reason: e.Reason})
				continue
			}
			var found int
			err := tx.QueryRowContext(ctx, `SELECT 1 FROM sync_ops WHERE site_id=$1 AND op_seq=$2`, e.SiteID, e.OpSeq).Scan(&found)
			if err == nil {
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return 0, nil, err
			}
			op := Op{Table: e.Table, PK: e.PK, SiteID: e.SiteID, OpSeq: e.OpSeq, WallTS: e.OpTS}
			if err := appendPayload(ctx, tx, op, e.Payload, now); err != nil {
				return 0, nil, err
			}
		}
		return maxTS, rejected, nil
	}
	return s.exchange(ctx, req, limits, verifiedSite, deviceName, writerOps, extra)
}

func (s Store) exchange(ctx context.Context, req bounded.Request, limits bounded.Limits, site, deviceName string, ops []Op, extra *batchExtension) ([]byte, []TablePK, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	// Generation first (FOR SHARE: waits behind a restore publish, then fails),
	// then the writer lock. Nothing is read or written before both are held.
	if err := generation.CheckTx(ctx, tx, site); err != nil {
		if errors.Is(err, generation.ErrReplaced) {
			return nil, nil, bounded.Fail(409, "library_replaced")
		}
		return nil, nil, err
	}
	if err := lockWriter(ctx, tx); err != nil {
		return nil, nil, err
	}
	var epoch string
	if err := tx.QueryRowContext(ctx, `SELECT epoch FROM sync_epoch WHERE id = 0`).Scan(&epoch); err != nil {
		return nil, nil, err
	}
	if req.Epoch != "" && req.Epoch != epoch {
		return rewound(ctx, tx, site, epoch, limits)
	}
	res, err := s.applyBatchTx(ctx, tx, site, ops, extra)
	if err != nil {
		return nil, nil, err
	}
	rejected, err := json.Marshal(res.Rejected)
	if err != nil {
		return nil, nil, err
	}
	page, err := bounded.NewPage(req.Cursor, res.AcceptedThrough, rejected, limits)
	if err != nil {
		return nil, nil, err
	}
	if _, err := page.WithEpoch(epoch); err != nil {
		return nil, nil, err
	}
	after := req.Cursor
	for {
		// A bounded scalar probe never loads an oversized historical payload, nor
		// the payload of the count-limit lookahead.
		limit := limits.MaxRowBytes
		if page.CountFull() {
			limit = 0
		}
		var seq, opSeq, size int64
		var opSite, table string
		var payload sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT seq, site_id, op_seq, table_name, octet_length(payload),
			CASE WHEN octet_length(payload) <= $1 THEN payload END
			FROM sync_ops WHERE seq > $2 AND site_id <> $3 ORDER BY seq LIMIT 1`, limit, after, site).
			Scan(&seq, &opSite, &opSeq, &table, &size, &payload)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		if page.CountFull() {
			page.Response.HasMore = true
			break
		}
		var raw []byte
		if payload.Valid {
			if extra != nil && extra.preservePayload(table) {
				raw = []byte(payload.String)
			} else {
				var op Op
				if err := json.Unmarshal([]byte(payload.String), &op); err != nil {
					return nil, nil, err
				}
				if raw, err = json.Marshal(withV5Defaults(op)); err != nil {
					return nil, nil, err
				}
				size = int64(len(raw))
			}
		}
		added, err := page.Add(seq, opSite, opSeq, size, raw)
		if err != nil {
			return nil, nil, err
		}
		if !added {
			break
		}
		after = seq
	}
	body, err := page.Encode()
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sync_cursors SET last_pull_seq=$1, updated_at=$2,
		device_name=CASE WHEN $3<>'' THEN $3 ELSE device_name END WHERE site_id=$4`,
		page.Response.Cursor, time.Now().UnixMilli(), deviceName, site); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return body, res.ChangedPages, nil
}

func canonicalPayload(payload string) (string, error) {
	decoder := json.NewDecoder(bytes.NewBufferString(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	b, err := json.Marshal(value)
	return string(b), err
}

// rewound answers a device that has not seen the current epoch: this server's
// history was rewound (restored from a backup) after the device last synced.
// Its ops are numbered past what this server holds, so none are applied or
// logged. The device re-queues what it authored after accepted_through and
// re-pulls from cursor 0 (Rhizome spec/protocol.md "Server rewind").
func rewound(ctx context.Context, tx *sql.Tx, site, epoch string, limits bounded.Limits) ([]byte, []TablePK, error) {
	var acked int64
	err := tx.QueryRowContext(ctx, `SELECT acked_op_seq FROM sync_cursors WHERE site_id = $1`, site).Scan(&acked)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	page, err := bounded.NewPage(0, acked, nil, limits)
	if err != nil {
		return nil, nil, err
	}
	if _, err := page.WithEpoch(epoch); err != nil {
		return nil, nil, err
	}
	page.Response.HasMore = true
	body, err := page.Encode()
	if err != nil {
		return nil, nil, err
	}
	return body, nil, tx.Commit()
}
