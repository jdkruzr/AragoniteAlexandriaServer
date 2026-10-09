package ocr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The vLLM-specific extension is only sent when explicitly enabled.
func TestRecognizeOpenAI_DisablesQwenThinking(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &capturedBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"transcribed"}}]}`))
	}))
	defer srv.Close()

	c := NewOCRClient(srv.URL, "test-key", "qwen2.5-vl-7b", OCRFormatOpenAI, WithVLLMDisableThinking())
	text, err := c.Recognize(context.Background(), []byte("fake-jpeg-bytes"), "Transcribe.")
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if text != "transcribed" {
		t.Errorf("text: got %q, want %q", text, "transcribed")
	}

	kwargs, ok := capturedBody["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs missing or wrong type; full body: %+v", capturedBody)
	}
	if think, ok := kwargs["enable_thinking"].(bool); !ok || think {
		t.Errorf("enable_thinking: got %v (type %T), want false (bool)", kwargs["enable_thinking"], kwargs["enable_thinking"])
	}
}

// TestRecognizeAnthropic_NoKwargs is a regression guard: the Anthropic
// Messages API has no chat-template concept, and shipping a kwargs key on
// that path would be a serializer leak. Confirm the request body is clean.
func TestRecognizeAnthropic_NoKwargs(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &capturedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"content":[{"type":"text","text":"transcribed"}]}`))
	}))
	defer srv.Close()

	c := NewOCRClient(srv.URL, "test-key", "claude-sonnet-4-6", OCRFormatAnthropic, WithVLLMDisableThinking())
	if _, err := c.Recognize(context.Background(), []byte("jpg"), "go"); err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if _, present := capturedBody["chat_template_kwargs"]; present {
		t.Errorf("Anthropic request must not carry chat_template_kwargs; body: %+v", capturedBody)
	}
}

func TestRecognizeOpenAIStrictEndpointByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("wrong path or auth")
		}
		var body struct {
			Model     string      `json:"model"`
			MaxTokens int         `json:"max_tokens"`
			Messages  []openAIMsg `json:"messages"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			http.Error(w, "unknown request field", 400)
			return
		}
		if body.Model != "vision-model" || len(body.Messages) != 1 || len(body.Messages[0].Content) != 2 || body.Messages[0].Content[1].ImageURL == nil {
			t.Error("image request lost")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"strict endpoint accepted"}}]}`))
	}))
	defer srv.Close()
	c := NewOCRClient(srv.URL, "test-key", "vision-model", OCRFormatOpenAI)
	got, err := c.Recognize(context.Background(), []byte("jpeg"), "Transcribe.")
	if err != nil || got != "strict endpoint accepted" {
		t.Fatalf("strict endpoint: %q %v", got, err)
	}
}
