// Package ocr is the page-recognition client (Anthropic Messages or
// OpenAI-compatible chat completions). Copied from UltraBridge
// (internal/processor ocrclient.go, transient.go) under Apache-2.0.
package ocr

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// OCRFormatAnthropic uses the Anthropic Messages API (/v1/messages).
	// Compatible with direct Anthropic API and OpenRouter.
	OCRFormatAnthropic = "anthropic"

	// OCRFormatOpenAI uses the OpenAI Chat Completions API (/v1/chat/completions).
	// Compatible with vLLM, Ollama, and any OpenAI-compatible endpoint.
	OCRFormatOpenAI = "openai"
)

// DefaultOCRPrompt is used when no custom prompt is configured.
const DefaultOCRPrompt = "Transcribe all handwritten text from this page exactly as written. Return only the text, no commentary."

// OCRClient posts JPEG images to a vision API and returns transcribed text.
// Supports both Anthropic Messages API format and OpenAI Chat Completions format.
type OCRClient struct {
	apiURL              string
	apiKey              string
	model               string
	format              string
	client              *http.Client
	vllmDisableThinking bool
	anthropicWorkspace  string
}

// Option configures optional provider-specific behavior.
type Option func(*OCRClient)

// WithVLLMDisableThinking sends the vLLM chat-template extension. Leave it off
// for standard OpenAI-compatible endpoints; it never affects Anthropic requests.
func WithVLLMDisableThinking() Option {
	return func(c *OCRClient) { c.vllmDisableThinking = true }
}

// WithAnthropicWorkspace selects a workspace for multi-workspace Anthropic keys.
// It is never sent to an OpenAI-format endpoint.
func WithAnthropicWorkspace(id string) Option {
	return func(c *OCRClient) { c.anthropicWorkspace = id }
}

// NewOCRClient creates an OCRClient.
// apiURL is the API base (e.g. "https://api.anthropic.com", "https://openrouter.ai/api",
// or "http://localhost:8000" for a local vLLM instance).
// format is OCRFormatAnthropic or OCRFormatOpenAI.
func NewOCRClient(apiURL, apiKey, model, format string, options ...Option) *OCRClient {
	if format != OCRFormatOpenAI {
		format = OCRFormatAnthropic // default
	}
	c := &OCRClient{
		apiURL: apiURL,
		apiKey: apiKey,
		model:  model,
		format: format,
		client: &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	for _, option := range options {
		option(c)
	}
	return c
}

// Model returns the model name this client posts to the vision API. Used
// by the Boox pipeline to tag completed jobs with the api_model they ran
// against. (The Supernote pipeline reaches into the unexported field
// directly because it lives in the same package.)
func (c *OCRClient) Model() string { return c.model }

// Recognize sends a JPEG page image to the vision API and returns the transcribed text.
// If prompt is empty, the default prompt is used.
func (c *OCRClient) Recognize(ctx context.Context, jpegData []byte, prompt string) (string, error) {
	if prompt == "" {
		prompt = DefaultOCRPrompt
	}
	if c.format == OCRFormatOpenAI {
		return c.recognizeOpenAI(ctx, jpegData, prompt)
	}
	return c.recognizeAnthropic(ctx, jpegData, prompt)
}

// ── Anthropic Messages API ────────────────────────────────────────────────────

type anthropicRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	Messages  []anthropicMsg `json:"messages"`
}

type anthropicMsg struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type   string           `json:"type"`
	Text   string           `json:"text,omitempty"`
	Source *anthropicSource `json:"source,omitempty"`
}

type anthropicSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (c *OCRClient) recognizeAnthropic(ctx context.Context, jpegData []byte, prompt string) (string, error) {
	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: 4096,
		Messages: []anthropicMsg{{
			Role: "user",
			Content: []anthropicContent{
				{Type: "text", Text: prompt},
				{Type: "image", Source: &anthropicSource{
					Type:      "base64",
					MediaType: "image/jpeg",
					Data:      base64.StdEncoding.EncodeToString(jpegData),
				}},
			},
		}},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ocrclient marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ocrclient request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	if c.anthropicWorkspace != "" {
		req.Header.Set("anthropic-workspace-id", c.anthropicWorkspace)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		// Dial failures, connection resets, and client-side timeouts: the
		// model never saw the request, so this is always worth a retry.
		return "", Transient(fmt.Errorf("ocrclient post: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := providerHTTPError(resp)
		if transientHTTPStatus(resp.StatusCode) {
			return "", Transient(err)
		}
		return "", err
	}

	var vResp anthropicResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&vResp); err != nil {
		return "", fmt.Errorf("ocrclient decode: %w", err)
	}
	if len(vResp.Content) == 0 {
		return "", fmt.Errorf("ocrclient: empty response")
	}
	var text strings.Builder
	for _, block := range vResp.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("ocrclient: response contains no text")
	}
	return text.String(), nil
}

// ── OpenAI Chat Completions API ───────────────────────────────────────────────

type openAIRequest struct {
	Model     string      `json:"model"`
	MaxTokens int         `json:"max_tokens"`
	Messages  []openAIMsg `json:"messages"`
	// Explicit opt-in only: standard requests omit the vLLM extension entirely.
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
}

type openAIMsg struct {
	Role    string          `json:"role"`
	Content []openAIContent `json:"content"`
}

type openAIContent struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *openAIImgURL `json:"image_url,omitempty"`
}

type openAIImgURL struct {
	URL string `json:"url"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *OCRClient) recognizeOpenAI(ctx context.Context, jpegData []byte, prompt string) (string, error) {
	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpegData)

	reqBody := openAIRequest{
		Model:     c.model,
		MaxTokens: 4096,
		Messages: []openAIMsg{{
			Role: "user",
			Content: []openAIContent{
				{Type: "text", Text: prompt},
				{Type: "image_url", ImageURL: &openAIImgURL{URL: dataURL}},
			},
		}},
	}
	if c.vllmDisableThinking {
		reqBody.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("ocrclient marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ocrclient request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		// Dial failures, connection resets, and client-side timeouts: the
		// model never saw the request, so this is always worth a retry.
		return "", Transient(fmt.Errorf("ocrclient post: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err := providerHTTPError(resp)
		if transientHTTPStatus(resp.StatusCode) {
			return "", Transient(err)
		}
		return "", err
	}

	var vResp openAIResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&vResp); err != nil {
		return "", fmt.Errorf("ocrclient decode: %w", err)
	}
	if len(vResp.Choices) == 0 {
		return "", fmt.Errorf("ocrclient: empty response")
	}
	return vResp.Choices[0].Message.Content, nil
}

// HTTPError exposes status without retaining or returning sensitive provider bodies.
type HTTPError struct {
	Status            int
	WorkspaceRequired bool
}

// Classify a known diagnostic without retaining arbitrary upstream text.
func providerHTTPError(resp *http.Response) *HTTPError {
	err := &HTTPError{Status: resp.StatusCode}
	if resp.StatusCode == http.StatusBadRequest {
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body) == nil {
			message := strings.ToLower(body.Error.Message)
			err.WorkspaceRequired = strings.Contains(message, "anthropic-workspace-id")
		}
	}
	return err
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("recognition endpoint returned HTTP %d", e.Status)
}
