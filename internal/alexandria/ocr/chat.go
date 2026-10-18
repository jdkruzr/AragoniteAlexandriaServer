package ocr

import "context"

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
	return c.postText(ctx, url, payload, "chat")
}
