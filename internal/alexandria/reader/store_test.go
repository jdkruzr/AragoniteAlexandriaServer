package reader

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"reflect"
	"sync"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

// Ported from UltraBridge internal/readerstore tests. Install/schema-repair
// cases are gone: migrations own the schema here.

const siteA = "0000000000000000000000000A"
const siteB = "0000000000000000000000000B"
const bookID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var ctx = context.Background()

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func installed(t *testing.T) (*sql.DB, Store) {
	t.Helper()
	db := testenv.Database(t).DB
	db.SetMaxOpenConns(8)
	if err := generation.Ensure(ctx, db); err != nil {
		t.Fatal(err)
	}
	return db, Store{DB: db}
}
func raw(table, id string, seq int64, cols map[string]any) []byte {
	b, err := json.Marshal(map[string]any{"table": table, "pk": id, "site_id": siteA, "op_seq": seq, "op_ts": seq, "cols": cols})
	if err != nil {
		panic(err)
	}
	return b
}
func fixture() [][]byte {
	points := make([]byte, 40)
	for i, x := range []uint32{100, 200} {
		binary.LittleEndian.PutUint32(points[i*20:], x)
		binary.LittleEndian.PutUint32(points[i*20+4:], 200)
		binary.LittleEndian.PutUint32(points[i*20+8:], 1000)
		binary.LittleEndian.PutUint32(points[i*20+16:], x)
	}
	return [][]byte{
		raw("reader_book", bookID, 1, map[string]any{"asset_id": bookID, "byte_length": int64(9007199254740993), "media_type": "application/epub+zip", "metadata_json": `{ "version":1, "title":"Keep Me" }`}),
		raw("reader_annotation", "n", 2, map[string]any{"book_id": bookID, "initial_anchor_json": `{ "version":1,"section":0,"start":0,"end":4,"quote":"text","prefix":"","suffix":"" }`, "canvas_width": 10000, "initial_height": 1000, "creator_session_id": "creator"}),
		raw("reader_edit_session", "creator", 3, map[string]any{"annotation_id": "n", "kind": "interactive", "owner_site": siteA, "state": "open"}),
		raw("reader_stroke", "ink", 4, map[string]any{"annotation_id": "n", "session_id": "creator", "paint_order": 1, "paint_site": siteA, "color": uint32(0xff000000), "pen_width_min": 10, "pen_width_max": 20, "brush_kind": "ballpoint", "brush_version": 1, "brush_seed": 7, "points": base64.StdEncoding.EncodeToString(points), "point_dynamics": nil}),
		raw("reader_edit_session", "creator", 5, map[string]any{"annotation_id": "n", "kind": "interactive", "owner_site": siteA, "state": "finished"}),
	}
}
func prepare(t *testing.T, ops ...[]byte) *Prepared {
	t.Helper()
	p, err := Prepare(siteA, ops)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// stageTx receives rows the way the relay does: under the relay writer lock.
func stageTx(s Store, p *Prepared, extra func(*sql.Tx)) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var seq int64
	if err = tx.QueryRowContext(ctx, `SELECT last_seq FROM sync_seq WHERE id=1 FOR UPDATE`).Scan(&seq); err != nil {
		return err
	}
	if err = p.CommitTx(ctx, tx); err != nil {
		return err
	}
	if extra != nil {
		extra(tx)
		return nil
	}
	return tx.Commit()
}
func stage(t *testing.T, s Store, ops ...[]byte) {
	t.Helper()
	if err := stageTx(s, prepare(t, ops...), nil); err != nil {
		t.Fatal(err)
	}
}
func drain(t *testing.T, s Store) {
	t.Helper()
	for sweep := 0; sweep < 8; sweep++ {
		var after int64
		for {
			page, err := s.Drain(ctx, after, 2)
			if err != nil {
				t.Fatal(err)
			}
			after = page.Next
			if after == 0 {
				break
			}
		}
	}
}

func TestRestartDependenciesAndTerminalBeforeOpen(t *testing.T) {
	db, s := installed(t)
	ops := fixture()
	stage(t, s, ops[3], ops[4])
	drain(t, s)
	if count(t, db, "fn_reader_stroke") != 0 {
		t.Fatal("orphan stroke applied")
	}
	stage(t, s, ops[3], ops[4])
	stage(t, s, ops[2], ops[1], ops[0])
	drain(t, s)
	if count(t, db, "reader_store_incoming") != 5 || count(t, db, "fn_reader_stroke") != 1 {
		t.Fatal("lost/duplicated operations")
	}
	p, err := s.Projection(ctx, "n", DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != contract.Ready || p.InputHash == nil || *p.InputHash != "0460b7bc690c89dcdfd18560434dd62321358ad81d63c9b997336302c6c5efe4" {
		t.Fatalf("bad projection %+v", p)
	}
	var state string
	var seq int64
	if err := db.QueryRow("SELECT state,lww_op_seq FROM fn_reader_edit_session WHERE id='creator'").Scan(&state, &seq); err != nil || state != "finished" || seq != 5 {
		t.Fatal("terminal state reopened/restamped")
	}
	var length int64
	if err := db.QueryRow("SELECT byte_length FROM fn_reader_book").Scan(&length); err != nil || length != 9007199254740993 {
		t.Fatal("int64 rounded")
	}
	stage(t, s, raw("reader_annotation_lifecycle", "n", 6, map[string]any{"deleted": 1, "changed_at": 6}))
	drain(t, s)
	if p, err = s.Projection(ctx, "n", DefaultLimits()); err != nil || p.Status != contract.Deleted {
		t.Fatal("delete failed")
	}
	stage(t, s, raw("reader_annotation_lifecycle", "n", 7, map[string]any{"deleted": 0, "changed_at": 7}))
	drain(t, s)
	if p, err = s.Projection(ctx, "n", DefaultLimits()); err != nil || p.Status != contract.Ready || len(p.Strokes) != 1 {
		t.Fatal("restore lost ink")
	}
}

func TestAtomicReceiptAndIdentityReuse(t *testing.T) {
	db, s := installed(t)
	exec(t, db, "CREATE TABLE host_ack(value integer)")
	if err := stageTx(s, prepare(t, fixture()[0]), func(tx *sql.Tx) { tx.Exec("INSERT INTO host_ack VALUES(1)") }); err != nil {
		t.Fatal(err)
	}
	if count(t, db, "reader_store_incoming") != 0 || count(t, db, "host_ack") != 0 {
		t.Fatal("host rollback leaked receipt")
	}
	stage(t, s, fixture()[0])
	changed := raw("reader_book_title", bookID, 1, map[string]any{"title": "reuse"})
	if err := stageTx(s, prepare(t, fixture()[1], changed), nil); !errors.Is(err, ErrIdentityReused) {
		t.Fatal("operation identity reused", err)
	}
	if count(t, db, "reader_store_incoming") != 1 {
		t.Fatal("partial receipt committed")
	}
	if _, err := Prepare(siteB, fixture()); err == nil {
		t.Fatal("wrong verified site accepted")
	}
	if _, err := Prepare("", nil); err == nil {
		t.Fatal("missing verified identity accepted")
	}
}

func TestApplyFailureRollsBackMirrorAndInboxTogether(t *testing.T) {
	db, s := installed(t)
	stage(t, s, fixture()...)
	exec(t, db, `CREATE FUNCTION fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected'; END $$`)
	exec(t, db, `CREATE TRIGGER fail_status BEFORE UPDATE ON reader_store_incoming FOR EACH ROW EXECUTE FUNCTION fail()`)
	result, err := s.Drain(ctx, 0, 64)
	if err == nil || len(result.Changed) != 0 {
		t.Fatal("failure leaked success")
	}
	if count(t, db, "fn_reader_book") != 0 || count(t, db, "reader_store_changes") != 0 {
		t.Fatal("mirror or journal escaped rollback")
	}
	var pending int
	if err := db.QueryRow("SELECT count(*) FROM reader_store_incoming WHERE state='pending'").Scan(&pending); err != nil || pending != 5 {
		t.Fatal("inbox escaped rollback")
	}
	exec(t, db, "DROP TRIGGER fail_status ON reader_store_incoming")
	drain(t, s)
	if count(t, db, "fn_reader_stroke") != 1 {
		t.Fatal("retry failed")
	}
}

func TestMalformedAndConflictingRowsStayQuarantined(t *testing.T) {
	db, s := installed(t)
	stage(t, s, fixture()...)
	drain(t, s)
	stage(t, s, raw("reader_book_title", bookID, 6, map[string]any{"title": 42}), raw("reader_annotation", "n", 7, map[string]any{"book_id": bookID, "initial_anchor_json": `{"version":2}`, "canvas_width": 500, "initial_height": 20, "creator_session_id": "other"}))
	drain(t, s)
	var n int
	if err := db.QueryRow("SELECT count(*) FROM reader_store_incoming WHERE state='quarantined'").Scan(&n); err != nil || n != 2 || count(t, db, "fn_reader_book_title") != 0 {
		t.Fatal("invalid rows applied or disappeared")
	}
	if p, err := s.Projection(ctx, "n", DefaultLimits()); err != nil || p.CanvasWidth != 10000 {
		t.Fatal("immutable annotation overwritten")
	}
}

func TestSnapshotBudgetsAndPreparedPayloadOwnership(t *testing.T) {
	db, s := installed(t)
	ops := fixture()
	p := prepare(t, ops...)
	for _, b := range ops {
		for i := range b {
			b[i] = 'x'
		}
	}
	if err := stageTx(s, p, nil); err != nil {
		t.Fatal(err)
	}
	drain(t, s)
	for _, limits := range []Limits{{1, MaxBatchBytes}, {4096, 1}, {0, 1}, {16385, MaxBatchBytes}} {
		if snapshot, err := s.Snapshot(ctx, "n", limits); !errors.Is(err, ErrBudget) || snapshot != nil {
			t.Fatal("partial/oversized snapshot returned")
		}
	}
	if r, err := s.Snapshot(ctx, "missing", DefaultLimits()); err != nil || r != nil {
		t.Fatal("missing snapshot")
	}
	// A scalar length probe must refuse a corrupted oversized cell before reading it.
	exec(t, db, "UPDATE fn_reader_stroke SET points=decode(repeat('00', $1), 'hex')", MaxRowBytes+1)
	if r, err := s.Snapshot(ctx, "n", DefaultLimits()); !errors.Is(err, ErrBudget) || r != nil {
		t.Fatal("oversized row materialized")
	}
}

func TestConcurrentDuplicateReceiptAndDrain(t *testing.T) {
	db, s := installed(t)
	p := prepare(t, fixture()...)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := stageTx(s, p, nil); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				if _, err := s.Drain(ctx, 0, 64); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if count(t, db, "reader_store_incoming") != 5 || count(t, db, "fn_reader_stroke") != 1 {
		t.Fatal("concurrent replay duplicated rows")
	}
	var journal, distinct int
	if err := db.QueryRow(`SELECT count(*), count(DISTINCT (table_name, pk)) FROM reader_store_changes`).Scan(&journal, &distinct); err != nil || journal != 5 {
		t.Fatal("concurrent drains duplicated journal entries", journal, distinct, err)
	}
}

func TestReceiptLimitsAndQuarantinePreservesInvalidBytes(t *testing.T) {
	db, s := installed(t)
	if _, err := Prepare(siteA, make([][]byte, 501)); !errors.Is(err, ErrBudget) {
		t.Fatal("batch count unbounded")
	}
	if _, err := Prepare(siteA, [][]byte{make([]byte, MaxRowBytes+1)}); !errors.Is(err, ErrBudget) {
		t.Fatal("row bytes unbounded")
	}
	input := []byte(`{"table":"reader_book_title","pk":"book","site_id":"0000000000000000000000000A","op_seq":1,"op_ts":1,"cols":{"title":"\ud800"}}`)
	stage(t, s, input)
	var payload []byte
	if err := db.QueryRow("SELECT payload FROM reader_store_incoming").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(payload, input) {
		t.Fatal("invalid text was silently repaired")
	}
	// Raw invalid UTF-8 has no usable identity: the whole batch is refused.
	invalid := []byte("{\"table\":\"reader_book_title\",\"pk\":\"book\",\"site_id\":\"0000000000000000000000000A\",\"op_seq\":2,\"op_ts\":2,\"cols\":{\"title\":\"\xff\"}}")
	if _, err := Prepare(siteA, [][]byte{invalid}); err == nil {
		t.Fatal("raw invalid UTF-8 accepted")
	}
}

func TestMissingBookStaysPending(t *testing.T) {
	_, s := installed(t)
	ops := fixture()
	stage(t, s, ops[1:]...)
	drain(t, s)
	p, err := s.Projection(ctx, "n", DefaultLimits())
	if err != nil || p.Status != contract.Pending || p.InputHash != nil {
		t.Fatalf("missing book claimed ready: %+v %v", p, err)
	}
	stage(t, s, ops[0])
	drain(t, s)
	if p, err = s.Projection(ctx, "n", DefaultLimits()); err != nil || p.Status != contract.Ready {
		t.Fatal("metadata arrival did not resolve pending")
	}
}

func TestSweepAndContiguousJournalCompletion(t *testing.T) {
	db, s := installed(t)
	ops := fixture()
	// Children before parents: one sweep page cannot apply them, the next can.
	stage(t, s, ops[4], ops[3], ops[2], ops[1], ops[0])
	changed, err := s.Sweep(ctx, 2, 64)
	if err != nil || changed != 4 || count(t, db, "fn_reader_stroke") != 1 {
		t.Fatal("sweep did not converge", changed, err)
	}
	if pending, err := s.HasPending(ctx); err != nil || pending {
		t.Fatal("rows left pending", err)
	}
	changes, err := s.Changes(ctx, 128)
	if err != nil || len(changes) != 4 {
		t.Fatal(changes, err)
	}
	if err := s.CompleteChange(ctx, changes[1].Seq); err == nil {
		t.Fatal("completion skipped pending work")
	}
	for _, c := range changes {
		if err := s.CompleteChange(ctx, c.Seq); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CompleteChange(ctx, changes[0].Seq); err != nil {
		t.Fatal("replayed completion failed")
	}
	if rest, err := s.Changes(ctx, 128); err != nil || len(rest) != 0 {
		t.Fatal(rest, err)
	}
}

// PostgreSQL-specific: NUL in free text is stored as U+FFFD (text cannot hold
// it), while identifiers with NUL are rejected by the contract.
func TestNULInFreeTextDoesNotWedgeTheDrain(t *testing.T) {
	db, s := installed(t)
	stage(t, s, fixture()[0], raw("reader_book_title", bookID, 2, map[string]any{"title": "a\u0000b"}))
	drain(t, s)
	var title string
	if err := db.QueryRow(`SELECT title FROM fn_reader_book_title`).Scan(&title); err != nil || title != "a�b" {
		t.Fatalf("title %q %v", title, err)
	}
}

func TestKotlinRowsThroughPostgreSQL(t *testing.T) {
	path := os.Getenv("FORESTREAD_CONTRACT_VECTORS")
	if path == "" {
		t.Skip("use the Alexandria Stage 2 runner for current Kotlin storage vectors")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var vectors struct {
		Wire []struct {
			Name  string
			Op    json.RawMessage
			Valid bool
		}
	}
	if err = json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	inputs := [][]byte{}
	expected := map[Key]contract.Record{}
	for _, v := range vectors.Wire {
		op, r, err := contract.DecodeJSON(v.Op)
		if v.Name != op.Table {
			continue
		}
		if err != nil || !v.Valid {
			t.Fatal("invalid base vector")
		}
		inputs = append(inputs, v.Op)
		expected[Key{op.Table, op.PK}] = r
	}
	if len(expected) != 15 {
		t.Fatal("missing table coverage")
	}
	for seed := int64(0); seed < 4; seed++ {
		db, s := installed(t)
		order := append([][]byte{}, inputs...)
		rand.New(rand.NewSource(seed)).Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
		stage(t, s, order...)
		drain(t, s)
		b := budget{1024, MaxBatchBytes}
		for key, want := range expected {
			got, err := load(ctx, db, key.Table, key.ID, &b)
			if err != nil || got == nil || !reflect.DeepEqual(*got, want) {
				t.Fatalf("stored Kotlin row %s differs: %v", key.Table, err)
			}
		}
	}
}
