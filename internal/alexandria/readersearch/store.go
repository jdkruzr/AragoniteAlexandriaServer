// Package readersearch indexes reader annotations for search: book title,
// highlighted quote and the current recognition (or human correction).
// Ported from UltraBridge (internal/readersearch) under Apache-2.0.
//
// PostgreSQL adaptation: journal changes become durable jobs in Pump (under
// the reader lock, before the journal cursor advances), jobs are claimed with
// a lease so gateway and worker processes can share them, and each publish
// holds the library generation FOR SHARE.
package readersearch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/reader"
)

var ErrChanged = errors.New("reader search snapshot changed; retry")

// jobLease bounds how long a crashed indexer can hold a job.
const jobLease = 60 * time.Second

type Store struct {
	db     pg.DB
	reader reader.Store
}

func New(db pg.DB) *Store { return &Store{db: db, reader: reader.Store{DB: db}} }

// Schedule returns only after a durable, idempotent handoff; the journal
// checkpoint may then advance.
func (s *Store) Schedule(ctx context.Context, c reader.Change) error {
	res, err := s.db.ExecContext(ctx, `INSERT INTO reader_search_jobs(seq,table_name,pk)
		SELECT seq,table_name,pk FROM reader_store_changes WHERE seq=$1 AND table_name=$2 AND pk=$3 ON CONFLICT (seq) DO NOTHING`, c.Seq, c.Key.Table, c.Key.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var found int
		if err = s.db.QueryRowContext(ctx, `SELECT 1 FROM reader_search_jobs WHERE seq=$1 AND table_name=$2 AND pk=$3`, c.Seq, c.Key.Table, c.Key.ID).Scan(&found); err != nil {
			return fmt.Errorf("invalid reader change handoff: %w", err)
		}
	}
	return nil
}

// Pump hands every undelivered journal change to the job queue, advancing the
// journal one entry at a time only after each durable handoff.
func (s *Store) Pump(ctx context.Context) (int, error) {
	n := 0
	for {
		changes, err := s.reader.Changes(ctx, 128)
		if err != nil || len(changes) == 0 {
			return n, err
		}
		for _, c := range changes {
			if err := s.Schedule(ctx, c); err != nil {
				return n, err
			}
			if err := s.reader.CompleteChange(ctx, c.Seq); err != nil {
				return n, err
			}
			n++
		}
	}
}

type job struct {
	seq              int64
	table, pk, after string
}

// Step handles at most limit annotation targets of one job; each target
// commits its index row and fan-out cursor atomically.
func (s *Store) Step(ctx context.Context, limit int) (bool, error) {
	if limit < 1 || limit > 128 {
		return false, reader.ErrBudget
	}
	now := time.Now().UnixMilli()
	var j job
	err := s.db.QueryRowContext(ctx, `UPDATE reader_search_jobs SET retry_at=$2 WHERE seq=(SELECT seq FROM reader_search_jobs
		WHERE NOT done AND retry_at<=$1 ORDER BY seq LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING seq,table_name,pk,after_id`,
		now, now+jobLease.Milliseconds()).Scan(&j.seq, &j.table, &j.pk, &j.after)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err = s.process(ctx, j, limit); err != nil {
		delay := int64(1000)
		if errors.Is(err, ErrChanged) {
			delay = 25
		}
		// Failure stays visible and durable; other ready jobs are not starved
		// behind an unsupported or oversized annotation.
		if ctx.Err() == nil {
			if _, e := s.db.ExecContext(ctx, `UPDATE reader_search_jobs SET retry_at=$1,error_code='projection_failed' WHERE seq=$2`, time.Now().UnixMilli()+delay, j.seq); e != nil {
				return true, errors.Join(err, e)
			}
		}
		return true, err
	}
	return true, nil
}

func (s *Store) process(ctx context.Context, j job, limit int) error {
	condition := affects(j.table, "$1")
	query := `SELECT a.id FROM fn_reader_annotation a WHERE ` + condition + ` AND a.id>$2 ORDER BY a.id LIMIT $3`
	args := []any{j.pk, j.after, limit}
	if condition == "false" {
		query = `SELECT a.id FROM fn_reader_annotation a WHERE false AND a.id>$1 LIMIT $2`
		args = []any{j.after, limit}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		input, err := s.reader.SearchSnapshot(ctx, id, reader.DefaultLimits())
		if err != nil {
			return err
		}
		doc, err := project(input)
		if err != nil {
			return err
		}
		if err = s.publish(ctx, j.seq, id, input.Revision, doc); err != nil {
			return err
		}
	}
	if len(ids) < limit {
		_, err = s.db.ExecContext(ctx, `UPDATE reader_search_jobs SET done=true,error_code='',retry_at=0 WHERE seq=$1`, j.seq)
	} else {
		_, err = s.db.ExecContext(ctx, `UPDATE reader_search_jobs SET retry_at=0 WHERE seq=$1`, j.seq)
	}
	return err
}

func (s *Store) publish(ctx context.Context, seq int64, id string, revision int64, d *document) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var generation string
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); err != nil {
		return err
	}
	var changed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fn_reader_annotation a WHERE a.id=$1 AND `+dirty("$2")+`)`, id, revision).Scan(&changed); err != nil {
		return err
	}
	if changed {
		return ErrChanged
	}
	if d == nil {
		if _, err = tx.ExecContext(ctx, `DELETE FROM reader_search_documents WHERE annotation_id=$1`, id); err != nil {
			return err
		}
	} else if _, err = tx.ExecContext(ctx, `INSERT INTO reader_search_documents(annotation_id,book_id,title,quote,recognized_text,anchor,input_hash,alternatives,revision)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (annotation_id) DO UPDATE SET book_id=EXCLUDED.book_id,title=EXCLUDED.title,quote=EXCLUDED.quote,recognized_text=EXCLUDED.recognized_text,
		anchor=EXCLUDED.anchor,input_hash=EXCLUDED.input_hash,alternatives=EXCLUDED.alternatives,revision=EXCLUDED.revision`,
		id, d.book, pgText(d.title), pgText(d.quote), pgText(d.text), pgText(d.anchor), d.hash, pgText(d.alternatives), revision); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE reader_search_jobs SET after_id=$1,error_code='' WHERE seq=$2`, id, seq); err != nil {
		return err
	}
	return tx.Commit()
}

// ReplaceDerived runs inside an authoritative restore: the restored journal
// rebuilds every document through Pump.
func ReplaceDerived(ctx context.Context, tx *sql.Tx) error {
	for _, q := range []string{`DELETE FROM reader_search_documents`, `DELETE FROM reader_search_jobs`} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
