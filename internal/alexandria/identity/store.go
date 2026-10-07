// Package identity owns Alexandria device credentials, not Rhizome row state.
// Selectively ported from UltraBridge (internal/syncidentity) under Apache-2.0.
//
// PostgreSQL adaptation: SQLite serialized enrollment by taking its single
// writer; here a transaction-scoped advisory lock does the same, so two
// connections cannot both observe a free site or token hash and claim it.
package identity

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/wire"
)

const TokenPrefix = "fn-device-v1_"

var (
	ErrInvalid    = errors.New("invalid_device_credential")
	ErrConflict   = errors.New("device_binding_conflict")
	ErrAdoption   = errors.New("legacy_site_requires_explicit_adoption")
	ErrServerSite = errors.New("server_site_cannot_be_enrolled")
)

type Store struct{ DB pg.DB }

// Enrollment contains a HASH, never the credential. The client must durably save
// its random secret in private storage before requesting enrollment. Retrying
// this exact request is safe even if the first committed response was lost.
type Enrollment struct {
	SiteID      string `json:"site_id"`
	TokenHash   string `json:"token_hash"`
	AdoptLegacy bool   `json:"adopt_legacy"`
}

// EnsureSite seeds the server's own authoring identity once. Plain DML: the
// runtime role never runs DDL. The ULID survives restarts.
func EnsureSite(ctx context.Context, db pg.Querier) error {
	_, err := db.ExecContext(ctx, `INSERT INTO sync_site(id,site_id) VALUES(1,$1) ON CONFLICT (id) DO NOTHING`, wire.NewULID())
	return err
}

func validHash(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// Enroll is an ADMINISTRATIVE approval of an existing local author identity, not
// evidence of hardware identity. It never silently replaces an enrolled key or
// revives a revoked key. Lost-key recovery/clone reconciliation is a separate gate.
func (s Store) Enroll(ctx context.Context, e Enrollment) error {
	if !wire.IsULID(e.SiteID) || e.SiteID[0] > '7' || !validHash(e.TokenHash) {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = pg.XactLock(ctx, tx, pg.IdentityLock); err != nil {
		return err
	}
	var server string
	if err = tx.QueryRowContext(ctx, `SELECT site_id FROM sync_site WHERE id=1`).Scan(&server); err != nil {
		return err
	}
	if e.SiteID == server {
		return ErrServerSite
	}
	var retired bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sync_retired_replica WHERE site_id=$1)`, e.SiteID).Scan(&retired); err != nil {
		return err
	}
	if retired {
		return ErrConflict
	}
	var hash string
	var revoked bool
	err = tx.QueryRowContext(ctx, `SELECT token_hash,revoked FROM sync_device_identity WHERE site_id=$1`, e.SiteID).Scan(&hash, &revoked)
	if err == nil {
		if hash != e.TokenHash || revoked {
			return ErrConflict
		}
		// Enrollment retry proves possession, not consent to replace this device's
		// library after a restore. Never advance its generation here.
		var bound, current string
		if err = tx.QueryRowContext(ctx, `SELECT d.generation,g.generation FROM sync_device_generation d CROSS JOIN sync_library_generation g WHERE d.site_id=$1 AND g.id=1`, e.SiteID).Scan(&bound, &current); err != nil {
			return err
		}
		if bound != current {
			return generation.ErrReplaced
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var used bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sync_device_identity WHERE token_hash=$1)`, e.TokenHash).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrConflict
	}
	if !e.AdoptLegacy {
		known, err := KnownAuthor(ctx, tx, e.SiteID)
		if err != nil {
			return err
		}
		if known {
			return ErrAdoption
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_device_identity(site_id,token_hash,created_at) VALUES($1,$2,$3)`, e.SiteID, e.TokenHash, time.Now().UnixMilli()); err != nil {
		if pg.UniqueViolation(err) {
			return ErrConflict
		}
		return err
	}
	if err = generation.EnrollTx(ctx, tx, e.SiteID); err != nil {
		return err
	}
	return tx.Commit()
}

// KnownAuthor checks relay and surviving mirror provenance: pruning a cursor or
// compacting relay history must not turn a known author into a fresh enrollment.
// Tables are discovered from the catalog, so later migrations (relay, mirrors,
// reader inbox) are covered without changing this function.
func KnownAuthor(ctx context.Context, tx *sql.Tx, site string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND (
			(table_name IN ('sync_ops','sync_cursors','reader_store_incoming') AND column_name = 'site_id') OR
			(table_name LIKE 'fn\_%' AND column_name = 'lww_site_id'))
		ORDER BY table_name`)
	if err != nil {
		return false, err
	}
	type source struct{ table, column string }
	var sources []source
	for rows.Next() {
		var s source
		if err = rows.Scan(&s.table, &s.column); err != nil {
			rows.Close()
			return false, err
		}
		sources = append(sources, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	for _, s := range sources {
		var found bool
		query := `SELECT EXISTS(SELECT 1 FROM ` + pgx.Identifier{s.table}.Sanitize() + ` WHERE ` + pgx.Identifier{s.column}.Sanitize() + `=$1)`
		if err := tx.QueryRowContext(ctx, query, site).Scan(&found); err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}

// Resolve returns only the site bound in server storage. Labels, request fields,
// operator API tokens and account passwords never enter this path.
func (s Store) Resolve(ctx context.Context, token string) (string, error) {
	if !strings.HasPrefix(token, TokenPrefix) || !validHash(strings.TrimPrefix(token, TokenPrefix)) {
		return "", ErrInvalid
	}
	h := sha256.Sum256([]byte(token))
	var site string
	err := s.DB.QueryRowContext(ctx, `SELECT site_id FROM sync_device_identity WHERE token_hash=$1 AND NOT revoked`, hex.EncodeToString(h[:])).Scan(&site)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalid
	}
	return site, err
}

// Revoke only disables authentication. It preserves content, binding reservation,
// ACK/cursor/clock and all authored provenance. Already admitted requests may finish.
func (s Store) Revoke(ctx context.Context, site string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE sync_device_identity SET revoked=true WHERE site_id=$1`, site)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrInvalid
	}
	return nil
}
