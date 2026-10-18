package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providerhttp"
)

// HTTPError remains available to existing OCR/speech failure classifiers.
type HTTPError = providerhttp.HTTPError

func (c *OCRClient) postText(ctx context.Context, url string, payload any, operation string) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%s marshal: %w", operation, err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("%s request: %w", operation, err)
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
		return "", Transient(fmt.Errorf("%s post: %w", operation, err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := providerhttp.ResponseError(resp)
		if providerhttp.Retryable(resp.StatusCode) {
			return "", Transient(err)
		}
		return "", err
	}
	if c.format == OCRFormatOpenAI {
		var out openAIResponse
		if err = providerhttp.ReadJSON(resp.Body, 2<<20, &out); err != nil {
			return "", err
		}
		if len(out.Choices) == 0 {
			return "", fmt.Errorf("%s: empty response", operation)
		}
		return out.Choices[0].Message.Content, nil
	}
	var out anthropicResponse
	if err = providerhttp.ReadJSON(resp.Body, 2<<20, &out); err != nil {
		return "", err
	}
	var text strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return "", fmt.Errorf("%s: response contains no text", operation)
	}
	return text.String(), nil
}
