package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("task not found")

const columns = `task_id,title,detail,status,importance,due_time,completed_time,
	completed_at,last_modified,recurrence,is_reminder_on,links,deleted,ical_blob,
	created_at,updated_at,forestnote_notebook_id,forestnote_page_id,
	forestnote_notebook_name,forestnote_source`

type dbtx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct{ db dbtx }

func NewStore(db dbtx) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context, includeDeleted bool) ([]Task, error) {
	query := `SELECT ` + columns + ` FROM alexandria_tasks`
	if !includeDeleted {
		query += ` WHERE deleted=false`
	}
	query += ` ORDER BY updated_at DESC, task_id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	var result []Task
	for rows.Next() {
		task, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (Task, error) {
	task, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM alexandria_tasks WHERE task_id=$1 AND deleted=false`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return task, err
}

func (s *Store) Upsert(ctx context.Context, task Task) error {
	if task.ID == "" {
		return fmt.Errorf("task ID is required")
	}
	now := time.Now().UnixMilli()
	if task.Status == "" {
		task.Status = "needsAction"
	}
	if task.CreatedAt == 0 {
		task.CreatedAt = now
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = now
	}
	if task.LastModified == 0 {
		task.LastModified = task.UpdatedAt
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO alexandria_tasks (`+columns+`)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT(task_id) DO UPDATE SET
		title=EXCLUDED.title,detail=EXCLUDED.detail,status=EXCLUDED.status,
		importance=EXCLUDED.importance,due_time=EXCLUDED.due_time,
		completed_time=EXCLUDED.completed_time,completed_at=EXCLUDED.completed_at,
		last_modified=EXCLUDED.last_modified,recurrence=EXCLUDED.recurrence,
		is_reminder_on=EXCLUDED.is_reminder_on,links=EXCLUDED.links,
		deleted=EXCLUDED.deleted,ical_blob=EXCLUDED.ical_blob,
		updated_at=EXCLUDED.updated_at,
		forestnote_notebook_id=EXCLUDED.forestnote_notebook_id,
		forestnote_page_id=EXCLUDED.forestnote_page_id,
		forestnote_notebook_name=EXCLUDED.forestnote_notebook_name,
		forestnote_source=EXCLUDED.forestnote_source`,
		task.ID, task.Title, task.Detail, task.Status, task.Importance, task.DueTime,
		task.CompletedTime, task.CompletedAt, task.LastModified, task.Recurrence,
		task.Reminder, task.Links, task.Deleted, task.ICalBlob, task.CreatedAt,
		task.UpdatedAt, task.ForestNoteNotebookID, task.ForestNotePageID,
		task.ForestNoteNotebookName, task.ForestNoteSource)
	if err != nil {
		return fmt.Errorf("upsert task %s: %w", task.ID, err)
	}
	return nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE alexandria_tasks SET deleted=true,updated_at=$2,last_modified=$2 WHERE task_id=$1 AND deleted=false`, id, now)
	if err != nil {
		return fmt.Errorf("delete task %s: %w", id, err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Task, error) {
	var task Task
	err := row.Scan(&task.ID, &task.Title, &task.Detail, &task.Status, &task.Importance,
		&task.DueTime, &task.CompletedTime, &task.CompletedAt, &task.LastModified,
		&task.Recurrence, &task.Reminder, &task.Links, &task.Deleted, &task.ICalBlob,
		&task.CreatedAt, &task.UpdatedAt, &task.ForestNoteNotebookID,
		&task.ForestNotePageID, &task.ForestNoteNotebookName, &task.ForestNoteSource)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}
