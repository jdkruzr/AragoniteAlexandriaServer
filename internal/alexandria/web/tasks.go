package web

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
)

// Tasks is the home page: owners usually open Alexandria to see what to do.

// taskSorts are the orders the Tasks page offers, in menu order.
var taskSorts = []string{"due", "created", "updated", "title"}

// taskView is the list state kept in the URL (?show=deleted&sort=updated&dir=desc,
// done=show to list finished tasks, edit=<id> for the task being edited).
type taskView struct {
	Trash, ShowDone bool
	Sort, Dir, Edit string
}

func parseTaskView(q url.Values) taskView {
	v := taskView{Trash: q.Get("show") == "deleted", ShowDone: q.Get("done") == "show", Sort: q.Get("sort"), Dir: q.Get("dir"), Edit: q.Get("edit")}
	if len(v.Edit) > 200 {
		v.Edit = ""
	}
	if !slices.Contains(taskSorts, v.Sort) {
		v.Sort = "due"
	}
	if v.Dir != "desc" {
		v.Dir = "asc"
	}
	return v
}

// Query is the view's query string, empty for the default view.
func (v taskView) Query() string {
	q := url.Values{}
	if v.Trash {
		q.Set("show", "deleted")
	}
	if v.Sort != "due" {
		q.Set("sort", v.Sort)
	}
	if v.Dir != "asc" {
		q.Set("dir", v.Dir)
	}
	if v.ShowDone && !v.Trash {
		q.Set("done", "show")
	}
	if v.Edit != "" {
		q.Set("edit", v.Edit)
	}
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// URL is the Tasks page in this view.
func (v taskView) URL() string { return "/" + v.Query() }

// Toggle is this view with the trash shown or hidden.
func (v taskView) Toggle() string { v.Trash, v.Edit = !v.Trash, ""; return v.URL() }

// List is this view with no task being edited: where forms return to.
func (v taskView) List() string { v.Edit = ""; return v.URL() }

// Editing is this view with the task id's editor open.
func (v taskView) Editing(id string) string { v.Edit = id; return v.URL() + "#task-" + id }

// Flip is this view sorted the other way.
func (v taskView) Flip() string {
	v.Edit = ""
	if v.Dir == "desc" {
		v.Dir = "asc"
	} else {
		v.Dir = "desc"
	}
	return v.URL()
}

// DoneToggle is this view with finished tasks shown or hidden.
func (v taskView) DoneToggle() string { v.ShowDone, v.Edit = !v.ShowDone, ""; return v.URL() + "#done" }

// Direction names the current order in the sort field's words.
func (v taskView) Direction() string { return directionLabel(v.Sort, v.Dir) }

// sortTasks orders tasks for a view. Tasks without a due date sort after dated
// ones in either direction; ties fall back to creation order.
func sortTasks(list []tasksvc.Task, v taskView) {
	desc := v.Dir == "desc"
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		switch v.Sort {
		case "due":
			if (a.DueAt == nil) != (b.DueAt == nil) {
				return a.DueAt != nil
			}
			if a.DueAt != nil && !a.DueAt.Equal(*b.DueAt) {
				return a.DueAt.Before(*b.DueAt) != desc
			}
		case "updated":
			if !a.UpdatedAt.Equal(b.UpdatedAt) {
				return a.UpdatedAt.Before(b.UpdatedAt) != desc
			}
		case "title":
			if x, y := strings.ToLower(a.Title), strings.ToLower(b.Title); x != y {
				return (x < y) != desc
			}
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt) != (desc && v.Sort == "created")
		}
		return a.ID < b.ID
	})
}

// taskActions are the bulk actions; restore and purge apply to the trash.
var taskActions = []string{"complete", "reopen", "delete", "restore", "purge"}

func (d Deps) taskRoutes(mux *http.ServeMux) {
	svc := func() tasksvc.TaskService { return tasksvc.NewTaskService(taskdb.NewStore(d.DB), nil) }
	list := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		v := parseTaskView(r.URL.Query())
		all, err := svc().ListIncludingDeleted(ctx)
		if err != nil {
			d.fail(w, err)
			return
		}
		var open, done []tasksvc.Task
		deleted := 0
		for _, t := range all {
			if t.Deleted {
				deleted++
			}
			if v.Trash != t.Deleted {
				continue
			}
			if t.Status == tasksvc.StatusCompleted || t.Status == tasksvc.StatusCancelled {
				done = append(done, t)
			} else {
				open = append(open, t)
			}
		}
		sortTasks(open, v)
		sortTasks(done, v)
		loc, now, l := settings.Zone(ctx, d.DB), time.Now(), d.Localizer(r)
		sources := d.taskSources(ctx, append(append([]tasksvc.Task{}, open...), done...))
		rows := func(list []tasksvc.Task) []todayTask {
			out := make([]todayTask, 0, len(list))
			for _, t := range list {
				row := todayTask{Task: t, Source: sources[t.ID], Editing: v.Edit == t.ID && !t.Deleted}
				if t.DueAt != nil {
					row.Due, row.Urgent = dueLabel(l, *t.DueAt, now, loc)
					row.DueInput = t.DueAt.In(loc).Format("2006-01-02")
					if t.Status == tasksvc.StatusCompleted || t.Status == tasksvc.StatusCancelled {
						row.Urgent = false
					}
				}
				out = append(out, row)
			}
			return out
		}
		data := map[string]any{"Open": rows(open), "Done": rows(done), "DoneCount": len(done), "Deleted": deleted, "Trash": v.Trash, "View": v, "Sorts": taskSorts,
			"Date": dateWords{l}.long(now.In(loc)), "NeedsRestore": d.needsRestore(ctx)}
		if !v.Trash {
			events, incomplete, hidden, err := d.nextEvents(ctx, r, loc, now)
			if err != nil {
				d.fail(w, err)
				return
			}
			data["Events"], data["EventsIncomplete"], data["HiddenCalendars"] = events, incomplete, hidden
		}
		d.render(w, r, "tasks", "Today", "tasks", data)
	}
	mux.HandleFunc("GET /{$}", list)
	mux.HandleFunc("GET /tasks", list)
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		create := tasksvc.TaskCreate{Title: strings.TrimSpace(r.FormValue("title")), Detail: strings.TrimSpace(r.FormValue("detail"))}
		if due := r.FormValue("due"); due != "" {
			if t, err := time.ParseInLocation("2006-01-02", due, settings.Zone(r.Context(), d.DB)); err == nil {
				create.DueAt = &t
			}
		}
		ret := returnTo(r)
		if create.Title == "" {
			back(w, r, ret, "A task needs a title.")
			return
		}
		if _, err := svc().Create(r.Context(), create); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, ret, "")
	})
	mux.HandleFunc("POST /tasks/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		if err := svc().Complete(r.Context(), r.PathValue("id")); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, returnTo(r), "")
	})
	mux.HandleFunc("POST /tasks/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		if err := svc().Delete(r.Context(), r.PathValue("id")); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, returnTo(r), "Task deleted.")
	})
	// The editor is inline on Today; older links (note actions, bookmarks) land there.
	mux.HandleFunc("GET /tasks/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		ret, _ := url.Parse(returnTo(r))
		v := parseTaskView(ret.Query())
		v.Edit = r.PathValue("id")
		http.Redirect(w, r, v.Editing(v.Edit), http.StatusSeeOther)
	})
	mux.HandleFunc("POST /tasks/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		title := strings.TrimSpace(r.FormValue("title"))
		ret := returnTo(r)
		if title == "" {
			u, _ := url.Parse(ret)
			v := parseTaskView(u.Query())
			v.Edit = r.PathValue("id")
			back(w, r, v.URL(), "A task needs a title.")
			return
		}
		detail := strings.TrimSpace(r.FormValue("detail"))
		patch := tasksvc.TaskPatch{Title: &title, Detail: &detail, ClearDueAt: true}
		if due := r.FormValue("due"); due != "" {
			if t, err := time.ParseInLocation("2006-01-02", due, settings.Zone(r.Context(), d.DB)); err == nil {
				patch.DueAt, patch.ClearDueAt = &t, false
			}
		}
		if _, err := svc().Update(r.Context(), r.PathValue("id"), patch); taskstore.IsNotFound(err) {
			back(w, r, ret, "That task is no longer here.")
			return
		} else if err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, ret, "Task saved.")
	})
	mux.HandleFunc("POST /tasks/bulk", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad form.", http.StatusBadRequest)
			return
		}
		action, ids, ret := r.PostForm.Get("action"), r.PostForm["id"], returnTo(r)
		if !slices.Contains(taskActions, action) {
			http.Error(w, "Unknown action.", http.StatusBadRequest)
			return
		}
		if len(ids) == 0 {
			back(w, r, ret, "Select one or more tasks first.")
			return
		}
		if action == "purge" && r.PostForm.Get("confirm") != "yes" {
			back(w, r, ret, "Tick the box to confirm permanent deletion.")
			return
		}
		s, ctx := svc(), r.Context()
		done := 0
		for _, id := range ids {
			var err error
			switch action {
			case "complete":
				err = s.Complete(ctx, id)
			case "reopen":
				err = s.Reopen(ctx, id)
			case "delete":
				err = s.Delete(ctx, id)
			case "restore":
				err = s.Restore(ctx, id)
			case "purge":
				var n int64
				n, err = s.Purge(ctx, []string{id})
				if n == 0 && err == nil {
					continue
				}
			}
			if taskstore.IsNotFound(err) || errors.Is(err, taskstore.ErrPurged) {
				continue // changed meanwhile (another tab or a device); skip it
			}
			if err != nil {
				d.fail(w, err)
				return
			}
			done++
		}
		switch action {
		case "complete":
			back(w, r, ret, "Marked {{.Count}} tasks done.", done)
		case "reopen":
			back(w, r, ret, "Reopened {{.Count}} tasks.", done)
		case "delete":
			back(w, r, ret, "Moved {{.Count}} tasks to Deleted Tasks.", done)
		case "restore":
			back(w, r, ret, "Restored {{.Count}} tasks.", done)
		case "purge":
			back(w, r, ret, "Permanently deleted {{.Count}} tasks.", done)
		}
	})
	mux.HandleFunc("POST /tasks/purge-completed", func(w http.ResponseWriter, r *http.Request) {
		if _, err := svc().PurgeCompleted(r.Context()); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, returnTo(r), "Completed tasks cleared.")
	})
}

// returnTo is the Tasks view a form came from (a same-site path), or the home page.
func returnTo(r *http.Request) string {
	ret := r.FormValue("return")
	if ret == "" || !strings.HasPrefix(ret, "/") || strings.HasPrefix(ret, "//") || strings.HasPrefix(ret, "/\\") {
		return "/"
	}
	return ret
}
