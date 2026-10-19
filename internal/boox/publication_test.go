package boox

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

type fencedPublicationOwner struct {
	blob.Store
	db       pg.DB
	t        *testing.T
	attempts int
}

func (s *fencedPublicationOwner) Put(ctx context.Context, key, ct string, r io.Reader, size int64) (blob.Info, error) {
	info, err := s.Store.Put(ctx, key, ct, r, size)
	if err != nil {
		return info, err
	}
	// Object binding now holds ownership throughout PUT. Simulate a competing
	// owner change with a bounded wait instead of deadlocking an inline callback.
	attempt, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, e := s.db.ExecContext(attempt, `UPDATE boox_identity SET native_uid='different-owner' WHERE id=1`)
	if e == nil || attempt.Err() == nil {
		s.t.Fatal("owner change bypassed publication fence", e)
	}
	s.attempts++
	return info, nil
}
func TestPublicationFailureLeavesNoLiveBindings(t *testing.T) {
	for _, kind := range []string{"book", "text"} {
		t.Run(kind, func(t *testing.T) {
			s := testService(t)
			ctx := context.Background()
			if _, err := s.DB.ExecContext(ctx, `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); err != nil {
				t.Fatal(err)
			}
			s.Objects = lostPutAcknowledgment{s.Objects}
			var err error
			if kind == "book" {
				_, err = s.QueueBook(ctx, "synthetic.epub", testEPUB(t))
			} else {
				_, err = s.QueueTextNote(ctx, TextNote{Title: "Synthetic", Request: "publication-test"})
			}
			if err == nil {
				t.Fatal("publication succeeded after prerequisite failed")
			}
			var count int
			if err = s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM boox_operation)+(SELECT count(*) FROM boox_blob_live)`).Scan(&count); err != nil || count != 0 {
				t.Fatal("failed publication left metadata", count, err)
			}
		})
	}
}
func TestPublicationOwnerIsFencedDuringObjectBinding(t *testing.T) {
	for _, kind := range []string{"book", "text"} {
		t.Run(kind, func(t *testing.T) {
			s := testService(t)
			ctx := context.Background()
			uid := s.uid()
			if _, e := s.DB.ExecContext(ctx, `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, uid); e != nil {
				t.Fatal(e)
			}
			objects := &fencedPublicationOwner{Store: s.Objects, db: s.DB, t: t}
			s.Objects = objects
			var e error
			if kind == "book" {
				_, e = s.QueueBook(ctx, "synthetic.epub", testEPUB(t))
			} else {
				_, e = s.QueueTextNote(ctx, TextNote{Title: "Synthetic", Request: "fenced-publication"})
			}
			if e != nil || objects.attempts == 0 {
				t.Fatal(e, objects.attempts)
			}
			var actual string
			if e = s.DB.QueryRowContext(ctx, `SELECT native_uid FROM boox_identity WHERE id=1`).Scan(&actual); e != nil || actual != uid {
				t.Fatal(actual, e)
			}
			s.identity = uid
			if _, e = s.DB.ExecContext(ctx, `UPDATE boox_identity SET native_uid='different-owner' WHERE id=1`); e != nil {
				t.Fatal(e)
			}
			tx, e := s.DB.BeginTx(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback()
			if _, e = s.publicationOwner(ctx, tx); !errors.Is(e, errBookEnrollment) {
				t.Fatal("stale publication owner accepted", e)
			}
		})
	}
}
