package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type recordingEnqueuer struct {
	jobType, key string
	maxAttempts  int
}

func (f *recordingEnqueuer) Enqueue(_ context.Context, jobType, key string, _ any, maxAttempts int) (uuid.UUID, bool, error) {
	f.jobType, f.key, f.maxAttempts = jobType, key, maxAttempts
	return uuid.MustParse("00000000-0000-0000-0000-000000000001"), true, nil
}

func TestVerifyBlobEnqueuesDurableJob(t *testing.T) {
	service := &recordingEnqueuer{}
	mux := http.NewServeMux()
	Jobs{Service: service}.Register(mux)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/blob-verify", strings.NewReader(
		`{"key":"objects/abc","size":5,"idempotency_key":"verify:abc","max_attempts":3}`,
	))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body)
	}
	if service.jobType != "blob.verify" || service.key != "verify:abc" || service.maxAttempts != 3 {
		t.Fatalf("enqueue = type %q, key %q, attempts %d", service.jobType, service.key, service.maxAttempts)
	}
}
