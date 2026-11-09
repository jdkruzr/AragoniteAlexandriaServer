package mcptools

import (
	"strings"
	"testing"
)

func TestClipLinesBoundsLinesAndLength(t *testing.T) {
	long := strings.Repeat("{\n  \"key\": 1,\n", 20)
	got := clipLines(long, 400, "  ")
	if n := strings.Count(got, "\n"); n != 5 {
		t.Fatalf("%d line breaks:\n%s", n, got)
	}
	if got := clipLines("a\n\n  b  c\nd", 400, "> "); got != "a\n> b c\n> d" {
		t.Fatalf("%q", got)
	}
	if got := clipLines(strings.Repeat("x", 500), 400, ""); len([]rune(got)) != 401 {
		t.Fatalf("length %d", len([]rune(got)))
	}
	if got := withoutMadeFrom("from the vendor\n\nFrom Synthetic Sync, page 3."); got != "from the vendor" {
		t.Fatalf("%q", got)
	}
	if got := withoutMadeFrom("From the vendor, page 3 of the contract"); got == "" {
		t.Fatal("stripped a detail that only looks like a source line")
	}
}
