// Package generation fences one author's shared library across deliberate
// whole-library replacement. A generation is not a user, tenant, or document ID.
// Selectively ported from UltraBridge (internal/syncgeneration) under Apache-2.0.
//
// PostgreSQL adaptation: SQLite serialized replacement against every write by
// its single writer. Here the generation row is the fence: mutations read it
// FOR SHARE inside their transaction, and Publish takes it FOR UPDATE first, so
// a request admitted before a restore waits, re-reads the new value and fails.
package generation

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

var (
	ErrReplaced = errors.New("library_replaced")
	ErrInvalid  = errors.New("invalid_library_replacement")
	ErrConflict = errors.New("library_replacement_conflict")
	ErrSchema   = errors.New("invalid_library_generation_schema")
)

const Header = "X-Alexandria-Library-Generation"

func digest(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func newGeneration() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

// Ensure seeds the first generation of a fresh library. It is plain DML (the
// runtime role may not run DDL) and is a no-op once a generation exists.
func Ensure(ctx context.Context, db pg.Querier) error {
	generation, err := newGeneration()
	if err != nil {
		return err
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO sync_library_generation(id,generation) VALUES(1,$1) ON CONFLICT (id) DO NOTHING`, generation); err != nil {
		return err
	}
	_, err = current(ctx, db, "")
	return err
}

// current reads the active generation; lock is "", "FOR SHARE" or "FOR UPDATE".
func current(ctx context.Context, q pg.Querier, lock string) (string, error) {
	var generation string
	if err := q.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 `+lock).Scan(&generation); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrSchema
		}
		return "", err
	}
	if !digest(generation) {
		return "", ErrSchema
	}
	return generation, nil
}

func Current(ctx context.Context, db pg.Querier) (string, error) { return current(ctx, db, "") }

// EnrollTx is only for a newly approved identity. Existing keys must use CheckTx;
// replaying an old enrollment request must never consent to adopting a restore.
func EnrollTx(ctx context.Context, tx *sql.Tx, site string) error {
	generation, err := current(ctx, tx, "FOR SHARE")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_device_generation(site_id,generation) VALUES($1,$2)`, site, generation)
	return err
}

type admission struct{ site, generation string }
type admissionKey struct{}

// Claim reports the admitted site and generation pinned to a request context.
func Claim(ctx context.Context) (site, generation string, ok bool) {
	claim, ok := ctx.Value(admissionKey{}).(admission)
	return claim.site, claim.generation, ok
}

// CheckRequestTx is the shared guard for host asset-store mutations.
func CheckRequestTx(ctx context.Context, tx *sql.Tx) error {
	claim, _ := ctx.Value(admissionKey{}).(admission)
	return CheckTx(ctx, tx, claim.site)
}

// Admit follows credential verification and pins its generation to the request.
// Reading this header/state is not permission to change the device's binding.
func Admit(ctx context.Context, db pg.Querier, site string) (context.Context, string, error) {
	var bound, active string
	err := db.QueryRowContext(ctx, `SELECT d.generation,g.generation FROM sync_device_generation d CROSS JOIN sync_library_generation g WHERE d.site_id=$1 AND g.id=1`, site).Scan(&bound, &active)
	if errors.Is(err, sql.ErrNoRows) {
		return ctx, "", ErrReplaced
	}
	if err != nil {
		return ctx, "", err
	}
	if !digest(active) || !digest(bound) {
		return ctx, "", ErrSchema
	}
	if bound != active {
		return ctx, active, ErrReplaced
	}
	return context.WithValue(ctx, admissionKey{}, admission{site, active}), active, nil
}

// CheckTx must execute inside the mutation's transaction, after taking any
// writer lock and BEFORE any mutation, ACK, cursor, receipt or relay. An
// admission made before a restore is not enough.
func CheckTx(ctx context.Context, tx *sql.Tx, site string) error {
	claim, claimed := ctx.Value(admissionKey{}).(admission)
	if !claimed || claim.site != site {
		return ErrReplaced
	}
	// FOR SHARE waits behind a concurrent Publish and then sees its successor.
	active, err := current(ctx, tx, "FOR SHARE")
	if err != nil {
		return err
	}
	var bound string
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM sync_device_generation WHERE site_id=$1`, site).Scan(&bound); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReplaced
		}
		return err
	}
	if claim.generation != active || bound != active {
		return ErrReplaced
	}
	// Credential revocation between HTTP admission and the transaction also
	// cannot sneak a late request into the replacement transaction's successor.
	var revoked bool
	if err = tx.QueryRowContext(ctx, `SELECT revoked FROM sync_device_identity WHERE site_id=$1`, site).Scan(&revoked); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReplaced
		}
		return err
	}
	if revoked {
		return ErrReplaced
	}
	return nil
}

// Request pins one explicit approval to a base generation and exact staged
// snapshot. The publisher must be a fresh local restore identity, never an old
// peer borrowing a current generation. Validation of that snapshot is the host's job.
type Request struct{ ID, Expected, SnapshotHash, Publisher string }
type Receipt struct {
	Generation string
	Replayed   bool
}

// Publish atomically couples the host's prevalidated replacement with the fence.
// replace must use only the supplied transaction. No-op retries do not run
// replace again. A superseded retry cannot roll back a later restore, even when
// a previous success response was lost.
func Publish(ctx context.Context, db pg.DB, r Request, replace func(context.Context, *sql.Tx) error) (Receipt, error) {
	if replace == nil {
		return Receipt{}, ErrInvalid
	}
	return PublishWithGeneration(ctx, db, r, func(c context.Context, t *sql.Tx, _ string) error { return replace(c, t) })
}

// PublishWithGeneration lets a host bind its baseline descriptor to the exact
// successor in the same transaction; there is never a separately committed pointer.
func PublishWithGeneration(ctx context.Context, db pg.DB, r Request, replace func(context.Context, *sql.Tx, string) error) (Receipt, error) {
	if !digest(r.ID) || !digest(r.Expected) || !digest(r.SnapshotHash) || r.Publisher == "" || len(r.Publisher) > 128 || replace == nil {
		return Receipt{}, ErrInvalid
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	// Take the fence exclusively first: every later mutation waits, then fails.
	active, err := current(ctx, tx, "FOR UPDATE")
	if err != nil {
		return Receipt{}, err
	}
	var old Request
	var next string
	err = tx.QueryRowContext(ctx, `SELECT expected_generation,snapshot_hash,publisher,generation FROM sync_library_replacement WHERE request_id=$1`, r.ID).Scan(&old.Expected, &old.SnapshotHash, &old.Publisher, &next)
	if err == nil {
		if old.Expected != r.Expected || old.SnapshotHash != r.SnapshotHash || old.Publisher != r.Publisher || active != next {
			return Receipt{}, ErrConflict
		}
		return Receipt{next, true}, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Receipt{}, err
	}
	if active != r.Expected {
		return Receipt{}, ErrConflict
	}
	// The host supplies prior admin approval and a validated staged snapshot;
	// this primitive additionally requires an active publisher binding.
	var bound string
	err = tx.QueryRowContext(ctx, `SELECT g.generation FROM sync_device_generation g JOIN sync_device_identity i USING(site_id) WHERE g.site_id=$1 AND NOT i.revoked`, r.Publisher).Scan(&bound)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Receipt{}, ErrInvalid
		}
		return Receipt{}, err
	}
	if bound != active {
		return Receipt{}, ErrReplaced
	}
	if next, err = newGeneration(); err != nil {
		return Receipt{}, err
	}
	if err = replace(ctx, tx, next); err != nil {
		return Receipt{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE sync_library_generation SET generation=$1 WHERE id=1 AND generation=$2`, next, r.Expected)
	if err != nil {
		return Receipt{}, err
	}
	if n, e := result.RowsAffected(); e != nil {
		return Receipt{}, e
	} else if n != 1 {
		return Receipt{}, ErrConflict
	}
	// Only the publishing replica already holds the approved snapshot. All other
	// replicas stay behind the old fence until whole-library adoption is complete.
	result, err = tx.ExecContext(ctx, `UPDATE sync_device_generation SET generation=$1 WHERE site_id=$2 AND generation=$3`, next, r.Publisher, r.Expected)
	if err != nil {
		return Receipt{}, err
	}
	if n, e := result.RowsAffected(); e != nil {
		return Receipt{}, e
	} else if n != 1 {
		return Receipt{}, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO sync_library_replacement(request_id,expected_generation,snapshot_hash,publisher,generation) VALUES($1,$2,$3,$4,$5)`,
		r.ID, r.Expected, r.SnapshotHash, r.Publisher, next); err != nil {
		return Receipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return Receipt{}, err
	}
	return Receipt{next, false}, nil
}
