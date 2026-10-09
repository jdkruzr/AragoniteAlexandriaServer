package ocr

import (
	"errors"
	"fmt"
)

// Failure returns a fixed, safe diagnostic and whether retrying may help.
// Never retain or display arbitrary provider messages or request content.
func Failure(err error) (bool, string) {
	var h *HTTPError
	if errors.As(err, &h) {
		if h.WorkspaceRequired {
			return false, "Anthropic requires a valid workspace ID. Update Server settings, then retry."
		}
		switch h.Status {
		case 401, 403:
			return false, "The provider rejected authentication or model access. Check Server settings, then retry."
		case 400, 404, 422:
			return false, fmt.Sprintf("Provider rejected the request (HTTP %d). Check endpoint, model and API format, then retry.", h.Status)
		case 429:
			return true, "Provider rate or quota limit; waiting to retry. Check provider billing if this persists."
		}
		return transientHTTPStatus(h.Status), fmt.Sprintf("Recognition provider returned HTTP %d.", h.Status)
	}
	return true, "Recognition or page processing failed; waiting to retry."
}
