package web

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFailureLogsSafeCode(t *testing.T) {
	var log bytes.Buffer
	d := Deps{Logger: slog.New(slog.NewJSONHandler(&log, nil))}
	w := httptest.NewRecorder()
	d.fail(w, errors.New("private-provider-body"))
	if w.Code != 500 {
		t.Fatal(w.Code)
	}
	if strings.Contains(log.String()+w.Body.String(), "private-provider-body") || !strings.Contains(log.String(), "operation_failed") {
		t.Fatal("unsafe or missing diagnostic")
	}
}
