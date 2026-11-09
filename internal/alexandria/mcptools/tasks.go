package mcptools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/calendarview"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
)

type listTasksInput struct {
	Status       string `json:"status,omitempty" jsonschema:"open (default: not done), done (completed or cancelled) or all"`
	DueAfter     string `json:"due_after,omitempty" jsonschema:"only tasks due on or after this (YYYY-MM-DD or RFC 3339)"`
	DueBefore    string `json:"due_before,omitempty" jsonschema:"only tasks due before this (YYYY-MM-DD or RFC 3339)"`
	Category     string `json:"category,omitempty"`
	Priority     string `json:"priority,omitempty" jsonschema:"1 (highest) to 9 (lowest)"`
	FromNotebook string `json:"from_notebook,omitempty" jsonschema:"only tasks made from this notebook (boox:<id> or client:<id>)"`
	Limit        int    `json:"limit,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
}

type taskIDInput struct {
	ID string `json:"id" jsonschema:"a task id"`
}

type createTaskInput struct {
	Title      string   `json:"title"`
	Due        string   `json:"due,omitempty" jsonschema:"YYYY-MM-DD for a day, or RFC 3339 / YYYY-MM-DDTHH:MM (owner's time zone) for a time"`
	Detail     string   `json:"detail,omitempty"`
	URL        string   `json:"url,omitempty" jsonschema:"a link to keep with the task"`
	Priority   string   `json:"priority,omitempty" jsonschema:"1 (highest) to 9 (lowest)"`
	Categories []string `json:"categories,omitempty"`
	Comment    string   `json:"comment,omitempty"`
}

type updateTaskInput struct {
	ID         string    `json:"id"`
	Title      *string   `json:"title,omitempty"`
	Due        *string   `json:"due,omitempty" jsonschema:"new due date or time; an empty string removes it"`
	Detail     *string   `json:"detail,omitempty" jsonschema:"an empty string clears it"`
	URL        *string   `json:"url,omitempty" jsonschema:"an empty string removes it"`
	Priority   *string   `json:"priority,omitempty" jsonschema:"an empty string removes it"`
	Categories *[]string `json:"categories,omitempty" jsonschema:"replaces them all; an empty list clears them"`
	Comment    *string   `json:"comment,omitempty" jsonschema:"an empty string clears it"`
	Done       *bool     `json:"done,omitempty" jsonschema:"true completes the task, false reopens it"`
}

type taskSource struct {
	Source   string `json:"source"`
	Notebook string `json:"notebook,omitempty"`
	Title    string `json:"title,omitempty"`
	Page     int    `json:"page,omitempty"`
	URL      string `json:"url,omitempty"`
}

type taskRow struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	Status     string      `json:"status" jsonschema:"open, in_progress, done or cancelled"`
	Due        string      `json:"due,omitempty"`
	Completed  string      `json:"completed,omitempty"`
	Created    string      `json:"created"`
	Priority   string      `json:"priority,omitempty"`
	Categories []string    `json:"categories,omitempty"`
	Detail     string      `json:"detail,omitempty"`
	Comment    string      `json:"comment,omitempty"`
	Link       string      `json:"link,omitempty" jsonschema:"the task's own URL property"`
	From       *taskSource `json:"from,omitempty" jsonschema:"the notebook page it was made from"`
	URL        string      `json:"url"`
}

type listTasksOutput struct {
	Total      int       `json:"total"`
	Tasks      []taskRow `json:"tasks"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

type taskOutput struct {
	Task taskRow `json:"task"`
}

type clearedOutput struct {
	Cleared int64 `json:"cleared"`
}

func taskPath(id string) string { return "/?edit=" + url.QueryEscape(id) + "#task-" + id }

var taskStatuses = map[tasksvc.TaskStatus]string{tasksvc.StatusNeedsAction: "open", tasksvc.StatusInProcess: "in_progress",
	tasksvc.StatusCompleted: "done", tasksvc.StatusCancelled: "cancelled"}

func done(t tasksvc.Task) bool {
	return t.Status == tasksvc.StatusCompleted || t.Status == tasksvc.StatusCancelled
}

// dueTime reads a due date (midnight in loc, an all-day due date) or time.
func dueTime(v string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04", v, loc); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("due must be YYYY-MM-DD, YYYY-MM-DDTHH:MM or RFC 3339")
}

// dueString shows a due date alone when it falls at midnight in loc.
func dueString(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	if t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 {
		return t.Format("2006-01-02")
	}
	return t.Format(time.RFC3339)
}

// taskRows describes tasks, with the notebook pages they came from.
func (d Deps) taskRows(ctx context.Context, list []tasksvc.Task, loc *time.Location) []taskRow {
	ids := make([]string, len(list))
	for i, t := range list {
		ids[i] = t.ID
	}
	sources := calendarview.NoteSources(ctx, d.DB, "task", ids)
	out := make([]taskRow, 0, len(list))
	for _, t := range list {
		r := taskRow{ID: t.ID, Title: t.Title, Status: taskStatuses[t.Status], Created: when(t.CreatedAt, loc), Categories: t.Categories,
			Comment: t.Comment, URL: d.link(taskPath(t.ID))}
		if r.Status == "" {
			r.Status = string(t.Status)
		}
		if t.DueAt != nil {
			r.Due = dueString(*t.DueAt, loc)
		}
		if t.CompletedAt != nil && done(t) {
			r.Completed = when(*t.CompletedAt, loc)
		}
		for _, f := range []struct {
			p   *string
			dst *string
		}{{t.Priority, &r.Priority}, {t.Detail, &r.Detail}, {t.URL, &r.Link}} {
			if f.p != nil {
				*f.dst = *f.p
			}
		}
		if s, ok := sources[t.ID]; ok {
			r.From = &taskSource{Source: sourceBOOX, Notebook: ref(sourceBOOX, s.NotebookID), Title: s.Notebook, Page: s.Page, URL: d.link(s.URL())}
			r.Detail = withoutMadeFrom(r.Detail)
		} else if fn := t.ForestNote; fn != nil && fn.NotebookID != "" {
			r.From = &taskSource{Source: sourceClient, Notebook: ref(sourceClient, fn.NotebookID), Title: fn.NotebookName,
				URL: d.link(notebookPath(sourceClient, fn.NotebookID, 0, fn.PageID))}
		}
		out = append(out, r)
	}
	return out
}

func formatTask(r taskRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (id %s): %s", r.Title, r.ID, r.Status)
	if r.Completed != "" {
		fmt.Fprintf(&b, " %s", r.Completed)
	}
	if r.Due != "" {
		fmt.Fprintf(&b, ", due %s", r.Due)
	}
	if r.Priority != "" {
		fmt.Fprintf(&b, ", priority %s", r.Priority)
	}
	if len(r.Categories) > 0 {
		fmt.Fprintf(&b, ", categories %s", strings.Join(r.Categories, ", "))
	}
	b.WriteString("\n")
	writeFrom(&b, r.From)
	if r.Detail != "" {
		fmt.Fprintf(&b, "  %s\n", clipLines(r.Detail, 400, "  "))
	}
	if r.Comment != "" {
		fmt.Fprintf(&b, "  Comment: %s\n", clipLines(r.Comment, 400, "  "))
	}
	return b.String()
}

func taskErr(id string, err error) error {
	if errors.Is(err, taskstore.ErrNotFound) {
		return fmt.Errorf("no task %s", id)
	}
	return err
}

func registerTasks(s *mcp.Server, d Deps) {
	svc := d.Tasks
	if svc == nil {
		return
	}
	add(s, d, tool{name: "list_tasks", title: "List tasks",
		description: "List tasks, soonest due first (undated last): open ones by default, or done or all. Filter by due dates, category, priority " +
			"or the notebook they were made from. Each task says which notebook page it came from, when coloured ink or the Client made it."},
		func(ctx context.Context, in listTasksInput) (string, listTasksOutput, error) {
			p, err := pageOf(in.Limit, in.Cursor)
			if err != nil {
				return "", listTasksOutput{}, err
			}
			if in.Status == "" {
				in.Status = "open"
			}
			if in.Status != "open" && in.Status != "done" && in.Status != "all" {
				return "", listTasksOutput{}, errors.New("status must be open, done or all")
			}
			loc := d.zone(ctx)
			var after, before time.Time
			if in.DueAfter != "" {
				if after, err = day(in.DueAfter, loc); err != nil {
					return "", listTasksOutput{}, errors.New("due_after must be YYYY-MM-DD or RFC 3339")
				}
			}
			if in.DueBefore != "" {
				if before, err = day(in.DueBefore, loc); err != nil {
					return "", listTasksOutput{}, errors.New("due_before must be YYYY-MM-DD or RFC 3339")
				}
			}
			var fromSource, fromID string
			if in.FromNotebook != "" {
				if fromSource, fromID, err = parseRef(in.FromNotebook); err != nil {
					return "", listTasksOutput{}, err
				}
			}
			all, err := svc.List(ctx)
			if err != nil {
				return "", listTasksOutput{}, err
			}
			var made map[string]calendarview.NoteSource
			if fromSource == sourceBOOX {
				ids := make([]string, len(all))
				for i, t := range all {
					ids[i] = t.ID
				}
				made = calendarview.NoteSources(ctx, d.DB, "task", ids)
			}
			list := slices.DeleteFunc(all, func(t tasksvc.Task) bool {
				switch {
				case in.Status == "open" && done(t), in.Status == "done" && !done(t):
					return true
				case (!after.IsZero() || !before.IsZero()) && t.DueAt == nil:
					return true
				case !after.IsZero() && t.DueAt.Before(after), !before.IsZero() && !t.DueAt.Before(before):
					return true
				case in.Category != "" && !slices.Contains(t.Categories, in.Category):
					return true
				case in.Priority != "" && (t.Priority == nil || *t.Priority != in.Priority):
					return true
				case fromSource == sourceBOOX && made[t.ID].NotebookID != fromID:
					return true
				case fromSource == sourceClient && (t.ForestNote == nil || t.ForestNote.NotebookID != fromID):
					return true
				}
				return false
			})
			slices.SortStableFunc(list, func(a, b tasksvc.Task) int {
				switch {
				case a.DueAt == nil && b.DueAt == nil:
					return b.CreatedAt.Compare(a.CreatedAt)
				case a.DueAt == nil:
					return 1
				case b.DueAt == nil:
					return -1
				}
				return a.DueAt.Compare(*b.DueAt)
			})
			shown := window(list, p)
			out := listTasksOutput{Total: len(list), Tasks: d.taskRows(ctx, shown, loc), NextCursor: p.next(len(shown), len(list))}
			var b strings.Builder
			fmt.Fprintf(&b, "%d tasks.\n", out.Total)
			for _, r := range out.Tasks {
				b.WriteString("- " + formatTask(r))
			}
			b.WriteString(more(out.NextCursor))
			return b.String(), out, nil
		})

	add(s, d, tool{name: "get_task", title: "Read a task", description: "Read one task by id, with the notebook page it came from."},
		func(ctx context.Context, in taskIDInput) (string, taskOutput, error) {
			t, err := svc.Get(ctx, in.ID)
			if err != nil {
				return "", taskOutput{}, taskErr(in.ID, err)
			}
			r := d.taskRows(ctx, []tasksvc.Task{t}, d.zone(ctx))[0]
			return formatTask(r), taskOutput{Task: r}, nil
		})

	add(s, d, tool{name: "create_task", title: "Add a task", write: true,
		description: "Add a task; only the title is required. It appears on Today and reaches CalDAV task apps on their next sync."},
		func(ctx context.Context, in createTaskInput) (string, taskOutput, error) {
			if strings.TrimSpace(in.Title) == "" {
				return "", taskOutput{}, errors.New("title is required")
			}
			loc := d.zone(ctx)
			c := tasksvc.TaskCreate{Title: in.Title, Detail: in.Detail, URL: in.URL, Priority: in.Priority, Categories: in.Categories, Comment: in.Comment}
			if in.Due != "" {
				due, err := dueTime(in.Due, loc)
				if err != nil {
					return "", taskOutput{}, err
				}
				c.DueAt = &due
			}
			t, err := svc.Create(ctx, c)
			if err != nil {
				return "", taskOutput{}, err
			}
			r := d.taskRows(ctx, []tasksvc.Task{t}, loc)[0]
			return "Added: " + formatTask(r), taskOutput{Task: r}, nil
		})

	add(s, d, tool{name: "update_task", title: "Change a task", write: true, idempotent: true,
		description: "Change a task: only the fields given change. An empty string removes a due date, link, priority, detail or comment. " +
			"done=true completes it and done=false reopens it."},
		func(ctx context.Context, in updateTaskInput) (string, taskOutput, error) {
			loc := d.zone(ctx)
			patch := tasksvc.TaskPatch{Title: in.Title, Categories: in.Categories}
			if in.Title != nil && strings.TrimSpace(*in.Title) == "" {
				return "", taskOutput{}, errors.New("a task needs a title")
			}
			if in.Due != nil {
				if *in.Due == "" {
					patch.ClearDueAt = true
				} else {
					due, err := dueTime(*in.Due, loc)
					if err != nil {
						return "", taskOutput{}, err
					}
					patch.DueAt = &due
				}
			}
			patch.Detail, patch.Comment = in.Detail, in.Comment
			if in.URL != nil {
				patch.URL, patch.ClearURL = in.URL, *in.URL == ""
			}
			if in.Priority != nil {
				patch.Priority, patch.ClearPriority = in.Priority, *in.Priority == ""
			}
			if in.Comment != nil && *in.Comment == "" {
				patch.Comment, patch.ClearComment = nil, true
			}
			if patch == (tasksvc.TaskPatch{}) && in.Done == nil {
				return "", taskOutput{}, errors.New("nothing to change")
			}
			if patch != (tasksvc.TaskPatch{}) {
				if _, err := svc.Update(ctx, in.ID, patch); err != nil {
					return "", taskOutput{}, taskErr(in.ID, err)
				}
			}
			if in.Done != nil {
				var err error
				if *in.Done {
					err = svc.Complete(ctx, in.ID)
				} else {
					err = svc.Reopen(ctx, in.ID)
				}
				if err != nil {
					return "", taskOutput{}, taskErr(in.ID, err)
				}
			}
			t, err := svc.Get(ctx, in.ID)
			if err != nil {
				return "", taskOutput{}, taskErr(in.ID, err)
			}
			r := d.taskRows(ctx, []tasksvc.Task{t}, loc)[0]
			return "Updated: " + formatTask(r), taskOutput{Task: r}, nil
		})

	add(s, d, tool{name: "complete_task", title: "Complete a task", write: true, idempotent: true,
		description: "Mark a task done. Completing a done task changes nothing."},
		func(ctx context.Context, in taskIDInput) (string, taskOutput, error) {
			if err := svc.Complete(ctx, in.ID); err != nil {
				return "", taskOutput{}, taskErr(in.ID, err)
			}
			t, err := svc.Get(ctx, in.ID)
			if err != nil {
				return "", taskOutput{}, taskErr(in.ID, err)
			}
			r := d.taskRows(ctx, []tasksvc.Task{t}, d.zone(ctx))[0]
			return "Done: " + formatTask(r), taskOutput{Task: r}, nil
		})

	add(s, d, tool{name: "delete_task", title: "Delete a task", write: true, destructive: true, idempotent: true,
		description: "Move a task to Deleted Tasks, where the owner can restore it on the Today page. It leaves CalDAV task apps on their next sync."},
		func(ctx context.Context, in taskIDInput) (string, taskOutput, error) {
			t, err := svc.Get(ctx, in.ID)
			if err != nil {
				return "", taskOutput{}, taskErr(in.ID, err)
			}
			if err := svc.Delete(ctx, in.ID); err != nil {
				return "", taskOutput{}, taskErr(in.ID, err)
			}
			r := d.taskRows(ctx, []tasksvc.Task{t}, d.zone(ctx))[0]
			return "Deleted: " + formatTask(r), taskOutput{Task: r}, nil
		})

	add(s, d, tool{name: "clear_completed_tasks", title: "Clear completed tasks", write: true, destructive: true, idempotent: true,
		description: "Move every completed task to Deleted Tasks, as Clear completed does on the Today page."},
		func(ctx context.Context, _ emptyInput) (string, clearedOutput, error) {
			n, err := svc.PurgeCompleted(ctx)
			if err != nil {
				return "", clearedOutput{}, err
			}
			return fmt.Sprintf("Moved %d completed tasks to Deleted Tasks.\n", n), clearedOutput{Cleared: n}, nil
		})
}
