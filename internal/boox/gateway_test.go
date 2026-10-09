package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Optional real-Gateway fixture; set REQUIRE_GATEWAY to forbid a skipped gate.
func TestRealGatewayJournalPublicationAndSessions(t *testing.T) {
	path := os.Getenv("ALEXANDRIA_TEST_BOOX_CREDENTIAL_FILE")
	if path == "" {
		if os.Getenv("ALEXANDRIA_REQUIRE_BOOX_GATEWAY") == "1" {
			t.Fatal("real BOOX Gateway fixture required")
		}
		t.Skip("private Gateway fixture not configured")
	}
	raw, e := os.ReadFile(filepath.Clean(path))
	if e != nil {
		t.Fatal(e)
	}
	var creds struct{ Username, Password string }
	if json.Unmarshal(raw, &creds) != nil {
		t.Fatal("invalid private fixture")
	}
	s := testService(t)
	s.Config.GatewayAdminURL = "http://127.0.0.1:19768"
	s.Config.GatewayPublicURL = "http://127.0.0.1:19767"
	s.Config.GatewayUsername = creds.Username
	s.Config.GatewayPassword = creds.Password
	ctx := context.Background()
	code, e := s.IssueCode(ctx)
	if e != nil {
		t.Fatal(e)
	}
	enrollment := enrollIdentity(t, s, code, "original_fixture_"+strings.ReplaceAll(s.LibraryID, "-", ""))
	if enrollment.Code != 200 {
		t.Fatalf("identity enrollment %d", enrollment.Code)
	}
	var p map[string]any
	if e = json.Unmarshal(enrollment.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	uid := p["nativeUid"].(string)
	sync := nativeSyncFunction("alexandria-boox-library:" + s.LibraryID)
	if e = s.Config.gateway(ctx, "PUT", "/_config", map[string]any{"bucket": "neocloud", "use_views": true, "num_index_replicas": 0, "enable_shared_bucket_access": true, "guest": map[string]any{"disabled": true}, "sync": sync}, nil); e != nil {
		t.Fatal(e)
	}
	docID := "synthetic-native-note-" + strings.ReplaceAll(s.LibraryID, "-", "")
	body := map[string]any{"_id": docID, "uniqueId": docID, "user": uid, "dbId": uid + "-NOTE_TREE", "title": "before", "status": 1, "type": 1, "opaque": map[string]any{"preserve": "exact"}}
	var ack struct {
		Rev string `json:"rev"`
	}
	if e = s.Config.gateway(ctx, "PUT", "/"+docID, body, &ack); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		_, e = s.Observe(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var n int
		if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_projection WHERE document_id=$1`, docID).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("gateway changes timeout")
		}
		time.Sleep(200 * time.Millisecond)
	}
	var n int
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_revision WHERE document_id=$1`, docID).Scan(&n); e != nil || n != 1 {
		t.Fatalf("journal count %d error %v", n, e)
	}
	_, e = s.Observe(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_revision WHERE document_id=$1`, docID).Scan(&n); e != nil || n != 1 {
		t.Fatal("duplicate revision")
	}
	input := map[string]any{"kind": "rename_note", "documentId": docID, "expectedRevision": ack.Rev, "idempotencyKey": "synthetic-rename-operation", "title": "after"}
	raw, _ = json.Marshal(input)
	r := httptest.NewRequest("POST", "/api/v1/boox/admin/preview", bytes.NewReader(raw))
	r.Header.Set("Origin", s.Config.PublicURL)
	w := httptest.NewRecorder()
	s.Admin(w, r)
	if w.Code != 200 {
		t.Fatalf("preview %d %s", w.Code, w.Body.String())
	}
	var preview struct {
		OperationID string `json:"operationId"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	if _, e = s.DB.ExecContext(ctx, `UPDATE boox_operation SET state='queued' WHERE id=$1`, preview.OperationID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishOne(ctx); e != nil {
		t.Fatal(e)
	}
	var current map[string]any
	if e = s.Config.gateway(ctx, "GET", "/"+docID, nil, &current); e != nil {
		t.Fatal(e)
	}
	if current["title"] != "after" || current["opaque"].(map[string]any)["preserve"] != "exact" {
		t.Fatal("roundtrip metadata/opaque mismatch")
	}
	firstRev := current["_rev"]
	if _, e = s.PublishOne(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Config.gateway(ctx, "GET", "/"+docID, nil, &current); e != nil || current["_rev"] != firstRev {
		t.Fatal("duplicate publication")
	}
	// Replay the lost-success state; stable operation markers avoid new revisions.
	if _, e = s.DB.ExecContext(ctx, `UPDATE boox_operation SET state='queued' WHERE id=$1`, preview.OperationID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishOne(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Config.gateway(ctx, "GET", "/"+docID, nil, &current); e != nil || current["_rev"] != firstRev {
		t.Fatal("lost ACK replay")
	}
	if e = s.Config.gateway(ctx, "GET", "/"+uuid.NewSHA1(uuid.NameSpaceOID, []byte("boox-message:"+preview.OperationID)).String(), nil, &current); e != nil || fmt.Sprint(current["msgType"]) != "6" || current["replicatorName"] != "NOTE_TREE" {
		t.Fatal("missing doorbell")
	}
	native := signinTest(t, s, p, "native-gateway")
	w = request(t, s, "GET", "/api/1/users/syncToken", nil, map[string]string{"Authorization": "Bearer " + native, "DeviceUniqueId": "native-gateway"})
	if w.Code != 200 {
		t.Fatalf("session %d %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data struct {
			Cookie  string `json:"cookie_name"`
			Session string `json:"session_id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &envelope)
	var backendCookie string
	if e = s.DB.QueryRowContext(ctx, `SELECT backend_session_id FROM boox_grant WHERE id=$1`, envelope.Data.Session).Scan(&backendCookie); e != nil {
		t.Fatal(e)
	}
	if backendCookie == envelope.Data.Session {
		t.Fatal("backend credential exposed to device")
	}
	// Native CBL filters by dbId, not the owner-wide authorization channel.
	// A successful unfiltered GET/push alone does not prove native pull access.
	filteredHas := func(channel string, wanted string) bool {
		t.Helper()
		target := s.Config.GatewayPublicURL + "/neocloud/_changes?filter=sync_gateway%2Fbychannel&channels=" + url.QueryEscape(channel)
		req, _ := http.NewRequest("GET", target, nil)
		req.AddCookie(&http.Cookie{Name: envelope.Data.Cookie, Value: backendCookie})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("filtered pull: %d", res.StatusCode)
		}
		var feed struct {
			Results []struct {
				ID string `json:"id"`
			} `json:"results"`
		}
		if err = json.NewDecoder(res.Body).Decode(&feed); err != nil {
			t.Fatal(err)
		}
		for _, row := range feed.Results {
			if row.ID == wanted {
				return true
			}
		}
		return false
	}
	if filteredHas(uid+"-NOTE_TREE", docID) {
		t.Fatal("unrequested channel unexpectedly granted")
	}
	headers := map[string]string{"Authorization": "Bearer " + native}
	w = request(t, s, "POST", "/api/1/users/updateSyncChannels", map[string]any{"action": "add", "channels": []string{uid + "-NOTE_TREE", "foreign_owner-NOTE_TREE"}}, headers)
	if w.Code != 200 {
		t.Fatalf("grant scoped channel: %d", w.Code)
	}
	if !filteredHas(uid+"-NOTE_TREE", docID) {
		t.Fatal("native filtered pull cannot see existing notebook")
	}
	if filteredHas("foreign_owner-NOTE_TREE", docID) {
		t.Fatal("foreign channel read")
	}
	// A second session must neither drop subscriptions nor invalidate the first.
	if w = request(t, s, "GET", "/api/1/users/syncToken", nil, headers); w.Code != 200 {
		t.Fatalf("second session: %d", w.Code)
	}
	if !filteredHas(uid+"-NOTE_TREE", docID) {
		t.Fatal("session creation broke existing channel grant/session")
	}
	w = request(t, s, "POST", "/api/1/users/updateSyncChannels", map[string]any{"action": "remove", "channels": []string{uid + "-NOTE_TREE"}}, headers)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	if filteredHas(uid+"-NOTE_TREE", docID) {
		t.Fatal("removed subscription still granted")
	}
	// Exercise real Gateway authorization with the enrolled device session.
	for _, tc := range []struct {
		uid  string
		want int
	}{{uid, 201}, {"foreign_owner", 403}} {
		doc := map[string]any{"user": tc.uid, "dbId": tc.uid + "-NOTE_TREE", "status": 1, "type": 1}
		raw, _ := json.Marshal(doc)
		req, _ := http.NewRequest("PUT", s.Config.GatewayPublicURL+"/neocloud/identity-"+uuid.NewString(), bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: envelope.Data.Cookie, Value: backendCookie})
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("native namespace permission got %d want %d", res.StatusCode, tc.want)
		}
	}

	proxy := httptest.NewServer(http.HandlerFunc(s.ServeHTTP))
	defer proxy.Close()
	req, _ := http.NewRequest("GET", proxy.URL+"/boox-neocloud", nil)
	req.AddCookie(&http.Cookie{Name: envelope.Data.Cookie, Value: envelope.Data.Session})
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("proxy %d", res.StatusCode)
	}
	// Simulate backend expiry without changing the device-facing cookie.
	if _, e = s.DB.ExecContext(ctx, `UPDATE boox_grant SET backend_expires_at=now()-interval '1 second' WHERE id=$1`, envelope.Data.Session); e != nil {
		t.Fatal(e)
	}
	res, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("backend renewal: %d", res.StatusCode)
	}
	var renewed string
	if e = s.DB.QueryRowContext(ctx, `SELECT backend_session_id FROM boox_grant WHERE id=$1`, envelope.Data.Session).Scan(&renewed); e != nil {
		t.Fatal(e)
	}
	if renewed == backendCookie {
		t.Fatal("expired backend was not replaced")
	}
	// Gateway losing a still-unexpired backend session must repair on the same
	// frontend request, not expose a permanent 401 to the native client.
	if e = s.Config.gateway(ctx, "DELETE", "/_session/"+url.PathEscape(renewed), nil, nil); e != nil {
		t.Fatal(e)
	}
	res, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("missing backend repair: %d", res.StatusCode)
	}
	if res.Header.Get("Set-Cookie") != "" {
		t.Fatal("backend cookie escaped proxy")
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE boox_device SET revoked=true WHERE id=$1`, p["deviceId"]); e != nil {
		t.Fatal(e)
	}
	res, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("revoked proxy %d", res.StatusCode)
	}
	// Exercise an exact retained native asset repair before restoring the device.
	c := getSTS(t, s, enrollTest(t, s, uuid.NewString()))
	key := uid + "/note/" + docID + "/resource/data/native.html"
	for _, v := range []string{"retained", "newer"} {
		if signed(t, s, c, "PUT", key, "", []byte(v), false).Code != 200 {
			t.Fatal("repair setup PUT")
		}
	}
	var version int64
	if e = s.DB.QueryRowContext(ctx, `SELECT min(id) FROM boox_blob_version WHERE native_key=$1`, key).Scan(&version); e != nil {
		t.Fatal(e)
	}
	_, e = s.Observe(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var parentRev string
	if e = s.DB.QueryRowContext(ctx, `SELECT revision FROM boox_projection WHERE document_id=$1`, docID).Scan(&parentRev); e != nil {
		t.Fatal(e)
	}
	// Make the retained version visibly old without changing its provenance on repair.
	if _, e = s.DB.ExecContext(ctx, `UPDATE boox_blob_version SET created_at='2020-01-01' WHERE id=$1`, version); e != nil {
		t.Fatal(e)
	}
	repairStarted := time.Now().UTC().Truncate(time.Second)
	repairInput := map[string]any{"documentId": docID, "expectedRevision": parentRev, "nativeKey": key, "versionId": version, "idempotencyKey": "synthetic-verified-asset-repair"}
	raw, _ = json.Marshal(repairInput)
	r = httptest.NewRequest("POST", "/api/v1/boox/admin/repair-preview", bytes.NewReader(raw))
	r.Header.Set("Origin", s.Config.PublicURL)
	w = httptest.NewRecorder()
	s.Admin(w, r)
	if w.Code != 200 {
		t.Fatalf("repair preview %d %s", w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &preview)
	if _, e = s.DB.ExecContext(ctx, `UPDATE boox_operation SET state='queued' WHERE id=$1`, preview.OperationID); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if _, e = s.PublishOne(ctx); e != nil {
			t.Fatal(e)
		}
	}
	w = signed(t, s, c, "GET", key, "", nil, false)
	if w.Code != 200 || w.Body.String() != "retained" {
		t.Fatalf("repair bytes %d", w.Code)
	}
	modified, err := http.ParseTime(w.Header().Get("Last-Modified"))
	if err != nil || modified.Before(repairStarted) {
		t.Fatalf("restored GET has stale publication time: %v %v", modified, err)
	}
	w = signed(t, s, c, "GET", "", "?prefix="+url.QueryEscape(key), nil, false)
	var objects listing
	if w.Code != 200 || xml.Unmarshal(w.Body.Bytes(), &objects) != nil || len(objects.Contents) != 1 {
		t.Fatalf("restored listing: %d %s", w.Code, w.Body.String())
	}
	listed, err := time.Parse(time.RFC3339Nano, objects.Contents[0].LastModified)
	if err != nil || listed.Before(repairStarted) || !listed.Truncate(time.Second).Equal(modified) {
		t.Fatalf("listing and GET publication differ: %v %v %v", listed, modified, err)
	}
	var created time.Time
	if e = s.DB.QueryRowContext(ctx, `SELECT created_at FROM boox_blob_version WHERE id=$1`, version).Scan(&created); e != nil || !created.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("retained provenance changed: %v %v", created, e)
	}
	// Never permit callers to route public cookies to a Gateway management path.
	req, _ = http.NewRequest("GET", proxy.URL+"/boox-neocloud/_config", nil)
	req.AddCookie(&http.Cookie{Name: envelope.Data.Cookie, Value: envelope.Data.Session})
	res, e = http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatal("admin proxy exposed")
	}
}
