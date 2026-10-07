package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/wire"
)

func deviceKey(seed string) (token, hash string) {
	raw := sha256.Sum256([]byte(seed))
	token = "fn-device-v1_" + hex.EncodeToString(raw[:])
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:])
}

func enroll(r *Runtime, body string, mutate func(*httptest.ResponseRecorder, *httptestReq)) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/sync/devices/v1/enroll", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("author", "a-long-test-password")
	out := httptest.NewRecorder()
	if mutate != nil {
		mutate(out, req)
	}
	r.ServeHTTP(out, req)
	return out
}

type httptestReq = http.Request

func device(r *Runtime, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	return out
}

func TestDeviceEnrollmentAndCapabilities(t *testing.T) {
	r, db, _ := fixture(t)
	ctx := context.Background()
	site := wire.NewULID()
	token, hash := deviceKey("device-a")
	body := `{"site_id":"` + site + `","token_hash":"` + hash + `"}`

	if got := device(r, "/sync/capabilities", token); got.Code != 401 || !strings.Contains(got.Header().Get("WWW-Authenticate"), "Bearer") {
		t.Fatalf("unenrolled capabilities: %d", got.Code)
	}
	if got := enroll(r, body, func(_ *httptest.ResponseRecorder, req *httptestReq) { req.Header.Del("Authorization") }); got.Code != 401 {
		t.Fatalf("no account: %d", got.Code)
	}
	if got := enroll(r, body, func(_ *httptest.ResponseRecorder, req *httptestReq) {
		req.SetBasicAuth("author", "wrong-password-here")
	}); got.Code != 401 {
		t.Fatalf("bad password: %d", got.Code)
	}
	apiToken, err := CreateToken(ctx, db, "operator")
	if err != nil {
		t.Fatal(err)
	}
	if got := enroll(r, body, func(_ *httptest.ResponseRecorder, req *httptestReq) {
		req.Header.Set("Authorization", "Bearer "+apiToken)
	}); got.Code != 401 {
		t.Fatalf("operator token enrolled a device: %d", got.Code)
	}
	if got := enroll(r, body, func(_ *httptest.ResponseRecorder, req *httptestReq) { req.Header.Set("Origin", "https://evil.example") }); got.Code != 403 {
		t.Fatalf("browser origin: %d", got.Code)
	}
	if got := enroll(r, body, nil); got.Code != 204 {
		t.Fatalf("enroll: %d %s", got.Code, got.Body)
	}
	if got := enroll(r, body, nil); got.Code != 204 {
		t.Fatalf("retry: %d %s", got.Code, got.Body)
	}
	if got := device(r, "/sync/capabilities", apiToken); got.Code != 401 {
		t.Fatalf("operator token read sync: %d", got.Code)
	}
	got := device(r, "/sync/capabilities", token)
	if got.Code != 200 {
		t.Fatalf("capabilities: %d %s", got.Code, got.Body)
	}
	current, err := generation.Current(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if got.Header().Get(generation.Header) != current || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers: %v", got.Header())
	}
	if !json.Valid(got.Body.Bytes()) {
		t.Fatalf("capabilities not JSON: %s", got.Body)
	}
	if !strings.Contains(got.Body.String(), "55c37f7f1d386ce37ab57c976bccae8d4efed385f852db6d807dff549ad77a54") || !strings.Contains(got.Body.String(), "bounded-rows-v1") {
		t.Fatalf("capabilities body: %s", got.Body)
	}
	if !strings.Contains(got.Body.String(), "assets-v1") {
		t.Fatal("assets-v1 not advertised")
	}
	if got := device(r, "/sync/nope", token); got.Code != 404 {
		t.Fatalf("unknown path: %d", got.Code)
	}
	if got := device(r, "/sync/capabilities/", token); got.Code != 404 || got.Header().Get("Location") != "" {
		t.Fatalf("trailing slash must not redirect: %d", got.Code)
	}

	revoke := httptest.NewRequest("POST", "/sync/devices/v1/revoke", strings.NewReader(`{"site_id":"`+site+`"}`))
	revoke.Header.Set("Content-Type", "application/json")
	revoke.SetBasicAuth("author", "a-long-test-password")
	out := httptest.NewRecorder()
	r.ServeHTTP(out, revoke)
	if out.Code != 204 {
		t.Fatalf("revoke: %d %s", out.Code, out.Body)
	}
	if got := device(r, "/sync/capabilities", token); got.Code != 401 {
		t.Fatalf("revoked key: %d", got.Code)
	}
	if got := enroll(r, body, nil); got.Code != 409 {
		t.Fatalf("revived revoked key: %d", got.Code)
	}
}

func TestServerSiteAndGenerationSurviveReopen(t *testing.T) {
	r, db, id := fixture(t)
	ctx := context.Background()
	var site string
	if err := db.QueryRowContext(ctx, `SELECT site_id FROM sync_site WHERE id=1`).Scan(&site); err != nil {
		t.Fatal(err)
	}
	gen, err := generation.Current(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Open(ctx, Config{ID: id, DatabaseURL: r.cfg.DatabaseURL, Objects: emptyObjects{}, MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	var site2 string
	if err := db.QueryRowContext(ctx, `SELECT site_id FROM sync_site WHERE id=1`).Scan(&site2); err != nil {
		t.Fatal(err)
	}
	gen2, _ := generation.Current(ctx, db)
	if site2 != site || gen2 != gen {
		t.Fatal("server identity changed on reopen")
	}
}

func TestEnrollmentFailsClosedWithCustomAuthenticator(t *testing.T) {
	r, _, _ := fixture(t)
	r.cfg.Authenticate = func(context.Context, *httptestReq, string) error { return nil }
	_, hash := deviceKey("device-b")
	if got := enroll(r, `{"site_id":"`+wire.NewULID()+`","token_hash":"`+hash+`"}`, nil); got.Code != 401 {
		t.Fatalf("enrollment without an account hook: %d", got.Code)
	}
}

func TestTwoDevicesSyncThroughRuntime(t *testing.T) {
	r, _, _ := fixture(t)
	type device struct{ site, token string }
	var devices []device
	for _, seed := range []string{"one", "two"} {
		site := wire.NewULID()
		token, hash := deviceKey(seed)
		if got := enroll(r, `{"site_id":"`+site+`","token_hash":"`+hash+`"}`, nil); got.Code != 204 {
			t.Fatalf("enroll: %d %s", got.Code, got.Body)
		}
		devices = append(devices, device{site, token})
	}
	exchange := func(d device, ops string) *httptest.ResponseRecorder {
		body := `{"protocol_version":1,"schema_hash":"55c37f7f1d386ce37ab57c976bccae8d4efed385f852db6d807dff549ad77a54","site_id":"` + d.site + `","cursor":0,"ops":[` + ops + `]}`
		req := httptest.NewRequest("POST", "/sync/v1", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+d.token)
		req.Header.Set("X-Rhizome-Bounded-Rows", "1")
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	nb := `{"table":"notebook","pk":"00000000000000000000000NB1","site_id":"` + devices[0].site + `","op_seq":1,"op_ts":5,"cols":{"name":"Field notes","sort_order":0,"created_at":1,"deleted_at":null,"folder_id":null,"aspect_long_axis":null,"page_width":null,"page_height":null}}`
	if got := exchange(devices[0], nb); got.Code != 200 || !strings.Contains(got.Body.String(), `"accepted_through":1`) {
		t.Fatalf("push: %d %s", got.Code, got.Body)
	}
	got := exchange(devices[1], "")
	if got.Code != 200 || !strings.Contains(got.Body.String(), "Field notes") || got.Header().Get(generation.Header) == "" {
		t.Fatalf("pull: %d %s", got.Code, got.Body)
	}
}

func TestReaderRowsMaterializeThroughWorkerStep(t *testing.T) {
	r, db, _ := fixture(t)
	site := wire.NewULID()
	token, hash := deviceKey("reader")
	if got := enroll(r, `{"site_id":"`+site+`","token_hash":"`+hash+`"}`, nil); got.Code != 204 {
		t.Fatal(got.Code)
	}
	book := `{"table":"reader_book","pk":"` + strings.Repeat("a", 64) + `","site_id":"` + site + `","op_seq":1,"op_ts":1,"cols":{"asset_id":"` + strings.Repeat("a", 64) + `","byte_length":9007199254740993,"media_type":"application/epub+zip","metadata_json":"{\"version\":1,\"title\":\"Shelf\"}"}}`
	body := `{"protocol_version":1,"schema_hash":"55c37f7f1d386ce37ab57c976bccae8d4efed385f852db6d807dff549ad77a54","site_id":"` + site + `","cursor":0,"ops":[` + book + `]}`
	req := httptest.NewRequest("POST", "/sync/v1", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Rhizome-Bounded-Rows", "1")
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"accepted_through":1`) {
		t.Fatal(out.Code, out.Body)
	}
	if n, err := r.MaterializeReader(context.Background()); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	var length int64
	if err := db.QueryRow(`SELECT byte_length FROM fn_reader_book`).Scan(&length); err != nil || length != 9007199254740993 {
		t.Fatal(length, err)
	}
	if n, err := r.MaterializeReader(context.Background()); err != nil || n != 0 {
		t.Fatal("idle step did work", n, err)
	}
}

func TestNoteSearchRouteUsesAPIAuthentication(t *testing.T) {
	r, _, _ := fixture(t)
	if got := request(r, "GET", "/api/v1/search?q=anything", "", ""); got.Code != 200 || !strings.Contains(got.Body.String(), `"results":[]`) {
		t.Fatalf("search: %d %s", got.Code, got.Body)
	}
	req := httptest.NewRequest("GET", "/api/v1/search?q=anything", nil)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, req)
	if out.Code != 401 {
		t.Fatalf("anonymous search: %d", out.Code)
	}
	if n, err := r.ProcessPages(context.Background(), 4); err != nil || n != 0 {
		t.Fatal("idle page step did work", n, err)
	}
}
