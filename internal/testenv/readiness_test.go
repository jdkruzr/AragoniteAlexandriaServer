package testenv

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

// HTTP readiness alone does not prove that the fresh object node can allocate
// storage. CI runs this before its full gate; all keys belong to this fixture.
func TestObjectReadiness(t *testing.T) {
	store := Objects(t, uuid.NewString())
	key := "readiness/probe"
	body := []byte("synthetic object readiness")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = store.Delete(ctx, key)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for {
		attempt, done := context.WithTimeout(ctx, 5*time.Second)
		_, err := store.Put(attempt, key, "text/plain", bytes.NewReader(body), int64(len(body)))
		done()
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("object store did not become writable within 60 seconds")
		case <-time.After(time.Second):
		}
	}
	reader, _, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal("object readiness read failed")
	}
	got, err := io.ReadAll(io.LimitReader(reader, int64(len(body)+1)))
	reader.Close()
	if err != nil || !bytes.Equal(body, got) {
		t.Fatal("object readiness bytes differ")
	}
	if err = store.Delete(ctx, key); err != nil {
		t.Fatal("object readiness delete failed")
	}
	if _, err = store.Stat(ctx, key); !errors.Is(err, blob.ErrNotFound) {
		t.Fatal("object readiness deletion not visible")
	}
}
