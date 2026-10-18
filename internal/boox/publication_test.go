package boox

import (
	"context"
	"io"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

type changedPublicationOwner struct {
	blob.Store
	db pg.DB
}

func (s changedPublicationOwner) Put(ctx context.Context, key, ct string, r io.Reader, size int64) (blob.Info, error) {
	info, err := s.Store.Put(ctx, key, ct, r, size)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE boox_identity SET native_uid='different-owner' WHERE id=1`)
	}
	return info, err
}
func TestPublicationFailureLeavesNoLiveBindings(t *testing.T) {
	for _, kind := range []string{"book", "text"} {
		for _, failure := range []string{"put", "owner"} {
			t.Run(kind+"/"+failure, func(t *testing.T) {
				s := testService(t)
				ctx := context.Background()
				if _, err := s.DB.ExecContext(ctx, `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); err != nil {
					t.Fatal(err)
				}
				if failure == "put" {
					s.Objects = lostPutAcknowledgment{s.Objects}
				} else {
					s.Objects = changedPublicationOwner{s.Objects, s.DB}
				}
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
}
