package relay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/rhizome/server-go/hlc"
)

// Store is the relay over one admitted connection (or a pool in tests).
type Store struct{ DB pg.DB }

// lockWriter takes the relay's single-writer lock. Callers must already hold
// the generation row FOR SHARE (or FOR UPDATE) when the generation matters:
// the lock order is generation, then sync_seq.
func lockWriter(ctx context.Context, tx *sql.Tx) error {
	var seq int64
	return tx.QueryRowContext(ctx, `SELECT last_seq FROM sync_seq WHERE id = 1 FOR UPDATE`).Scan(&seq)
}

// ApplyResult reports one batch for the requesting device.
type ApplyResult struct {
	AcceptedThrough int64
	Rejected        []RejectedOp
	ChangedPages    []TablePK // pages whose live render input may have changed
}

// batchExtension lets the reader half participate after the pre-batch ACK read
// and before the shared merge/contiguous-ACK/HLC work.
type batchExtension struct {
	stage           func(context.Context, *sql.Tx, int64) (int64, []RejectedOp, error)
	receipt         func(context.Context, *sql.Tx, string, int64) (bool, error)
	preservePayload func(string) bool
}

func (s Store) applyBatchTx(ctx context.Context, tx *sql.Tx, siteID string, ops []Op, extra *batchExtension) (ApplyResult, error) {
	var res ApplyResult
	now := time.Now().UnixMilli()
	// Device ops keep their own op_ts; observing them only guarantees that the
	// server's NEXT authored op sorts after everything it has seen.
	var lastHlc int64
	if err := tx.QueryRowContext(ctx, `SELECT last_hlc FROM sync_site WHERE id = 1`).Scan(&lastHlc); err != nil {
		return res, fmt.Errorf("read last_hlc: %w", err)
	}
	// Read accepted_through BEFORE this batch's inserts. A missing cursor row
	// (new or pruned device) seeds from the pre-batch MAX(op_seq): walking from 0
	// would wedge at the first historic hole the client can never refill.
	var acked int64
	err := tx.QueryRowContext(ctx, `SELECT acked_op_seq FROM sync_cursors WHERE site_id = $1`, siteID).Scan(&acked)
	if errors.Is(err, sql.ErrNoRows) {
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(op_seq), 0) FROM sync_ops WHERE site_id = $1`, siteID).Scan(&acked)
	}
	if err != nil {
		return res, fmt.Errorf("read acked_op_seq: %w", err)
	}

	var maxIncoming int64
	rejectedSeqs := map[int64]bool{}
	reject := func(op Op, reason string) {
		res.Rejected = append(res.Rejected, RejectedOp{op.SiteID, op.OpSeq, reason})
		if op.SiteID == siteID {
			rejectedSeqs[op.OpSeq] = true
		}
	}
	if extra != nil {
		maxIncoming, res.Rejected, err = extra.stage(ctx, tx, now)
		if err != nil {
			return res, err
		}
		for _, r := range res.Rejected {
			rejectedSeqs[r.OpSeq] = true
		}
	}
	for _, incoming := range ops {
		op := withV5Defaults(incoming)
		if op.WallTS > maxIncoming {
			maxIncoming = op.WallTS // observe every op_ts, even a rejected one
		}
		if reason := validateOp(op); reason != "" {
			reject(op, reason)
			continue
		}
		var dummy int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM sync_ops WHERE site_id = $1 AND op_seq = $2`, op.SiteID, op.OpSeq).Scan(&dummy)
		if err == nil {
			continue // already settled
		} else if !errors.Is(err, sql.ErrNoRows) {
			return res, fmt.Errorf("dedup check: %w", err)
		}
		changed, pagePK, err := mergeRow(ctx, tx, Normalize(op))
		if err != nil {
			var malformed errMalformed
			if errors.As(err, &malformed) {
				reject(op, malformed.Error())
				continue
			}
			return res, err
		}
		// A losing op is still recorded for relay completeness.
		if err := appendOp(ctx, tx, op, now); err != nil {
			return res, err
		}
		if changed && pagePK != "" {
			res.ChangedPages = append(res.ChangedPages, TablePK{Table: "page", PK: pagePK})
		}
	}
	if err := MarkDirty(ctx, tx, res.ChangedPages); err != nil {
		return res, err
	}
	if res.AcceptedThrough, err = advanceAccepted(ctx, tx, siteID, acked, rejectedSeqs, now, extra); err != nil {
		return res, err
	}
	clock := hlc.New(lastHlc, func() int64 { return now })
	clock.ReceiveEvent(maxIncoming)
	if _, err := tx.ExecContext(ctx, `UPDATE sync_site SET last_hlc = $1 WHERE id = 1`, clock.Last()); err != nil {
		return res, fmt.Errorf("persist last_hlc: %w", err)
	}
	return res, nil
}

func appendOp(ctx context.Context, tx *sql.Tx, op Op, now int64) error {
	payload, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	return appendPayload(ctx, tx, op, string(payload), now)
}

// appendPayload assigns the next global seq under the writer lock and appends
// the exact relay payload.
func appendPayload(ctx context.Context, tx *sql.Tx, op Op, payload string, now int64) error {
	var seq int64
	if err := tx.QueryRowContext(ctx, `UPDATE sync_seq SET last_seq = last_seq + 1 WHERE id = 1 RETURNING last_seq`).Scan(&seq); err != nil {
		return fmt.Errorf("bump seq: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_ops (seq, site_id, op_seq, table_name, pk, wall_ts, payload, applied_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, seq, op.SiteID, op.OpSeq, op.Table, pgText(op.PK), op.WallTS, payload, now); err != nil {
		return fmt.Errorf("insert sync_ops: %w", err)
	}
	return nil
}

// advanceAccepted computes and persists the contiguous accepted_through: the
// greatest N such that every op_seq 1..N is settled (logged, received by the
// reader inbox, or permanently rejected this call). A poison op is counted
// once and never revisited, so it neither wedges the water nor is lost.
func advanceAccepted(ctx context.Context, tx *sql.Tx, siteID string, acked int64, rejectedSeqs map[int64]bool, now int64, extra *batchExtension) (int64, error) {
	h := acked
	for h < 1<<63-1 {
		next := h + 1
		if rejectedSeqs[next] {
			h = next
			continue
		}
		var dummy int
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM sync_ops WHERE site_id = $1 AND op_seq = $2`, siteID, next).Scan(&dummy)
		if err == nil {
			h = next
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("accepted walk: %w", err)
		}
		if extra != nil {
			found, err := extra.receipt(ctx, tx, siteID, next)
			if err != nil {
				return 0, err
			}
			if found {
				h = next
				continue
			}
		}
		break
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_cursors (site_id, acked_op_seq, updated_at) VALUES ($1, $2, $3)
		ON CONFLICT(site_id) DO UPDATE SET acked_op_seq = EXCLUDED.acked_op_seq, updated_at = EXCLUDED.updated_at`, siteID, h, now); err != nil {
		return 0, fmt.Errorf("persist acked_op_seq: %w", err)
	}
	return h, nil
}

// SiteID returns the server's own authoring ULID.
func (s Store) SiteID(ctx context.Context) (string, error) {
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT site_id FROM sync_site WHERE id = 1`).Scan(&id)
	return id, err
}

// LastSeq returns the current global high-water.
func (s Store) LastSeq(ctx context.Context) (int64, error) {
	var seq int64
	err := s.DB.QueryRowContext(ctx, `SELECT last_seq FROM sync_seq WHERE id = 1`).Scan(&seq)
	return seq, err
}

// MarkDirty queues pages whose render input changed for the page pipeline, in
// the caller's transaction (outbox): a committed change is never lost.
func MarkDirty(ctx context.Context, tx *sql.Tx, pages []TablePK) error {
	ids := make([]string, 0, len(pages))
	for _, p := range pages {
		if p.Table == "page" && p.PK != "" {
			ids = append(ids, pgText(p.PK))
		}
	}
	if len(ids) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO alexandria_page_dirty(page_id) SELECT DISTINCT unnest($1::text[])
		ON CONFLICT (page_id) DO UPDATE SET dirtied_at=clock_timestamp(), attempts=0, next_at=now()`, ids)
	return err
}

// AuthorOps makes the server a first-class authoring site: each op gets the
// server's site_id, the next op_seq and an op_ts from the durable HLC (strictly
// greater than anything observed), then merges and appends in ONE transaction.
// Callers pass full-row upserts; SiteID/OpSeq/WallTS are overwritten. Columns
// must look like a decoded device op (numbers as float64).
func (s Store) AuthorOps(ctx context.Context, ops []Op) ([]TablePK, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()
	// Same lock order as a device exchange: a restore publish waits for us or
	// we wait for it, never a deadlock.
	var generation string
	if err := tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id = 1 FOR SHARE`).Scan(&generation); err != nil {
		return nil, err
	}
	changed, err := AuthorOpsTx(ctx, tx, ops)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return changed, nil
}

// AuthorOpsTx is AuthorOps inside a caller's transaction, which must already
// hold the generation row FOR SHARE (lock order). It takes the writer lock.
func AuthorOpsTx(ctx context.Context, tx *sql.Tx, ops []Op) ([]TablePK, error) {
	if len(ops) == 0 {
		return nil, nil
	}
	if err := lockWriter(ctx, tx); err != nil {
		return nil, err
	}
	var siteID string
	var lastOpSeq, lastHlc int64
	if err := tx.QueryRowContext(ctx, `SELECT site_id, last_op_seq, last_hlc FROM sync_site WHERE id = 1`).Scan(&siteID, &lastOpSeq, &lastHlc); err != nil {
		return nil, fmt.Errorf("load authoring site: %w", err)
	}
	now := time.Now().UnixMilli()
	clock := hlc.New(lastHlc, func() int64 { return now })
	seq := lastOpSeq
	var changedPages []TablePK
	for _, authored := range ops {
		op := withV5Defaults(authored)
		seq++
		op.SiteID = siteID
		op.OpSeq = seq
		op.WallTS = clock.LocalEvent()
		if reason := validateOp(op); reason != "" {
			return nil, fmt.Errorf("author op %s/%s invalid: %s", op.Table, op.PK, reason)
		}
		changed, pagePK, err := mergeRow(ctx, tx, Normalize(op))
		if err != nil {
			return nil, fmt.Errorf("author merge %s/%s: %w", op.Table, op.PK, err)
		}
		if err := appendOp(ctx, tx, op, now); err != nil {
			return nil, err
		}
		if changed && pagePK != "" {
			changedPages = append(changedPages, TablePK{Table: "page", PK: pagePK})
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sync_site SET last_op_seq = $1, last_hlc = $2 WHERE id = 1`, seq, clock.Last()); err != nil {
		return nil, fmt.Errorf("persist op_seq/last_hlc: %w", err)
	}
	if err := MarkDirty(ctx, tx, changedPages); err != nil {
		return nil, err
	}
	return changedPages, nil
}
