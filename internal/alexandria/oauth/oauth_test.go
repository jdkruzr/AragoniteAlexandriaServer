package oauth

import (
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
)

func TestRequestedScope(t *testing.T) {
	for in, want := range map[string]auth.Scope{"": auth.ScopeFull, "read": auth.ScopeRead, " read ": auth.ScopeRead,
		"read full": auth.ScopeFull, "full": auth.ScopeFull, "openid": auth.ScopeFull, "openid read": auth.ScopeRead} {
		if got := requestedScope(in); got != want {
			t.Errorf("requestedScope(%q) = %q, want %q", in, got, want)
		}
	}
}
