package auth

import "testing"

func TestTokenHashDoesNotStoreToken(t *testing.T) {
	token := "alexandria_a-secret-token"
	hash := tokenHash(token)
	if hash == token || len(hash) != 64 {
		t.Fatalf("tokenHash() = %q", hash)
	}
}
