package assetstore_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/assetstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/host"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"github.com/jdkruzr/rhizome/server-go/assets"
)

// Ported from UltraBridge internal/syncassets tests, plus S3-specific faults.

const site = "0000000000000000000000000A"

var bg = context.Background()

type fixture struct {
	db      *sql.DB
	objects blob.Store
	ctx     context.Context
}

func setup(t *testing.T) fixture {
	t.Helper()
	lib := testenv.Database(t)
	objects := testenv.Objects(t, lib.ID)
	if err := identity.EnsureSite(bg, lib.DB); err != nil {
		t.Fatal(err)
	}
	if err := generation.Ensure(bg, lib.DB); err != nil {
		t.Fatal(err)
	}
	var key [32]byte
	_, _ = rand.Read(key[:])
	hash := sha256.Sum256(key[:])
	if err := (identity.Store{DB: lib.DB}).Enroll(bg, identity.Enrollment{SiteID: site, TokenHash: hex.EncodeToString(hash[:])}); err != nil {
		t.Fatal(err)
	}
	ctx, _, err := generation.Admit(bg, lib.DB, site)
	if err != nil {
		t.Fatal(err)
	}
	return fixture{lib.DB, objects, ctx}
}

func (f fixture) store() assetstore.Store {
	return host.Assets(host.Library{DB: f.db, Objects: f.objects})
}

func (f fixture) request(t *testing.T, method, path string, b []byte, digest string, want int) []byte {
	t.Helper()
	r := httptest.NewRequest(method, "/sync/assets/v1/"+path, bytes.NewReader(b)).WithContext(f.ctx)
	if digest != "" {
		r.Header.Set("Content-Type", "application/octet-stream")
		r.Header.Set("X-Rhizome-Chunk-SHA256", digest)
	}
	w := httptest.NewRecorder()
	assets.NewHandler(f.store()).ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("%s %s: got %d %s; want %d", method, path, w.Code, w.Body.String(), want)
	}
	return w.Body.Bytes()
}

func descriptor(b []byte) assets.Descriptor {
	return assets.Descriptor{ID: assets.Digest(b), ByteLength: int64(len(b)), ChunkBytes: assets.ChunkBytes}
}
func (f fixture) stage(t *testing.T, d assets.Descriptor, status int) {
	t.Helper()
	b, _ := json.Marshal(d)
	f.request(t, "PUT", d.ID, b, "", status)
}
func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func objectExists(t *testing.T, objects blob.Store, digest string) bool {
	t.Helper()
	key, _ := assetstore.ObjectKey(digest)
	_, err := objects.Stat(bg, key)
	if errors.Is(err, blob.ErrNotFound) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

func TestDurableRoundTrip(t *testing.T) {
	f := setup(t)
	b := bytes.Repeat([]byte("A"), assets.ChunkBytes+5)
	d := descriptor(b)
	if d.ID != "27b977cd4cd686fa69afab5a0d46a4aa2d3dba56e5f459b63ef5db53062fbe77" {
		t.Fatal("fixture digest")
	}
	f.stage(t, d, 201)
	f.stage(t, d, 200)
	f.request(t, "GET", d.ID+"/chunks/0", nil, "", 409)
	f.request(t, "POST", d.ID+"/complete", nil, "", 409)
	f.request(t, "PUT", d.ID+"/chunks/0", b[:assets.ChunkBytes], assets.Digest([]byte("wrong")), 422)
	if count(t, f.db, `SELECT count(*) FROM rhizome_asset_chunk`) != 0 || count(t, f.db, `SELECT count(*) FROM rhizome_asset_object`) != 0 {
		t.Fatal("bad chunk persisted")
	}
	f.request(t, "PUT", d.ID+"/chunks/0", b[:assets.ChunkBytes], assets.Digest(b[:assets.ChunkBytes]), 204)
	// A lost acknowledgement: the retry is idempotent.
	f.request(t, "PUT", d.ID+"/chunks/0", b[:assets.ChunkBytes], assets.Digest(b[:assets.ChunkBytes]), 204)
	f.request(t, "PUT", d.ID+"/chunks/1", b[assets.ChunkBytes:], assets.Digest(b[assets.ChunkBytes:]), 204)
	// Verification interrupted by process death must restart, not assume ready.
	if _, err := f.db.Exec(`UPDATE rhizome_asset SET state='verifying'`); err != nil {
		t.Fatal(err)
	}
	f.request(t, "POST", d.ID+"/complete", nil, "", 200)
	f.request(t, "POST", d.ID+"/complete", nil, "", 200)
	got := append(f.request(t, "GET", d.ID+"/chunks/0", nil, "", 200), f.request(t, "GET", d.ID+"/chunks/1", nil, "", 200)...)
	if !bytes.Equal(got, b) {
		t.Fatal("not byte-exact")
	}
	f.request(t, "POST", d.ID+"/reset-invalid", nil, "", 409)
	d.ByteLength++
	f.stage(t, d, 409)
	// Assets never author row operations or advance an acknowledgement.
	for _, table := range []string{"sync_ops", "sync_cursors"} {
		if n := count(t, f.db, "SELECT count(*) FROM "+table); n != 0 {
			t.Fatalf("%s changed: %d", table, n)
		}
	}
}

func TestInvalidRootResetEmptyAndBounds(t *testing.T) {
	f := setup(t)
	d := descriptor([]byte("right"))
	f.stage(t, d, 201)
	f.request(t, "PUT", d.ID+"/chunks/0", []byte("wrong"), assets.Digest([]byte("wrong")), 204)
	f.request(t, "PUT", d.ID+"/chunks/0", []byte("right"), assets.Digest([]byte("right")), 409)
	f.request(t, "POST", d.ID+"/complete", nil, "", 422)
	f.request(t, "GET", d.ID+"/chunks/0", nil, "", 409)
	f.request(t, "POST", d.ID+"/reset-invalid", nil, "", 204)
	f.request(t, "PUT", d.ID+"/chunks/0", []byte("right"), assets.Digest([]byte("right")), 204)
	f.request(t, "POST", d.ID+"/complete", nil, "", 200)
	for _, query := range []string{"start=-1&limit=1", "start=2&limit=1", "start=0&limit=257", "start=0&limit=0", "start=9223372036854775808&limit=1"} {
		f.request(t, "GET", d.ID+"/chunks?"+query, nil, "", 400)
	}
	f.request(t, "GET", d.ID+"/chunks?start=1&limit=1", nil, "", 200)
	f.request(t, "PUT", d.ID+"/chunks/1", []byte("right"), assets.Digest([]byte("right")), 400)
	f.request(t, "PUT", d.ID+"/chunks/0", []byte("x"), assets.Digest([]byte("x")), 400)
	empty := descriptor(nil)
	f.stage(t, empty, 201)
	f.request(t, "POST", empty.ID+"/complete", nil, "", 200)
	max := assets.Descriptor{ID: d.ID, ByteLength: math.MaxInt64, ChunkBytes: assets.ChunkBytes}
	if max.ChunkCount() != 35184372088832 {
		t.Fatal("overflow")
	}
	// A maximal descriptor stores and pages without overflow.
	other := assets.Descriptor{ID: assets.Digest([]byte("max")), ByteLength: math.MaxInt64, ChunkBytes: assets.ChunkBytes}
	f.stage(t, other, 201)
	f.request(t, "GET", other.ID+"/chunks?start=35184372088831&limit=1", nil, "", 200)
}

func TestConcurrentRetriesAndBoundedManifest(t *testing.T) {
	f := setup(t)
	f.db.SetMaxOpenConns(8)
	s := f.store()
	b := []byte("hello")
	d := descriptor(b)
	if _, _, err := s.Stage(f.ctx, d); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for j := 0; j < 8; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.WriteChunk(f.ctx, d.ID, 0, b, assets.Digest(b)); err != nil {
				t.Error(err)
			}
			if _, err := s.Complete(f.ctx, d.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	large := assets.Descriptor{ID: assets.Digest([]byte("large")), ByteLength: assets.ChunkBytes * 1000, ChunkBytes: assets.ChunkBytes}
	if _, _, err := s.Stage(f.ctx, large); err != nil {
		t.Fatal(err)
	}
	p, err := s.ListChunks(f.ctx, large.ID, 0, 256)
	if err != nil || len(p.Entries) != 256 || p.NextStart == nil || *p.NextStart != 256 {
		t.Fatal(p, err)
	}
	for _, e := range p.Entries {
		if e.SHA256 != nil {
			t.Fatal("missing chunk appeared verified")
		}
	}
	if body := f.request(t, "GET", large.ID+"/chunks?start=0&limit=256", nil, "", 200); len(body) > 65536 {
		t.Fatal(len(body))
	}
}

// The object is written but the transaction fails: no acknowledgement, no row,
// and GC later removes the orphan.
func TestObjectWrittenButTransactionFailsLeavesCollectableOrphan(t *testing.T) {
	f := setup(t)
	b := []byte("orphan bytes")
	d := descriptor(b)
	f.stage(t, d, 201)
	exec := func(q string) {
		if _, err := f.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE FUNCTION fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture failure'; END $$`)
	exec(`CREATE TRIGGER fail BEFORE INSERT ON rhizome_asset_chunk FOR EACH ROW EXECUTE FUNCTION fail()`)
	body := f.request(t, "PUT", d.ID+"/chunks/0", b, assets.Digest(b), 503)
	if strings.Contains(string(body), "fixture") {
		t.Fatal("internal error leaked")
	}
	if count(t, f.db, `SELECT count(*) FROM rhizome_asset_chunk`) != 0 || !objectExists(t, f.objects, assets.Digest(b)) {
		t.Fatal("expected an orphan object and no row")
	}
	s := f.store()
	// Inside the grace period nothing is collected.
	if n, err := s.CollectGarbage(bg, time.Hour, 10); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	exec(`UPDATE rhizome_asset_object SET touched_at = now() - interval '8 days'`)
	if n, err := s.CollectGarbage(bg, 7*24*time.Hour, 10); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if objectExists(t, f.objects, assets.Digest(b)) || count(t, f.db, `SELECT count(*) FROM rhizome_asset_object`) != 0 {
		t.Fatal("orphan survived GC")
	}
	exec(`DROP TRIGGER fail ON rhizome_asset_chunk`)
	f.request(t, "PUT", d.ID+"/chunks/0", b, assets.Digest(b), 204)
	f.request(t, "POST", d.ID+"/complete", nil, "", 200)
	// A referenced object is never collected, however old its intent.
	exec(`UPDATE rhizome_asset_object SET touched_at = now() - interval '8 days'`)
	if n, err := s.CollectGarbage(bg, 7*24*time.Hour, 10); err != nil || n != 0 || !objectExists(t, f.objects, assets.Digest(b)) {
		t.Fatal("referenced object collected", n, err)
	}
}

// A chunk row whose object vanished is forgotten at Complete so the client
// re-uploads it; the asset never turns ready with missing bytes.
func TestMissingObjectBeforeCompleteReturnsToStaging(t *testing.T) {
	f := setup(t)
	b := bytes.Repeat([]byte("B"), assets.ChunkBytes+7)
	d := descriptor(b)
	f.stage(t, d, 201)
	f.request(t, "PUT", d.ID+"/chunks/0", b[:assets.ChunkBytes], assets.Digest(b[:assets.ChunkBytes]), 204)
	f.request(t, "PUT", d.ID+"/chunks/1", b[assets.ChunkBytes:], assets.Digest(b[assets.ChunkBytes:]), 204)
	key, _ := assetstore.ObjectKey(assets.Digest(b[assets.ChunkBytes:]))
	if err := f.objects.Delete(bg, key); err != nil {
		t.Fatal(err)
	}
	body := f.request(t, "POST", d.ID+"/complete", nil, "", 409)
	if !strings.Contains(string(body), "missing_chunks") {
		t.Fatal(string(body))
	}
	var state string
	if err := f.db.QueryRow(`SELECT state FROM rhizome_asset`).Scan(&state); err != nil || state != "staging" {
		t.Fatal(state, err)
	}
	var manifest assets.Page
	if err := json.Unmarshal(f.request(t, "GET", d.ID+"/chunks?start=0&limit=2", nil, "", 200), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Entries[0].SHA256 == nil || manifest.Entries[1].SHA256 != nil {
		t.Fatal("manifest does not ask for the lost chunk again")
	}
	f.request(t, "PUT", d.ID+"/chunks/1", b[assets.ChunkBytes:], assets.Digest(b[assets.ChunkBytes:]), 204)
	f.request(t, "POST", d.ID+"/complete", nil, "", 200)
}

// A restore between admission and a chunk write fails the write.
func TestGenerationChangeMidUploadRefusesWrites(t *testing.T) {
	f := setup(t)
	b := []byte("fenced")
	d := descriptor(b)
	f.stage(t, d, 201)
	if _, err := f.db.Exec(`UPDATE sync_library_generation SET generation=$1`, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(f.request(t, "PUT", d.ID+"/chunks/0", b, assets.Digest(b), 409)), "library_replaced") {
		t.Fatal("not fenced")
	}
	f.request(t, "POST", d.ID+"/complete", nil, "", 409)
	if count(t, f.db, `SELECT count(*) FROM rhizome_asset_chunk`) != 0 {
		t.Fatal("fenced write landed")
	}
	other := descriptor([]byte("new asset"))
	f.stage(t, other, 409)
}

// Without an admission (no device binding) nothing can mutate.
func TestUnadmittedRequestsCannotWrite(t *testing.T) {
	f := setup(t)
	f.ctx = bg
	f.stage(t, descriptor([]byte("x")), 409)
	if count(t, f.db, `SELECT count(*) FROM rhizome_asset`) != 0 {
		t.Fatal("unadmitted stage")
	}
}
