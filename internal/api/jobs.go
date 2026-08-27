package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

type JobEnqueuer interface {
	Enqueue(context.Context, string, string, any, int) (uuid.UUID, bool, error)
}

type Jobs struct{ Service JobEnqueuer }

func (a Jobs) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/jobs/blob-verify", a.verifyBlob)
}

func (a Jobs) verifyBlob(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Key            string `json:"key"`
		Size           int64  `json:"size"`
		IdempotencyKey string `json:"idempotency_key"`
		MaxAttempts    int    `json:"max_attempts,omitempty"`
	}
	if err := decode(w, r, &request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	request.Key = strings.TrimSpace(request.Key)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	if request.Key == "" || request.IdempotencyKey == "" || request.Size < 0 {
		http.Error(w, "key and idempotency_key are required; size cannot be negative", http.StatusBadRequest)
		return
	}
	if request.MaxAttempts < 0 || request.MaxAttempts > 20 {
		http.Error(w, "max_attempts must be between 0 and 20", http.StatusBadRequest)
		return
	}
	id, created, err := a.Service.Enqueue(r.Context(), "blob.verify", request.IdempotencyKey, map[string]any{
		"key": request.Key, "size": request.Size,
	}, request.MaxAttempts)
	if err != nil {
		http.Error(w, "job scheduling failed", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "created": created})
}
