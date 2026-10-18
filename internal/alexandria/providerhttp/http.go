// Package providerhttp shares bounded, credential-safe provider HTTP plumbing.
// Payloads, deadlines and semantic retry policies remain with each client.
package providerhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func Retryable(status int) bool {
	return status >= 500 || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout
}

// HTTPError retains only status and a recognized diagnostic, never raw bodies.
type HTTPError struct {
	Status            int
	WorkspaceRequired bool
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("provider endpoint returned HTTP %d", e.Status)
}
func ResponseError(resp *http.Response) *HTTPError {
	e := &HTTPError{Status: resp.StatusCode}
	if resp.StatusCode == http.StatusBadRequest {
		var body struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if ReadJSON(resp.Body, 64<<10, &body) == nil {
			e.WorkspaceRequired = strings.Contains(strings.ToLower(body.Error.Message), "anthropic-workspace-id")
		}
	} else {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	}
	return e
}

// ReadJSON enforces the complete response bound, including trailing bytes.
// Parse errors are deliberately generic: JSON errors can include upstream text.
func ReadJSON(r io.Reader, limit int64, out any) error {
	raw, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return errors.New("provider response read failed")
	}
	if int64(len(raw)) > limit {
		return errors.New("provider response exceeds limit")
	}
	if json.Unmarshal(raw, out) != nil {
		return errors.New("unreadable provider response")
	}
	return nil
}
