package blob

import (
	"strings"
	"testing"
)

func TestContentKey(t *testing.T) {
	digest, n, err := Hash(strings.NewReader("loom"))
	if err != nil || n != 4 {
		t.Fatalf("Hash() = %q, %d, %v", digest, n, err)
	}
	key, err := ContentKey("notes", digest)
	if err != nil {
		t.Fatal(err)
	}
	want := "notes/sha256/78/4a/784a2ddf5d4d61b6ae157ca800ce8ab7694b7141dc6f652a2600323d706a3d86"
	if key != want {
		t.Fatalf("ContentKey() = %q, want %q", key, want)
	}
}

func TestContentKeyRejectsTraversal(t *testing.T) {
	if _, err := ContentKey("", strings.Repeat("a", 64)); err == nil {
		t.Fatal("expected empty namespace to fail")
	}
}
