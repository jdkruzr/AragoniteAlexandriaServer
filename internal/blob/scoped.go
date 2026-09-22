package blob

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Scoped is the only blob capability supplied to library code. Physical keys
// are never accepted from HTTP requests or job payloads.
type Scoped struct {
	store  Store
	prefix string
}

func ForLibrary(store Store, libraryID string) (*Scoped, error) {
	id, err := uuid.Parse(libraryID)
	if err != nil || id == uuid.Nil || id.String() != libraryID || store == nil {
		return nil, errors.New("invalid library storage binding")
	}
	return &Scoped{store: store, prefix: "libraries/" + libraryID + "/"}, nil
}

func (s *Scoped) key(key string) (string, error) {
	if key == "" || len(key) > 1024 || strings.HasPrefix(key, "/") || strings.ContainsAny(key, "\\%:\x00\r\n") || strings.HasPrefix(key, "libraries/") {
		return "", errors.New("invalid library object key")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", errors.New("invalid library object key")
		}
	}
	return s.prefix + key, nil
}
func (s *Scoped) Put(ctx context.Context, key, media string, body io.Reader, size int64) (Info, error) {
	k, e := s.key(key)
	if e != nil {
		return Info{}, e
	}
	i, e := s.store.Put(ctx, k, media, body, size)
	i.Key = key
	return i, e
}
func (s *Scoped) Get(ctx context.Context, key string) (io.ReadCloser, Info, error) {
	k, e := s.key(key)
	if e != nil {
		return nil, Info{}, e
	}
	r, i, e := s.store.Get(ctx, k)
	i.Key = key
	return r, i, e
}
func (s *Scoped) Stat(ctx context.Context, key string) (Info, error) {
	k, e := s.key(key)
	if e != nil {
		return Info{}, e
	}
	i, e := s.store.Stat(ctx, k)
	i.Key = key
	return i, e
}
func (s *Scoped) Delete(ctx context.Context, key string) error {
	k, e := s.key(key)
	if e != nil {
		return e
	}
	return s.store.Delete(ctx, k)
}
func (s *Scoped) SignedGetURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	k, e := s.key(key)
	if e != nil {
		return "", e
	}
	if ttl > 15*time.Minute {
		return "", errors.New("signed URL lifetime exceeds 15 minutes")
	}
	return s.store.SignedGetURL(ctx, k, ttl)
}
