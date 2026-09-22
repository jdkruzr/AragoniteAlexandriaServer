package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/tasks"
)

type Tasks struct{ Store *tasks.Store }

func (a Tasks) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/tasks", a.list)
	mux.HandleFunc("POST /api/v1/tasks", a.create)
	mux.HandleFunc("GET /api/v1/tasks/{id}", a.get)
	mux.HandleFunc("PUT /api/v1/tasks/{id}", a.put)
	mux.HandleFunc("DELETE /api/v1/tasks/{id}", a.delete)
}

func (a Tasks) list(w http.ResponseWriter, r *http.Request) {
	items, err := a.Store.List(r.Context(), r.URL.Query().Get("include_deleted") == "true")
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": items})
}

func (a Tasks) create(w http.ResponseWriter, r *http.Request) {
	var task tasks.Task
	if err := decode(w, r, &task); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	if err := a.Store.Upsert(r.Context(), task); err != nil {
		writeError(w, err)
		return
	}
	saved, err := a.Store.Get(r.Context(), task.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

func (a Tasks) get(w http.ResponseWriter, r *http.Request) {
	task, err := a.Store.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (a Tasks) put(w http.ResponseWriter, r *http.Request) {
	var task tasks.Task
	if err := decode(w, r, &task); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	task.ID = r.PathValue("id")
	if err := a.Store.Upsert(r.Context(), task); err != nil {
		writeError(w, err)
		return
	}
	saved, err := a.Store.Get(r.Context(), task.ID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (a Tasks) delete(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decode(w http.ResponseWriter, r *http.Request, target any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return errors.New("Content-Type must be application/json")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, tasks.ErrNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
