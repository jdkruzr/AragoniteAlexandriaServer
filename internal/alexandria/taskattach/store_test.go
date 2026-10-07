package taskattach

import (
	"bytes"
	"io"
	"testing"

	"github.com/google/uuid"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestPutDedupsAndOpenReturnsTheBytes(t *testing.T) {
	store := BlobStore{Objects: testenv.Objects(t, uuid.NewString())}
	data := []byte("a scanned receipt")
	sha, size, err := store.Put(data)
	if err != nil || size != int64(len(data)) || !validSHA(sha) {
		t.Fatal(sha, size, err)
	}
	if again, _, err := store.Put(data); err != nil || again != sha {
		t.Fatal("not content-addressed", err)
	}
	f, n, err := store.Open(sha)
	if err != nil || n != size {
		t.Fatal(n, err)
	}
	got, err := io.ReadAll(f)
	f.Close()
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("bytes differ", err)
	}
	for _, bad := range []string{"../../etc/passwd", "ABC", sha[:63]} {
		if _, _, err := store.Open(bad); err == nil {
			t.Fatalf("accepted key %q", bad)
		}
	}
}
