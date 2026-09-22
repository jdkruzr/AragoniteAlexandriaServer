package blob

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

type recordingStore struct{ keys []string }

func (s *recordingStore) Put(_ context.Context, k, _ string, _ io.Reader, _ int64) (Info, error) {
	s.keys = append(s.keys, k)
	return Info{Key: k}, nil
}
func (s *recordingStore) Get(_ context.Context, k string) (io.ReadCloser, Info, error) {
	s.keys = append(s.keys, k)
	return io.NopCloser(strings.NewReader("")), Info{Key: k}, nil
}
func (s *recordingStore) Stat(_ context.Context, k string) (Info, error) {
	s.keys = append(s.keys, k)
	return Info{Key: k}, nil
}
func (s *recordingStore) Delete(_ context.Context, k string) error {
	s.keys = append(s.keys, k)
	return nil
}
func (s *recordingStore) SignedGetURL(_ context.Context, k string, _ time.Duration) (string, error) {
	s.keys = append(s.keys, k)
	return k, nil
}

func TestScopedBoundaries(t *testing.T) {
	backend := &recordingStore{}
	id := "00000000-0000-4000-8000-000000000001"
	s, err := ForLibrary(backend, id)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, key := range []string{"../other", "/absolute", "a//b", "a/../b", "a/%2e%2e/b", "libraries/other/file", "s3:bucket", "a\\b"} {
		if _, err := s.Stat(ctx, key); err == nil {
			t.Errorf("accepted %q", key)
		}
		if _, err := s.SignedGetURL(ctx, key, time.Minute); err == nil {
			t.Errorf("signed %q", key)
		}
	}
	if len(backend.keys) != 0 {
		t.Fatal("unsafe keys reached backend")
	}
	info, err := s.Put(ctx, "books/abc", "application/pdf", strings.NewReader("pdf"), 3)
	if err != nil || info.Key != "books/abc" {
		t.Fatal(info, err)
	}
	if backend.keys[0] != "libraries/"+id+"/books/abc" {
		t.Fatal(backend.keys)
	}
}
