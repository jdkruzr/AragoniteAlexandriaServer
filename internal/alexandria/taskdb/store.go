// Package taskdb stores tasks in alexandria_tasks for CalDAV, the task
// service and MCP. Copied from UltraBridge (internal/taskdb store.go) under
// Apache-2.0; PostgreSQL adaptation: placeholders, boolean is_reminder_on and
// deleted columns presented as UltraBridge's "Y"/"N" flags, and the CalDAV
// sync floor kept in alexandria_task_sync_state.
package taskdb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
)

// DB is satisfied by *sql.DB, *sql.Conn and (for writes without
// HardDeleteOlderThan) *sql.Tx's owner.
type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Store implements caldav.TaskStore against alexandria_tasks.
type Store struct {
	db DB
}

func NewStore(db DB) *Store {
	return &Store{db: db}
}

// yn presents a boolean column as UltraBridge's "Y"/"N" flag.
func yn(column string) string { return "CASE WHEN " + column + " THEN 'Y' ELSE 'N' END" }

// pgText keeps a value storable in PostgreSQL text (no U+0000).
func pgText(s sql.NullString) sql.NullString {
	s.String = strings.ReplaceAll(s.String, "\x00", "\uFFFD")
	return s
}

var taskColumns = `task_id, title, detail, status, importance, due_time,
	completed_time, last_modified, recurrence, ` + yn("is_reminder_on") + `, links, ` + yn("deleted") + `,
	ical_blob, created_at, updated_at, completed_at,
	forestnote_notebook_id, forestnote_page_id,
	forestnote_notebook_name, forestnote_source`

func (s *Store) List(ctx context.Context) ([]taskstore.Task, error) {
	return s.listRows(ctx, "SELECT "+taskColumns+" FROM alexandria_tasks WHERE NOT deleted ORDER BY created_at, task_id")
}

// ListIncludingDeleted returns every row — soft-deleted included. Powers
// MCP "what's in the trash" queries and the hard-purge tool's pre-flight
// inventory. The Deleted flag on the returned service.Task tells the
// caller which is which.
func (s *Store) ListIncludingDeleted(ctx context.Context) ([]taskstore.Task, error) {
	return s.listRows(ctx, "SELECT "+taskColumns+" FROM alexandria_tasks ORDER BY created_at, task_id")
}

func (s *Store) listRows(ctx context.Context, query string) ([]taskstore.Task, error) {
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []taskstore.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) Get(ctx context.Context, taskID string) (*taskstore.Task, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+taskColumns+" FROM alexandria_tasks WHERE task_id = $1 AND NOT deleted",
		taskID)
	t, err := scanTaskRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, taskstore.ErrNotFound
		}
		return nil, fmt.Errorf("get task %s: %w", taskID, err)
	}
	return &t, nil
}

func (s *Store) Create(ctx context.Context, t *taskstore.Task) error {
	now := time.Now().UnixMilli()
	if t.TaskID == "" {
		t.TaskID = taskstore.GenerateTaskID(taskstore.NullStr(t.Title), now)
	}
	if !t.CompletedTime.Valid {
		t.CompletedTime = sql.NullInt64{Int64: now, Valid: true}
	}
	if !t.LastModified.Valid {
		t.LastModified = sql.NullInt64{Int64: now, Valid: true}
	}
	if t.IsDeleted == "" {
		t.IsDeleted = "N"
	}
	if t.IsReminderOn == "" {
		t.IsReminderOn = "N"
	}
	if !t.Status.Valid {
		t.Status = sql.NullString{String: "needsAction", Valid: true}
	}

	t.CreatedAt, t.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `INSERT INTO alexandria_tasks
		(task_id, title, detail, status, importance, due_time,
		 completed_time, last_modified, recurrence, is_reminder_on,
		 links, deleted, ical_blob, created_at, updated_at, completed_at,
		 forestnote_notebook_id, forestnote_page_id, forestnote_notebook_name, forestnote_source)
		VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7::bigint, 0), $8, $9, $10 = 'Y', $11, $12 = 'Y', $13, $14, $15, $16, $17, $18, $19, $20)`,
		t.TaskID, pgText(t.Title), pgText(t.Detail), t.Status, t.Importance, t.DueTime,
		t.CompletedTime, t.LastModified, pgText(t.Recurrence), t.IsReminderOn,
		pgText(t.Links), t.IsDeleted, pgText(t.ICalBlob), now, now, t.CompletedAt,
		pgText(t.ForestNoteNotebookID), pgText(t.ForestNotePageID), pgText(t.ForestNoteNotebookName), pgText(t.ForestNoteSource))
	if err != nil {
		return fmt.Errorf("create task: %w", err)
	}
	return nil
}

func (s *Store) Update(ctx context.Context, t *taskstore.Task) error {
	now := time.Now().UnixMilli()
	// last_modified mirrors updated_at: it is an SPC-facing field only, and the
	// device requires it to be non-zero to show the task at all. It deliberately
	// no longer carries the completion time — that lives in completed_at, which
	// this method persists verbatim from the caller.
	t.LastModified = sql.NullInt64{Int64: now, Valid: true}
	t.UpdatedAt = now

	result, err := s.db.ExecContext(ctx, `UPDATE alexandria_tasks SET
		title = $1, detail = $2, status = COALESCE($3, status), importance = $4, due_time = $5,
		completed_time = COALESCE($6::bigint, 0), last_modified = $7, recurrence = $8,
		is_reminder_on = ($9 = 'Y'), links = $10, ical_blob = $11, updated_at = $12,
		completed_at = $13,
		forestnote_notebook_id = $14, forestnote_page_id = $15,
		forestnote_notebook_name = $16, forestnote_source = $17
		WHERE task_id = $18`,
		pgText(t.Title), pgText(t.Detail), t.Status, t.Importance, t.DueTime,
		t.CompletedTime, t.LastModified, pgText(t.Recurrence),
		t.IsReminderOn, pgText(t.Links), pgText(t.ICalBlob), now, t.CompletedAt,
		pgText(t.ForestNoteNotebookID), pgText(t.ForestNotePageID),
		pgText(t.ForestNoteNotebookName), pgText(t.ForestNoteSource),
		t.TaskID)
	if err != nil {
		return fmt.Errorf("update task %s: %w", t.TaskID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update task %s rows affected: %w", t.TaskID, err)
	}
	if affected == 0 {
		return taskstore.ErrNotFound
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, taskID string) error {
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE alexandria_tasks SET
		deleted = true, last_modified = $1, updated_at = $1
		WHERE task_id = $2`,
		now, taskID)
	if err != nil {
		return fmt.Errorf("delete task %s: %w", taskID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete task %s rows affected: %w", taskID, err)
	}
	if affected == 0 {
		return taskstore.ErrNotFound
	}
	return nil
}

// DeleteCompleted soft-deletes all completed tasks. Returns the number of tasks deleted.
func (s *Store) DeleteCompleted(ctx context.Context) (int64, error) {
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE alexandria_tasks SET
		deleted = true, last_modified = $1, updated_at = $1
		WHERE NOT deleted AND status = 'completed'`,
		now)
	if err != nil {
		return 0, fmt.Errorf("delete completed tasks: %w", err)
	}
	return result.RowsAffected()
}

// HardDeleteOlderThan permanently removes soft-deleted rows whose
// last_modified is older than cutoffMs, and returns (purged, skipped, error).
// This is the *only* path that does real DELETE FROM tasks — everything
// else in the system soft-deletes. The caller picks the cutoff; the
// service layer translates a user-facing "older_than_days" into a
// millisecond instant before calling here.
//
// Skipped is the count of rows that were eligible by virtue of being
// soft-deleted but were too recent to purge (inside the safety window) —
// surfaced so callers can distinguish "the gate works and there was
// nothing to do" from "the gate is broken and silently skipped everything."
//
// Both the COUNT and the DELETE run inside an explicit transaction so the
// two counts can't drift under concurrent writes. Without this, a Delete
// or Update landing between the COUNT and the DELETE could move a row
// between the two buckets, leaving the caller with inconsistent totals.
// The transaction runs at REPEATABLE READ so the count and the delete see
// one snapshot.
func (s *Store) HardDeleteOlderThan(ctx context.Context, cutoffMs int64) (purged, skipped int64, err error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return 0, 0, fmt.Errorf("begin tx for hard delete: %w", err)
	}
	defer func() {
		// Best-effort rollback on any non-committing exit. Commit's
		// idempotent-Rollback contract makes the post-Commit call a no-op.
		_ = tx.Rollback()
	}()

	if err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM alexandria_tasks WHERE deleted AND updated_at >= $1`,
		cutoffMs).Scan(&skipped); err != nil {
		return 0, 0, fmt.Errorf("count skipped tasks at cutoff %d: %w", cutoffMs, err)
	}
	result, err := tx.ExecContext(ctx,
		`DELETE FROM alexandria_tasks WHERE deleted AND updated_at < $1`,
		cutoffMs)
	if err != nil {
		return 0, 0, fmt.Errorf("hard delete tasks older than %d: %w", cutoffMs, err)
	}
	purged, err = result.RowsAffected()
	if err != nil {
		return 0, skipped, fmt.Errorf("rows affected: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit hard delete tx: %w", err)
	}

	// Purging destroys the tombstones a CalDAV client would need to learn about
	// those deletions, so any sync token older than this cutoff can no longer be
	// answered honestly. Record it; the sync-collection handler rejects such
	// tokens and the client falls back to a full resync. Only on a purge that
	// actually removed something — a no-op purge invalidates nothing.
	//
	// Deliberately outside the transaction: the rows are already gone, and
	// failing to record the floor must not roll that back. The consequence of a
	// missed floor is a client that keeps a token it should have been asked to
	// refresh, so it is logged rather than swallowed.
	if purged > 0 {
		if ferr := s.raiseSyncFloor(ctx, cutoffMs); ferr != nil {
			slog.Warn("purged tombstones but failed to raise the sync floor; "+
				"clients holding older sync tokens may miss those deletions",
				"cutoff_ms", cutoffMs, "purged", purged, "err", ferr)
		}
	}
	return purged, skipped, nil
}

// IsEmpty returns true if the task store has no tasks (including deleted ones).
// Used to detect first-run state for migration.
func (s *Store) IsEmpty(ctx context.Context) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM alexandria_tasks)").Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check tasks empty: %w", err)
	}
	return !exists, nil
}

// MaxUpdatedAt returns the highest updated_at across non-deleted tasks — the
// collection change watermark, used as the SPC sync token. It reads updated_at
// rather than last_modified because only updated_at is guaranteed to advance on
// every write; last_modified is now an SPC-facing mirror.
func (s *Store) MaxUpdatedAt(ctx context.Context) (int64, error) {
	var max sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT MAX(updated_at) FROM alexandria_tasks WHERE NOT deleted").Scan(&max)
	if err != nil {
		return 0, fmt.Errorf("max updated_at: %w", err)
	}
	if !max.Valid {
		return 0, nil
	}
	return max.Int64, nil
}

// syncFloorAdapterID is the alexandria_task_sync_state row holding the CalDAV
// sync-token floor: the purge cutoff below which tokens can no longer be
// answered.
const syncFloorAdapterID = "caldav-sync-floor"

// MaxUpdatedAtAll returns the highest updated_at across **every** row,
// tombstones included. This is the CalDAV collection change token.
//
// Distinct from MaxUpdatedAt, which excludes soft-deleted rows and feeds the SPC
// sync token. A collection token must move when anything changes, and deleting
// any task other than the most recent one leaves the live-only maximum exactly
// where it was — so a client polling that value would never learn about the
// deletion. Delete() stamps updated_at before flipping is_deleted, so counting
// tombstones makes the token move.
func (s *Store) MaxUpdatedAtAll(ctx context.Context) (int64, error) {
	var max sql.NullInt64
	if err := s.db.QueryRowContext(ctx,
		"SELECT MAX(updated_at) FROM alexandria_tasks").Scan(&max); err != nil {
		return 0, fmt.Errorf("max updated_at (all rows): %w", err)
	}
	if !max.Valid {
		return 0, nil
	}
	return max.Int64, nil
}

// ListChangedSince returns every row written at or after sinceMs, tombstones
// included — callers split on IsDeleted to decide between reporting a resource
// and reporting its removal.
//
// The bound is deliberately **inclusive**. Timestamps are milliseconds and a
// bulk device sync writes several rows inside one, so an exclusive comparison
// against a token equal to one of those stamps would silently drop the rest.
// Inclusive re-reports the boundary rows; the client compares ETags, finds them
// unchanged, and does nothing. Over-reporting costs a little bandwidth,
// under-reporting costs silent divergence.
// Ordered by updated_at so a caller applying DAV:limit/nresults can truncate at
// a well-defined point and hand back a token that resumes there.
func (s *Store) ListChangedSince(ctx context.Context, sinceMs int64) ([]taskstore.Task, error) {
	return s.listRows(ctx,
		fmt.Sprintf("SELECT %s FROM alexandria_tasks WHERE updated_at >= %d ORDER BY updated_at ASC, task_id",
			taskColumns, sinceMs))
}

// SyncFloor returns the oldest still-answerable sync token, or 0 when every
// token is answerable (nothing has ever been hard-purged).
func (s *Store) SyncFloor(ctx context.Context) (int64, error) {
	var floor sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT last_sync_token FROM alexandria_task_sync_state WHERE adapter_id = $1`, syncFloorAdapterID).Scan(&floor)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read sync floor: %w", err)
	}
	if !floor.Valid {
		return 0, nil
	}
	ms, convErr := strconv.ParseInt(floor.String, 10, 64)
	if convErr != nil {
		// Unparseable floor is treated as "no floor" rather than as a hard
		// error: rejecting every sync token is a far worse failure than
		// occasionally answering one we could have refused.
		return 0, nil
	}
	return ms, nil
}

// raiseSyncFloor records a purge cutoff, keeping the highest seen. Called only
// when a purge actually removed rows.
func (s *Store) raiseSyncFloor(ctx context.Context, cutoffMs int64) error {
	current, err := s.SyncFloor(ctx)
	if err != nil {
		return err
	}
	if cutoffMs <= current {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO alexandria_task_sync_state (adapter_id, last_sync_token, last_sync_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT(adapter_id) DO UPDATE SET last_sync_token = excluded.last_sync_token,
		                                       last_sync_at = excluded.last_sync_at`,
		syncFloorAdapterID, strconv.FormatInt(cutoffMs, 10), time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("record sync floor: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanTask(s scanner) (taskstore.Task, error) {
	var t taskstore.Task
	err := s.Scan(
		&t.TaskID, &t.Title, &t.Detail, &t.Status, &t.Importance,
		&t.DueTime, &t.CompletedTime, &t.LastModified, &t.Recurrence,
		&t.IsReminderOn, &t.Links, &t.IsDeleted, &t.ICalBlob,
		&t.CreatedAt, &t.UpdatedAt, &t.CompletedAt,
		&t.ForestNoteNotebookID, &t.ForestNotePageID,
		&t.ForestNoteNotebookName, &t.ForestNoteSource,
	)
	return t, err
}

func scanTaskRow(row *sql.Row) (taskstore.Task, error) {
	return scanTask(row)
}
