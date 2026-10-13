package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEmbeddingAvailabilityNeverRunsInference(t *testing.T) {
	for _, tc := range []struct{ name, body, model, label string }{
		{"installed", `{"models":[{"name":"nomic-embed-text:v1.5"}]}`, "nomic-embed-text:v1.5", "Ready"},
		{"latest alias", `{"models":[{"model":"nomic-embed-text:latest"}]}`, "nomic-embed-text", "Ready"},
		{"missing", `{"models":[]}`, "synthetic", "Model missing"},
		{"wrong tag", `{"models":[{"name":"nomic-embed-text:latest"}]}`, "nomic-embed-text:v1.5", "Model missing"},
		{"invalid", `{"secret":"private upstream body"}`, "synthetic", "Unavailable"},
		{"too large", strings.Repeat("x", (1<<20)+1), "synthetic", "Unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.URL.Path != "/api/tags" {
					t.Error("unexpected inference request", r.Method, r.URL.Path)
				}
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			got := EmbeddingAvailability(context.Background(), EmbedConfig{URL: srv.URL, Model: tc.model})
			if got.Label != tc.label || got.Ready != (tc.label == "Ready") || calls != 1 || strings.Contains(got.Detail, "private upstream") {
				t.Fatal(got, calls)
			}
		})
	}
	if got := EmbeddingAvailability(context.Background(), EmbedConfig{}); got.Label != "Not configured" {
		t.Fatal(got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := EmbeddingAvailability(ctx, EmbedConfig{URL: "http://127.0.0.1:1", Model: "synthetic"}); got.Label != "Unavailable" {
		t.Fatal(got)
	}
}

func TestEmbeddingAvailabilityDoesNotFollowRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed redirect") }))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer srv.Close()
	if got := EmbeddingAvailability(context.Background(), EmbedConfig{URL: srv.URL, Model: "synthetic"}); got.Ready || got.Label != "Unavailable" {
		t.Fatal(got)
	}
}
