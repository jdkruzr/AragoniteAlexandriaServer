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
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskattach"
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

func TestMCPOverHTTPRequiresAPIAuthentication(t *testing.T) {
	r, _, _ := fixture(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	send := func(auth bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if auth {
			req.SetBasicAuth("author", "a-long-test-password")
		}
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	if got := send(false); got.Code != 401 {
		t.Fatalf("anonymous MCP: %d", got.Code)
	}
	if got := send(true); got.Code != 200 || !strings.Contains(got.Body.String(), "aragonite-alexandria") {
		t.Fatalf("MCP initialize: %d %s", got.Code, got.Body)
	}
}

func TestCalDAVTasksRoundTripThroughRuntime(t *testing.T) {
	r, db, _ := fixture(t)
	call := func(method, path, body string, auth bool, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if auth {
			req.SetBasicAuth("author", "a-long-test-password")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	vtodo := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VTODO\r\nUID:task-1\r\nSUMMARY:Water the figs\r\n" +
		"DTSTAMP:20261001T000000Z\r\nSTATUS:NEEDS-ACTION\r\nX-FORESTNOTE-NOTEBOOK-ID:00000000000000000000000NB1\r\n" +
		"X-FORESTNOTE-PAGE-ID:00000000000000000000000PG1\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	if got := call("PUT", "/caldav/user/calendars/tasks/task-1.ics", vtodo, false, nil); got.Code != 401 {
		t.Fatalf("anonymous PUT: %d", got.Code)
	}
	if got := call("PUT", "/caldav/user/calendars/tasks/task-1.ics", vtodo, true, map[string]string{"Content-Type": "text/calendar"}); got.Code/100 != 2 {
		t.Fatalf("PUT: %d %s", got.Code, got.Body)
	}
	var title, notebook string
	if err := db.QueryRow(`SELECT title, forestnote_notebook_id FROM alexandria_tasks WHERE task_id='task-1'`).Scan(&title, &notebook); err != nil || title != "Water the figs" || notebook != "00000000000000000000000NB1" {
		t.Fatal(title, notebook, err)
	}
	if got := call("GET", "/caldav/user/calendars/tasks/task-1.ics", "", true, nil); got.Code != 200 || !strings.Contains(got.Body.String(), "SUMMARY:Water the figs") {
		t.Fatalf("GET: %d %s", got.Code, got.Body)
	}
	propfind := `<?xml version="1.0"?><d:propfind xmlns:d="DAV:"><d:prop><d:displayname/><d:sync-token/></d:prop></d:propfind>`
	if got := call("PROPFIND", "/caldav/user/calendars/tasks/", propfind, true, map[string]string{"Depth": "0", "Content-Type": "application/xml"}); got.Code != 207 || !strings.Contains(got.Body.String(), "Tasks") {
		t.Fatalf("PROPFIND: %d %s", got.Code, got.Body)
	}
	if got := call("GET", "/.well-known/caldav", "", true, nil); got.Code != 301 || got.Header().Get("Location") != "/caldav/" {
		t.Fatalf("well-known: %d %s", got.Code, got.Header().Get("Location"))
	}
	// Signed public attachment URLs: no credentials, signature required.
	sha := strings.Repeat("a", 64)
	if got := call("GET", "/api/v1/attachments/"+sha+"?sig=forged", "", false, nil); got.Code != 403 {
		t.Fatalf("forged signature: %d", got.Code)
	}
	secret := r.attachSecret
	signed := (taskattach.Signer{Secret: secret}).SignedAttachmentPath(sha)
	if got := call("GET", signed, "", false, nil); got.Code != 200 || got.Body.String() != "hello" {
		t.Fatalf("signed download: %d %s", got.Code, got.Body)
	}
	if got := call("GET", (taskattach.Signer{Secret: secret}).SignedFNRenderPath("forestnote://nb/missing"), "", false, nil); got.Code != 404 {
		t.Fatalf("render of a missing page: %d", got.Code)
	}
	// The secret is stable across restarts.
	again, err := Open(context.Background(), Config{ID: r.cfg.ID, DatabaseURL: r.cfg.DatabaseURL, Objects: emptyObjects{}, MaxConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.attachSecret != secret || len(secret) != 64 {
		t.Fatal("attachment secret changed on reopen")
	}
}

func TestCalDAVEventsThroughRuntime(t *testing.T) {
	r, db, _ := fixture(t)
	call := func(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.SetBasicAuth("author", "a-long-test-password")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	ics := func(comp, uid, start string) string {
		return "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:" + comp + "\r\nUID:" + uid + "\r\nDTSTAMP:20261001T000000Z\r\n" +
			"DTSTART:" + start + "\r\nSUMMARY:Synthetic " + uid + "\r\nEND:" + comp + "\r\nEND:VCALENDAR\r\n"
	}
	cal := map[string]string{"Content-Type": "text/calendar"}
	xml := func(depth string) map[string]string {
		return map[string]string{"Depth": depth, "Content-Type": "application/xml"}
	}
	// Discovery lists both collections.
	home := call("PROPFIND", "/caldav/user/calendars/", `<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:displayname/><c:supported-calendar-component-set/></d:prop></d:propfind>`, xml("1"))
	if home.Code != 207 || !strings.Contains(home.Body.String(), "/caldav/user/calendars/events/") || !strings.Contains(home.Body.String(), "/caldav/user/calendars/tasks/") || !strings.Contains(home.Body.String(), "VEVENT") {
		t.Fatalf("home: %d %s", home.Code, home.Body)
	}
	if got := call("PUT", "/caldav/user/calendars/events/ev-1.ics", ics("VEVENT", "ev-1", "20261022T200000Z"), cal); got.Code/100 != 2 {
		t.Fatalf("PUT event: %d %s", got.Code, got.Body)
	}
	call("PUT", "/caldav/user/calendars/events/ev-2.ics", ics("VEVENT", "ev-2", "20261201T200000Z"), cal)
	if got := call("PUT", "/caldav/user/calendars/events/td.ics", ics("VTODO", "td", "20261022T200000Z"), cal); got.Code/100 == 2 {
		t.Fatalf("VTODO accepted into events: %d", got.Code)
	}
	if got := call("PUT", "/caldav/user/calendars/tasks/ev-3.ics", ics("VEVENT", "ev-3", "20261022T200000Z"), cal); got.Code/100 == 2 {
		t.Fatalf("VEVENT accepted into tasks: %d", got.Code)
	}
	var title string
	var start time.Time
	if err := db.QueryRow(`SELECT title,start_at FROM alexandria_events WHERE event_id='ev-1'`).Scan(&title, &start); err != nil || title != "Synthetic ev-1" || !start.Equal(time.Date(2026, 10, 22, 20, 0, 0, 0, time.UTC)) {
		t.Fatal(title, start, err)
	}
	query := `<?xml version="1.0"?><c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:getetag/></d:prop>
<c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="20261020T000000Z" end="20261031T000000Z"/></c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`
	if got := call("REPORT", "/caldav/user/calendars/events/", query, xml("1")); got.Code != 207 || !strings.Contains(got.Body.String(), "ev-1.ics") || strings.Contains(got.Body.String(), "ev-2.ics") {
		t.Fatalf("time-range: %d %s", got.Code, got.Body)
	}
	sync := `<?xml version="1.0"?><d:sync-collection xmlns:d="DAV:"><d:sync-token/><d:prop><d:getetag/></d:prop></d:sync-collection>`
	first := call("REPORT", "/caldav/user/calendars/events/", sync, xml("1"))
	body := first.Body.String()
	if first.Code != 207 || !strings.Contains(body, "ev-1.ics") || strings.Contains(body, "/tasks/") {
		t.Fatalf("sync: %d %s", first.Code, body)
	}
	token := body[strings.Index(body, "<D:sync-token>")+len("<D:sync-token>") : strings.Index(body, "</D:sync-token>")]
	if got := call("DELETE", "/caldav/user/calendars/events/ev-1.ics", "", nil); got.Code/100 != 2 {
		t.Fatalf("DELETE: %d", got.Code)
	}
	next := call("REPORT", "/caldav/user/calendars/events/", strings.Replace(sync, "<d:sync-token/>", "<d:sync-token>"+token+"</d:sync-token>", 1), xml("1"))
	if !strings.Contains(next.Body.String(), "404 Not Found") {
		t.Fatalf("deletion not reported: %s", next.Body)
	}
	if got := call("PUT", "/caldav/user/calendars/events/ev-1.ics", ics("VEVENT", "ev-1", "20261022T200000Z"), cal); got.Code != 412 {
		t.Fatalf("deleted event recreated: %d", got.Code)
	}
}
