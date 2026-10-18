package boox

import (
	"strings"
	"unicode/utf8"
)

// prefixUpper returns the exclusive bound for valid UTF-8 PostgreSQL text under
// C collation. No bound exists for empty or all-max-rune prefixes. Callers in
// that case retain starts_with rather than invent an invalid UTF-8 sentinel.
func prefixUpper(prefix string) (string, bool) {
	if !utf8.ValidString(prefix) {
		return "", false
	}
	runes := []rune(prefix)
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == utf8.MaxRune {
			continue
		}
		next := runes[i] + 1
		if next >= 0xD800 && next <= 0xDFFF {
			next = 0xE000
		}
		return string(append(runes[:i], next)), true
	}
	return "", false
}

// Directory bounds are always finite: '/' is followed by '0' in C collation.
func directoryUpper(prefix string) string {
	if !strings.HasSuffix(prefix, "/") {
		panic("directory prefix must end in slash")
	}
	upper, _ := prefixUpper(prefix)
	return upper
}
