package taskdb

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
)

// Locks are transaction scoped, including for IDs whose tombstone was pruned.
// Hash collisions only serialize unrelated tasks; they cannot weaken exclusion.
func lockTasks(ctx context.Context, tx *sql.Tx, ids []string) error {
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('alexandria-task:' || $1,0))`, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) write(ctx context.Context, ids []string, f func(*Store) error) error {
	tx, external := s.db.(*sql.Tx)
	if !external {
		var err error
		tx, err = s.db.(DB).BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
	}
	if err := lockTasks(ctx, tx, ids); err != nil {
		return err
	}
	if err := f(NewStore(tx)); err != nil {
		return err
	}
	if external {
		return nil
	}
	return tx.Commit()
}

func selectedIDs(ctx context.Context, tx *sql.Tx, predicate string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT task_id FROM alexandria_tasks WHERE `+predicate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func lockSelected(ctx context.Context, tx *sql.Tx, predicate string) error {
	ids, err := selectedIDs(ctx, tx, predicate)
	if err != nil {
		return err
	}
	return lockTasks(ctx, tx, ids)
}

func (s *Store) checkLive(ctx context.Context, id string) error {
	var deleted, purged bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alexandria_tasks WHERE task_id=$1 AND deleted),
 EXISTS(SELECT 1 FROM alexandria_task_purged WHERE task_id=$1)`, id).Scan(&deleted, &purged)
	if err != nil {
		return err
	}
	if purged {
		return taskstore.ErrPurged
	}
	if deleted {
		return taskstore.ErrDeleted
	}
	return nil
}

// Upsert preserves the REST live-upsert contract, while sharing lifecycle rules.
func (s *Store) Upsert(ctx context.Context, t *taskstore.Task) error {
	return s.upsert(ctx, t, false)
}

// Import preserves historical timestamps only for the explicit offline importer.
// Existing deletion and purge reservations always win over imported content.
func (s *Store) Import(ctx context.Context, t *taskstore.Task) error {
	return s.upsert(ctx, t, true)
}
func (s *Store) upsert(ctx context.Context, t *taskstore.Task, imported bool) error {
	if t.TaskID == "" {
		return errors.New("task ID is required")
	}
	if !imported && t.IsDeleted == "Y" {
		return taskstore.ErrDeleted
	}
	return s.write(ctx, []string{t.TaskID}, func(w *Store) error {
		if err := w.checkLive(ctx, t.TaskID); err != nil {
			return err
		}
		created, updated, modified, deleted := t.CreatedAt, t.UpdatedAt, t.LastModified, t.IsDeleted
		_, err := w.Get(ctx, t.TaskID)
		if errors.Is(err, taskstore.ErrNotFound) {
			err = w.create(ctx, t)
		} else if err == nil {
			err = w.update(ctx, t)
		}
		if err != nil {
			return err
		}
		if imported {
			now := time.Now().UnixMilli()
			if created == 0 {
				created = now
			}
			if updated == 0 {
				updated = now
			}
			if !modified.Valid || modified.Int64 == 0 {
				modified = sql.NullInt64{Int64: updated, Valid: true}
			}
			_, err = w.db.ExecContext(ctx, `UPDATE alexandria_tasks SET created_at=$2,updated_at=$3,last_modified=$4,deleted=$5 WHERE task_id=$1`, t.TaskID, created, updated, modified, deleted == "Y")
		}
		return err
	})
}
