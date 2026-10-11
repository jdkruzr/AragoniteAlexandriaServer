package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestRuntimeReadinessDependenciesAndRecovery(t *testing.T) {
	db := testenv.Database(t)
	var failed bool
	calls := 0
	check := func(ctx context.Context) error {
		calls++
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Error("dependency probe has no bounded deadline")
		}
		if failed {
			return errors.New("private upstream URL and credentials")
		}
		return nil
	}
	g := NewRuntimeGateway(db.DB, ":0", ":0", time.Second, http.NotFoundHandler(), slog.Default(), check)
	for _, handler := range []http.Handler{g.main.Handler, g.spc.Handler} {
		for _, failure := range []bool{false, true, false} {
			failed = failure
			for _, path := range []string{"/livez", "/health", "/readyz"} {
				before := calls
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				want := 200
				if failure && path == "/readyz" {
					want = 503
				}
				if w.Code != want || strings.Contains(w.Body.String(), "private") {
					t.Fatalf("%s dependency failure=%v: %d %s", path, failure, w.Code, w.Body.String())
				}
				if (calls > before) != (path == "/readyz") {
					t.Fatalf("%s checked the wrong dependencies", path)
				}
			}
		}
	}
	// Database failure must short circuit dependencies, but not liveness.
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}
	before := calls
	w := httptest.NewRecorder()
	g.main.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 || calls != before {
		t.Fatal("closed database reported ready or called dependencies")
	}
}

func TestRuntimeReadinessDeadlineAndShortCircuit(t *testing.T) {
	db := testenv.Database(t)
	check := func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	unexpected := func(context.Context) error {
		t.Error("checked later dependency after failure")
		return nil
	}
	g := NewRuntimeGateway(db.DB, ":0", ":0", time.Second, http.NotFoundHandler(), slog.Default(), check, unexpected)
	start := time.Now()
	w := httptest.NewRecorder()
	g.main.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 503 || time.Since(start) > 4*time.Second {
		t.Fatal("dependency timeout not bounded")
	}
}
