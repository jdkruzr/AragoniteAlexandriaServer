package boox

import (
	"bytes"
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
)

// publicationOwner holds generation before identity, matching publication and
// restore lock order. Operation-specific locks must be acquired after this.
func (s Service) publicationOwner(ctx context.Context, tx *sql.Tx) (string, error) {
	if e := notebookWriteLock(ctx, tx); e != nil {
		return "", e
	}
	var generation string
	if err := tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); err != nil {
		return "", err
	}
	var owner sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT native_uid FROM boox_identity WHERE id=1 FOR SHARE`).Scan(&owner); err != nil {
		return "", err
	}
	if owner.String != s.uid() {
		return "", errBookEnrollment
	}
	return generation, nil
}

type publicationAsset struct {
	SHA256, MD5, ContentType string
	Size                     int
	body                     []byte
}

// stagePublicationAsset prepares immutable bytes; binding writes them under
// the same library lock as reference creation and garbage collection.
func (s Service) stagePublicationAsset(ctx context.Context, raw []byte, contentType string) (publicationAsset, error) {
	md := md5.Sum(raw)
	asset := publicationAsset{SHA256: hash(raw), MD5: hex.EncodeToString(md[:]), ContentType: contentType, Size: len(raw), body: raw}
	return asset, nil
}

// bindPublicationAsset introduces a new native key. Collision/replacement
// decisions belong to the operation; this helper never overwrites a live key.
func (s Service) bindPublicationAsset(ctx context.Context, tx *sql.Tx, key string, asset publicationAsset) error {
	if _, err := s.Objects.Put(ctx, "boox/bodies/"+asset.SHA256, asset.ContentType, bytes.NewReader(asset.body), int64(asset.Size)); err != nil {
		return err
	}
	var version int64
	if err := tx.QueryRowContext(ctx, `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes,content_type) VALUES($1,$2,$3,$4,$5) RETURNING id`, key, asset.SHA256, asset.MD5, asset.Size, asset.ContentType).Scan(&version); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO boox_blob_live(native_key,version_id) VALUES($1,$2)`, key, version)
	return err
}
