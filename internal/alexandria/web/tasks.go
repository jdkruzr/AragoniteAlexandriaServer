package web

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
)

// Tasks is the home page: owners usually open Alexandria to see what to do.

// taskSorts are the orders the Tasks page offers, in menu order.
var taskSorts = []string{"due", "created", "updated", "title"}

// taskView is the list state kept in the URL (?show=deleted&sort=updated&dir=desc).
type taskView struct {
	Trash     bool
	Sort, Dir string
}

func parseTaskView(q url.Values) taskView {
	v := taskView{Trash: q.Get("show") == "deleted", Sort: q.Get("sort"), Dir: q.Get("dir")}
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
	if len(q) == 0 {
		return ""
	}
	return "?" + q.Encode()
}

// URL is the Tasks page in this view.
func (v taskView) URL() string { return "/" + v.Query() }

// Toggle is this view with the trash shown or hidden.
func (v taskView) Toggle() string { v.Trash = !v.Trash; return v.URL() }

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
		v := parseTaskView(r.URL.Query())
		var all []tasksvc.Task
		var err error
		if v.Trash {
			all, err = svc().ListIncludingDeleted(r.Context())
		} else {
			all, err = svc().List(r.Context())
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		var open, done []tasksvc.Task
		for _, t := range all {
			if v.Trash && !t.Deleted {
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
		d.render(w, r, "tasks", "Tasks", "tasks", map[string]any{"Open": open, "Done": done, "Trash": v.Trash, "View": v, "Sorts": taskSorts})
	}
	mux.HandleFunc("GET /{$}", list)
	mux.HandleFunc("GET /tasks", list)
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		create := tasksvc.TaskCreate{Title: strings.TrimSpace(r.FormValue("title")), Detail: strings.TrimSpace(r.FormValue("detail"))}
		if due := r.FormValue("due"); due != "" {
			if t, err := time.ParseInLocation("2006-01-02", due, time.Local); err == nil {
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
	mux.HandleFunc("GET /tasks/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		t, err := svc().Get(r.Context(), r.PathValue("id"))
		if taskstore.IsNotFound(err) {
			http.NotFound(w, r)
			return
		} else if err != nil {
			d.fail(w, err)
			return
		}
		due := ""
		if t.DueAt != nil {
			due = t.DueAt.Local().Format("2006-01-02")
		}
		d.render(w, r, "task-edit", "Edit Task", "tasks", map[string]any{"Task": t, "Due": due, "Return": returnTo(r)})
	})
	mux.HandleFunc("POST /tasks/{id}/edit", func(w http.ResponseWriter, r *http.Request) {
		title := strings.TrimSpace(r.FormValue("title"))
		ret := returnTo(r)
		if title == "" {
			back(w, r, "/tasks/"+url.PathEscape(r.PathValue("id"))+"/edit", "A task needs a title.")
			return
		}
		detail := strings.TrimSpace(r.FormValue("detail"))
		patch := tasksvc.TaskPatch{Title: &title, Detail: &detail, ClearDueAt: true}
		if due := r.FormValue("due"); due != "" {
			if t, err := time.ParseInLocation("2006-01-02", due, time.Local); err == nil {
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
			d.confirmPurge(w, r, svc(), ids, ret)
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

// confirmPurge asks before deleting tasks permanently, listing what will go.
func (d Deps) confirmPurge(w http.ResponseWriter, r *http.Request, s tasksvc.TaskService, ids []string, ret string) {
	all, err := s.ListIncludingDeleted(r.Context())
	if err != nil {
		d.fail(w, err)
		return
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var chosen []tasksvc.Task
	for _, t := range all {
		if t.Deleted && want[t.ID] {
			chosen = append(chosen, t)
		}
	}
	if len(chosen) == 0 {
		back(w, r, ret, "Select one or more tasks first.")
		return
	}
	d.render(w, r, "task-purge", "Delete Permanently", "tasks", map[string]any{"Tasks": chosen, "Return": ret})
}

// returnTo is the Tasks view a form came from (a same-site path), or the home page.
func returnTo(r *http.Request) string {
	ret := r.FormValue("return")
	if ret == "" || !strings.HasPrefix(ret, "/") || strings.HasPrefix(ret, "//") || strings.HasPrefix(ret, "/\\") {
		return "/"
	}
	return ret
}
