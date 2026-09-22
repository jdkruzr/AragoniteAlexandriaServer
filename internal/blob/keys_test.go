package blob

import (
	"strings"
	"testing"
)

func TestContentKey(t *testing.T) {
	digest, n, err := Hash(strings.NewReader("alexandria"))
	if err != nil || n != 10 {
		t.Fatalf("Hash() = %q, %d, %v", digest, n, err)
	}
	key, err := ContentKey("notes", digest)
	if err != nil {
		t.Fatal(err)
	}
	want := "notes/sha256/f8/ce/f8ce85591c31f9f949a210e89e1337ba902745da3835682d114f55ba720995c9"
	if key != want {
		t.Fatalf("ContentKey() = %q, want %q", key, want)
	}
}

func TestContentKeyRejectsTraversal(t *testing.T) {
	if _, err := ContentKey("", strings.Repeat("a", 64)); err == nil {
		t.Fatal("expected empty namespace to fail")
	}
}
