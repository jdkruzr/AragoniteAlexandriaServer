package taskattach

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

// errBadKey is returned when an attachment key isn't a sha256 hex digest. The
// key flows in from a URL path segment; it is validated before any storage
// access so the URL signature is never the only guard.
var errBadKey = errors.New("taskattach: key is not a sha256 hex digest")

// Namespace is the object-key namespace for attachment bytes in a library.
const Namespace = "task-attachments"

// validSHA reports whether s is exactly 64 lowercase hex chars.
func validSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// opTimeout bounds one object operation; CalDAV callers pass no context.
const opTimeout = time.Minute

// BlobStore is a content-addressed store for inline-binary attachment bytes
// lifted out of a task's ical_blob (the de-bloat path). Identical attachments
// across tasks dedup to one object; the task row keeps only a reference.
type BlobStore struct{ Objects blob.Store }

// Put stores data by its sha256 and returns the digest and size. Idempotent.
func (b BlobStore) Put(data []byte) (sha string, size int64, err error) {
	sum := sha256.Sum256(data)
	sha = hex.EncodeToString(sum[:])
	key, err := blob.ContentKey(Namespace, sha)
	if err != nil {
		return "", 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	if info, statErr := b.Objects.Stat(ctx, key); statErr == nil && info.Size == int64(len(data)) {
		return sha, info.Size, nil // already present
	}
	if _, err = b.Objects.Put(ctx, key, "application/octet-stream", bytes.NewReader(data), int64(len(data))); err != nil {
		return "", 0, err
	}
	return sha, int64(len(data)), nil
}

// Open returns the stored content and its size. The caller closes it.
func (b BlobStore) Open(sha string) (io.ReadCloser, int64, error) {
	if !validSHA(sha) {
		return nil, 0, errBadKey
	}
	key, err := blob.ContentKey(Namespace, sha)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	body, info, err := b.Objects.Get(ctx, key)
	if err != nil {
		cancel()
		return nil, 0, err
	}
	return &cancelOnClose{ReadCloser: body, cancel: cancel}, info.Size, nil
}

type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelOnClose) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
