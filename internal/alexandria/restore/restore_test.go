package restore_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jdkruzr/rhizome/server-go/assets"
	"github.com/jdkruzr/rhizome/server-go/bounded"
	"github.com/jdkruzr/rhizome/server-go/registry"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/assetstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/reader"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/restore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

// Ported from UltraBridge internal/libraryrestore tests, plus PostgreSQL
// concurrency cases that replace the worker-join test.

const pub = "0000000000000000000000000A"
const peer = "0000000000000000000000000B"
const fresh = "0000000000000000000000000C"

var ctx = context.Background()

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func token(site string) string {
	return identity.TokenPrefix + strings.Repeat(strings.ToLower(site[len(site)-1:]), 64)
}
func tokenHash(site string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(token(site)))) }

type fixture struct {
	db      *sql.DB
	objects blob.Store
}

func (f fixture) service() restore.Service { return restore.Service{DB: f.db, Objects: f.objects} }

func setup(t *testing.T) fixture {
	t.Helper()
	lib := testenv.Database(t)
	lib.DB.SetMaxOpenConns(8)
	f := fixture{lib.DB, testenv.Objects(t, lib.ID)}
	must(t, identity.EnsureSite(ctx, f.db))
	must(t, generation.Ensure(ctx, f.db))
	for _, site := range []string{pub, peer} {
		must(t, (identity.Store{DB: f.db}).Enroll(ctx, identity.Enrollment{SiteID: site, TokenHash: tokenHash(site)}))
	}
	_, err := f.db.Exec(`CREATE TABLE unrelated_settings(value text); INSERT INTO unrelated_settings VALUES('keep me')`)
	must(t, err)
	_, err = f.db.Exec(`INSERT INTO fn_notebook(id,name,lww_wall_ts,lww_op_seq,lww_site_id) VALUES('0000000000000000000000000Z','discard me',99,1,$1)`, peer)
	must(t, err)
	return f
}

func row(t *testing.T, table, id, author string, seq int64, overrides map[string]any) []byte {
	cols := map[string]any{}
	for _, c := range contract.CandidateCombined().ByName()[table].Columns {
		if c.Nullable {
			cols[c.Name] = nil
		} else {
			switch c.Type {
			case registry.Text, registry.Blob:
				cols[c.Name] = ""
			default:
				cols[c.Name] = int64(0)
			}
		}
	}
	for k, v := range overrides {
		cols[k] = v
	}
	raw, err := json.Marshal(map[string]any{"table": table, "pk": id, "site_id": author, "op_seq": seq, "op_ts": int64(9007199254740993), "cols": cols})
	must(t, err)
	return raw
}
func (f fixture) archive(t *testing.T, rows [][]byte, books map[string][]byte) string {
	return f.archiveManifest(t, rows, books, restore.Manifest{Version: 1, Schema: contract.CandidateCombined().SchemaHash(), Publisher: pub, HighWater: 20})
}
func (f fixture) archiveManifest(t *testing.T, rows [][]byte, books map[string][]byte, manifest any) string {
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	m, err := z.Create("manifest.json")
	must(t, err)
	must(t, json.NewEncoder(m).Encode(manifest))
	r, err := z.Create("rows.jsonl")
	must(t, err)
	for _, v := range rows {
		_, err = r.Write(append(v, '\n'))
		must(t, err)
	}
	for id, b := range books {
		w, err := z.Create("assets/" + id)
		must(t, err)
		_, err = w.Write(b)
		must(t, err)
	}
	must(t, z.Close())
	return f.upload(t, out.Bytes())
}
func (f fixture) upload(t *testing.T, b []byte) string {
	a := assetstore.Store{DB: f.db, Objects: f.objects}
	id := assets.Digest(b)
	d := assets.Descriptor{ID: id, ByteLength: int64(len(b)), ChunkBytes: assets.ChunkBytes}
	_, _, err := a.Stage(ctx, d)
	must(t, err)
	for n := int64(0); n < d.ChunkCount(); n++ {
		l, _ := d.ChunkLength(n)
		chunk := b[n*assets.ChunkBytes : n*assets.ChunkBytes+int64(l)]
		must(t, a.WriteChunk(ctx, id, n, chunk, assets.Digest(chunk)))
	}
	_, err = a.Complete(ctx, id)
	must(t, err)
	return id
}
func (f fixture) request(t *testing.T, id string) generation.Request {
	g, err := generation.Current(ctx, f.db)
	must(t, err)
	return generation.Request{ID: strings.Repeat("1", 64), Expected: g, SnapshotHash: id, Publisher: pub}
}
func (f fixture) scalar(t *testing.T, q string) string {
	t.Helper()
	var v string
	must(t, f.db.QueryRow(q).Scan(&v))
	return v
}

func TestPublicationReplacesOnlyLibraryAndAdoptionFencesOldWork(t *testing.T) {
	f := setup(t)
	s := f.service()
	s.ReplaceDerived = notes.ReplaceDerived
	book := bytes.Repeat([]byte("NO PANCAKES."), 100000)
	id := assets.Digest(book)
	rows := [][]byte{
		row(t, "notebook", pub, peer, 7, map[string]any{"name": "restored", "page_width": int64(10000), "page_height": int64(15000)}),
		row(t, "page", "0000000000000000000000PAGE", peer, 9, map[string]any{"notebook_id": pub}),
		row(t, "reader_book", id, peer, 8, map[string]any{"asset_id": id, "byte_length": len(book), "media_type": "application/epub+zip", "metadata_json": `{"version":1,"title":"A book"}`}),
	}
	r := f.request(t, f.archive(t, rows, map[string][]byte{id: book}))
	b, err := s.Publish(ctx, r)
	must(t, err)
	if b.Generation == r.Expected || b.Cursor != 3 || b.HighWater != 20 {
		t.Fatalf("bad baseline: %+v", b)
	}
	if f.scalar(t, "SELECT name FROM fn_notebook") != "restored" || f.scalar(t, "SELECT count(*) FROM fn_notebook") != "1" || f.scalar(t, "SELECT value FROM unrelated_settings") != "keep me" {
		t.Fatal("wrong replacement scope")
	}
	if f.scalar(t, "SELECT string_agg(page_id, ',') FROM alexandria_page_dirty") != "0000000000000000000000PAGE" {
		t.Fatal("restored pages not queued for the page pipeline")
	}
	if f.scalar(t, "SELECT lww_wall_ts FROM fn_notebook") != "9007199254740993" || f.scalar(t, "SELECT lww_site_id FROM fn_notebook") != peer {
		t.Fatal("provenance lost")
	}
	if f.scalar(t, "SELECT title FROM fn_reader_book_title UNION ALL SELECT metadata_json FROM fn_reader_book") == "" {
		t.Fatal("reader row not materialized")
	}
	store := assetstore.Store{DB: f.db, Objects: f.objects}
	asset, err := store.Describe(ctx, id)
	must(t, err)
	if asset.State != "ready" || asset.ByteLength != int64(len(book)) {
		t.Fatal("book lost")
	}
	var got []byte
	for n := int64(0); n < asset.ChunkCount(); n++ {
		c, err := store.ReadChunk(ctx, id, n)
		must(t, err)
		got = append(got, c.Bytes...)
	}
	if !bytes.Equal(got, book) {
		t.Fatal("book bytes differ")
	}
	if _, _, err = generation.Admit(ctx, f.db, peer); !errors.Is(err, generation.ErrReplaced) {
		t.Fatal("old work admitted")
	}
	// A lost publication response replays without publishing twice.
	retry, err := s.Publish(ctx, r)
	must(t, err)
	if retry != b {
		t.Fatal("retry changed baseline")
	}
	a := restore.Adoption{Generation: b.Generation, Snapshot: b.Snapshot, Site: fresh, TokenHash: tokenHash(fresh)}
	adopted, err := s.Adopt(ctx, peer, a)
	must(t, err)
	if adopted != b {
		t.Fatal("wrong baseline")
	}
	_, err = s.Adopt(ctx, peer, a)
	must(t, err)
	_, _, err = generation.Admit(ctx, f.db, fresh)
	must(t, err)
	if f.scalar(t, "SELECT acked_op_seq FROM sync_cursors WHERE site_id='"+pub+"'") != "20" || f.scalar(t, "SELECT last_pull_seq FROM sync_cursors WHERE site_id='"+fresh+"'") != "3" {
		t.Fatal("wrong resume checkpoint")
	}
	a.Site = "0000000000000000000000000D"
	if _, err = s.Adopt(ctx, peer, a); !errors.Is(err, generation.ErrConflict) {
		t.Fatal("changed adoption retry accepted")
	}
	if _, _, err = generation.Admit(ctx, f.db, peer); !errors.Is(err, generation.ErrReplaced) {
		t.Fatal("old actor was promoted")
	}
}

func TestBadSnapshotNeverTouchesLiveLibrary(t *testing.T) {
	for _, kind := range []string{"duplicate-row", "duplicate-op", "future-publisher-op", "missing-columns", "bad-book", "unreferenced-book", "orphan-reader-row"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			rows := [][]byte{row(t, "notebook", pub, peer, 1, map[string]any{"name": "restored"})}
			books := map[string][]byte{}
			switch kind {
			case "duplicate-row":
				rows = append(rows, rows[0])
			case "duplicate-op":
				rows = append(rows, row(t, "notebook", fresh, peer, 1, nil))
			case "future-publisher-op":
				rows = append(rows, row(t, "notebook", fresh, pub, 21, nil))
			case "missing-columns":
				rows = append(rows, []byte(`{"table":"notebook","pk":"`+fresh+`","site_id":"`+peer+`","op_seq":2,"op_ts":1,"cols":{}}`))
			case "bad-book":
				books[strings.Repeat("a", 64)] = []byte("bad")
			case "unreferenced-book":
				books[assets.Digest([]byte("stray"))] = []byte("stray")
			case "orphan-reader-row":
				// An edit session whose annotation never arrives stays pending.
				rows = append(rows, row(t, "reader_edit_session", "orphan", peer, 2, map[string]any{"annotation_id": "missing", "kind": "interactive", "owner_site": peer, "state": "open"}))
			}
			r := f.request(t, f.archive(t, rows, books))
			if _, err := f.service().Publish(ctx, r); err == nil {
				t.Fatal("bad snapshot published")
			}
			g, err := generation.Current(ctx, f.db)
			must(t, err)
			if g != r.Expected || f.scalar(t, "SELECT name FROM fn_notebook") != "discard me" || f.scalar(t, "SELECT count(*) FROM sync_restore_baseline") != "0" {
				t.Fatal("validation altered live state")
			}
		})
	}
}

func TestMissingOrNullPublicationHighWaterIsNotAnEmptySnapshot(t *testing.T) {
	for _, null := range []bool{false, true} {
		f := setup(t)
		m := map[string]any{"version": 1, "schema": contract.CandidateCombined().SchemaHash(), "publisher": pub}
		if null {
			m["high_water"] = nil
		}
		r := f.request(t, f.archiveManifest(t, nil, nil, m))
		if _, err := f.service().Publish(ctx, r); !errors.Is(err, restore.ErrSnapshot) {
			t.Fatalf("missing high water accepted: %v", err)
		}
		g, err := generation.Current(ctx, f.db)
		must(t, err)
		if g != r.Expected || f.scalar(t, "SELECT name FROM fn_notebook") != "discard me" {
			t.Fatal("invalid snapshot altered library")
		}
	}
}

func TestFailedPublicationRollsBackActualContentAndBaseline(t *testing.T) {
	f := setup(t)
	r := f.request(t, f.archive(t, [][]byte{row(t, "notebook", pub, peer, 1, map[string]any{"name": "restore"})}, nil))
	// Fail late, while writing the baseline after every table was replaced.
	_, err := f.db.Exec(`CREATE FUNCTION fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected'; END $$;
		CREATE TRIGGER fail_restore BEFORE INSERT ON sync_restore_baseline FOR EACH ROW EXECUTE FUNCTION fail()`)
	must(t, err)
	if _, err = f.service().Publish(ctx, r); err == nil {
		t.Fatal("failure published")
	}
	g, err := generation.Current(ctx, f.db)
	must(t, err)
	if g != r.Expected || f.scalar(t, "SELECT name FROM fn_notebook") != "discard me" || f.scalar(t, "SELECT count(*) FROM sync_restore_baseline") != "0" || f.scalar(t, "SELECT count(*) FROM sync_library_replacement") != "0" {
		t.Fatal("partial publication")
	}
}

// PostgreSQL replaces UltraBridge's worker barrier: an exchange and a drain
// that start during publication wait for it, then see only the successor.
func TestPublicationExcludesConcurrentWritersAcrossConnections(t *testing.T) {
	f := setup(t)
	reached, release := make(chan struct{}), make(chan struct{})
	s := f.service()
	s.ReplaceDerived = func(context.Context, *sql.Tx) error {
		close(reached)
		<-release
		return nil
	}
	r := f.request(t, f.archive(t, [][]byte{row(t, "notebook", pub, peer, 1, map[string]any{"name": "restored"})}, nil))
	published := make(chan error, 1)
	go func() { _, err := s.Publish(ctx, r); published <- err }()
	select {
	case <-reached:
	case <-time.After(10 * time.Second):
		t.Fatal("publication never reached replacement")
	}
	admitted, _, err := generation.Admit(ctx, f.db, peer)
	must(t, err)
	body, _ := json.Marshal(bounded.Request{ProtocolVersion: 1, SchemaHash: relay.SchemaHash, SiteID: peer,
		Ops: []json.RawMessage{row(t, "notebook", "0000000000000000000000000Y", peer, 2, map[string]any{"name": "late"})}})
	exchanged := make(chan int, 1)
	go func() {
		q := httptest.NewRequest("POST", "/sync/v1", bytes.NewReader(body)).WithContext(admitted)
		q.Header.Set(bounded.Header, "1")
		w := httptest.NewRecorder()
		relay.Store{DB: f.db}.Handler(peer, nil).ServeHTTP(w, q)
		exchanged <- w.Code
	}()
	drained := make(chan error, 1)
	go func() { _, err := (reader.Store{DB: f.db}).Drain(ctx, 0, 8); drained <- err }()
	select {
	case code := <-exchanged:
		t.Fatalf("exchange did not wait for publication: %d", code)
	case err := <-drained:
		t.Fatalf("drain did not wait for publication: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	must(t, <-published)
	if code := <-exchanged; code != 409 {
		t.Fatalf("stale exchange wrote into the successor: %d", code)
	}
	must(t, <-drained)
	if f.scalar(t, "SELECT count(*) FROM fn_notebook WHERE name='late'") != "0" || f.scalar(t, "SELECT name FROM fn_notebook") != "restored" {
		t.Fatal("writer crossed publication")
	}
}

func TestNativeRestoreAuthorizationAndBaselineReadBoundary(t *testing.T) {
	f := setup(t)
	s := f.service()
	id := f.archive(t, [][]byte{row(t, "notebook", pub, peer, 1, map[string]any{"name": "restored"})}, nil)
	account := func(r *http.Request) error {
		if u, p, ok := r.BasicAuth(); ok && u == "owner" && p == "fixture" {
			return nil
		}
		return identity.ErrAccount
	}
	h := s.Handler(account)
	call := func(method, path, body, key string, admin, origin bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Authorization", "Bearer "+key)
		}
		if admin {
			r.SetBasicAuth("owner", "fixture")
		}
		if origin {
			r.Header.Set("Origin", "https://untrusted.invalid")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	r := f.request(t, id)
	body := `{"request_id":"` + r.ID + `","expected_generation":"` + r.Expected + `","snapshot":"` + id + `","publisher":"` + pub + `"}`
	if w := call("POST", "/sync/restore/v1/publish", body, token(peer), false, false); w.Code == 200 {
		t.Fatal("device key published")
	}
	if w := call("POST", "/sync/restore/v1/publish", body, "", true, true); w.Code != 403 {
		t.Fatal("browser published")
	}
	if w := call("POST", "/sync/restore/v1/publish", body, "", true, false); w.Code != 200 {
		t.Fatalf("publish: %d %s", w.Code, w.Body.String())
	}
	b, err := s.Baseline(ctx)
	must(t, err)
	if w := call("GET", "/sync/restore/v1/publications/"+r.ID, "", token(pub), false, false); w.Code != 200 {
		t.Fatal("publisher cannot recover receipt")
	}
	if w := call("GET", "/sync/restore/v1/publications/"+r.ID, "", token(peer), false, false); w.Code != 404 {
		t.Fatal("peer can claim publisher receipt")
	}
	if w := call("GET", "/sync/restore/v1/publications/"+strings.Repeat("0", 64), "", token(pub), false, false); w.Code != 404 {
		t.Fatal("absent receipt not distinguished")
	}
	if w := call("GET", "/sync/restore/v1/state", "", token(peer), false, false); w.Code != 200 || !strings.Contains(w.Body.String(), `"needs_adoption":true`) {
		t.Fatal("old key cannot discover replacement")
	}
	path := "/sync/restore/v1/assets/" + id + "?generation=" + b.Generation
	if w := call("GET", path, "", token(peer), false, false); w.Code != 200 {
		t.Fatalf("old key cannot download baseline: %d %s", w.Code, w.Body)
	}
	if w := call("GET", "/sync/restore/v1/assets/"+id+"/chunks/0?generation="+b.Generation, "", token(peer), false, false); w.Code != 200 {
		t.Fatal("old key cannot download baseline bytes")
	}
	if w := call("PUT", path, "{}", token(peer), false, false); w.Code != 405 {
		t.Fatal("old key can modify baseline")
	}
	if w := call("GET", "/sync/restore/v1/assets/"+strings.Repeat("a", 64)+"?generation="+b.Generation, "", token(peer), false, false); w.Code != 404 {
		t.Fatal("old key can read arbitrary assets")
	}
	if w := call("GET", path, "", "", false, false); w.Code != 401 {
		t.Fatal("baseline public")
	}
	if w := call("GET", "/sync/restore/v1/assets/"+id+"?generation="+r.Expected, "", token(peer), false, false); w.Code != 409 {
		t.Fatal("wrong baseline generation accepted")
	}
	normal := (identity.Store{DB: f.db}).Bind(func(string, http.ResponseWriter, *http.Request) { t.Fatal("old key reached row/asset router") })
	q := httptest.NewRequest("POST", "/sync/v1", nil)
	q.Header.Set("Authorization", "Bearer "+token(peer))
	w := httptest.NewRecorder()
	normal.ServeHTTP(w, q)
	if w.Code != 409 {
		t.Fatal("old key admitted")
	}
}
