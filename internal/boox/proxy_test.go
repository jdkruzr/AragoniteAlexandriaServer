package boox

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestActiveBLIPRevocationClosesSocket(t *testing.T) {
	s := testService(t)
	p := enrollTest(t, s, "c3d3d214-c182-4f02-b03e-117fa4392f37")
	grant := "synthetic-session-cookie"
	if _, e := s.DB.ExecContext(context.Background(), `INSERT INTO boox_grant(id,device_id,kind,secret,expires_at,backend_session_id,backend_expires_at) VALUES($1,$2,'session','',now()+interval '1 hour',$1,now()+interval '1 hour')`, grant, p["deviceId"]); e != nil {
		t.Fatal(e)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "SyncGatewaySession="+grant || r.URL.Path != "/neocloud/_blipsync" {
			http.Error(w, "invalid forwarded request", 400)
			return
		}
		conn, rw, e := w.(http.Hijacker).Hijack()
		if e != nil {
			return
		}
		defer conn.Close()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = rw.Flush()
		_, _ = rw.ReadByte()
	}))
	defer upstream.Close()
	s.Config.GatewayPublicURL = upstream.URL
	proxy := httptest.NewServer(http.HandlerFunc(s.ServeHTTP))
	defer proxy.Close()
	conn, e := net.Dial("tcp", strings.TrimPrefix(proxy.URL, "http://"))
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	_, _ = fmt.Fprintf(conn, "GET /boox-neocloud/_blipsync HTTP/1.1\r\nHost: native.test\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nCookie: SyncGatewaySession=%s\r\n\r\n", grant)
	reader := bufio.NewReader(conn)
	response, e := http.ReadResponse(reader, nil)
	if e != nil || response.StatusCode != 101 {
		t.Fatalf("upgrade failed status=%v error=%v", response, e)
	}
	if _, e = s.DB.ExecContext(context.Background(), `UPDATE boox_device SET revoked=true WHERE id=$1`, p["deviceId"]); e != nil {
		t.Fatal(e)
	}
	_, e = reader.ReadByte()
	if e == nil {
		t.Fatal("revoked connection stayed open")
	}
	if n, ok := e.(net.Error); ok && n.Timeout() {
		t.Fatal("revocation did not close the upgraded socket")
	}
}
