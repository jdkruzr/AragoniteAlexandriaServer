package providers

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/embed"
	"math"
	"strconv"
	"strings"
	"time"
)

// BuildIndex explicitly authorizes a text-only rebuild. The old generation
// remains searchable until the replacement has no unfinished work.
func (s Store) BuildIndex(ctx context.Context, revision int64) error {
	if s.Bootstrap.Locked {
		return errors.New("Provider settings are locked.")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(716204821001)`); e != nil {
		return e
	}
	var rev int64
	var raw []byte
	if e = tx.QueryRowContext(ctx, `SELECT revision,config FROM alexandria_provider_settings WHERE singleton FOR SHARE`).Scan(&rev, &raw); e != nil {
		return e
	}
	if rev != revision {
		return ErrStale
	}
	var c Config
	if e = decodeConfig(raw, &c); e != nil {
		return e
	}
	if !c.Embedding.Enabled {
		return errors.New("Enable and save semantic search settings first.")
	}
	id := uuid.NewString()
	if _, e = tx.ExecContext(ctx, `UPDATE alexandria_embedding_generation SET state='retired' WHERE state='building'`); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO alexandria_embedding_generation(id,endpoint,model,state) VALUES($1,$2,$3,'building')`, id, c.Embedding.URL, c.Embedding.Model); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO alexandria_embedding_work(generation,note_key,page) SELECT $1,note_key,page FROM alexandria_note_content WHERE source='forestnote'`, id); e != nil {
		return e
	}
	if e = activate(ctx, tx, id); e != nil {
		return e
	}
	return tx.Commit()
}
func activate(ctx context.Context, tx *sql.Tx, id string) error {
	var done bool
	if e := tx.QueryRowContext(ctx, `SELECT state='building' AND NOT EXISTS(SELECT 1 FROM alexandria_embedding_work WHERE generation=$1) FROM alexandria_embedding_generation WHERE id=$1`, id).Scan(&done); e != nil {
		return e
	}
	if !done {
		return nil
	}
	if _, e := tx.ExecContext(ctx, `UPDATE alexandria_embedding_generation SET state='retired' WHERE state='active'`); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `UPDATE alexandria_embedding_generation SET state='active' WHERE id=$1`, id)
	return e
}
func (s Store) RetryIndex(ctx context.Context) error {
	if s.Bootstrap.Locked {
		return errors.New("Provider settings are locked.")
	}
	_, e := s.DB.ExecContext(ctx, `UPDATE alexandria_embedding_work w SET failed=false,attempts=0,next_at=now() FROM alexandria_embedding_generation g WHERE g.id=w.generation AND g.state IN ('active','building') AND w.failed`)
	return e
}
func (s Store) ProcessIndex(ctx context.Context) (bool, error) {
	var id, key, endpoint, model string
	var page, attempt int
	var version int64
	token := uuid.NewString()
	e := s.DB.QueryRowContext(ctx, `UPDATE alexandria_embedding_work w SET lease=$1,lease_until=now()+interval '5 minutes' WHERE (generation,note_key,page)=(SELECT w.generation,w.note_key,w.page FROM alexandria_embedding_work w JOIN alexandria_embedding_generation g ON g.id=w.generation WHERE g.state IN ('active','building') AND NOT w.failed AND w.next_at<=now() AND (w.lease_until IS NULL OR w.lease_until<now()) ORDER BY w.next_at,w.generation,w.note_key,w.page LIMIT 1 FOR UPDATE OF w SKIP LOCKED) RETURNING generation,note_key,page,version,attempts`, token).Scan(&id, &key, &page, &version, &attempt)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	fail := func() (bool, error) {
		_, e := s.DB.ExecContext(ctx, `UPDATE alexandria_embedding_work SET attempts=attempts+1,failed=attempts>=4,lease='',lease_until=NULL,next_at=now()+$5::interval WHERE generation=$1 AND note_key=$2 AND page=$3 AND lease=$4`, id, key, page, token, fmt.Sprintf("%d seconds", 30*(1<<min(attempt, 7))))
		return true, e
	}
	if e = s.DB.QueryRowContext(ctx, `SELECT endpoint,model FROM alexandria_embedding_generation WHERE id=$1`, id).Scan(&endpoint, &model); e != nil {
		return fail()
	}
	var body string
	e = s.DB.QueryRowContext(ctx, `SELECT body_text FROM alexandria_note_content WHERE note_key=$1 AND page=$2`, key, page).Scan(&body)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return fail()
	}
	if len(body) > 4<<20 {
		return fail()
	}
	hash := md5.Sum([]byte(body))
	digest := hex.EncodeToString(hash[:])
	var vectors [][]float32
	dim := 0
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	client := embed.NewOllama(endpoint, model)
	chunks := embed.ChunkText(body)
	if len(chunks) > 4096 {
		return fail()
	}
	for _, chunk := range chunks {
		v, e := client.Embed(callCtx, chunk)
		if e != nil || len(v) == 0 || len(v) > 16000 {
			return fail()
		}
		if dim != 0 && dim != len(v) {
			return fail()
		}
		dim = len(v)
		for _, f := range v {
			if math.IsNaN(float64(f)) || math.IsInf(float64(f), 0) {
				return fail()
			}
		}
		vectors = append(vectors, v)
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return true, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(716204821001)`); e != nil {
		return true, e
	}
	var valid bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alexandria_embedding_work w JOIN alexandria_embedding_generation g ON g.id=w.generation WHERE w.generation=$1 AND w.note_key=$2 AND w.page=$3 AND w.lease=$4 AND w.version=$5 AND g.state IN ('active','building'))`, id, key, page, token, version).Scan(&valid); e != nil {
		return true, e
	}
	if !valid {
		return true, nil
	}
	var current string
	e = tx.QueryRowContext(ctx, `SELECT md5(body_text) FROM alexandria_note_content WHERE note_key=$1 AND page=$2`, key, page).Scan(&current)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return true, e
	}
	if e == nil && current != digest {
		tx.Rollback()
		return fail()
	}
	var expected sql.NullInt64
	if e = tx.QueryRowContext(ctx, `SELECT dimensions FROM alexandria_embedding_generation WHERE id=$1`, id).Scan(&expected); e != nil {
		return true, e
	}
	if dim > 0 && expected.Valid && int(expected.Int64) != dim {
		tx.Rollback()
		return fail()
	}
	if dim > 0 && !expected.Valid {
		if _, e = tx.ExecContext(ctx, `UPDATE alexandria_embedding_generation SET dimensions=$2 WHERE id=$1`, id, dim); e != nil {
			return true, e
		}
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM alexandria_embedding_vectors WHERE generation=$1 AND note_key=$2 AND page=$3`, id, key, page); e != nil {
		return true, e
	}
	// A removed row must never gain a replacement vector.
	if current != "" {
		for i, v := range vectors {
			parts := make([]string, len(v))
			for j, f := range v {
				parts[j] = strconv.FormatFloat(float64(f), 'g', -1, 32)
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO alexandria_embedding_vectors(generation,note_key,page,chunk,body_hash,dimensions,embedding) VALUES($1,$2,$3,$4,$5,$6,$7::vector)`, id, key, page, i, digest, len(v), "["+strings.Join(parts, ",")+"]"); e != nil {
				return true, e
			}
		}
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM alexandria_embedding_work WHERE generation=$1 AND note_key=$2 AND page=$3`, id, key, page); e != nil {
		return true, e
	}
	if e = activate(ctx, tx, id); e != nil {
		return true, e
	}
	return true, tx.Commit()
}
