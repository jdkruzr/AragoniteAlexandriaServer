// Package embed turns note text into vectors: whitespace chunking and an
// Ollama /api/embed client. Copied from UltraBridge (internal/rag chunk.go,
// embedder.go) under Apache-2.0.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const embedTimeout = 120 * time.Second

// Embedder turns one chunk of text into a vector.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Model() string
}

// Ollama calls Ollama's /api/embed endpoint.
type Ollama struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllama(baseURL, model string) *Ollama {
	return &Ollama{baseURL: strings.TrimRight(baseURL, "/"), model: model, client: &http.Client{Timeout: embedTimeout}}
}

// Model is stored with each vector; vectors from different models never mix.
func (e *Ollama) Model() string { return e.model }

type embedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
}

func (e *Ollama) Embed(ctx context.Context, text string) ([]float32, error) {
	body, err := json.Marshal(embedRequest{Model: e.model, Input: text})
	if err != nil {
		return nil, fmt.Errorf("marshal embed request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", e.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Ollama's error body (e.g. "the input length exceeds the context
		// length") makes failures diagnosable.
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var result embedResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("empty embeddings response")
	}
	f32 := make([]float32, len(result.Embeddings[0]))
	for i, v := range result.Embeddings[0] {
		f32[i] = float32(v)
	}
	return f32, nil
}
