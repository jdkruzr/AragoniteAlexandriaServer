package web

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/books"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/oauth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/readersearch"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskdb"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskhost"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/tasksvc"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/wire"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/fnpath"
)

// --- Notebooks ---

func (d Deps) notebookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /files/forestnote", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if id := q.Get("notebook"); id != "" {
			name, pages, err := notes.NotebookPages(r.Context(), d.DB, id)
			if errors.Is(err, notes.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			if err != nil {
				d.fail(w, err)
				return
			}
			crumbs, err := notes.NotebookFolder(r.Context(), d.DB, id)
			if err != nil {
				d.fail(w, err)
				return
			}
			d.render(w, r, "notebook", name, "notebooks", map[string]any{"ID": id, "Name": name, "Pages": pages, "Crumbs": crumbs, "Focus": q.Get("page")})
			return
		}
		sortField, order := q.Get("sort"), q.Get("order")
		crumbs, entries, err := notes.Folder(r.Context(), d.DB, q.Get("folder"), sortField, order)
		if err != nil {
			d.fail(w, err)
			return
		}
		d.render(w, r, "notebooks", "Notebooks", "notebooks", map[string]any{"Crumbs": crumbs, "Entries": entries, "Folder": q.Get("folder"), "Sort": sortField, "Order": order})
	})
	mux.HandleFunc("GET /files/forestnote/render", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if !fnpath.Is(path) {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		image, err := notes.PageImage(r.Context(), d.DB, fnpath.PageID(path))
		if errors.Is(err, notes.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(image)
	})
	mux.HandleFunc("GET /files/forestnote/export", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("notebook")
		var buf bytes.Buffer
		name, err := notes.ExportPDF(r.Context(), d.DB, id, &buf)
		if errors.Is(err, notes.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `attachment; filename="`+safeName(name, "notebook")+`.pdf"`)
		_, _ = buf.WriteTo(w)
	})
	mux.HandleFunc("POST /files/forestnote/delete", func(w http.ResponseWriter, r *http.Request) {
		id := r.FormValue("notebook")
		if err := notes.DeleteNotebook(r.Context(), d.DB, id); err != nil && !errors.Is(err, notes.ErrNotFound) {
			d.fail(w, err)
			return
		}
		back(w, r, "/files/forestnote", "Notebook deleted. Devices remove it on their next sync.")
	})
	mux.HandleFunc("POST /files/forestnote/reprocess", func(w http.ResponseWriter, r *http.Request) {
		id := r.FormValue("notebook")
		if _, err := notes.Reprocess(r.Context(), d.DB, id); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/files/forestnote?notebook="+id, "Pages queued for recognition.")
	})
}

var unsafeName = regexp.MustCompile(`[\x00-\x1f\x7f"\\/:*?<>|]`)

func safeName(name, fallback string) string {
	name = strings.Trim(strings.TrimSpace(unsafeName.ReplaceAllString(name, "_")), ".")
	if name == "" {
		return fallback
	}
	if r := []rune(name); len(r) > 120 {
		name = string(r[:120])
	}
	return name
}

// --- Books ---

func (d Deps) bookRoutes(mux *http.ServeMux) {
	library := func() books.LibraryService { return books.NewLibraryService(d.DB, d.Objects) }
	mux.HandleFunc("GET /files/forestnote/books", func(w http.ResponseWriter, r *http.Request) {
		if id := r.URL.Query().Get("book"); id != "" {
			detail, err := library().GetBook(r.Context(), id)
			if errors.Is(err, books.ErrLibraryBookNotFound) {
				http.NotFound(w, r)
				return
			}
			if err != nil {
				d.fail(w, err)
				return
			}
			d.render(w, r, "book", detail.Book.Title, "books", detail)
			return
		}
		list, err := library().ListBooks(r.Context())
		if err != nil {
			d.fail(w, err)
			return
		}
		d.render(w, r, "books", "Books", "books", list)
	})
	mux.HandleFunc("GET /files/forestnote/books/ink", func(w http.ResponseWriter, r *http.Request) {
		png, err := library().RenderAnnotationInk(r.Context(), r.URL.Query().Get("annotation"))
		if errors.Is(err, books.ErrLibraryAnnotationNotFound) || errors.Is(err, books.ErrLibraryNoInk) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, max-age=60")
		_, _ = w.Write(png)
	})
	mux.HandleFunc("GET /files/forestnote/books/file", func(w http.ResponseWriter, r *http.Request) {
		file, err := library().OpenBookFile(r.Context(), r.URL.Query().Get("book"))
		switch {
		case errors.Is(err, books.ErrLibraryBookNotFound):
			http.NotFound(w, r)
			return
		case errors.Is(err, books.ErrLibraryFileUnavailable):
			http.Error(w, "This book's file has not finished uploading from a device.", http.StatusConflict)
			return
		case err != nil:
			d.fail(w, err)
			return
		}
		defer file.Body.Close()
		h := w.Header()
		h.Set("Content-Type", file.ContentType)
		h.Set("Content-Disposition", `attachment; filename="`+safeName(file.Filename, "book")+`"`)
		h.Set("Cache-Control", "private, no-cache")
		buf := make([]byte, 256<<10)
		for {
			n, rerr := file.Body.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				// A digest failure mid-stream truncates the download; the
				// client sees an incomplete file rather than corrupt bytes.
				return
			}
		}
	})
}

// --- Search ---

func (d Deps) searchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		mode := r.URL.Query().Get("mode")
		scope := r.URL.Query().Get("source")
		if len(q) > 256 || (scope != "" && scope != "client" && scope != "boox") || (scope == "boox" && !d.Status.NativeBOOX) {
			http.Error(w, "Invalid search query or source.", 400)
			return
		}
		data := map[string]any{"Query": q, "Mode": mode, "Source": scope, "Semantic": d.Status.Embedding != "", "BOOX": d.Status.NativeBOOX}
		if q != "" && d.Status.NativeBOOX && scope != "client" {
			matches, err := d.searchBOOX(r.Context(), q)
			if err != nil {
				d.fail(w, err)
				return
			}
			data["Native"] = matches
		}
		if q != "" && scope != "boox" {
			pages, err := d.Search.Search(r.Context(), q, 50, mode != "keyword")
			if err != nil {
				d.fail(w, err)
				return
			}
			data["Pages"] = pages
			if len(q) <= 256 && len(strings.Fields(q)) <= 16 {
				annotations, err := readersearch.New(d.DB).Search(r.Context(), q, "", 50)
				if err != nil {
					d.fail(w, err)
					return
				}
				data["Annotations"] = annotations
			}
		}
		d.render(w, r, "search", "Search", "search", data)
	})
}

// --- Tasks ---

func (d Deps) taskRoutes(mux *http.ServeMux) {
	svc := func() tasksvc.TaskService { return tasksvc.NewTaskService(taskdb.NewStore(d.DB), nil) }
	mux.HandleFunc("GET /tasks", func(w http.ResponseWriter, r *http.Request) {
		var list []tasksvc.Task
		var err error
		trash := r.URL.Query().Get("show") == "deleted"
		if trash {
			list, err = svc().ListIncludingDeleted(r.Context())
		} else {
			list, err = svc().List(r.Context())
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		var open, done []tasksvc.Task
		for _, t := range list {
			if trash && !t.Deleted {
				continue
			}
			if t.Status == tasksvc.StatusCompleted || t.Status == tasksvc.StatusCancelled {
				done = append(done, t)
			} else {
				open = append(open, t)
			}
		}
		d.render(w, r, "tasks", "Tasks", "tasks", map[string]any{"Open": open, "Done": done, "Trash": trash})
	})
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		create := tasksvc.TaskCreate{Title: strings.TrimSpace(r.FormValue("title")), Detail: strings.TrimSpace(r.FormValue("detail"))}
		if due := r.FormValue("due"); due != "" {
			if t, err := time.ParseInLocation("2006-01-02", due, time.Local); err == nil {
				create.DueAt = &t
			}
		}
		if create.Title == "" {
			back(w, r, "/tasks", "A task needs a title.")
			return
		}
		if _, err := svc().Create(r.Context(), create); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/tasks", "")
	})
	mux.HandleFunc("POST /tasks/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		if err := svc().Complete(r.Context(), r.PathValue("id")); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/tasks", "")
	})
	mux.HandleFunc("POST /tasks/{id}/delete", func(w http.ResponseWriter, r *http.Request) {
		if err := svc().Delete(r.Context(), r.PathValue("id")); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/tasks", "Task deleted.")
	})
	mux.HandleFunc("POST /tasks/purge-completed", func(w http.ResponseWriter, r *http.Request) {
		if _, err := svc().PurgeCompleted(r.Context()); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/tasks", "Completed tasks cleared.")
	})
}

// --- Devices ---

var ulid = func(s string) bool { return wire.IsULID(s) }

func (d Deps) deviceRoutes(mux *http.ServeMux) {
	store := func() relay.Store { return relay.Store{DB: d.DB} }
	mux.HandleFunc("GET /settings/devices", func(w http.ResponseWriter, r *http.Request) {
		devices, err := store().ListDevices(r.Context())
		if err != nil {
			d.fail(w, err)
			return
		}
		d.render(w, r, "devices", "Devices", "devices", map[string]any{"Devices": devices})
	})
	mux.HandleFunc("POST /settings/devices/{action}", func(w http.ResponseWriter, r *http.Request) {
		site := r.FormValue("site_id")
		if !ulid(site) {
			http.Error(w, "unknown device", http.StatusBadRequest)
			return
		}
		var err error
		notice := ""
		switch r.PathValue("action") {
		case "rename":
			label := strings.TrimSpace(r.FormValue("label"))
			if r := []rune(label); len(r) > 128 {
				label = string(r[:128])
			}
			_, err = store().SetDeviceLabel(r.Context(), site, label)
			notice = "Device renamed."
		case "revoke":
			err = identity.Store{DB: d.DB}.Revoke(r.Context(), site)
			if errors.Is(err, identity.ErrInvalid) {
				err = nil
			}
			notice = "Device key revoked. It can no longer sync."
		case "prune":
			_, err = store().PruneDevice(r.Context(), site)
			notice = "Sync position cleared."
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/settings/devices", notice)
	})
}

// --- Settings: CalDAV and API tokens ---

type apiToken struct {
	Hash, Label      string
	Created          time.Time
	LastUsed, Revoke *time.Time
}

func (d Deps) settingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		collection, err := settings.Get(ctx, d.DB, settings.CalDAVCollectionName, "Tasks")
		if err != nil {
			d.fail(w, err)
			return
		}
		due, err := settings.Get(ctx, d.DB, settings.DueTimeMode, "preserve")
		if err != nil {
			d.fail(w, err)
			return
		}
		rows, err := d.DB.QueryContext(ctx, `SELECT token_hash, label, created_at, last_used_at FROM alexandria_api_tokens WHERE revoked_at IS NULL ORDER BY created_at DESC`)
		if err != nil {
			d.fail(w, err)
			return
		}
		var tokens []apiToken
		for rows.Next() {
			var t apiToken
			if err := rows.Scan(&t.Hash, &t.Label, &t.Created, &t.LastUsed); err != nil {
				rows.Close()
				d.fail(w, err)
				return
			}
			tokens = append(tokens, t)
		}
		rows.Close()
		d.render(w, r, "settings", "Settings", "settings", map[string]any{
			"Collection": collection, "DueMode": due, "Tokens": tokens, "NewToken": r.URL.Query().Get("new_token") != "",
			"Status": d.Status, "CalDAV": taskhost.Prefix + "/user/calendars/tasks/", "OAuthLabel": oauth.TokenLabel})
	})
	mux.HandleFunc("POST /settings/caldav", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSpace(r.FormValue("collection"))
		mode := r.FormValue("due_mode")
		if mode != "preserve" && mode != "date_only" {
			mode = "preserve"
		}
		if name == "" {
			name = "Tasks"
		}
		if err := settings.Set(r.Context(), d.DB, settings.CalDAVCollectionName, name, false); err != nil {
			d.fail(w, err)
			return
		}
		if err := settings.Set(r.Context(), d.DB, settings.DueTimeMode, mode, false); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/settings", "CalDAV settings saved.")
	})
	mux.HandleFunc("POST /settings/tokens/create", func(w http.ResponseWriter, r *http.Request) {
		label := strings.TrimSpace(r.FormValue("label"))
		if label == "" {
			label = "MCP client"
		}
		token, err := auth.NewStore(d.DB).CreateToken(r.Context(), label)
		if err != nil {
			d.fail(w, err)
			return
		}
		// Shown exactly once, never stored in plain text or put in a URL.
		w.Header().Set("Cache-Control", "no-store")
		d.render(w, r, "token", "New API token", "settings", map[string]any{"Token": token, "Label": label})
	})
	mux.HandleFunc("POST /settings/tokens/revoke", func(w http.ResponseWriter, r *http.Request) {
		if _, err := d.DB.ExecContext(r.Context(), `UPDATE alexandria_api_tokens SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, r.FormValue("token_hash")); err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/settings", "Token revoked.")
	})
}

// --- OAuth consent ---

func (d Deps) authorizeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /authorize", func(w http.ResponseWriter, r *http.Request) {
		req, err := oauth.Validate(r.Context(), d.DB, r.URL.Query())
		if err != nil {
			http.Error(w, "This authorization request is not valid. Start again from the app that sent you here.", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		d.render(w, r, "authorize", "Allow access?", "", map[string]any{"Request": req, "Query": r.URL.RawQuery})
	})
	mux.HandleFunc("POST /authorize", func(w http.ResponseWriter, r *http.Request) {
		// The original query rides back in a hidden field and is validated again.
		q, err := urlValues(r.FormValue("query"))
		var req oauth.Request
		if err == nil {
			req, err = oauth.Validate(r.Context(), d.DB, q)
		}
		if err != nil {
			http.Error(w, "This authorization request is not valid. Start again from the app that sent you here.", http.StatusBadRequest)
			return
		}
		target := oauth.Deny(req)
		if r.FormValue("decision") == "allow" {
			if target, err = oauth.Approve(r.Context(), d.DB, req); err != nil {
				d.fail(w, err)
				return
			}
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
}

func urlValues(raw string) (url.Values, error) { return url.ParseQuery(raw) }
