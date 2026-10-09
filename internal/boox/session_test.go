package boox

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestStableSessionRenewalGuardsAndRestart(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	p := enrollTest(t, s, uuid.NewString())
	bearer := signinTest(t, s, p, "fixture")
	d, err := s.auth(ctx, "native", bearer, "")
	if err != nil {
		t.Fatal(err)
	}
	var issued atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/neocloud/_session" && r.Method == "POST" {
			issued.Add(1)
			reply(w, 200, map[string]any{"cookie_name": "SyncGatewaySession", "session_id": uuid.NewString(), "expires": time.Now().Add(nativeBackendSessionTTL)})
			return
		}
		reply(w, 200, map[string]any{})
	}))
	defer gateway.Close()
	s.Config.GatewayAdminURL = gateway.URL
	result, err := s.session(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	front := result.(map[string]string)["session_id"]
	first, err := s.sessionBackend(ctx, front, "")
	if err != nil {
		t.Fatal(err)
	}
	if front == first || issued.Load() != 1 {
		t.Fatal("invalid initial session separation")
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE boox_grant SET backend_expires_at=now() WHERE id=$1`, front); err != nil {
		t.Fatal(err)
	}
	// Multiple native streams can reconnect concurrently. Exactly one renewal wins.
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func() {
			b, e := s.sessionBackend(ctx, front, "")
			if e != nil {
				results <- ""
			} else {
				results <- b
			}
		}()
	}
	refreshed := ""
	for i := 0; i < 8; i++ {
		b := <-results
		if b == "" || b == first {
			t.Fatal("renewal failed")
		}
		if refreshed != "" && refreshed != b {
			t.Fatal("competing renewals")
		}
		refreshed = b
	}
	if issued.Load() != 2 {
		t.Fatal("redundant backend sessions")
	}
	restarted := Service{Config: s.Config, DB: s.DB, Objects: s.Objects, LibraryID: s.LibraryID}
	if err = restarted.ResolveIdentity(ctx); err != nil {
		t.Fatal(err)
	}
	if b, e := restarted.sessionBackend(ctx, front, ""); e != nil || b != refreshed {
		t.Fatal("session depends on process memory")
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE boox_grant SET expires_at=now()+interval '2 hours' WHERE id=$1`, front); err != nil {
		t.Fatal(err)
	}
	if err = s.keepSessionActive(ctx, front); err != nil {
		t.Fatal(err)
	}
	var lifetime float64
	if err = s.DB.QueryRowContext(ctx, `SELECT extract(epoch from expires_at-now()) FROM boox_grant WHERE id=$1`, front).Scan(&lifetime); err != nil {
		t.Fatal(err)
	}
	if lifetime < nativeSessionIdleTTL.Seconds()-60 {
		t.Fatal("active handle not maintained")
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE boox_grant SET expires_at=now()-interval '1 second' WHERE id=$1`, front); err != nil {
		t.Fatal(err)
	}
	if _, err = s.sessionBackend(ctx, front, refreshed); !errors.Is(err, errAuth) {
		t.Fatal("expired front handle resurrected")
	}
	if err = s.keepSessionActive(ctx, front); err != nil {
		t.Fatal(err)
	}
	if _, err = s.sessionBackend(ctx, front, ""); !errors.Is(err, errAuth) {
		t.Fatal("keepalive revived expired handle")
	}
}
func TestAccountExpiryIsStoredDeadlineAndRefreshUsesProductionLifetime(t *testing.T) {
	s := testService(t)
	p := enrollTest(t, s, uuid.NewString())
	b := signinTest(t, s, p, "fixture")
	ctx := context.Background()
	var deadline time.Time
	if err := s.DB.QueryRowContext(ctx, `SELECT bearer_expires FROM boox_device WHERE id=$1`, p["deviceId"]).Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	if time.Until(deadline) < nativeAccountTTL-time.Minute {
		t.Fatal("lab token lifetime leaked into production")
	}
	headers := map[string]string{"Authorization": b}
	w := request(t, s, "GET", "/api/1/users/me", nil, headers)
	var body struct {
		Expired int64 `json:"tokenExpiredAt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Expired != deadline.Unix() {
		t.Fatal("response invented a later token deadline")
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE boox_device SET bearer_expires=now()-interval '1 second' WHERE id=$1`, p["deviceId"]); err != nil {
		t.Fatal(err)
	}
	if w = request(t, s, "GET", "/api/1/token/refresh", nil, headers); w.Code != 200 {
		t.Fatal("expired bearer could not refresh", w.Code)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE boox_device SET revoked=true WHERE id=$1`, p["deviceId"]); err != nil {
		t.Fatal(err)
	}
	if w = request(t, s, "GET", "/api/1/token/refresh", nil, headers); w.Code != 401 {
		t.Fatal("revoked bearer refreshed", w.Code)
	}
}
