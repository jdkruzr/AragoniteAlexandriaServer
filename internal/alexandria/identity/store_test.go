package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

const siteA = "0000000000000000000000000A"
const siteB = "0000000000000000000000000B"

var ctx = context.Background()

func key() (string, string) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	token := TokenPrefix + hex.EncodeToString(b[:])
	h := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(h[:])
}

// open returns a store on a fresh pool to the library; calling it again on the
// same URL simulates a server restart.
func open(t *testing.T, url string) Store {
	t.Helper()
	db, err := sql.Open("pgx", url)
	check(t, err)
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { db.Close() })
	check(t, EnsureSite(ctx, db))
	check(t, generation.Ensure(ctx, db))
	return Store{db}
}

func check(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestEnrollmentRetryRestartRevoke(t *testing.T) {
	lib := testenv.Database(t)
	s := open(t, lib.URL)
	token, hash := key()
	e := Enrollment{SiteID: siteA, TokenHash: hash}
	check(t, s.Enroll(ctx, e))
	check(t, s.Enroll(ctx, e))
	var stored string
	check(t, s.DB.QueryRowContext(ctx, `SELECT token_hash FROM sync_device_identity`).Scan(&stored))
	if stored != hash || stored == token {
		t.Fatal("Wrong credential persistence")
	}
	s = open(t, lib.URL)
	got, err := s.Resolve(ctx, token)
	check(t, err)
	if got != siteA {
		t.Fatal("Identity lost after restart")
	}
	for _, bad := range []string{hash, "assetlab", token + "x", TokenPrefix + hash, "alexandria_operator-token"} {
		if _, err = s.Resolve(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Noncredential %q authenticated", bad)
		}
	}
	_, other := key()
	if err = s.Enroll(ctx, Enrollment{SiteID: siteA, TokenHash: other}); !errors.Is(err, ErrConflict) {
		t.Fatal("Replaced enrolled site")
	}
	if err = s.Enroll(ctx, Enrollment{SiteID: siteB, TokenHash: hash}); !errors.Is(err, ErrConflict) {
		t.Fatal("Credential bound to two sites")
	}
	check(t, s.Revoke(ctx, siteA))
	check(t, s.Revoke(ctx, siteA))
	if _, err = s.Resolve(ctx, token); !errors.Is(err, ErrInvalid) {
		t.Fatal("Revoked key admitted")
	}
	if err = s.Enroll(ctx, e); !errors.Is(err, ErrConflict) {
		t.Fatal("Retry revived revoked key")
	}
	s = open(t, lib.URL)
	if _, err = s.Resolve(ctx, token); !errors.Is(err, ErrInvalid) {
		t.Fatal("Restart revived revoked key")
	}
}

func TestConcurrentEnrollmentCannotReplaceBinding(t *testing.T) {
	s := open(t, testenv.Database(t).URL)
	tokens := make([]string, 12)
	hashes := make([]string, 12)
	result := make([]error, 12)
	var wg sync.WaitGroup
	for i := range tokens {
		tokens[i], hashes[i] = key()
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result[i] = s.Enroll(ctx, Enrollment{SiteID: siteA, TokenHash: hashes[i]})
		}(i)
	}
	wg.Wait()
	winners := 0
	for i, err := range result {
		if err == nil {
			winners++
			got, e := s.Resolve(ctx, tokens[i])
			check(t, e)
			if got != siteA {
				t.Fatal("wrong winner")
			}
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d winners", winners)
	}
}

func TestLegacyAdoptionChecksSurvivingMirrorAndPreservesProvenance(t *testing.T) {
	s := open(t, testenv.Database(t).URL)
	_, hash := key()
	// A mirror-only author remains known even after cursor/relay compaction.
	_, err := s.DB.ExecContext(ctx, `CREATE TABLE fn_fixture(id text, lww_site_id text, lww_op_seq bigint)`)
	check(t, err)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO fn_fixture VALUES('old',$1,123)`, siteA)
	check(t, err)
	e := Enrollment{SiteID: siteA, TokenHash: hash}
	if err = s.Enroll(ctx, e); !errors.Is(err, ErrAdoption) {
		t.Fatal("Silently adopted known author")
	}
	e.AdoptLegacy = true
	check(t, s.Enroll(ctx, e))
	var seq int
	check(t, s.DB.QueryRowContext(ctx, `SELECT lww_op_seq FROM fn_fixture WHERE lww_site_id=$1`, siteA).Scan(&seq))
	if seq != 123 {
		t.Fatal("Adoption re-authored history")
	}
	var server string
	check(t, s.DB.QueryRowContext(ctx, `SELECT site_id FROM sync_site`).Scan(&server))
	_, hash = key()
	if err = s.Enroll(ctx, Enrollment{SiteID: server, TokenHash: hash, AdoptLegacy: true}); !errors.Is(err, ErrServerSite) {
		t.Fatal("Enrolled the server as a client")
	}
}

func TestInvalidEnrollmentAndFailedWriteDoNotReserveIdentity(t *testing.T) {
	s := open(t, testenv.Database(t).URL)
	_, hash := key()
	for _, e := range []Enrollment{{SiteID: "bad", TokenHash: hash}, {SiteID: "Z000000000000000000000000A", TokenHash: hash}, {SiteID: siteA, TokenHash: "bad"}} {
		if !errors.Is(s.Enroll(ctx, e), ErrInvalid) {
			t.Fatal("Accepted invalid enrollment")
		}
	}
	_, err := s.DB.ExecContext(ctx, `CREATE FUNCTION fail_identity() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'failure'; END $$;
		CREATE TRIGGER fail_identity BEFORE INSERT ON sync_device_identity FOR EACH ROW EXECUTE FUNCTION fail_identity()`)
	check(t, err)
	if s.Enroll(ctx, Enrollment{SiteID: siteA, TokenHash: hash}) == nil {
		t.Fatal("Ignored failed commit")
	}
	_, err = s.DB.ExecContext(ctx, `DROP TRIGGER fail_identity ON sync_device_identity`)
	check(t, err)
	check(t, s.Enroll(ctx, Enrollment{SiteID: siteA, TokenHash: hash}))
}

func TestServerSiteAndGenerationSurviveRepeatedStartup(t *testing.T) {
	lib := testenv.Database(t)
	s := open(t, lib.URL)
	var site, gen string
	check(t, s.DB.QueryRowContext(ctx, `SELECT s.site_id, g.generation FROM sync_site s, sync_library_generation g`).Scan(&site, &gen))
	s = open(t, lib.URL)
	var site2, gen2 string
	check(t, s.DB.QueryRowContext(ctx, `SELECT s.site_id, g.generation FROM sync_site s, sync_library_generation g`).Scan(&site2, &gen2))
	if site != site2 || gen != gen2 {
		t.Fatal("startup re-minted the server site or generation")
	}
}
