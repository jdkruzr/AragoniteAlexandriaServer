// Package assetstore implements Rhizome's assets-v1 store over PostgreSQL
// metadata and S3-compatible chunk objects. It follows Rhizome's SQLStore
// (assets/sqlite.go, Apache-2.0) state machine exactly; only where bytes live
// differs. Rhizome's own assets.NewHandler serves it.
//
// Order of effects for a chunk: touch the object intent row, PUT the
// content-addressed object, then a short transaction inserts the chunk row.
// The row is the sole authority; a crash between the PUT and the row leaves an
// orphan object that GC removes, never an acknowledged chunk without bytes.
package assetstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/rhizome/server-go/assets"
)

// Namespace is the object-key namespace for chunk bytes inside a library.
const Namespace = "assets/chunks"

// Store satisfies assets.Store for one admitted library.
type Store struct {
	DB      pg.DB
	Objects blob.Store
	// BeforeWrite runs first inside every mutation transaction (the generation
	// fence). It must use only tx. Nil skips it.
	BeforeWrite func(context.Context, *sql.Tx) error
}

var _ assets.Store = Store{}

// ObjectKey is the S3 key for a chunk's bytes.
func ObjectKey(digest string) (string, error) { return blob.ContentKey(Namespace, digest) }

// storageError keeps PostgreSQL's disk-full condition distinguishable, as
// Rhizome does for SQLite's SQLITE_FULL; everything else stays a 503.
func storageError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "53100" || pgErr.Code == "53200") {
		return assets.Fail(507, "storage_full")
	}
	return err
}

func (s Store) mutate(ctx context.Context, work func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if s.BeforeWrite != nil {
		if err = s.BeforeWrite(ctx, tx); err != nil {
			return err
		}
	}
	if err = work(tx); err != nil {
		return storageError(err)
	}
	return storageError(tx.Commit())
}

func describe(ctx context.Context, q pg.Querier, id string, lock string) (assets.Info, int64, error) {
	var info assets.Info
	var generation int64
	if !assets.ValidID(id) {
		return info, 0, assets.Fail(400, "invalid_asset_id")
	}
	err := q.QueryRowContext(ctx, `SELECT asset_id,byte_length,chunk_bytes,state,generation FROM rhizome_asset WHERE asset_id=$1 `+lock, id).
		Scan(&info.ID, &info.ByteLength, &info.ChunkBytes, &info.State, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		err = assets.Fail(404, "asset_not_found")
	}
	return info, generation, err
}

func (s Store) Describe(ctx context.Context, id string) (assets.Info, error) {
	i, _, err := describe(ctx, s.DB, id, "")
	return i, err
}

func (s Store) Stage(ctx context.Context, d assets.Descriptor) (assets.Info, bool, error) {
	if err := d.Validate(); err != nil {
		return assets.Info{}, false, err
	}
	var info assets.Info
	var created bool
	err := s.mutate(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `INSERT INTO rhizome_asset(asset_id,byte_length,chunk_bytes,state) VALUES($1,$2,$3,'staging') ON CONFLICT (asset_id) DO NOTHING`,
			d.ID, d.ByteLength, d.ChunkBytes)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if info, _, err = describe(ctx, tx, d.ID, ""); err != nil {
			return err
		}
		if info.Descriptor != d {
			return assets.Fail(409, "descriptor_conflict")
		}
		created = n == 1
		return nil
	})
	if err != nil {
		return assets.Info{}, false, err
	}
	return info, created, nil
}

func (s Store) ListChunks(ctx context.Context, id string, start int64, limit int) (assets.Page, error) {
	i, err := s.Describe(ctx, id)
	if err != nil {
		return assets.Page{}, err
	}
	if start < 0 || start > i.ChunkCount() || limit < 1 || limit > assets.PageEntries {
		return assets.Page{}, assets.Fail(400, "invalid_page")
	}
	end := start + int64(limit)
	if end > i.ChunkCount() {
		end = i.ChunkCount()
	}
	p := assets.Page{Entries: make([]assets.Entry, 0, end-start)}
	rows, err := s.DB.QueryContext(ctx, `SELECT chunk_index,sha256 FROM rhizome_asset_chunk WHERE asset_id=$1 AND chunk_index>=$2 AND chunk_index<$3 ORDER BY chunk_index`, id, start, end)
	if err != nil {
		return assets.Page{}, err
	}
	defer rows.Close()
	present := map[int64]string{}
	for rows.Next() {
		var index int64
		var digest string
		if err := rows.Scan(&index, &digest); err != nil {
			return assets.Page{}, err
		}
		present[index] = digest
	}
	if err := rows.Err(); err != nil {
		return assets.Page{}, err
	}
	for index := start; index < end; index++ {
		length, _ := i.ChunkLength(index)
		e := assets.Entry{Index: index, ByteLength: length}
		if digest, ok := present[index]; ok {
			e.SHA256 = &digest
		}
		p.Entries = append(p.Entries, e)
	}
	if end < i.ChunkCount() {
		p.NextStart = &end
	}
	return p, nil
}

func (s Store) ReadChunk(ctx context.Context, id string, index int64) (assets.Chunk, error) {
	i, err := s.Describe(ctx, id)
	if err != nil {
		return assets.Chunk{}, err
	}
	length, err := i.ChunkLength(index)
	if err != nil {
		return assets.Chunk{}, err
	}
	if i.State != "ready" {
		return assets.Chunk{}, assets.Fail(409, "asset_not_ready")
	}
	c, err := s.readStoredChunk(ctx, id, index)
	if err != nil {
		return assets.Chunk{}, err
	}
	// A ready asset's bytes were verified at Complete; never serve a changed object.
	if len(c.Bytes) != length || assets.Digest(c.Bytes) != c.SHA256 {
		return assets.Chunk{}, assets.Fail(503, "asset_storage_corrupt")
	}
	return c, nil
}

// errObjectMissing marks a chunk row whose object is gone.
var errObjectMissing = errors.New("chunk object missing")

func (s Store) readStoredChunk(ctx context.Context, id string, index int64) (assets.Chunk, error) {
	var c assets.Chunk
	err := s.DB.QueryRowContext(ctx, `SELECT sha256 FROM rhizome_asset_chunk WHERE asset_id=$1 AND chunk_index=$2`, id, index).Scan(&c.SHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return c, assets.Fail(409, "missing_chunks")
	}
	if err != nil {
		return c, err
	}
	key, err := ObjectKey(c.SHA256)
	if err != nil {
		return c, err
	}
	body, _, err := s.Objects.Get(ctx, key)
	if errors.Is(err, blob.ErrNotFound) {
		return c, errObjectMissing
	}
	if err != nil {
		return c, err
	}
	defer body.Close()
	c.Bytes, err = io.ReadAll(io.LimitReader(body, assets.ChunkBytes+1))
	return c, err
}

func (s Store) WriteChunk(ctx context.Context, id string, index int64, b []byte, digest string) error {
	// Hash before any write; a rejected chunk never gets an object or a row.
	if len(b) > assets.ChunkBytes {
		return assets.Fail(413, "chunk_too_large")
	}
	if !assets.ValidID(digest) {
		return assets.Fail(400, "invalid_chunk_digest")
	}
	if assets.Digest(b) != digest {
		return assets.Fail(422, "chunk_hash_mismatch")
	}
	// Cheap precheck so a refused chunk does not cost an upload. The
	// authoritative checks repeat under the row lock below.
	i, err := s.Describe(ctx, id)
	if err != nil {
		return err
	}
	length, err := i.ChunkLength(index)
	if err != nil {
		return err
	}
	if len(b) != length {
		return assets.Fail(400, "invalid_chunk_length")
	}
	if err := s.putObject(ctx, digest, b); err != nil {
		return err
	}
	return s.mutate(ctx, func(tx *sql.Tx) error {
		i, _, err := describe(ctx, tx, id, "FOR UPDATE")
		if err != nil {
			return err
		}
		length, err := i.ChunkLength(index)
		if err != nil {
			return err
		}
		if len(b) != length {
			return assets.Fail(400, "invalid_chunk_length")
		}
		var old string
		err = tx.QueryRowContext(ctx, `SELECT sha256 FROM rhizome_asset_chunk WHERE asset_id=$1 AND chunk_index=$2`, id, index).Scan(&old)
		if err == nil {
			if old != digest {
				return assets.Fail(409, "chunk_conflict")
			}
			if i.State == "invalid" {
				return assets.Fail(409, "asset_invalid")
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if i.State != "staging" {
			return assets.Fail(409, "asset_not_staging")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO rhizome_asset_chunk(asset_id,chunk_index,sha256,byte_length) VALUES($1,$2,$3,$4)`, id, index, digest, len(b))
		return err
	})
}

// putObject records the upload intent (blocking while GC deletes the same
// object), then writes the content-addressed bytes.
func (s Store) putObject(ctx context.Context, digest string, b []byte) error {
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO rhizome_asset_object(sha256) VALUES($1)
		ON CONFLICT (sha256) DO UPDATE SET touched_at = now()`, digest); err != nil {
		return storageError(err)
	}
	key, err := ObjectKey(digest)
	if err != nil {
		return err
	}
	if _, err = s.Objects.Put(ctx, key, "application/octet-stream", bytes.NewReader(b), int64(len(b))); err != nil {
		return fmt.Errorf("store chunk: %w", err)
	}
	return nil
}

func (s Store) Complete(ctx context.Context, id string) (assets.Info, error) {
	// Verifying is durable; a later request after a crash restarts hashing. The
	// generation CAS stops an older verifier publishing after an invalid reset.
	if !assets.ValidID(id) {
		return assets.Info{}, assets.Fail(400, "invalid_asset_id")
	}
	if err := s.mutate(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE rhizome_asset SET state='verifying' WHERE asset_id=$1 AND state='staging'`, id)
		return err
	}); err != nil {
		return assets.Info{}, err
	}
	i, generation, err := describe(ctx, s.DB, id, "")
	if err != nil {
		return assets.Info{}, err
	}
	if i.State == "ready" {
		return i, nil
	}
	if i.State != "verifying" {
		return assets.Info{}, assets.Fail(409, "asset_invalid")
	}
	h := sha256.New()
	for index := int64(0); index < i.ChunkCount(); index++ {
		c, err := s.readStoredChunk(ctx, id, index)
		if errors.Is(err, errObjectMissing) {
			// The row promised bytes the object store lost: forget the row so the
			// client re-uploads it, exactly as for a chunk never received.
			if err := s.mutate(ctx, func(tx *sql.Tx) error {
				_, err := tx.ExecContext(ctx, `DELETE FROM rhizome_asset_chunk WHERE asset_id=$1 AND chunk_index=$2 AND sha256=$3`, id, index, c.SHA256)
				return err
			}); err != nil {
				return assets.Info{}, err
			}
			err = assets.Fail(409, "missing_chunks")
		}
		if err != nil {
			var ae *assets.Error
			if errors.As(err, &ae) && ae.Code == "missing_chunks" {
				if resetErr := s.mutate(ctx, func(tx *sql.Tx) error {
					_, err := tx.ExecContext(ctx, `UPDATE rhizome_asset SET state='staging' WHERE asset_id=$1 AND state='verifying' AND generation=$2`, id, generation)
					return err
				}); resetErr != nil {
					return assets.Info{}, resetErr
				}
			}
			return assets.Info{}, err
		}
		length, _ := i.ChunkLength(index)
		if len(c.Bytes) != length || assets.Digest(c.Bytes) != c.SHA256 {
			return s.finish(ctx, i, generation, "invalid")
		}
		_, _ = h.Write(c.Bytes)
	}
	if hex.EncodeToString(h.Sum(nil)) != id {
		return s.finish(ctx, i, generation, "invalid")
	}
	return s.finish(ctx, i, generation, "ready")
}

func (s Store) finish(ctx context.Context, i assets.Info, generation int64, state string) (assets.Info, error) {
	var n int64
	if err := s.mutate(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE rhizome_asset SET state=$1 WHERE asset_id=$2 AND state='verifying' AND generation=$3`, state, i.ID, generation)
		if err != nil {
			return err
		}
		n, err = r.RowsAffected()
		return err
	}); err != nil {
		return assets.Info{}, err
	}
	if n == 0 {
		current, err := s.Describe(ctx, i.ID)
		if err != nil {
			return assets.Info{}, err
		}
		if current.State == "ready" {
			return current, nil
		}
		return assets.Info{}, assets.Fail(409, "verification_changed")
	}
	if state == "invalid" {
		return assets.Info{}, assets.Fail(422, "asset_hash_mismatch")
	}
	i.State = state
	return i, nil
}

func (s Store) ResetInvalid(ctx context.Context, id string) error {
	if !assets.ValidID(id) {
		return assets.Fail(400, "invalid_asset_id")
	}
	return s.mutate(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, `UPDATE rhizome_asset SET state='staging',generation=generation+1 WHERE asset_id=$1 AND state='invalid'`, id)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return assets.Fail(409, "asset_not_invalid")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM rhizome_asset_chunk WHERE asset_id=$1`, id)
		return err
	})
}

// CollectGarbage deletes chunk objects that no chunk row references and whose
// last upload intent is older than grace. It returns how many it removed.
// Each deletion holds the intent row's lock across the object DELETE, so a
// concurrent upload of the same bytes waits and then re-creates the object.
func (s Store) CollectGarbage(ctx context.Context, grace time.Duration, limit int) (int, error) {
	if grace <= 0 || limit < 1 {
		return 0, errors.New("invalid garbage collection bounds")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT sha256 FROM rhizome_asset_object o
		WHERE touched_at < now() - $1::interval
		AND NOT EXISTS (SELECT 1 FROM rhizome_asset_chunk c WHERE c.sha256 = o.sha256)
		ORDER BY touched_at LIMIT $2`, fmt.Sprintf("%d milliseconds", grace.Milliseconds()), limit)
	if err != nil {
		return 0, err
	}
	var candidates []string
	for rows.Next() {
		var digest string
		if err := rows.Scan(&digest); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, digest)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, digest := range candidates {
		ok, err := s.collect(ctx, digest, grace)
		if err != nil {
			return removed, err
		}
		if ok {
			removed++
		}
	}
	return removed, nil
}

func (s Store) collect(ctx context.Context, digest string, grace time.Duration) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var locked string
	err = tx.QueryRowContext(ctx, `SELECT sha256 FROM rhizome_asset_object o WHERE sha256 = $1
		AND touched_at < now() - $2::interval
		AND NOT EXISTS (SELECT 1 FROM rhizome_asset_chunk c WHERE c.sha256 = o.sha256)
		FOR UPDATE`, digest, fmt.Sprintf("%d milliseconds", grace.Milliseconds())).Scan(&locked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil // touched or referenced meanwhile
	}
	if err != nil {
		return false, err
	}
	key, err := ObjectKey(digest)
	if err != nil {
		return false, err
	}
	if err := s.Objects.Delete(ctx, key); err != nil && !errors.Is(err, blob.ErrNotFound) {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM rhizome_asset_object WHERE sha256 = $1`, digest); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
