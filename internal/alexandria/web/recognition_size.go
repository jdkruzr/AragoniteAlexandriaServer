package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
)

func (d Deps) recognitionSizeRoutes(mux *http.ServeMux) {
	store := recognition.Store{DB: d.DB}
	mux.HandleFunc("POST /settings/recognition-limit", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		limit, _ := strconv.Atoi(r.PostForm.Get("limit"))
		expected, _ := strconv.Atoi(r.PostForm.Get("expected"))
		err := store.SetPageLimit(r.Context(), limit, expected)
		if limit < 1 || limit > 20000 {
			back(w, r, "/settings/recognition?section=page-limit#page-limit", "Enter a page limit from 1 to 20,000.")
			return
		}
		if errors.Is(err, recognition.ErrSelectionChanged) {
			back(w, r, "/settings/recognition?section=page-limit#page-limit", "The page limit changed. Review it and save again.")
			return
		}
		if err != nil {
			d.fail(w, err)
			return
		}
		back(w, r, "/settings/recognition?section=page-limit#page-limit", "Recognition page limit saved. No new work was queued.")
	})
	mux.HandleFunc("GET /jobs/size", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(q) > 256 {
			http.Error(w, "Search is too long", 400)
			return
		}
		rows, e := store.Oversized(r.Context(), q)
		if e != nil {
			d.fail(w, e)
			return
		}
		var limit int
		if e = d.DB.QueryRowContext(r.Context(), `SELECT alexandria_recognition_page_limit()`).Scan(&limit); e != nil {
			d.fail(w, e)
			return
		}
		d.render(w, r, "jobs-size", "Notebook size limits", "jobs", map[string]any{"Rows": rows[:min(50, len(rows))], "More": len(rows) > 50, "Query": q, "Limit": limit})
	})
	mux.HandleFunc("POST /jobs/size", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		source, id := r.PostForm.Get("source"), r.PostForm.Get("notebook")
		allow := r.PostForm.Get("allow")
		if allow != "1" && allow != "0" {
			http.Error(w, "Invalid form", 400)
			return
		}
		if e := store.AllowLarge(r.Context(), source, id, allow == "1"); e != nil {
			if errors.Is(e, recognition.ErrNotebookDeleted) {
				back(w, r, "/jobs/size", "Notebook is no longer available.")
				return
			}
			d.fail(w, e)
			return
		}
		back(w, r, "/jobs/new?source="+url.QueryEscape(source)+"&notebook="+url.QueryEscape(id), "Notebook size policy saved. Skips still apply; no new work was queued.")
	})
}
