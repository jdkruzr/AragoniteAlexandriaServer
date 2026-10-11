package boox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// A successful historical upload is not evidence that a later deleted book
// has been restored. Replaying its idempotency key must not imply restoration.
func TestBookReuploadAfterRemoval(t *testing.T) {
	for _, removal := range []string{"metadata", "source"} {
		t.Run(removal, func(t *testing.T) {
			s := testService(t)
			ctx := context.Background()
			if _, err := s.DB.ExecContext(ctx, `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); err != nil {
				t.Fatal(err)
			}
			raw := testEPUB(t)
			id, err := s.QueueBook(ctx, "fixture.epub", raw)
			if err != nil {
				t.Fatal(err)
			}
			var doc, key string
			var body []byte
			if err = s.DB.QueryRowContext(ctx, `UPDATE boox_operation SET state='published' WHERE id=$1 RETURNING document_id,payload->>'nativeKey',payload->'metadata'`, id).Scan(&doc, &key, &body); err != nil {
				t.Fatal(err)
			}
			if removal == "metadata" {
				var metadata map[string]any
				if err = json.Unmarshal(body, &metadata); err != nil {
					t.Fatal(err)
				}
				metadata["status"] = 1
				body, err = json.Marshal(metadata)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'2-deleted','reading','',$2,$3)`, doc, s.uid(), body); err != nil {
				t.Fatal(err)
			}
			if removal == "source" {
				if _, err = s.DB.ExecContext(ctx, `DELETE FROM boox_blob_live WHERE native_key=$1`, key); err != nil {
					t.Fatal(err)
				}
			}
			replay, err := s.QueueBook(ctx, "fixture-again.epub", raw)
			if !errors.Is(err, errBookRemoved) || replay != "" {
				t.Fatalf("removed book accepted as successful replay: id=%q err=%v", replay, err)
			}
			var operations, versions int
			if err = s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM boox_operation),(SELECT count(*) FROM boox_blob_version)`).Scan(&operations, &versions); err != nil {
				t.Fatal(err)
			}
			if operations != 1 || versions != 1 {
				t.Fatalf("reupload mutated retained state: operations=%d versions=%d", operations, versions)
			}
		})
	}
}
