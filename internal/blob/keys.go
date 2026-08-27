package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

// ContentKey returns an immutable, content-addressed object key. Names and
// directory relationships live in PostgreSQL and can change without copying a
// potentially large object.
func ContentKey(namespace, digest string) (string, error) {
	namespace = strings.Trim(strings.TrimSpace(namespace), "/")
	digest = strings.ToLower(strings.TrimSpace(digest))
	if namespace == "" || len(digest) != sha256.Size*2 {
		return "", fmt.Errorf("invalid namespace or sha256 digest")
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("invalid sha256 digest: %w", err)
	}
	return fmt.Sprintf("%s/sha256/%s/%s/%s", namespace, digest[:2], digest[2:4], digest), nil
}

func Hash(reader io.Reader) (string, int64, error) {
	h := sha256.New()
	n, err := io.Copy(h, reader)
	if err != nil {
		return "", n, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
