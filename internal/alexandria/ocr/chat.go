package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Complete sends a text-only conversation (system instructions plus one user
// message) and returns the model's text. It shares transport, authentication
// and error classification with Recognize.
func (c *OCRClient) Complete(ctx context.Context, system, prompt string, maxTokens int) (string, error) {
	var url string
	var payload any
	if c.format == OCRFormatOpenAI {
		req := openAIRequest{Model: c.model, MaxTokens: maxTokens, Messages: []openAIMsg{
			{Role: "system", Content: []openAIContent{{Type: "text", Text: system}}},
			{Role: "user", Content: []openAIContent{{Type: "text", Text: prompt}}},
		}}
		if c.vllmDisableThinking {
			req.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
		}
		url, payload = c.apiURL+"/v1/chat/completions", req
	} else {
		url, payload = c.apiURL+"/v1/messages", struct {
			anthropicRequest
			System string `json:"system,omitempty"`
		}{anthropicRequest{Model: c.model, MaxTokens: maxTokens, Messages: []anthropicMsg{{Role: "user", Content: []anthropicContent{{Type: "text", Text: prompt}}}}}, system}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("chat marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.format == OCRFormatOpenAI {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	} else {
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		if c.anthropicWorkspace != "" {
			req.Header.Set("anthropic-workspace-id", c.anthropicWorkspace)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", Transient(fmt.Errorf("chat post: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := providerHTTPError(resp)
		if transientHTTPStatus(resp.StatusCode) {
			return "", Transient(err)
		}
		return "", err
	}
	limited := io.LimitReader(resp.Body, 2<<20)
	if c.format == OCRFormatOpenAI {
		var out openAIResponse
		if err := json.NewDecoder(limited).Decode(&out); err != nil || len(out.Choices) == 0 {
			return "", fmt.Errorf("chat: unreadable response")
		}
		return out.Choices[0].Message.Content, nil
	}
	var out anthropicResponse
	if err := json.NewDecoder(limited).Decode(&out); err != nil {
		return "", fmt.Errorf("chat: unreadable response")
	}
	var text strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" { // thinking blocks are never returned
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("chat: response contains no text")
	}
	return text.String(), nil
}
