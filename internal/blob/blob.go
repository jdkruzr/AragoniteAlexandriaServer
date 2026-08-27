// Package blob defines Loom's only durable binary-storage contract. Domain
// packages deal in keys and streams; they never depend on host paths.
package blob

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrNotFound = errors.New("blob not found")

type Info struct {
	Key         string
	Size        int64
	ContentType string
	ETag        string
	ModifiedAt  time.Time
}

type Store interface {
	Put(ctx context.Context, key, contentType string, body io.Reader, size int64) (Info, error)
	Get(ctx context.Context, key string) (io.ReadCloser, Info, error)
	Stat(ctx context.Context, key string) (Info, error)
	Delete(ctx context.Context, key string) error
	SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error)
}
