package tasksvc

import (
	"fmt"
	"time"
)

// ListFilter is the list-tasks filter of UltraBridge's /api/v1/tasks
// (internal/web/api_v1.go handleV1ListTasks), Apache-2.0. All fields optional.
type ListFilter struct {
	Status               string // needs_action|in_process|completed|cancelled|all ("" = all)
	DueBefore, DueAfter  *time.Time
	NotebookID           string
	NotebookName, Source string
	Category, Priority   string
}

// Validate rejects an unknown status before any list query runs.
func (f ListFilter) Validate() error {
	switch f.Status {
	case "", "all", "needs_action", "in_process", "completed", "cancelled":
		return nil
	}
	return fmt.Errorf("status must be needs_action, in_process, completed, cancelled, or all")
}

// Filter keeps the tasks matching f. Tasks with no due date are excluded when
// either due bound is set: a "when's this due" filter cannot match them.
func Filter(tasks []Task, f ListFilter) []Task {
	statuses := map[string]TaskStatus{"needs_action": StatusNeedsAction, "in_process": StatusInProcess, "completed": StatusCompleted, "cancelled": StatusCancelled}
	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if want, ok := statuses[f.Status]; ok && t.Status != want {
			continue
		}
		if f.DueBefore != nil || f.DueAfter != nil {
			if t.DueAt == nil || (f.DueBefore != nil && !t.DueAt.Before(*f.DueBefore)) || (f.DueAfter != nil && t.DueAt.Before(*f.DueAfter)) {
				continue
			}
		}
		if f.NotebookID != "" && (t.ForestNote == nil || t.ForestNote.NotebookID != f.NotebookID) {
			continue
		}
		if f.NotebookName != "" && (t.ForestNote == nil || t.ForestNote.NotebookName != f.NotebookName) {
			continue
		}
		if f.Source != "" && (t.ForestNote == nil || t.ForestNote.Source != f.Source) {
			continue
		}
		if f.Category != "" && !contains(t.Categories, f.Category) {
			continue
		}
		if f.Priority != "" && (t.Priority == nil || *t.Priority != f.Priority) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
