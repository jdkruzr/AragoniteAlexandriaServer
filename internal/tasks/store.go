// Package tasks adapts the REST task representation to the shared task store.
package tasks

import (
	"context"
	"database/sql"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
)

var ErrNotFound = taskstore.ErrNotFound

type Store struct{ domain *taskdb.Store }

func NewStore(db taskdb.Queryer) *Store { return &Store{domain: taskdb.NewStore(db)} }
func (s *Store) List(ctx context.Context, includeDeleted bool) ([]Task, error) {
	rows, err := s.domain.ListByUpdated(ctx, includeDeleted)
	if err != nil {
		return nil, err
	}
	var out []Task
	for _, t := range rows {
		out = append(out, wire(t))
	}
	return out, nil
}
func (s *Store) Get(ctx context.Context, id string) (Task, error) {
	t, err := s.domain.Get(ctx, id)
	if err != nil {
		return Task{}, err
	}
	return wire(*t), nil
}
func (s *Store) Upsert(ctx context.Context, t Task) error {
	v := domain(t)
	return s.domain.Upsert(ctx, &v)
}
func (s *Store) Import(ctx context.Context, t Task) error {
	v := domain(t)
	return s.domain.Import(ctx, &v)
}
func (s *Store) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return s.domain.Delete(ctx, id)
}
func wire(t taskstore.Task) Task {
	str := func(v sql.NullString) *string {
		if !v.Valid {
			return nil
		}
		return &v.String
	}
	num := func(v sql.NullInt64) *int64 {
		if !v.Valid {
			return nil
		}
		return &v.Int64
	}
	return Task{ID: t.TaskID, Title: str(t.Title), Detail: str(t.Detail), Status: t.Status.String, Importance: str(t.Importance), DueTime: t.DueTime, CompletedTime: t.CompletedTime.Int64, CompletedAt: num(t.CompletedAt), LastModified: t.LastModified.Int64, Recurrence: str(t.Recurrence), Reminder: t.IsReminderOn == "Y", Links: str(t.Links), Deleted: t.IsDeleted == "Y", ICalBlob: str(t.ICalBlob), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, ForestNoteNotebookID: str(t.ForestNoteNotebookID), ForestNotePageID: str(t.ForestNotePageID), ForestNoteNotebookName: str(t.ForestNoteNotebookName), ForestNoteSource: str(t.ForestNoteSource)}
}
func domain(t Task) taskstore.Task {
	flag := func(v bool) string {
		if v {
			return "Y"
		}
		return "N"
	}
	str := func(v *string) sql.NullString {
		if v == nil {
			return sql.NullString{}
		}
		return sql.NullString{String: *v, Valid: true}
	}
	num := func(v *int64) sql.NullInt64 {
		if v == nil {
			return sql.NullInt64{}
		}
		return sql.NullInt64{Int64: *v, Valid: true}
	}
	if t.Status == "" {
		t.Status = "needsAction"
	}
	return taskstore.Task{TaskID: t.ID, Title: str(t.Title), Detail: str(t.Detail), Status: sql.NullString{String: t.Status, Valid: true}, Importance: str(t.Importance), DueTime: t.DueTime,
		CompletedTime: sql.NullInt64{Int64: t.CompletedTime, Valid: true}, CompletedAt: num(t.CompletedAt), LastModified: sql.NullInt64{Int64: t.LastModified, Valid: true}, Recurrence: str(t.Recurrence), IsReminderOn: flag(t.Reminder), Links: str(t.Links), IsDeleted: flag(t.Deleted), ICalBlob: str(t.ICalBlob), CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
		ForestNoteNotebookID: str(t.ForestNoteNotebookID), ForestNotePageID: str(t.ForestNotePageID), ForestNoteNotebookName: str(t.ForestNoteNotebookName), ForestNoteSource: str(t.ForestNoteSource)}
}
