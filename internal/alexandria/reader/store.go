package reader

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	merge "github.com/jdkruzr/rhizome/server-go/syncstore"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

// Store materializes the reader inbox. PostgreSQL adaptation of UltraBridge's
// readerstore: the SQLite writer becomes the generation row FOR SHARE plus the
// ReaderLock advisory lock, and snapshots use REPEATABLE READ, because READ
// COMMITTED would give each statement its own snapshot.
type Store struct{ DB pg.DB }

type Key struct{ Table, ID string }
type Receipt struct {
	Seq           int64
	State, Reason string
}
type DrainResult struct {
	Records []Receipt
	Changed []Key
	Next    int64
}

// writer opens a transaction holding the drain locks in the global order.
func (s Store) writer(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var generation string
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); err == nil {
		err = pg.XactLock(ctx, tx, pg.ReaderLock)
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func snapshotTx(ctx context.Context, db pg.DB) (*sql.Tx, error) {
	return db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
}

type queued struct {
	seq     int64
	payload []byte
}

// Drain processes one bounded keyset page. Resume at Next; when zero, end the
// sweep. Begin a fresh sweep at zero after dependencies arrive.
func (s Store) Drain(ctx context.Context, after int64, limit int) (DrainResult, error) {
	if after < 0 || limit < 1 || limit > 128 {
		return DrainResult{}, ErrBudget
	}
	// Read and decode the page before taking the drain locks.
	queue, next, err := pendingPage(ctx, s.DB, after, limit)
	if err != nil {
		return DrainResult{}, err
	}
	tx, err := s.writer(ctx)
	if err != nil {
		return DrainResult{}, err
	}
	defer tx.Rollback()
	result, err := apply(ctx, tx, queue)
	if err != nil {
		return DrainResult{}, err
	}
	result.Next = next
	return result, tx.Commit()
}

// DrainTx is Drain inside a caller's transaction that already excludes every
// other writer (a restore publication holding the generation FOR UPDATE).
func DrainTx(ctx context.Context, tx *sql.Tx, after int64, limit int) (DrainResult, error) {
	if after < 0 || limit < 1 || limit > 128 {
		return DrainResult{}, ErrBudget
	}
	queue, next, err := pendingPage(ctx, tx, after, limit)
	if err != nil {
		return DrainResult{}, err
	}
	result, err := apply(ctx, tx, queue)
	result.Next = next
	return result, err
}

type decoded struct {
	seq int64
	op  contract.WireOp
	row contract.Record
}

func pendingPage(ctx context.Context, q pg.Querier, after int64, limit int) ([]decoded, int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT seq, octet_length(payload), CASE WHEN octet_length(payload) <= $1 THEN payload END
		FROM reader_store_incoming WHERE state='pending' AND seq>$2 ORDER BY seq LIMIT $3`, MaxRowBytes, after, limit+1)
	if err != nil {
		return nil, 0, err
	}
	type queued struct {
		seq     int64
		payload []byte
	}
	queue := []queued{}
	var next, size int64
	for rows.Next() {
		var seq, n int64
		var payload []byte
		if err = rows.Scan(&seq, &n, &payload); err != nil {
			break
		}
		if len(queue) == limit || size+n > MaxBatchBytes {
			if len(queue) == 0 {
				err = ErrBudget
			} else {
				next = queue[len(queue)-1].seq
			}
			break
		}
		if payload == nil {
			err = ErrBudget
			break
		}
		queue = append(queue, queued{seq, payload})
		size += n
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	out := make([]decoded, 0, len(queue))
	for _, q := range queue {
		op, r, e := contract.DecodeJSON(q.payload)
		if e != nil {
			return nil, 0, fmt.Errorf("corrupt pending row: %w", e)
		}
		out = append(out, decoded{q.seq, op, r})
	}
	return out, next, nil
}

func apply(ctx context.Context, tx *sql.Tx, prepared []decoded) (DrainResult, error) {
	var result DrainResult
	b := budget{rows: 1024, bytes: MaxBatchBytes}
	cache := map[Key]*contract.Record{}
	var lookupErr error
	lookup := func(table, id string) *contract.Record {
		key := Key{table, id}
		if r, ok := cache[key]; ok {
			return r
		}
		r, e := load(ctx, tx, table, id, &b)
		if e != nil {
			lookupErr = e
		}
		cache[key] = r
		return r
	}
	for _, p := range prepared {
		var state string
		if err := tx.QueryRowContext(ctx, `SELECT state FROM reader_store_incoming WHERE seq=$1`, p.seq).Scan(&state); err != nil {
			return DrainResult{}, err
		}
		if state != "pending" {
			continue
		}
		decision := contract.CheckPrepared(p.op.Table, p.row, lookup)
		if lookupErr != nil {
			return DrainResult{}, lookupErr
		}
		if decision.State == "applied" {
			old := lookup(p.op.Table, p.op.PK)
			if lookupErr != nil {
				return DrainResult{}, lookupErr
			}
			version := func(v *contract.Version) merge.Op {
				return merge.Op{SiteID: v.SiteID, OpSeq: v.OpSeq, OpTs: v.OpTS}
			}
			if old == nil || old.Version == nil || merge.Less(version(old.Version), version(p.row.Version)) {
				if err := upsert(ctx, tx, p.op.Table, p.row); err != nil {
					return DrainResult{}, err
				}
				key := Key{p.op.Table, p.op.PK}
				r := p.row
				cache[key] = &r
				result.Changed = append(result.Changed, key)
				if _, err := tx.ExecContext(ctx, `INSERT INTO reader_store_changes(table_name,pk) VALUES($1,$2)`, key.Table, key.ID); err != nil {
					return DrainResult{}, err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reader_store_incoming SET state=$1, reason=$2 WHERE seq=$3`, decision.State, decision.Reason, p.seq); err != nil {
			return DrainResult{}, err
		}
		result.Records = append(result.Records, Receipt{p.seq, decision.State, decision.Reason})
	}
	return result, nil
}

// pgText stores a string PostgreSQL text can hold. The contract already
// rejects U+0000 in identifiers; free text keeps every other character.
func pgText(s string) string { return strings.ReplaceAll(s, "\x00", "�") }

func upsert(ctx context.Context, tx *sql.Tx, table string, r contract.Record) error {
	cols := columns(table)
	names := make([]string, 0, len(cols))
	marks := make([]string, 0, len(cols))
	updates := []string{}
	args := []any{r.ID}
	for _, c := range definitions[table].Columns {
		v := r.Columns[c.Name]
		if s, ok := v.(string); ok {
			v = pgText(s)
		}
		args = append(args, v)
	}
	args = append(args, r.Version.OpTS, r.Version.OpSeq, r.Version.SiteID)
	for i, c := range cols {
		names = append(names, c.name)
		marks = append(marks, fmt.Sprintf("$%d", i+1))
		if c.name != "id" {
			updates = append(updates, c.name+"=EXCLUDED."+c.name)
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO fn_"+table+"("+strings.Join(names, ",")+") VALUES("+strings.Join(marks, ",")+") ON CONFLICT(id) DO UPDATE SET "+strings.Join(updates, ","), args...)
	return err
}

// Change is a durable invalidation, not a snapshot of the row at Seq.
type Change struct {
	Seq int64
	Key Key
}

// Changes returns one detached key-only page after the downstream checkpoint.
func (s Store) Changes(ctx context.Context, limit int) ([]Change, error) {
	if limit < 1 || limit > 128 {
		return nil, ErrBudget
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT seq,table_name,pk FROM reader_store_changes
		WHERE seq>(SELECT seq FROM reader_store_change_cursor WHERE id=1) ORDER BY seq LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var changes []Change
	for rows.Next() {
		var c Change
		if err := rows.Scan(&c.Seq, &c.Key.Table, &c.Key.ID); err != nil {
			return nil, err
		}
		changes = append(changes, c)
	}
	return changes, rows.Err()
}

// CompleteChange advances exactly one contiguous journal entry. Replaying a
// completed entry is safe; an old callback cannot clear newer work.
func (s Store) CompleteChange(ctx context.Context, seq int64) error {
	if seq <= 0 {
		return fmt.Errorf("invalid change sequence")
	}
	tx, err := s.writer(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var cursor int64
	if err = tx.QueryRowContext(ctx, `SELECT seq FROM reader_store_change_cursor WHERE id=1`).Scan(&cursor); err != nil {
		return err
	}
	if seq <= cursor {
		return tx.Commit()
	}
	var next sql.NullInt64
	if err = tx.QueryRowContext(ctx, `SELECT MIN(seq) FROM reader_store_changes WHERE seq>$1`, cursor).Scan(&next); err != nil {
		return err
	}
	if !next.Valid || seq != next.Int64 {
		return fmt.Errorf("change completion would skip pending work")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE reader_store_change_cursor SET seq=$1 WHERE id=1`, seq); err != nil {
		return err
	}
	return tx.Commit()
}

// Sweep drains every currently pending row in bounded pages, repeating while
// rows change state (a later row can satisfy an earlier one's dependency). It
// is the worker's bounded step; a fresh sweep starts at zero next time.
func (s Store) Sweep(ctx context.Context, pageSize, maxPages int) (changed int, err error) {
	for pages := 0; pages < maxPages; {
		var after int64
		progress := false
		for {
			page, err := s.Drain(ctx, after, pageSize)
			if err != nil {
				return changed, err
			}
			pages++
			changed += len(page.Changed)
			for _, r := range page.Records {
				if r.State != "pending" {
					progress = true
				}
			}
			after = page.Next
			if after == 0 || pages >= maxPages {
				break
			}
		}
		if !progress || after != 0 {
			return changed, nil
		}
	}
	return changed, nil
}

// HasPending reports whether any inbox row awaits materialization.
func (s Store) HasPending(ctx context.Context) (bool, error) {
	var pending bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM reader_store_incoming WHERE state='pending')`).Scan(&pending)
	return pending, err
}
