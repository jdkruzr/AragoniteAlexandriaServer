package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func enrollIdentity(t *testing.T, s Service, code, uid string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, s, "POST", "/api/v1/boox/enroll", map[string]string{"code": code, "installationId": uuid.NewString(), "nativeDeviceId": "synthetic-device", "nativeUid": uid}, nil)
}

func TestNativeIdentityAdoption(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	code, e := s.IssueCode(ctx)
	if e != nil {
		t.Fatal(e)
	}
	wMissing := request(t, s, "POST", "/api/v1/boox/enroll", map[string]any{"code": code, "installationId": uuid.NewString(), "nativeUid": "", "preserveNativeIdentity": true}, nil)
	if wMissing.Code != 409 {
		t.Fatalf("missing original identity: %d", wMissing.Code)
	}
	for _, uid := range []string{"*", "user/other", "user\nother"} {
		if w := enrollIdentity(t, s, code, uid); w.Code != 400 {
			t.Fatalf("invalid identity: %d", w.Code)
		}
	}
	if w := enrollIdentity(t, s, "not-an-enrollment-code", "original-owner"); w.Code != 401 {
		t.Fatalf("invalid code: %d", w.Code)
	}
	var before sql.NullString
	if e = s.DB.QueryRowContext(ctx, `SELECT native_uid FROM boox_identity WHERE id=1`).Scan(&before); e != nil || before.Valid {
		t.Fatal("unauthorized request bound identity", e)
	}
	w := enrollIdentity(t, s, code, "original-owner")
	if w.Code != 200 {
		t.Fatalf("first enrollment: %d", w.Code)
	}
	var p map[string]any
	if e = json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	if p["nativeUid"] != "original-owner" {
		t.Fatal("owner was replaced")
	}
	bearer := signinTest(t, s, p, "synthetic-device")
	me := request(t, s, "GET", "/api/1/users/me", nil, map[string]string{"Authorization": "Bearer " + bearer, "DeviceUniqueId": "synthetic-device"})
	var account struct {
		Data struct {
			UID string `json:"uid"`
		} `json:"data"`
	}
	if json.Unmarshal(me.Body.Bytes(), &account) != nil || account.Data.UID != "original-owner" {
		t.Fatalf("native owner response: %d", me.Code)
	}
	if e = s.ResolveIdentity(ctx); e != nil {
		t.Fatal(e)
	}
	if !s.Owns(httptest.NewRequest("GET", "/original-owner/note/resource", nil)) {
		t.Fatal("original asset namespace not routed")
	}
	next, e := s.IssueCode(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if w = enrollIdentity(t, s, next, "another-owner"); w.Code != 409 {
		t.Fatalf("foreign identity: %d", w.Code)
	}
	if w = enrollIdentity(t, s, next, "original-owner"); w.Code != 200 {
		t.Fatalf("mismatch consumed code: %d", w.Code)
	}
	var p2 map[string]any
	if e = json.Unmarshal(w.Body.Bytes(), &p2); e != nil {
		t.Fatal(e)
	}
	if p2["deviceId"] == p["deviceId"] || p2["companionToken"] == p["companionToken"] {
		t.Fatal("device credentials were shared")
	}
	legacy := enrollTest(t, s, uuid.NewString())
	if legacy["nativeUid"] != "original-owner" {
		t.Fatal("legacy enrollment changed binding")
	}
}

func TestConcurrentIdentityAdoption(t *testing.T) {
	s := testService(t)
	codes := make([]string, 2)
	for i := range codes {
		var e error
		codes[i], e = s.IssueCode(context.Background())
		if e != nil {
			t.Fatal(e)
		}
	}
	results := make([]int, 2)
	var wg sync.WaitGroup
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = enrollIdentity(t, s, codes[i], []string{"owner-a", "owner-b"}[i]).Code
		}(i)
	}
	wg.Wait()
	if !(results[0] == 200 && results[1] == 409 || results[0] == 409 && results[1] == 200) {
		t.Fatalf("concurrent binding: %v", results)
	}
}
