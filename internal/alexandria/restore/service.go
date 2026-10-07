// Package restore implements authoritative whole-library replacement from a
// device-published snapshot, and adoption of the result by the other devices.
// Selectively ported from UltraBridge (internal/libraryrestore) under Apache-2.0.
//
// PostgreSQL adaptation: UltraBridge validated the snapshot into a scratch
// SQLite file, then copied it in. Here the snapshot's book bytes are uploaded
// to object storage first (content-addressed and harmless if unused), and every
// row is validated and ingested INSIDE the publication transaction, which holds
// the library generation FOR UPDATE. Every other library writer takes that row
// FOR SHARE first, so the transaction has the library to itself, and any
// invalid row rolls back the entire replacement. No scratch disk is used.
package restore

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jdkruzr/rhizome/server-go/assets"
	"github.com/jdkruzr/rhizome/server-go/registry"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/assetstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/reader"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/wire"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

const MaxSnapshotBytes int64 = 64 << 30

var ErrSnapshot = errors.New("invalid_library_snapshot")

// Manifest is manifest.json. The archive holds it, rows.jsonl, and complete
// original books at assets/<sha256>. Row metadata is never restamped.
type Manifest struct {
	Version   int    `json:"version"`
	Schema    string `json:"schema"`
	Publisher string `json:"publisher"`
	HighWater int64  `json:"high_water"`
}

type Service struct {
	DB      pg.DB
	Objects blob.Store
	// ReplaceDerived clears host-owned derived state (search indexes) in the
	// SAME transaction as the replacement. No external I/O.
	ReplaceDerived func(context.Context, *sql.Tx) error
}

type Baseline struct {
	Generation string `json:"generation"`
	Snapshot   string `json:"snapshot"`
	Publisher  string `json:"publisher"`
	HighWater  int64  `json:"high_water"`
	Cursor     int64  `json:"cursor"`
}

func baseline(ctx context.Context, q pg.Querier) (Baseline, error) {
	var b Baseline
	err := q.QueryRowContext(ctx, `SELECT b.generation,b.snapshot,b.publisher,b.high_water,b.cursor FROM sync_restore_baseline b
		JOIN sync_library_generation g ON g.id=1 AND b.generation=g.generation WHERE b.id=1`).Scan(&b.Generation, &b.Snapshot, &b.Publisher, &b.HighWater, &b.Cursor)
	return b, err
}

func (s Service) Baseline(ctx context.Context) (Baseline, error) { return baseline(ctx, s.DB) }

// Receipt is a read-only lost-response check with the publisher's own device
// key. It never grants a device key permission to initiate a replacement.
func (s Service) Receipt(ctx context.Context, publisher, id string) (Baseline, error) {
	if !assets.ValidID(id) {
		return Baseline{}, generation.ErrInvalid
	}
	var gen string
	if err := s.DB.QueryRowContext(ctx, `SELECT generation FROM sync_library_replacement WHERE request_id=$1 AND publisher=$2`, id, publisher).Scan(&gen); err != nil {
		return Baseline{}, err
	}
	b, err := s.Baseline(ctx)
	if err != nil {
		return Baseline{}, err
	}
	if b.Generation != gen {
		return Baseline{}, generation.ErrConflict
	}
	return b, nil
}

func validSite(site string) bool { return wire.IsULID(site) && site[0] <= '7' }

// book is one snapshot book already uploaded to object storage.
type book struct {
	id     string
	length int64
	chunks []string
}

func (s Service) Publish(ctx context.Context, r generation.Request) (Baseline, error) {
	if !assets.ValidID(r.ID) || !assets.ValidID(r.Expected) || !assets.ValidID(r.SnapshotHash) || !validSite(r.Publisher) {
		return Baseline{}, generation.ErrInvalid
	}
	// Resolve lost replies before reading any snapshot bytes.
	var oldExpected, oldHash, oldPublisher, oldGeneration string
	err := s.DB.QueryRowContext(ctx, `SELECT expected_generation,snapshot_hash,publisher,generation FROM sync_library_replacement WHERE request_id=$1`, r.ID).
		Scan(&oldExpected, &oldHash, &oldPublisher, &oldGeneration)
	if err == nil {
		b, e := s.Baseline(ctx)
		if e != nil || oldExpected != r.Expected || oldHash != r.SnapshotHash || oldPublisher != r.Publisher || b.Generation != oldGeneration {
			return Baseline{}, generation.ErrConflict
		}
		return b, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Baseline{}, err
	}
	active, err := generation.Current(ctx, s.DB)
	if err != nil {
		return Baseline{}, err
	}
	if active != r.Expected {
		return Baseline{}, generation.ErrConflict
	}
	store := assetstore.Store{DB: s.DB, Objects: s.Objects}
	info, digests, err := store.ReadyChunks(ctx, r.SnapshotHash)
	if err != nil {
		var ae *assets.Error
		if errors.As(err, &ae) && ae.Status < 500 {
			return Baseline{}, ErrSnapshot
		}
		return Baseline{}, err
	}
	if info.ByteLength > MaxSnapshotBytes {
		return Baseline{}, ErrSnapshot
	}
	z, err := zip.NewReader(assetstore.NewReader(ctx, s.Objects, info, digests), info.ByteLength)
	if err != nil {
		return Baseline{}, ErrSnapshot
	}
	files, manifest, err := open(z)
	if err != nil {
		return Baseline{}, err
	}
	if manifest.Publisher != r.Publisher {
		return Baseline{}, ErrSnapshot
	}
	// Upload books before taking the fence; nothing references them yet.
	var books []book
	for name, entry := range files {
		if !strings.HasPrefix(name, "assets/") {
			continue
		}
		b, err := upload(ctx, store, strings.TrimPrefix(name, "assets/"), entry)
		if err != nil {
			return Baseline{}, err
		}
		books = append(books, b)
	}
	_, err = generation.PublishWithGeneration(ctx, s.DB, r, func(ctx context.Context, tx *sql.Tx, next string) error {
		return s.replace(ctx, tx, next, r, manifest, files["rows.jsonl"], books)
	})
	if err != nil {
		return Baseline{}, err
	}
	return s.Baseline(ctx)
}

// open validates the archive layout and manifest.
func open(z *zip.Reader) (map[string]*zip.File, Manifest, error) {
	var m Manifest
	if len(z.File) < 2 || len(z.File) > 100000 {
		return nil, m, ErrSnapshot
	}
	files := map[string]*zip.File{}
	var total uint64
	for _, entry := range z.File {
		if files[entry.Name] != nil || entry.UncompressedSize64 > uint64(MaxSnapshotBytes)-total {
			return nil, m, ErrSnapshot
		}
		total += entry.UncompressedSize64
		if entry.Name != "manifest.json" && entry.Name != "rows.jsonl" && !(strings.HasPrefix(entry.Name, "assets/") && assets.ValidID(strings.TrimPrefix(entry.Name, "assets/"))) {
			return nil, m, ErrSnapshot
		}
		files[entry.Name] = entry
	}
	mf, rows := files["manifest.json"], files["rows.jsonl"]
	if mf == nil || rows == nil || mf.UncompressedSize64 > 4096 {
		return nil, m, ErrSnapshot
	}
	r, err := mf.Open()
	if err != nil {
		return nil, m, ErrSnapshot
	}
	defer r.Close()
	dec := json.NewDecoder(io.LimitReader(r, 4097))
	dec.DisallowUnknownFields()
	var w struct {
		Version   int    `json:"version"`
		Schema    string `json:"schema"`
		Publisher string `json:"publisher"`
		HighWater *int64 `json:"high_water"`
	}
	err = dec.Decode(&w)
	var extra any
	if err != nil || dec.Decode(&extra) != io.EOF || w.HighWater == nil {
		return nil, m, ErrSnapshot
	}
	m = Manifest{w.Version, w.Schema, w.Publisher, *w.HighWater}
	if m.Version != 1 || m.Schema != contract.CandidateCombined().SchemaHash() || !validSite(m.Publisher) || m.HighWater < 0 {
		return nil, m, ErrSnapshot
	}
	return files, m, nil
}

// upload streams one book entry into content-addressed chunk objects and
// verifies the whole-book digest equals its name.
func upload(ctx context.Context, store assetstore.Store, id string, entry *zip.File) (book, error) {
	b := book{id: id, length: int64(entry.UncompressedSize64)}
	r, err := entry.Open()
	if err != nil {
		return b, ErrSnapshot
	}
	defer r.Close()
	whole := sha256.New()
	buf := make([]byte, assets.ChunkBytes)
	var read int64
	for {
		n, err := io.ReadFull(r, buf)
		if n > 0 {
			chunk := buf[:n]
			whole.Write(chunk)
			digest := assets.Digest(chunk)
			if err := store.PutObject(ctx, digest, chunk); err != nil {
				return b, err
			}
			b.chunks = append(b.chunks, digest)
			read += int64(n)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			return b, ErrSnapshot
		}
	}
	if read != b.length || hex.EncodeToString(whole.Sum(nil)) != id {
		return b, ErrSnapshot
	}
	return b, nil
}

// replace runs inside the publication transaction holding the generation
// FOR UPDATE. Explicit allowlist: no settings, sources, credentials or tasks.
func (s Service) replace(ctx context.Context, tx *sql.Tx, next string, r generation.Request, m Manifest, rows *zip.File, books []book) error {
	if s.ReplaceDerived != nil {
		if err := s.ReplaceDerived(ctx, tx); err != nil {
			return err
		}
	}
	// The relay writer lock too, so the global lock order holds everywhere.
	var lastSeq int64
	if err := tx.QueryRowContext(ctx, `SELECT last_seq FROM sync_seq WHERE id=1 FOR UPDATE`).Scan(&lastSeq); err != nil {
		return err
	}
	if err := pg.XactLock(ctx, tx, pg.ReaderLock); err != nil {
		return err
	}
	var clear []string
	for _, def := range contract.CandidateCombined().Tables {
		clear = append(clear, "DELETE FROM fn_"+def.Name)
	}
	clear = append(clear, `DELETE FROM sync_ops`, `DELETE FROM reader_store_incoming`, `DELETE FROM reader_store_changes`,
		`UPDATE reader_store_change_cursor SET seq=0 WHERE id=1`, `DELETE FROM sync_cursors`)
	for _, q := range clear {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	in := ingest{tx: tx, manifest: m, seen: map[[32]byte]bool{}}
	f, err := rows.Open()
	if err != nil {
		return ErrSnapshot
	}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 64<<10), reader.MaxRowBytes)
	for scan.Scan() {
		if err = ctx.Err(); err != nil {
			break
		}
		if err = in.row(ctx, scan.Bytes()); err != nil {
			break
		}
	}
	if err == nil && scan.Err() != nil {
		err = ErrSnapshot
	}
	f.Close()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sync_seq SET last_seq=$1 WHERE id=1`, in.seq); err != nil {
		return err
	}
	// Materialize dependency-ordered reader rows through the normal domain gate.
	for sweep := 0; sweep < 16; sweep++ {
		progress := 0
		var after int64
		for {
			page, err := reader.DrainTx(ctx, tx, after, 32)
			if err != nil {
				return err
			}
			for _, v := range page.Records {
				if v.State != "pending" {
					progress++
				}
			}
			if after = page.Next; after == 0 {
				break
			}
		}
		if progress == 0 {
			break
		}
	}
	var unresolved int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM reader_store_incoming WHERE state<>'applied'`).Scan(&unresolved); err != nil {
		return err
	}
	if unresolved != 0 {
		return ErrSnapshot
	}
	// Included books must be the books the restored rows describe. Their rows
	// replace any existing state for those ids (also repairing an invalid one).
	for _, b := range books {
		var length int64
		err := tx.QueryRowContext(ctx, `SELECT byte_length FROM fn_reader_book WHERE asset_id=$1 LIMIT 1`, b.id).Scan(&length)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && length != b.length) {
			return ErrSnapshot
		}
		if err != nil {
			return err
		}
		for _, q := range []string{`DELETE FROM rhizome_asset_chunk WHERE asset_id=$1`, `DELETE FROM rhizome_asset WHERE asset_id=$1`} {
			if _, err := tx.ExecContext(ctx, q, b.id); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO rhizome_asset(asset_id,byte_length,chunk_bytes,state) VALUES($1,$2,$3,'ready')`, b.id, b.length, assets.ChunkBytes); err != nil {
			return err
		}
		for index, digest := range b.chunks {
			size := assets.ChunkBytes
			if rest := b.length - int64(index)*assets.ChunkBytes; rest < int64(size) {
				size = int(rest)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO rhizome_asset_chunk(asset_id,chunk_index,sha256,byte_length) VALUES($1,$2,$3,$4)`, b.id, index, digest, size); err != nil {
				return err
			}
		}
	}
	for _, q := range []string{
		`UPDATE sync_site SET last_hlc=GREATEST(last_hlc,COALESCE((SELECT MAX(wall_ts) FROM sync_ops),0)),
			last_op_seq=GREATEST(last_op_seq,COALESCE((SELECT MAX(op_seq) FROM sync_ops WHERE sync_ops.site_id=sync_site.site_id),0)) WHERE id=1`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sync_cursors(site_id,last_pull_seq,acked_op_seq,updated_at) VALUES($1,$2,$3,$4)`,
		r.Publisher, in.seq, m.HighWater, time.Now().UnixMilli()); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sync_restore_baseline(id,generation,snapshot,publisher,high_water,cursor) VALUES(1,$1,$2,$3,$4,$5)
		ON CONFLICT (id) DO UPDATE SET generation=EXCLUDED.generation, snapshot=EXCLUDED.snapshot, publisher=EXCLUDED.publisher,
		high_water=EXCLUDED.high_water, cursor=EXCLUDED.cursor`, next, r.SnapshotHash, r.Publisher, m.HighWater, in.seq)
	return err
}

type ingest struct {
	tx       *sql.Tx
	manifest Manifest
	seen     map[[32]byte]bool
	seq      int64
}

var combined = contract.CandidateCombined().ByName()
var readerTables = contract.Registry().ByName()

// row validates and ingests one snapshot row: writer rows straight into their
// mirror, reader rows into the inbox for the domain gate; both into the relay.
func (in *ingest) row(ctx context.Context, raw []byte) error {
	if !utf8.Valid(raw) {
		return ErrSnapshot
	}
	var op contract.WireOp
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&op); err != nil {
		return ErrSnapshot
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return ErrSnapshot
	}
	def, ok := combined[op.Table]
	if !ok || !validSite(op.SiteID) || op.OpTS < 0 || op.OpSeq <= 0 || len(op.Cols) != len(def.Columns) || op.PK == "" || len(op.PK) > 2048 || strings.ContainsRune(op.PK, 0) {
		return ErrSnapshot
	}
	if op.SiteID == in.manifest.Publisher && op.OpSeq > in.manifest.HighWater {
		return ErrSnapshot
	}
	_, isReader := readerTables[op.Table]
	if !isReader && !wire.IsULID(op.PK) {
		return ErrSnapshot
	}
	// Duplicate row winners mean the snapshot is ambiguous.
	key := sha256.Sum256([]byte(op.Table + "\x00" + op.PK))
	if in.seen[key] {
		return ErrSnapshot
	}
	in.seen[key] = true
	var prepared *reader.Prepared
	cols := []string{"id"}
	args := []any{op.PK}
	if isReader {
		if _, _, err := contract.DecodeJSON(raw); err != nil {
			return ErrSnapshot
		}
		p, err := reader.Prepare(op.SiteID, [][]byte{raw})
		if err != nil {
			return ErrSnapshot
		}
		prepared = p
	} else {
		for _, c := range def.Columns {
			v, exists := op.Cols[c.Name]
			if !exists {
				return ErrSnapshot
			}
			value, err := column(c, bytes.TrimSpace(v))
			if err != nil {
				return err
			}
			cols = append(cols, c.Name)
			args = append(args, value)
		}
	}
	in.seq++
	var err error
	if _, err = in.tx.ExecContext(ctx, `INSERT INTO sync_ops(seq,site_id,op_seq,table_name,pk,wall_ts,payload,applied_at) VALUES($1,$2,$3,$4,$5,$6,$7,0)`,
		in.seq, op.SiteID, op.OpSeq, op.Table, op.PK, op.OpTS, string(raw)); err != nil {
		if pg.UniqueViolation(err) {
			err = fmt.Errorf("%w: duplicate operation", ErrSnapshot)
		}
		return err
	}
	if isReader {
		if err = prepared.CommitTx(ctx, in.tx); err != nil {
			return ErrSnapshot
		}
	} else {
		cols = append(cols, "lww_wall_ts", "lww_op_seq", "lww_site_id")
		args = append(args, op.OpTS, op.OpSeq, op.SiteID)
		marks := make([]string, len(cols))
		for i := range marks {
			marks[i] = fmt.Sprintf("$%d", i+1)
		}
		if _, err = in.tx.ExecContext(ctx, "INSERT INTO fn_"+op.Table+"("+strings.Join(cols, ",")+") VALUES("+strings.Join(marks, ",")+")", args...); err != nil {
			return err
		}
	}
	return nil
}

// column decodes one writer value exactly as UltraBridge did.
func column(c registry.Column, v json.RawMessage) (any, error) {
	if bytes.Equal(v, []byte("null")) {
		if !c.Nullable {
			return nil, ErrSnapshot
		}
		return nil, nil
	}
	switch c.Type {
	case registry.Text, registry.Blob:
		var text string
		if json.Unmarshal(v, &text) != nil {
			return nil, ErrSnapshot
		}
		if c.Type == registry.Blob {
			b, err := base64.StdEncoding.Strict().DecodeString(text)
			if err != nil {
				return nil, ErrSnapshot
			}
			return b, nil
		}
		// PostgreSQL text cannot hold U+0000; the relay payload keeps it exactly.
		return strings.ReplaceAll(text, "\x00", "�"), nil
	case registry.Real:
		var n float64
		if json.Unmarshal(v, &n) != nil {
			return nil, ErrSnapshot
		}
		return n, nil
	default:
		var n int64
		if json.Unmarshal(v, &n) != nil {
			return nil, ErrSnapshot
		}
		return n, nil
	}
}

type Adoption struct {
	Generation string `json:"generation"`
	Snapshot   string `json:"snapshot"`
	Site       string `json:"site_id"`
	TokenHash  string `json:"token_hash"`
}

// Adopt is called only with an authenticated previous device key. The device
// must durably install the baseline and save its fresh key BEFORE this. Old
// keys stay read/adoption-only so a lost response can be retried safely.
func (s Service) Adopt(ctx context.Context, old string, a Adoption) (Baseline, error) {
	if !validSite(a.Site) || !assets.ValidID(a.TokenHash) || !assets.ValidID(a.Generation) || !assets.ValidID(a.Snapshot) {
		return Baseline{}, ErrSnapshot
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Baseline{}, err
	}
	defer tx.Rollback()
	// Same order as enrollment: identity lock, then the generation row.
	if err = pg.XactLock(ctx, tx, pg.IdentityLock); err != nil {
		return Baseline{}, err
	}
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&current); err != nil {
		return Baseline{}, err
	}
	b, err := baseline(ctx, tx)
	if err != nil {
		return Baseline{}, err
	}
	if b.Generation != a.Generation || b.Snapshot != a.Snapshot {
		return Baseline{}, generation.ErrConflict
	}
	var bound string
	var revoked bool
	if err = tx.QueryRowContext(ctx, `SELECT g.generation,i.revoked FROM sync_device_identity i JOIN sync_device_generation g USING(site_id) WHERE site_id=$1`, old).Scan(&bound, &revoked); err != nil {
		return Baseline{}, err
	}
	if revoked || bound == b.Generation {
		return Baseline{}, generation.ErrConflict
	}
	var site, hash string
	err = tx.QueryRowContext(ctx, `SELECT new_site,token_hash FROM sync_restore_adoption WHERE old_site=$1 AND generation=$2`, old, b.Generation).Scan(&site, &hash)
	if err == nil {
		if site != a.Site || hash != a.TokenHash {
			return Baseline{}, generation.ErrConflict
		}
		return b, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Baseline{}, err
	}
	checks := []struct {
		q    string
		args []any
	}{
		{`SELECT count(*) FROM sync_device_identity WHERE site_id=$1 OR token_hash=$2`, []any{a.Site, a.TokenHash}},
		{`SELECT count(*) FROM sync_retired_replica WHERE site_id=$1`, []any{a.Site}},
		{`SELECT count(*) FROM sync_site WHERE site_id=$1`, []any{a.Site}},
		{`SELECT count(*) FROM sync_ops WHERE site_id=$1`, []any{a.Site}},
	}
	for _, def := range contract.CandidateCombined().Tables {
		checks = append(checks, struct {
			q    string
			args []any
		}{"SELECT count(*) FROM fn_" + def.Name + " WHERE lww_site_id=$1", []any{a.Site}})
	}
	for _, c := range checks {
		var used int
		if err = tx.QueryRowContext(ctx, c.q, c.args...).Scan(&used); err != nil {
			return Baseline{}, err
		}
		if used != 0 {
			return Baseline{}, generation.ErrConflict
		}
	}
	now := time.Now().UnixMilli()
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO sync_device_identity(site_id,token_hash,created_at) VALUES($1,$2,$3)`, []any{a.Site, a.TokenHash, now}},
		{`INSERT INTO sync_device_generation(site_id,generation) VALUES($1,$2)`, []any{a.Site, b.Generation}},
		{`INSERT INTO sync_cursors(site_id,last_pull_seq,acked_op_seq,updated_at) VALUES($1,$2,0,$3)`, []any{a.Site, b.Cursor, now}},
		{`INSERT INTO sync_restore_adoption(old_site,generation,new_site,token_hash,snapshot) VALUES($1,$2,$3,$4,$5)`, []any{old, b.Generation, a.Site, a.TokenHash, b.Snapshot}},
	} {
		if _, err = tx.ExecContext(ctx, stmt.q, stmt.args...); err != nil {
			return Baseline{}, fmt.Errorf("adoption: %w", err)
		}
	}
	return b, tx.Commit()
}
