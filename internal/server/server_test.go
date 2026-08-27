package server

import (
	"log/slog"
	"testing"
	"time"
)

func TestNewGatewayRoutePatternsDoNotConflict(t *testing.T) {
	t.Parallel()

	// Go's ServeMux checks pattern conflicts during registration, so merely
	// constructing the gateway is the regression test for the root/API overlap.
	_ = NewGateway(nil, ":0", ":0", time.Second, nil, slog.Default())
}
