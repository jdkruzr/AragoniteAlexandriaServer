package boox

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

type lostPutAcknowledgment struct{ blob.Store }

func (s lostPutAcknowledgment) Put(ctx context.Context, key, ct string, r io.Reader, size int64) (blob.Info, error) {
	info, err := s.Store.Put(ctx, key, ct, r, size)
	if err != nil {
		return info, err
	}
	return info, errors.New("synthetic lost acknowledgment after durable object PUT")
}

func TestOSSFailureRecoveryPreservesDurability(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	c := getSTS(t, s, enrollTest(t, s, uuid.NewString()))
	key := nativeUID(s.LibraryID) + "/note/qualification/resource/data/synthetic.html"
	raw := []byte("synthetic original resource")
	underlying := s.Objects
	s.Objects = lostPutAcknowledgment{underlying}
	// Simulate the observed finite native retry count, without claiming this
	// Go HTTP harness executes the native retry scheduler.
	for attempt := 0; attempt < 9; attempt++ {
		if w := signed(t, s, c, "PUT", key, "", raw, false); w.Code != 503 {
			t.Fatalf("failed PUT acknowledged: %d", w.Code)
		}
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_blob_version WHERE native_key=$1`, key).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed upload published: count=%d err=%v", count, err)
	}
	s.Objects = underlying
	if w := signed(t, s, c, "GET", key, "", nil, false); w.Code != 404 {
		t.Fatalf("orphan object became visible: %d", w.Code)
	}
	if w := signed(t, s, c, "PUT", key, "", raw, false); w.Code != 200 {
		t.Fatalf("retry failed: %d", w.Code)
	}
	// Same-length corruption must fail both HEAD and GET, rather than allowing
	// clients to mark a broken resource complete using metadata alone.
	bad := bytes.Repeat([]byte("x"), len(raw))
	if _, err := underlying.Put(ctx, "boox/bodies/"+hash(raw), "application/octet-stream", bytes.NewReader(bad), int64(len(bad))); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"GET", "HEAD"} {
		if w := signed(t, s, c, method, key, "", nil, false); w.Code != 503 {
			t.Fatalf("%s accepted corruption: %d", method, w.Code)
		}
	}
	if _, err := underlying.Put(ctx, "boox/bodies/"+hash(raw), "application/octet-stream", bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	if w := signed(t, s, c, "GET", key, "", nil, false); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), raw) {
		t.Fatal("restored exact bytes not readable")
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_blob_version WHERE native_key=$1`, key).Scan(&count); err != nil || count != 1 {
		t.Fatalf("recovery altered history: count=%d err=%v", count, err)
	}
}
