package web

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"net/http"
	"net/url"
)

func (d Deps) libraryJobRoutes(mux *http.ServeMux) {
	store := recognition.Store{DB: d.DB}
	mux.HandleFunc("GET /jobs/library", func(w http.ResponseWriter, r *http.Request) {
		source := r.URL.Query().Get("source")
		if source == "" {
			source = "client"
		}
		data := map[string]any{"Token": uuid.NewString(), "Enabled": d.Status.OCR != "", "Model": d.Status.OCR, "NativeBOOX": d.Status.NativeBOOX}
		if id := r.URL.Query().Get("id"); id != "" {
			v, err := store.GetRun(r.Context(), id)
			if err != nil {
				d.fail(w, err)
				return
			}
			data["Run"] = v
			source = v.Source
			if v.State == "preparing" || v.State == "queued" && (v.Awaiting+v.Pending+v.Processing) > 0 {
				w.Header().Set("Refresh", "5")
			}
		}
		if source != "client" && source != "boox" {
			http.Error(w, "Unknown recognition source", 400)
			return
		}
		if source == "boox" && !d.Status.NativeBOOX {
			data["Enabled"] = false
			data["SourceUnavailable"] = true
			if data["Run"] == nil {
				http.NotFound(w, r)
				return
			}
		}
		data["Source"] = source
		d.render(w, r, "jobs-library", "Library recognition", "jobs", data)
	})
	mux.HandleFunc("POST /jobs/library", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		action, id := r.PostForm.Get("action"), r.PostForm.Get("id")
		var err error
		notice := ""
		if action == "prepare" {
			source := r.PostForm.Get("source")
			// Old BOOX preparation forms omitted source; preserve their meaning.
			if source == "" {
				if d.Status.NativeBOOX {
					source = "boox"
				} else {
					source = "client"
				}
			}
			if source != "client" && (source != "boox" || !d.Status.NativeBOOX) {
				http.Error(w, "Recognition source unavailable", 400)
				return
			}
			id, err = store.CreateRun(r.Context(), source, r.PostForm.Get("token"))
			notice = "Preparing notebook page catalogs. No recognition has been requested."
		} else {
			if _, e := uuid.Parse(id); e != nil {
				http.Error(w, "Invalid library batch", 400)
				return
			}
			source, e := store.RunSource(r.Context(), id)
			if e != nil {
				http.Error(w, "Library batch unavailable", 404)
				return
			}
			if action == "queue" {
				if source == "boox" && !d.Status.NativeBOOX {
					http.Error(w, "BOOX source unavailable", 400)
					return
				}
				// The send button names the page count and model; pressing it is
				// the confirmation.
				if d.Status.OCR == "" {
					http.Error(w, "Enable recognition before sending these pages to your provider.", 400)
					return
				}
				err = store.ApproveRun(r.Context(), id, r.PostForm["exclude"], r.PostForm.Get("force") == "on")
				notice = "Library batch confirmed. Pages will be queued in the background."
			} else {
				if action == "retry_catalog" {
					action = "prepare"
				}
				var count int64
				count, err = store.ControlRun(r.Context(), id, action)
				notice = fmt.Sprintf("Updated %d items. Requests already sent to a provider may still finish.", count)
			}
		}
		if err != nil {
			notice = "Could not update this library batch. Reload its status and check the selection."
			var safe recognition.RunError
			if errors.As(err, &safe) {
				notice = safe.Error()
			}
		}
		dest := "/jobs/library"
		if id != "" {
			dest += "?id=" + url.QueryEscape(id)
		}
		back(w, r, dest, notice)
	})
}
