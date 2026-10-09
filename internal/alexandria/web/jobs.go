package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
)

func (d Deps) jobRoutes(mux *http.ServeMux) {
	store := recognition.Store{DB: d.DB}
	mux.HandleFunc("GET /jobs", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		offset, _ := strconv.Atoi(q.Get("offset"))
		f := recognition.Filter{Source: q.Get("source"), State: q.Get("state"), Batch: q.Get("batch"), Offset: offset}
		v, e := store.List(r.Context(), f)
		if e != nil {
			d.fail(w, e)
			return
		}
		link := func(n int) string {
			copy := url.Values{}
			for k, values := range q {
				copy[k] = append([]string{}, values...)
			}
			copy.Set("offset", strconv.Itoa(n))
			return "/jobs?" + copy.Encode()
		}
		previous, next := "", ""
		if offset > 0 {
			previous = link(max(0, offset-50))
		}
		if offset+50 < v.Total {
			next = link(offset + 50)
		}
		live := q.Get("live") == "1"
		if live && (v.Counts["queued"]+v.Counts["processing"]) > 0 {
			w.Header().Set("Refresh", "5")
		}
		liveQuery := url.Values{}
		for k, vals := range q {
			liveQuery[k] = append([]string{}, vals...)
		}
		if live {
			liveQuery.Del("live")
		} else {
			liveQuery.Set("live", "1")
		}
		liveURL := "/jobs?" + liveQuery.Encode()
		d.render(w, r, "jobs", "Jobs", "jobs", map[string]any{"View": v, "Filter": f, "Live": live, "LiveURL": liveURL, "Previous": previous, "Next": next, "Refresh": r.URL.RequestURI(), "Start": min(offset+1, v.Total), "End": min(offset+50, v.Total)})
	})
	mux.HandleFunc("POST /jobs/control", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		batch := r.Form.Get("kind") == "batch"
		n, e := store.Control(r.Context(), r.Form.Get("id"), batch, r.Form.Get("action"))
		if e != nil {
			d.fail(w, e)
			return
		}
		dest := "/jobs"
		if batch {
			dest += "?batch=" + url.QueryEscape(r.Form.Get("id"))
		}
		back(w, r, dest, fmt.Sprintf("Updated %d jobs. Requests already sent to a provider may still incur charges.", n))
	})
	mux.HandleFunc("GET /jobs/new", func(w http.ResponseWriter, r *http.Request) {
		source, id := r.URL.Query().Get("source"), r.URL.Query().Get("notebook")
		data := map[string]any{"Source": source, "Enabled": d.Status.OCR != "", "Token": uuid.NewString()}
		if id != "" && d.RecognitionCatalog != nil {
			n, e := d.RecognitionCatalog(r.Context(), source, id)
			if e != nil {
				http.Error(w, "Notebook unavailable or page catalog incomplete.", 422)
				return
			}
			data["Notebook"] = n
			data["Count"] = len(n.Pages)
			data["Version"] = n.Version()
		} else {
			query := strings.TrimSpace(r.URL.Query().Get("q"))
			if len(query) > 256 {
				http.Error(w, "Search is too long", 400)
				return
			}
			choices, e := store.Choices(r.Context(), d.Status.NativeBOOX, query)
			if e != nil {
				d.fail(w, e)
				return
			}
			data["More"] = len(choices) > 50
			data["Choices"] = choices[:min(50, len(choices))]
			data["Query"] = query
		}
		d.render(w, r, "jobs-new", "Queue recognition", "jobs", data)
	})
	mux.HandleFunc("POST /jobs/queue", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		if d.Status.OCR == "" {
			back(w, r, "/jobs/new", "Enable a recognition provider in Server settings first.")
			return
		}
		if r.Form.Get("confirm") != "on" || d.RecognitionCatalog == nil {
			http.Error(w, "Confirm sending the selected pages to your provider.", 400)
			return
		}
		source, id := r.Form.Get("source"), r.Form.Get("notebook")
		n, e := d.RecognitionCatalog(r.Context(), source, id)
		if e != nil {
			http.Error(w, "Notebook unavailable", 422)
			return
		}
		if n.Version() != r.Form.Get("version") {
			http.Error(w, "The notebook's page list changed. Reload the selection before queueing.", 409)
			return
		}
		from, _ := strconv.Atoi(r.Form.Get("from"))
		to, _ := strconv.Atoi(r.Form.Get("to"))
		batch, count, e := store.Queue(r.Context(), source, n, from, to, r.Form.Get("force") == "on", r.Form.Get("token"))
		if e != nil {
			back(w, r, "/jobs/new?source="+url.QueryEscape(source)+"&notebook="+url.QueryEscape(id), e.Error())
			return
		}
		back(w, r, "/jobs?batch="+batch, fmt.Sprintf("%d pages queued. Already active or paused pages were left with their existing jobs.", count))
	})
}
