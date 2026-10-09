package boox

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testService(t *testing.T) Service {
	t.Helper()
	db := testenv.Database(t)
	if e := generation.Ensure(context.Background(), db.DB); e != nil {
		t.Fatal(e)
	}
	return Service{Config: Config{PublicURL: "https://boox.example", GatewayPublicURL: "http://127.0.0.1:4984", GatewayAdminURL: "http://127.0.0.1:4985", GatewayUsername: "private", GatewayPassword: "private", Database: "neocloud"}, DB: db.DB, Objects: testenv.Objects(t, db.ID), LibraryID: db.ID}
}
func request(t *testing.T, s Service, method, path string, input any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var b []byte
	if input != nil {
		if raw, ok := input.([]byte); ok {
			b = raw
		} else {
			b, _ = json.Marshal(input)
		}
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func enrollTest(t *testing.T, s Service, installation string) map[string]any {
	t.Helper()
	code, e := s.IssueCode(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	w := request(t, s, "POST", "/api/v1/boox/enroll", map[string]string{"code": code, "installationId": installation, "model": "synthetic", "nativeDeviceId": "hint"}, nil)
	if w.Code != 200 {
		t.Fatalf("enroll status %d", w.Code)
	}
	var p map[string]any
	if e = json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	w = request(t, s, "POST", "/api/v1/boox/enroll", map[string]string{"code": code, "installationId": uuid.NewString()}, nil)
	if w.Code != 401 {
		t.Fatalf("reused enrollment %d", w.Code)
	}
	return p
}
func signinTest(t *testing.T, s Service, p map[string]any, identity string) string {
	t.Helper()
	a := p["account"].(map[string]any)
	raw := "account=" + a["account"].(string) + "&code=" + a["code"].(string)
	w := request(t, s, "POST", "/api/1/users/signin", []byte(raw), map[string]string{"DeviceUniqueId": identity, "Content-Type": "application/x-www-form-urlencoded"})
	if w.Code != 200 {
		t.Fatalf("signin status %d", w.Code)
	}
	var envelope struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	return envelope.Data.Token
}
func TestEnrollmentIdentityAndRevocation(t *testing.T) {
	s := testService(t)
	p1 := enrollTest(t, s, uuid.NewString())
	p2 := enrollTest(t, s, uuid.NewString())
	b1 := signinTest(t, s, p1, "synthetic-device-1")
	b2 := signinTest(t, s, p2, "synthetic-device-2")
	for _, v := range []struct {
		token, id string
		status    int
	}{{b1, "synthetic-device-1", 200}, {b2, "synthetic-device-2", 200}, {b1, "synthetic-device-2", 200}} {
		w := request(t, s, "GET", "/api/1/users/me", nil, map[string]string{"Authorization": "Bearer " + v.token, "DeviceUniqueId": v.id})
		if w.Code != v.status {
			t.Fatalf("device binding status %d expected %d", w.Code, v.status)
		}
	}
	if _, e := s.DB.ExecContext(context.Background(), `UPDATE boox_device SET revoked=true WHERE id=$1`, p1["deviceId"]); e != nil {
		t.Fatal(e)
	}
	w := request(t, s, "GET", "/api/v1/boox/device/status", nil, map[string]string{"Authorization": "Bearer " + p1["companionToken"].(string)})
	if w.Code != 401 {
		t.Fatalf("revoked companion %d", w.Code)
	}
	w = request(t, s, "GET", "/api/1/users/me", nil, map[string]string{"Authorization": "Bearer " + b2, "DeviceUniqueId": "synthetic-device-2"})
	if w.Code != 200 {
		t.Fatalf("peer revoked %d", w.Code)
	}
}

type ossCredentials struct{ ID, Secret, Security string }

func getSTS(t *testing.T, s Service, p map[string]any) ossCredentials {
	t.Helper()
	b := signinTest(t, s, p, "native")
	w := request(t, s, "GET", "/api/1/config/stss", nil, map[string]string{"Authorization": "Bearer " + b, "DeviceUniqueId": "native"})
	if w.Code != 200 {
		t.Fatalf("STS %d", w.Code)
	}
	var v struct {
		Data struct {
			ID       string `json:"AccessKeyId"`
			Secret   string `json:"AccessKeySecret"`
			Security string `json:"SecurityToken"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &v)
	return ossCredentials{v.Data.ID, v.Data.Secret, v.Data.Security}
}
func signed(t *testing.T, s Service, c ossCredentials, method, key, query string, raw []byte, bad bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/"+key+query, bytes.NewReader(raw))
	r.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	r.Header.Set("X-Oss-Security-Token", c.Security)
	if method == "PUT" {
		m := md5.Sum(raw)
		r.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(m[:]))
		r.Header.Set("Content-Type", "application/octet-stream")
	}
	mac := hmac.New(sha1.New, []byte(c.Secret))
	mac.Write([]byte(ossCanonical(method, "/"+s.Config.bucket(s.LibraryID)+"/"+key, r.Header)))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if bad {
		signature = "bad"
	}
	r.Header.Set("Authorization", "OSS "+c.ID+":"+signature)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestOSSHistoryPaginationAndGeneration(t *testing.T) {
	s := testService(t)
	p := enrollTest(t, s, uuid.NewString())
	c := getSTS(t, s, p)
	key := nativeUID(s.LibraryID) + "/note/test/resource/data/a.html"
	for _, raw := range []string{"first", "second"} {
		w := signed(t, s, c, "PUT", key, "", []byte(raw), false)
		if w.Code != 200 {
			t.Fatalf("PUT %d: %s", w.Code, w.Body.String())
		}
	}
	w := signed(t, s, c, "PUT", key, "", []byte("bad"), true)
	if w.Code != 403 {
		t.Fatalf("bad signature %d", w.Code)
	}
	w = signed(t, s, c, "GET", key, "", nil, false)
	if w.Code != 200 || w.Body.String() != "second" {
		t.Fatalf("GET status %d body mismatch", w.Code)
	}
	w = signed(t, s, c, "HEAD", key, "", nil, false)
	if w.Code != 200 || w.Header().Get("Content-Length") != "6" || w.Body.Len() != 0 {
		t.Fatalf("HEAD %d", w.Code)
	}
	for _, suffix := range []string{"b.html", "c.html"} {
		w = signed(t, s, c, "PUT", strings.TrimSuffix(key, "a.html")+suffix, "", []byte(suffix), false)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	w = signed(t, s, c, "GET", "", "?prefix="+nativeUID(s.LibraryID)+"%2F&max-keys=1", nil, false)
	var list listing
	if e := xml.Unmarshal(w.Body.Bytes(), &list); e != nil || !list.IsTruncated || len(list.Contents) != 1 {
		t.Fatalf("pagination %d", w.Code)
	}
	// Historical data from a prior namespace must not leak through an empty prefix.
	if _, e := s.DB.ExecContext(context.Background(), `INSERT INTO boox_blob_live(native_key,version_id) SELECT 'foreign/note/old',version_id FROM boox_blob_live WHERE native_key=$1`, key); e != nil {
		t.Fatal(e)
	}
	w = signed(t, s, c, "GET", "", "", nil, false)
	var all listing
	if e := xml.Unmarshal(w.Body.Bytes(), &all); e != nil || w.Code != 200 || len(all.Contents) != 3 {
		t.Fatalf("namespace listing %d count %d", w.Code, len(all.Contents))
	}
	for _, entry := range all.Contents {
		if !strings.HasPrefix(entry.Key, nativeUID(s.LibraryID)+"/") {
			t.Fatal("foreign namespace listed")
		}
	}
	w = signed(t, s, c, "GET", "", "?prefix=foreign%2F", nil, false)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = signed(t, s, c, "DELETE", key, "", nil, false)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	w = signed(t, s, c, "GET", key, "", nil, false)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	var n int
	if e := s.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM boox_blob_version WHERE native_key=$1`, key).Scan(&n); e != nil || n != 3 {
		t.Fatalf("history n=%d err=%v", n, e)
	}
	if _, e := s.DB.ExecContext(context.Background(), `UPDATE sync_library_generation SET generation=$1 WHERE id=1`, strings.Repeat("a", 64)); e != nil {
		t.Fatal(e)
	}
	w = signed(t, s, c, "PUT", key, "", []byte("late"), false)
	if w.Code != 403 {
		t.Fatalf("stale generation %d", w.Code)
	}
}
func TestConfigAndCanonical(t *testing.T) {
	c := Config{PublicURL: "https://boox.example", GatewayPublicURL: "http://localhost:4984", GatewayAdminURL: "http://localhost:4985", GatewayUsername: "private", GatewayPassword: "private", Database: "neocloud"}
	if c.Validate() != nil {
		t.Fatal(c.Validate())
	}
	for _, origin := range []string{"http://boox.example", "https://user:pass@boox.example", "https://boox.example/path", "https://boox.example?x=y"} {
		c.PublicURL = origin
		if c.Validate() == nil {
			t.Fatalf("unsafe public origin %s", origin)
		}
	}
	h := http.Header{"Content-Type": []string{"text/plain"}, "Date": []string{"today"}, "X-Oss-Security-Token": []string{"token"}}
	if got := ossCanonical("PUT", "/bucket/key", h); got != "PUT\n\ntext/plain\ntoday\nx-oss-security-token:token\n/bucket/key" {
		t.Fatalf("canonical mismatch %q", got)
	}
}

var _ = io.EOF

func TestReaderRestoreKeepsIdentityAndOpaqueFields(t *testing.T) {
	uid := "ps_fixture"
	current := map[string]any{"user": uid, "dbId": uid + "-READER_LIBRARY", "_id": uid + "#book", "uniqueId": "book", "modeType": json.Number("4"), "progress": "8/32", "extraAttributes": `{"backend":{"current_page_position_v3":"now","user_doc_data_update_time":20,"opaque":9007199254740993}}`}
	selected := map[string]any{"user": uid, "dbId": uid + "-READER_LIBRARY", "_id": uid + "#book", "uniqueId": "book", "modeType": json.Number("4"), "progress": "3/64", "extraAttributes": `{"backend":{"current_page_position_v3":"then","opaque":1}}`}
	patch, e := readerPatch(current, selected, uid)
	if e != nil {
		t.Fatal(e)
	}
	if patch["progress"] != "3/64" || !strings.Contains(patch["extraAttributes"].(string), "9007199254740993") || patch["uniqueId"] != nil {
		t.Fatal("progress restore changed identity/opaque value")
	}
	selected["uniqueId"] = "different"
	if _, e = readerPatch(current, selected, uid); e == nil {
		t.Fatal("accepted identity mismatch")
	}
}

func TestReenrollmentRotatesCapabilitiesAndExpiredBearerRefresh(t *testing.T) {
	s := testService(t)
	installation := uuid.NewString()
	p := enrollTest(t, s, installation)
	b := signinTest(t, s, p, "native")
	if _, e := s.DB.ExecContext(context.Background(), `UPDATE boox_device SET bearer_expires=now()-interval '1 minute' WHERE id=$1`, p["deviceId"]); e != nil {
		t.Fatal(e)
	}
	w := request(t, s, "GET", "/api/1/users/me", nil, map[string]string{"Authorization": "Bearer " + b, "DeviceUniqueId": "native"})
	if w.Code != 401 {
		t.Fatal("expired bearer accepted")
	}
	w = request(t, s, "GET", "/api/1/token/refresh", nil, map[string]string{"Authorization": "Bearer " + b, "DeviceUniqueId": "native"})
	if w.Code != 200 {
		t.Fatalf("refresh %d", w.Code)
	}
	next := enrollTest(t, s, installation)
	if next["deviceId"] != p["deviceId"] || next["companionToken"] == p["companionToken"] {
		t.Fatal("reenrollment mapping/rotation mismatch")
	}
	w = request(t, s, "GET", "/api/v1/boox/device/status", nil, map[string]string{"Authorization": "Bearer " + p["companionToken"].(string)})
	if w.Code != 401 {
		t.Fatal("old companion credential survived")
	}
	_ = signinTest(t, s, next, "native")
}

func TestRefreshPreservesBearerForConcurrentNativeRequests(t *testing.T) {
	s := testService(t)
	p := enrollTest(t, s, uuid.NewString())
	b := signinTest(t, s, p, "native")
	for i := 0; i < 2; i++ {
		w := request(t, s, "GET", "/api/v2/token/refresh", nil, map[string]string{"Authorization": "Bearer " + b, "DeviceUniqueId": "native"})
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		var v struct {
			Data struct {
				Token string `json:"token"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &v)
		if v.Data.Token != b {
			t.Fatal("refresh invalidated another request’s bearer")
		}
	}
}

func TestNativeSubscriptionActionsDoNotGrantForeignChannels(t *testing.T) {
	s := testService(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]any{}) }))
	defer gateway.Close()
	s.Config.GatewayAdminURL = gateway.URL
	p := enrollTest(t, s, uuid.NewString())
	b := signinTest(t, s, p, "native")
	headers := map[string]string{"Authorization": "Bearer " + b, "DeviceUniqueId": "native"}
	own := nativeUID(s.LibraryID) + "-NOTE_TREE"
	for _, a := range []string{"add", "remove"} {
		w := request(t, s, "POST", "/api/1/users/updateSyncChannels", map[string]any{"action": a, "channels": []string{own, "foreign-NOTE_TREE"}}, headers)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		var envelope struct {
			Data struct {
				Channels []string `json:"channels"`
			} `json:"data"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &envelope)
		if a == "add" && (len(envelope.Data.Channels) != 1 || envelope.Data.Channels[0] != own) {
			t.Fatal("foreign channel retained")
		}
		if a == "remove" && len(envelope.Data.Channels) != 0 {
			t.Fatal("remove ignored")
		}
	}
}

func TestAuthenticatedCallsUseCredentialPrincipalNotAdvisoryHeaders(t *testing.T) {
	s := testService(t)
	p := enrollTest(t, s, uuid.NewString())
	b := signinTest(t, s, p, "native")
	for _, header := range []string{"", strings.Repeat("x", 300), "another-advisory-id"} {
		w := request(t, s, "GET", "/api/1/statistics/v2/user/storage", nil, map[string]string{"Authorization": "Bearer " + b, "DeviceUniqueId": header})
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
		d, e := s.auth(context.Background(), "native", b, header)
		if e != nil || d.ID != p["deviceId"] {
			t.Fatal("header redirected credential identity")
		}
	}
}

func TestNativeStorageAcceptsBareCapabilityWithoutWeakeningCompanionAuth(t *testing.T) {
	s := testService(t)
	p := enrollTest(t, s, uuid.NewString())
	b := signinTest(t, s, p, "native")
	for _, value := range []string{b, "Bearer " + b} {
		w := request(t, s, "GET", "/api/1/statistics/v2/user/storage", nil, map[string]string{"Authorization": value})
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	for _, value := range []string{"Basic " + b, "not-a-valid-token", token()} {
		w := request(t, s, "GET", "/api/1/statistics/v2/user/storage", nil, map[string]string{"Authorization": value})
		if w.Code != 401 {
			t.Fatal("invalid capability accepted", w.Code)
		}
	}
	w := request(t, s, "GET", "/api/v1/boox/device/status", nil, map[string]string{"Authorization": p["companionToken"].(string)})
	if w.Code != 401 {
		t.Fatal("bare companion capability accepted", w.Code)
	}
}

func TestConcurrentNativeChannelUpdatesAvoidLockUpgradeDeadlock(t *testing.T) {
	s := testService(t)
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reply(w, 200, map[string]any{}) }))
	defer gateway.Close()
	s.Config.GatewayAdminURL = gateway.URL
	p := enrollTest(t, s, uuid.NewString())
	b := signinTest(t, s, p, "native")
	d, e := s.auth(context.Background(), "native", b, "")
	if e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			_, e := s.updateChannels(context.Background(), d, "add", []string{nativeUID(s.LibraryID) + "-NOTE_TREE"})
			results <- e
		}()
	}
	for i := 0; i < 8; i++ {
		if e := <-results; e != nil {
			t.Fatal("concurrent channel update failed", e)
		}
	}
}

func TestSelectedNotebookLifecyclePreservesNumericActiveStatus(t *testing.T) {
	s := testService(t)
	uid := nativeUID(s.LibraryID)
	id := uuid.NewString()
	for _, rev := range []string{"1-active", "2-removed"} {
		status := 1
		if rev == "2-removed" {
			status = 0
		}
		raw, _ := json.Marshal(map[string]any{"_id": id, "_rev": rev, "uniqueId": id, "dbId": uid + "-NOTE_TREE", "user": uid, "type": 1, "status": status, "title": "Synthetic note"})
		if _, e := s.DB.ExecContext(context.Background(), `INSERT INTO boox_revision(document_id,revision,body_sha256,body,deleted) VALUES($1,$2,$3,$4,false)`, id, rev, hash(raw), raw); e != nil {
			t.Fatal(e)
		}
		if rev == "2-removed" {
			if _, e := s.DB.ExecContext(context.Background(), `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body,supported) VALUES($1,$2,'notebook','1',$3,$4,true)`, id, rev, uid, raw); e != nil {
				t.Fatal(e)
			}
		}
	}
	raw, _ := json.Marshal(map[string]any{"kind": "set_note_status", "documentId": id, "expectedRevision": "2-removed", "selectedRevision": "1-active", "idempotencyKey": uuid.NewString()})
	r := httptest.NewRequest("POST", "/api/v1/boox/admin/preview", bytes.NewReader(raw))
	r.Header.Set("Origin", s.Config.PublicURL)
	w := httptest.NewRecorder()
	s.Admin(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var preview struct {
		Changes struct {
			Status int `json:"status"`
		} `json:"changes"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &preview); e != nil || preview.Changes.Status != 1 {
		t.Fatal("active selected version was converted into deletion", w.Body.String())
	}
}

func TestLauncherStorageReport(t *testing.T) {
	s := testService(t)
	p := enrollTest(t, s, uuid.NewString())
	cap := getSTS(t, s, p)
	uid := nativeUID(s.LibraryID)
	for _, body := range []string{"old body", "new"} {
		if w := signed(t, s, cap, "PUT", uid+"/note/fixture/resource/data/a", "", []byte(body), false); w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if w := signed(t, s, cap, "PUT", uid+"/reader/fixture/resource/data/a", "", []byte("reader"), false); w.Code != 200 {
		t.Fatal(w.Code)
	}
	token := signinTest(t, s, p, "native")
	w := request(t, s, "GET", "/api/1/statistics/v2/user/storage", nil, map[string]string{"Authorization": token})
	var report struct {
		Total int64 `json:"total"`
		Data  map[string]struct {
			Name string `json:"name"`
			Data int64  `json:"data"`
		} `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &report) != nil {
		t.Fatalf("launcher storage format %d", w.Code)
	}
	if report.Total != nativeStorageLimit || report.Data["note"].Data != 3 || report.Data["reader"].Data != 6 || report.Data["left"].Data != nativeStorageLimit-9 {
		t.Fatal("incorrect current-resource totals or envelope")
	}
	if report.Data["note"].Name == "" {
		t.Fatal("missing native display label")
	}
}
