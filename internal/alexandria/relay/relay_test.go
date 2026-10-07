package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"github.com/jdkruzr/rhizome/server-go/bounded"
)

// Ported from UltraBridge internal/syncstore, synchttp and readerlab tests.

const (
	siteA  = "0000000000000000000000000A"
	siteB  = "0000000000000000000000000B"
	nb1    = "00000000000000000000000NB1"
	bookID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

var ctx = context.Background()

func library(t *testing.T, sites ...string) *sql.DB {
	t.Helper()
	db := testenv.Database(t).DB
	if err := identity.EnsureSite(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := generation.Ensure(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, site := range sites {
		var key [32]byte
		_, _ = rand.Read(key[:])
		hash := sha256.Sum256(key[:])
		if err := (identity.Store{DB: db}).Enroll(ctx, identity.Enrollment{SiteID: site, TokenHash: hex.EncodeToString(hash[:])}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func admitted(t *testing.T, db *sql.DB, site string) context.Context {
	t.Helper()
	c, _, err := generation.Admit(ctx, db, site)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func op(site, table, pk string, seq int64, cols map[string]any) json.RawMessage {
	b, err := json.Marshal(map[string]any{"table": table, "pk": pk, "site_id": site, "op_seq": seq, "op_ts": seq, "cols": cols})
	if err != nil {
		panic(err)
	}
	return b
}
func notebookAt(site string, seq, ts int64, name any) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"table": "notebook", "pk": nb1, "site_id": site, "op_seq": seq, "op_ts": ts,
		"cols": map[string]any{"name": name, "sort_order": 0, "created_at": 1000, "deleted_at": nil, "folder_id": nil, "aspect_long_axis": nil, "page_width": nil, "page_height": nil}})
	return b
}
func notebook(site string, seq int64) json.RawMessage {
	return notebookAt(site, seq, seq, "Writer survives")
}
func title(site string, seq int64, value any) json.RawMessage {
	return op(site, "reader_book_title", bookID, seq, map[string]any{"title": value})
}
func request(site string, ops ...json.RawMessage) bounded.Request {
	return bounded.Request{ProtocolVersion: 1, SchemaHash: SchemaHash, SiteID: site, Ops: ops}
}

type result struct {
	code int
	body string
	resp bounded.Response
}

func post(t *testing.T, db *sql.DB, site string, req bounded.Request, headers map[string]string, want int) bounded.Response {
	t.Helper()
	r := send(t, db, admitted(t, db, site), site, req, headers)
	if r.code != want {
		t.Fatalf("HTTP %d want %d: %s", r.code, want, r.body)
	}
	return r.resp
}

func send(t testing.TB, db *sql.DB, c context.Context, site string, req bounded.Request, headers map[string]string) result {
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/sync/v1", bytes.NewReader(body)).WithContext(c)
	r.Header.Set(bounded.Header, "1")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	Store{DB: db}.Handler(site, nil).ServeHTTP(w, r)
	out := result{code: w.Code, body: w.Body.String()}
	if w.Code == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &out.resp); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func scalar(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func exec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatal(err)
	}
}
func state(t *testing.T, db *sql.DB) []int64 {
	t.Helper()
	var out []int64
	for _, q := range []string{"SELECT count(*) FROM reader_store_incoming", "SELECT count(*) FROM sync_ops", "SELECT last_seq FROM sync_seq",
		"SELECT last_hlc FROM sync_site", "SELECT count(*) FROM sync_cursors", "SELECT COALESCE(sum(acked_op_seq),0) FROM sync_cursors",
		"SELECT COALESCE(sum(last_pull_seq),0) FROM sync_cursors", "SELECT count(*) FROM fn_notebook"} {
		out = append(out, scalar(t, db, q))
	}
	return out
}
func rejected(r bounded.Response) []RejectedOp {
	var out []RejectedOp
	_ = json.Unmarshal(r.Rejected, &out)
	return out
}

func TestWriterMergeProvenanceLWWAndReplay(t *testing.T) {
	db := library(t, siteA)
	batch := request(siteA, notebookAt(siteA, 1, 1000, "Old"), notebookAt(siteA, 2, 2000, "New"))
	if r := post(t, db, siteA, batch, nil, 200); r.AcceptedThrough != 2 {
		t.Fatalf("accepted %d", r.AcceptedThrough)
	}
	var name string
	var lww int64
	if err := db.QueryRow(`SELECT name, lww_op_seq FROM fn_notebook WHERE id=$1`, nb1).Scan(&name, &lww); err != nil || name != "New" || lww != 2 {
		t.Fatalf("mirror %q/%d %v", name, lww, err)
	}
	if r := post(t, db, siteA, batch, nil, 200); r.AcceptedThrough != 2 || scalar(t, db, `SELECT count(*) FROM sync_ops`) != 2 {
		t.Fatal("replay was not idempotent")
	}
	// An older op still relays but never wins.
	post(t, db, siteA, request(siteA, notebookAt(siteA, 3, 500, "Stale")), nil, 200)
	if err := db.QueryRow(`SELECT name FROM fn_notebook WHERE id=$1`, nb1).Scan(&name); err != nil || name != "New" {
		t.Fatalf("stale op won: %q", name)
	}
	if scalar(t, db, `SELECT count(*) FROM sync_ops`) != 3 {
		t.Fatal("losing op not relayed")
	}
}

func TestRejectionsDoNotWedgeAndGapsCapTheWater(t *testing.T) {
	db := library(t, siteA)
	missing := op(siteA, "notebook", nb1, 3, map[string]any{"name": "x"})
	r := post(t, db, siteA, request(siteA,
		op(siteA, "bogus", nb1, 1, map[string]any{}),
		op(siteA, "notebook", "short", 2, map[string]any{}),
		missing,
		notebookAt(siteA, 4, 4, 42), // wrong value type in a winning op
		notebook(siteA, 5),
		notebook(siteA, 7), // 6 never sent
	), nil, 200)
	if len(rejected(r)) != 4 || r.AcceptedThrough != 5 {
		t.Fatalf("rejected %+v accepted %d", rejected(r), r.AcceptedThrough)
	}
	// Persisted across calls: sending 6 alone advances past the stored 7.
	if r = post(t, db, siteA, request(siteA, notebook(siteA, 6)), nil, 200); r.AcceptedThrough != 7 {
		t.Fatalf("accepted %d", r.AcceptedThrough)
	}
}

func TestRelayExcludesSelfAndRespectsByteAndCountCaps(t *testing.T) {
	db := library(t, siteA, siteB)
	for start := int64(1); start <= 501; start += 250 {
		var ops []json.RawMessage
		for i := start; i < start+250 && i <= 501; i++ {
			ops = append(ops, notebookAt(siteB, i, i, "text <&> 漢字"))
		}
		post(t, db, siteB, request(siteB, ops...), nil, 200)
	}
	if r := post(t, db, siteB, request(siteB), nil, 200); len(r.Ops) != 0 {
		t.Fatal("device received its own ops")
	}
	r := post(t, db, siteA, request(siteA), nil, 200)
	if len(r.Ops) != 500 || !r.HasMore || r.Cursor != 500 {
		t.Fatal(len(r.Ops), r.Cursor, r.HasMore)
	}
	res := send(t, db, admitted(t, db, siteA), siteA, request(siteA), map[string]string{"X-Rhizome-Max-Response-Bytes": "1024", "X-Rhizome-Max-Row-Bytes": "1024"})
	if res.code != 200 || len(res.body) > 1024 || len(res.resp.Ops) == 0 || len(res.resp.Ops) >= 500 || !res.resp.HasMore {
		t.Fatal(res.code, len(res.body), len(res.resp.Ops))
	}
	req := request(siteA)
	req.Cursor = 500
	if r = post(t, db, siteA, req, map[string]string{"X-Rhizome-Max-Response-Bytes": "1024"}, 200); len(r.Ops) != 1 || r.HasMore || r.Cursor != 501 {
		t.Fatal(r)
	}
}

func TestOversizedPullAndInvalidBudgetsNeverCommit(t *testing.T) {
	db := library(t, siteA, siteB)
	post(t, db, siteB, request(siteB, notebookAt(siteB, 1, 1, strings.Repeat("漢字🙂", 300))), nil, 200)
	before := state(t, db)
	res := send(t, db, admitted(t, db, siteA), siteA, request(siteA, notebook(siteA, 1)), map[string]string{"X-Rhizome-Max-Response-Bytes": "1024", "X-Rhizome-Max-Row-Bytes": "512"})
	if res.code != 413 || !strings.Contains(res.body, "oversized_op") {
		t.Fatal(res.code, res.body)
	}
	if !reflect.DeepEqual(before, state(t, db)) {
		t.Fatal("413 changed durable state")
	}
	for _, limit := range []string{"-1", "0", "1", "x", "999999999999999999999"} {
		if res := send(t, db, admitted(t, db, siteA), siteA, request(siteA, notebook(siteA, 1)), map[string]string{"X-Rhizome-Max-Response-Bytes": limit}); res.code != 400 {
			t.Fatal(limit, res.code)
		}
	}
	if !reflect.DeepEqual(before, state(t, db)) {
		t.Fatal("bad budget changed state")
	}
	r := httptest.NewRequest("POST", "/sync/v1", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	Store{DB: db}.Handler(siteA, nil).ServeHTTP(w, r.WithContext(admitted(t, db, siteA)))
	if w.Code != 409 {
		t.Fatalf("unbounded request: %d", w.Code)
	}
}

func TestMixedGapRejectedReceiptAndLosslessRelay(t *testing.T) {
	db := library(t, siteA, siteB)
	book := op(siteA, "reader_book", bookID, 1, map[string]any{"asset_id": bookID, "byte_length": int64(9007199254740993), "media_type": "application/epub+zip", "metadata_json": `{ "version":1,"title":"No Pancakes" }`})
	// First contact cannot reseed its ACK from this batch's inserts. An invalid
	// reader row above a missing writer op stays durably rejected.
	r := post(t, db, siteA, request(siteA, book, title(siteA, 3, 42), title(siteA, 4, "New title")), nil, 200)
	if r.AcceptedThrough != 1 || len(rejected(r)) != 1 {
		t.Fatalf("wrong gap ACK: %+v", r)
	}
	if r = post(t, db, siteA, request(siteA, notebook(siteA, 2)), nil, 200); r.AcceptedThrough != 4 {
		t.Fatalf("durable rejection wedged ACK: %+v", r)
	}
	post(t, db, siteA, request(siteA, book, title(siteA, 3, 42), notebook(siteA, 2)), nil, 200)
	if scalar(t, db, "SELECT count(*) FROM sync_ops") != 3 || scalar(t, db, "SELECT count(*) FROM reader_store_incoming") != 3 {
		t.Fatal("retry duplicated receipts/relay")
	}
	r = post(t, db, siteB, request(siteB), nil, 200)
	if len(r.Ops) != 3 {
		t.Fatalf("wrong relay: %+v", r)
	}
	var exact bool
	for _, raw := range r.Ops {
		if bytes.Contains(raw, []byte(`"byte_length":9007199254740993`)) && bytes.Contains(raw, []byte(`{ \"version\":1`)) {
			exact = true
		}
	}
	if !exact {
		t.Fatal("reader payload rounded or selector string rewritten")
	}
}

func TestBoundedFailureRollsBackAllParticipants(t *testing.T) {
	db := library(t, siteA, siteB)
	post(t, db, siteB, request(siteB, title(siteB, 1, strings.Repeat("large", 200))), nil, 200)
	before := state(t, db)
	req := request(siteA, title(siteA, 1, "reader"), notebook(siteA, 2))
	if res := send(t, db, admitted(t, db, siteA), siteA, req, map[string]string{"X-Rhizome-Max-Response-Bytes": "512"}); res.code != 413 {
		t.Fatal(res.code, res.body)
	}
	if !reflect.DeepEqual(before, state(t, db)) {
		t.Fatal("413 committed receipt/relay/writer/clock/cursor/ACK")
	}
	post(t, db, siteA, req, nil, 200)
	before = state(t, db)
	var invalid []json.RawMessage
	for seq := int64(3); seq < 12; seq++ {
		invalid = append(invalid, title(siteA, seq, 42))
	}
	if res := send(t, db, admitted(t, db, siteA), siteA, request(siteA, invalid...), map[string]string{"X-Rhizome-Max-Response-Bytes": "256"}); res.code != 413 {
		t.Fatal(res.code, res.body)
	}
	if !reflect.DeepEqual(before, state(t, db)) {
		t.Fatal("oversized rejection envelope committed state")
	}
}

func TestInjectedCommitFailuresAndIdentityReuse(t *testing.T) {
	for _, table := range []string{"reader_store_incoming", "sync_ops", "sync_cursors"} {
		t.Run(table, func(t *testing.T) {
			db := library(t, siteA)
			before := state(t, db)
			exec(t, db, `CREATE FUNCTION fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END $$`)
			exec(t, db, "CREATE TRIGGER fail BEFORE INSERT ON "+table+" FOR EACH ROW EXECUTE FUNCTION fail()")
			req := request(siteA, title(siteA, 1, "original"), notebook(siteA, 2))
			res := send(t, db, admitted(t, db, siteA), siteA, req, nil)
			if res.code != 503 || strings.Contains(res.body, "fixture") {
				t.Fatal(res.code, res.body)
			}
			if !reflect.DeepEqual(before, state(t, db)) {
				t.Fatal("failed transaction changed state")
			}
			exec(t, db, "DROP TRIGGER fail ON "+table)
			post(t, db, siteA, req, nil, 200)
			before = state(t, db)
			for _, bad := range []bounded.Request{request(siteA, title(siteA, 1, "changed")), request(siteA, notebook(siteA, 1)),
				request(siteA, title(siteA, 2, "writer collision")), request(siteA, title(siteA, 3, "x"), notebook(siteA, 3))} {
				post(t, db, siteA, bad, nil, 409)
				if !reflect.DeepEqual(before, state(t, db)) {
					t.Fatal("identity conflict changed state")
				}
			}
		})
	}
}

func TestCredentialBindingAndSchemaGate(t *testing.T) {
	db := library(t, siteA, siteB)
	before := state(t, db)
	post(t, db, siteA, request(siteB), nil, 403)
	post(t, db, siteA, request(siteA, title(siteB, 1, "forged")), nil, 403)
	post(t, db, siteA, request(siteA, notebook(siteB, 1)), nil, 403)
	req := request(siteA)
	req.SchemaHash = strings.Repeat("0", 64)
	post(t, db, siteA, req, nil, 409)
	// Without a generation admission the transaction refuses to start.
	if res := send(t, db, ctx, siteA, request(siteA), nil); res.code != 409 || !strings.Contains(res.body, "library_replaced") {
		t.Fatal(res.code, res.body)
	}
	if !reflect.DeepEqual(before, state(t, db)) {
		t.Fatal("unauthorized request changed state")
	}
	post(t, db, siteA, request(siteA, title(siteA, 1, 42)), nil, 200)
	before = state(t, db)
	// Shape-invalid rows have no relay entry; their durable identity still
	// cannot be stolen by a writer row or repaired in place under the same seq.
	post(t, db, siteA, request(siteA, notebook(siteA, 1)), nil, 409)
	post(t, db, siteA, request(siteA, title(siteA, 1, "changed")), nil, 409)
	if !reflect.DeepEqual(before, state(t, db)) {
		t.Fatal("quarantine identity overwritten")
	}
}

func TestRevocationAfterAdmissionFailsInsideTransaction(t *testing.T) {
	db := library(t, siteA)
	c := admitted(t, db, siteA)
	if err := (identity.Store{DB: db}).Revoke(ctx, siteA); err != nil {
		t.Fatal(err)
	}
	if res := send(t, db, c, siteA, request(siteA, notebook(siteA, 1)), nil); res.code != 409 {
		t.Fatal(res.code, res.body)
	}
	if scalar(t, db, `SELECT count(*) FROM sync_ops`) != 0 {
		t.Fatal("revoked device wrote")
	}
}

// PostgreSQL-specific: a request admitted before a restore waits behind the
// publish's generation lock, then fails instead of writing into the successor.
func TestExchangeWaitsBehindReplacementThenFails(t *testing.T) {
	db := library(t, siteA)
	c := admitted(t, db, siteA)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT generation FROM sync_library_generation WHERE id=1 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	done := make(chan result, 1)
	go func() { done <- send(t, db, c, siteA, request(siteA, notebook(siteA, 1)), nil) }()
	select {
	case r := <-done:
		t.Fatalf("exchange did not wait: %d", r.code)
	case <-time.After(300 * time.Millisecond):
	}
	if _, err := tx.Exec(`UPDATE sync_library_generation SET generation=$1 WHERE id=1`, strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.code != 409 || !strings.Contains(r.body, "library_replaced") {
		t.Fatal(r.code, r.body)
	}
	if scalar(t, db, `SELECT count(*) FROM sync_ops`) != 0 {
		t.Fatal("stale request wrote into the successor")
	}
}

// PostgreSQL-specific: concurrent devices on separate connections never leave
// a relay gap, so a puller's cursor can never skip a later-committed seq.
func TestConcurrentDevicesProduceGaplessRelay(t *testing.T) {
	sites := []string{siteA, siteB, "0000000000000000000000000C", "0000000000000000000000000D"}
	db := library(t, sites...)
	db.SetMaxOpenConns(8)
	const rounds = 15
	var wg sync.WaitGroup
	errs := make(chan string, len(sites)*rounds)
	for _, site := range sites {
		wg.Add(1)
		go func(site string) {
			defer wg.Done()
			var cursor int64
			for i := int64(1); i <= rounds; i++ {
				req := request(site, op(site, "notebook", fmt.Sprintf("%023d%s%02d", 0, site[25:], i), i,
					map[string]any{"name": "n", "sort_order": 0, "created_at": 1, "deleted_at": nil, "folder_id": nil, "aspect_long_axis": nil, "page_width": nil, "page_height": nil}))
				req.Cursor = cursor
				c, _, err := generation.Admit(ctx, db, site)
				if err != nil {
					errs <- err.Error()
					return
				}
				res := send(t, db, c, site, req, nil)
				if res.code != 200 || res.resp.AcceptedThrough != i {
					errs <- fmt.Sprintf("%s round %d: %d %s", site, i, res.code, res.body)
					return
				}
				cursor = res.resp.Cursor
			}
		}(site)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	total := int64(len(sites) * rounds)
	if scalar(t, db, `SELECT count(*) FROM sync_ops`) != total || scalar(t, db, `SELECT max(seq) FROM sync_ops`) != total || scalar(t, db, `SELECT last_seq FROM sync_seq`) != total {
		t.Fatal("relay sequence has gaps")
	}
	var all []json.RawMessage
	req := request(siteA)
	for {
		r := post(t, db, siteA, req, nil, 200)
		all = append(all, r.Ops...)
		req.Cursor = r.Cursor
		if !r.HasMore {
			break
		}
	}
	if int64(len(all)) != total-rounds {
		t.Fatalf("pulled %d of %d", len(all), total-rounds)
	}
}

func TestNULInTextIsSanitizedInMirrorButRelayedExactly(t *testing.T) {
	db := library(t, siteA, siteB)
	post(t, db, siteA, request(siteA, notebookAt(siteA, 1, 1, "a\x00b")), nil, 200)
	var name string
	if err := db.QueryRow(`SELECT name FROM fn_notebook WHERE id=$1`, nb1).Scan(&name); err != nil || name != "a�b" {
		t.Fatalf("mirror %q %v", name, err)
	}
	r := post(t, db, siteB, request(siteB), nil, 200)
	if len(r.Ops) != 1 || !bytes.Contains(r.Ops[0], []byte(`a\u0000b`)) {
		t.Fatalf("relay changed the value: %s", r.Ops)
	}
}

func TestAuthorOpsSortAfterSkewedDeviceAndRelay(t *testing.T) {
	db := library(t, siteA)
	s := Store{DB: db}
	future := time.Now().UnixMilli() + 1_000_000_000
	post(t, db, siteA, request(siteA, notebookAt(siteA, 1, future, "skewed")), nil, 200)
	authored := Op{Table: "notebook", PK: nb1, Cols: map[string]any{"name": "server", "sort_order": float64(0), "created_at": float64(1000),
		"deleted_at": nil, "folder_id": nil, "aspect_long_axis": nil}}
	for i := 0; i < 2; i++ {
		if _, err := s.AuthorOps(ctx, []Op{authored}); err != nil {
			t.Fatal(err)
		}
	}
	server, _ := s.SiteID(ctx)
	if scalar(t, db, `SELECT min(wall_ts) FROM sync_ops WHERE site_id=$1`, server) <= future {
		t.Fatal("authored op_ts not after skewed device op")
	}
	if scalar(t, db, `SELECT max(op_seq) FROM sync_ops WHERE site_id=$1`, server) != 2 {
		t.Fatal("authored op_seq not monotonic")
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM fn_notebook WHERE id=$1`, nb1).Scan(&name); err != nil || name != "server" {
		t.Fatalf("authored op lost: %q", name)
	}
	r := post(t, db, siteA, request(siteA), nil, 200)
	if len(r.Ops) != 2 {
		t.Fatalf("device did not receive server ops: %d", len(r.Ops))
	}
	if _, err := s.AuthorOps(ctx, []Op{{Table: "notebook", PK: nb1, Cols: map[string]any{"name": "x"}}}); err == nil {
		t.Fatal("malformed authored op accepted")
	}
}

func TestDeviceListingRenamePruneAndStates(t *testing.T) {
	db := library(t, siteA, siteB, "0000000000000000000000000C")
	r := request(siteA, notebook(siteA, 1))
	r.DeviceName = "Ocean"
	post(t, db, siteA, r, nil, 200)
	post(t, db, siteB, request(siteB), nil, 200)
	post(t, db, siteA, request(siteA, notebook(siteA, 2)), nil, 200) // B has not pulled this
	s := Store{DB: db}
	if ok, err := s.SetDeviceLabel(ctx, siteB, "Kitchen tablet"); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, _ := s.SetDeviceLabel(ctx, "0000000000000000000000000C", "never synced"); ok {
		t.Fatal("label conjured a cursor row")
	}
	if err := (identity.Store{DB: db}).Revoke(ctx, siteB); err != nil {
		t.Fatal(err)
	}
	exec(t, db, `UPDATE sync_device_generation SET generation='`+strings.Repeat("e", 64)+`' WHERE site_id='`+siteA+`'`)
	devices, err := s.ListDevices(ctx)
	if err != nil || len(devices) != 3 {
		t.Fatal(devices, err)
	}
	byID := map[string]Device{}
	for _, d := range devices {
		byID[d.SiteID] = d
	}
	a, b, c := byID[siteA], byID[siteB], byID["0000000000000000000000000C"]
	if a.DisplayName() != "Ocean" || !a.Enrolled || !a.NeedsAdoption || a.AckedOpSeq != 2 || a.LastSeenMs == 0 {
		t.Fatalf("A: %+v", a)
	}
	if b.DisplayName() != "Kitchen tablet" || !b.Revoked || b.PendingOps != 1 {
		t.Fatalf("B: %+v", b)
	}
	if c.LastSeenMs != 0 || !c.Enrolled || c.DisplayName() != "Unnamed device" {
		t.Fatalf("C: %+v", c)
	}
	if ok, err := s.PruneDevice(ctx, siteB); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if devices, _ = s.ListDevices(ctx); len(devices) != 3 {
		t.Fatal("pruning a cursor dropped an enrolled device from the list")
	}
}

func TestEpochIsStampedAndARewoundServerIgnoresStaleOps(t *testing.T) {
	db := library(t, siteA, siteB)
	first := post(t, db, siteA, request(siteA, notebook(siteA, 1), notebook(siteA, 2)), nil, 200)
	if first.Epoch == "" || first.AcceptedThrough != 2 {
		t.Fatalf("epoch %q accepted %d", first.Epoch, first.AcceptedThrough)
	}
	// A device that already holds this epoch syncs normally.
	req := request(siteA, notebook(siteA, 3))
	req.Epoch = first.Epoch
	if r := post(t, db, siteA, req, nil, 200); r.Epoch != first.Epoch || r.AcceptedThrough != 3 {
		t.Fatalf("same epoch: %+v", r)
	}
	post(t, db, siteB, request(siteB, notebook(siteB, 1)), nil, 200)

	var rotated string
	if err := db.QueryRow(`SELECT alexandria_rewind_sync_epoch()`).Scan(&rotated); err != nil || rotated == first.Epoch {
		t.Fatalf("rewind %q %v", rotated, err)
	}
	before := state(t, db)
	// A stale device's ops are numbered past what a restored server holds: none
	// are applied, and the device is told to re-queue from accepted_through and
	// re-pull from 0.
	stale := request(siteA, notebook(siteA, 9))
	stale.Epoch, stale.Cursor = first.Epoch, 50
	r := post(t, db, siteA, stale, nil, 200)
	if r.Epoch != rotated || r.AcceptedThrough != 3 || r.Cursor != 0 || !r.HasMore || len(r.Ops) != 0 {
		t.Fatalf("rewound response %+v", r)
	}
	if after := state(t, db); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("stale request changed state: %v -> %v", before, after)
	}
	// The re-queued ops continue contiguously, and the full re-pull includes B.
	req = request(siteA, notebook(siteA, 4))
	req.Epoch = rotated
	r = post(t, db, siteA, req, nil, 200)
	if r.AcceptedThrough != 4 || r.Epoch != rotated || len(r.Ops) != 1 {
		t.Fatalf("after rewind %+v", r)
	}
	// A device without an epoch (older client) is served normally.
	if r := post(t, db, siteB, request(siteB, notebook(siteB, 2)), nil, 200); r.AcceptedThrough != 2 || r.Epoch != rotated {
		t.Fatalf("epochless %+v", r)
	}
}
