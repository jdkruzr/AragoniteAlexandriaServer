package library

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
)

type browser struct {
	t *testing.T
	r *Runtime
}

func (b browser) do(method, target, form string, headers map[string]string, auth bool) *httptest.ResponseRecorder {
	b.t.Helper()
	req := httptest.NewRequest(method, "http://library.test"+target, strings.NewReader(form))
	if form != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if auth {
		req.SetBasicAuth("author", "a-long-test-password")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	out := httptest.NewRecorder()
	b.r.ServeHTTP(out, req)
	return out
}

var sameSite = map[string]string{"Origin": "http://library.test", "Sec-Fetch-Site": "same-origin"}

func TestWebPagesRenderAndFormsRequireSameOrigin(t *testing.T) {
	r, db, _ := fixture(t)
	b := browser{t, r}
	ctx := context.Background()
	if _, err := (relay.Store{DB: db}).AuthorOps(ctx, []relay.Op{
		{Table: "notebook", PK: "00000000000000000000000NB1", Cols: map[string]any{"name": "Seed <Catalogue>", "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "folder_id": nil, "aspect_long_axis": nil, "page_width": nil, "page_height": nil}},
		{Table: "page", PK: "00000000000000000000000PG1", Cols: map[string]any{"notebook_id": "00000000000000000000000NB1", "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil, "template": nil, "template_pitch_mm": nil}},
		{Table: "text_box", PK: "00000000000000000000000TB1", Cols: map[string]any{"page_id": "00000000000000000000000PG1", "x": float64(100), "y": float64(100), "width": float64(3000), "height": float64(800), "text": "heirloom tomatoes",
			"font_name": "", "font_size": float64(300), "color": float64(4278190080), "weight": float64(400), "border_width": float64(0), "z": float64(1), "created_at": float64(1), "deleted_at": nil}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (notes.Pipeline{}).Step(ctx, db); err != nil {
		t.Fatal(err)
	}
	if got := b.do("GET", "/files/forestnote", "", nil, false); got.Code != 401 {
		t.Fatalf("anonymous page: %d", got.Code)
	}
	for path, want := range map[string]string{
		"/files/forestnote": "Seed &lt;Catalogue&gt;",
		"/files/forestnote?notebook=00000000000000000000000NB1": "heirloom tomatoes",
		"/files/forestnote/books":                               "No books yet",
		"/search?q=tomatoes":                                    "Seed &lt;Catalogue&gt;, page 1",
		"/tasks":                                                "Nothing to do",
		"/settings/devices":                                     "No devices have enrolled yet",
		"/settings":                                             "/caldav/user/calendars/tasks/",
	} {
		got := b.do("GET", path, "", nil, true)
		if got.Code != 200 || !strings.Contains(got.Body.String(), want) {
			t.Fatalf("%s: %d, missing %q\n%s", path, got.Code, want, got.Body)
		}
		if !strings.Contains(got.Header().Get("Content-Security-Policy"), "default-src 'self'") || got.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%s: missing security headers", path)
		}
	}
	// Links are escaped exactly once (html/template does it; %25 means twice).
	for _, path := range []string{"/files/forestnote", "/files/forestnote?notebook=00000000000000000000000NB1", "/search?q=tomatoes"} {
		if body := b.do("GET", path, "", nil, true).Body.String(); strings.Contains(body, "%25") {
			t.Fatalf("%s: double-escaped link", path)
		}
	}
	if got := b.do("GET", "/", "", nil, true); got.Code != 303 {
		t.Fatalf("home: %d", got.Code)
	}
	if got := b.do("GET", "/files/forestnote/render?path=forestnote://00000000000000000000000NB1/00000000000000000000000PG1", "", nil, true); got.Code != 200 || got.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("render: %d", got.Code)
	}
	if got := b.do("GET", "/files/forestnote/export?notebook=00000000000000000000000NB1", "", nil, true); got.Code != 200 || !strings.HasPrefix(got.Body.String(), "%PDF") {
		t.Fatalf("export: %d", got.Code)
	}
	if got := b.do("GET", "/static/app.css", "", nil, true); got.Code != 200 {
		t.Fatalf("stylesheet: %d", got.Code)
	}

	// Cross-site and header-less posts are refused; same-origin posts work.
	if got := b.do("POST", "/tasks", "title=Forged", map[string]string{"Origin": "https://evil.example"}, true); got.Code != 403 {
		t.Fatalf("cross-site post: %d", got.Code)
	}
	if got := b.do("POST", "/tasks", "title=Forged", nil, true); got.Code != 403 {
		t.Fatalf("header-less post: %d", got.Code)
	}
	if got := b.do("POST", "/tasks", "title=Save+seeds&due=2026-11-01", sameSite, true); got.Code != 303 {
		t.Fatalf("create task: %d %s", got.Code, got.Body)
	}
	var id string
	if err := db.QueryRow(`SELECT task_id FROM alexandria_tasks WHERE title='Save seeds'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if got := b.do("POST", "/tasks/"+id+"/complete", "", sameSite, true); got.Code != 303 {
		t.Fatalf("complete: %d", got.Code)
	}
	if got := b.do("GET", "/tasks", "", nil, true); !strings.Contains(got.Body.String(), "Clear Completed Tasks") {
		t.Fatal("completed task not listed as done")
	}
	if got := b.do("POST", "/settings/caldav", "collection=Garden+jobs&due_mode=date_only", sameSite, true); got.Code != 303 {
		t.Fatalf("caldav settings: %d", got.Code)
	}
	if got := b.do("POST", "/settings/tokens/create", "label=Laptop", sameSite, true); got.Code != 200 || !strings.Contains(got.Body.String(), "alexandria_") || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token: %d", got.Code)
	}
	if got := b.do("POST", "/files/forestnote/delete", "notebook=00000000000000000000000NB1", sameSite, true); got.Code != 303 {
		t.Fatalf("delete: %d", got.Code)
	}
	if got := b.do("GET", "/files/forestnote", "", nil, true); strings.Contains(got.Body.String(), "Seed &lt;Catalogue&gt;") {
		t.Fatal("deleted notebook still listed")
	}
}

func TestOAuthConsentIssuesAnMCPToken(t *testing.T) {
	r, _, _ := fixture(t)
	r.cfg.PublicURL = "https://library.example"
	b := browser{t, r}
	if got := b.do("POST", "/mcp", `{}`, nil, false); got.Code != 401 || !strings.Contains(got.Header().Get("WWW-Authenticate"), "resource_metadata=\"https://library.example/.well-known/oauth-protected-resource/mcp\"") {
		t.Fatalf("challenge: %d %v", got.Code, got.Header())
	}
	var meta map[string]any
	if got := b.do("GET", "/.well-known/oauth-authorization-server", "", nil, false); got.Code != 200 || json.Unmarshal(got.Body.Bytes(), &meta) != nil || meta["token_endpoint"] != "https://library.example/token" {
		t.Fatalf("discovery: %d %s", got.Code, got.Body)
	}
	if got := b.do("POST", "/register", `{"client_name":"Claude","redirect_uris":["http://evil.example/cb"]}`, nil, false); got.Code != 400 {
		t.Fatalf("plain-HTTP redirect accepted: %d", got.Code)
	}
	reg := b.do("POST", "/register", `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"]}`, nil, false)
	var client struct {
		ClientID string `json:"client_id"`
	}
	if reg.Code != 201 || json.Unmarshal(reg.Body.Bytes(), &client) != nil || client.ClientID == "" {
		t.Fatalf("register: %d %s", reg.Code, reg.Body)
	}
	verifier := "a-sufficiently-long-pkce-verifier-for-this-test-0123456789"
	sum := sha256.Sum256([]byte(verifier))
	query := url.Values{"response_type": {"code"}, "client_id": {client.ClientID}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"},
		"state": {"xyz"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}.Encode()
	if got := b.do("GET", "/authorize?"+query, "", nil, false); got.Code != 401 {
		t.Fatalf("anonymous consent page: %d", got.Code)
	}
	consent := b.do("GET", "/authorize?"+query, "", nil, true)
	if consent.Code != 200 || !strings.Contains(consent.Body.String(), "Allow Access?") || !strings.Contains(consent.Body.String(), "Claude") {
		t.Fatalf("consent: %d %s", consent.Code, consent.Body)
	}
	same := map[string]string{"Sec-Fetch-Site": "same-origin"}
	denied := b.do("POST", "/authorize", url.Values{"query": {query}, "decision": {"deny"}}.Encode(), same, true)
	if denied.Code != 302 || !strings.Contains(denied.Header().Get("Location"), "error=access_denied") {
		t.Fatalf("deny: %d %s", denied.Code, denied.Header().Get("Location"))
	}
	allowed := b.do("POST", "/authorize", url.Values{"query": {query}, "decision": {"allow"}}.Encode(), same, true)
	location, _ := url.Parse(allowed.Header().Get("Location"))
	code := location.Query().Get("code")
	if allowed.Code != 302 || code == "" || location.Query().Get("state") != "xyz" || location.Host != "claude.ai" {
		t.Fatalf("allow: %d %s", allowed.Code, allowed.Header().Get("Location"))
	}
	exchange := func(code, verifier string) *httptest.ResponseRecorder {
		return b.do("POST", "/token", url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {client.ClientID},
			"redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "code_verifier": {verifier}}.Encode(), nil, false)
	}
	tok := exchange(code, verifier)
	var issued struct {
		AccessToken string `json:"access_token"`
	}
	if tok.Code != 200 || json.Unmarshal(tok.Body.Bytes(), &issued) != nil || !strings.HasPrefix(issued.AccessToken, "alexandria_") {
		t.Fatalf("token: %d %s", tok.Code, tok.Body)
	}
	if again := exchange(code, verifier); again.Code != 400 {
		t.Fatalf("replayed code: %d", again.Code)
	}
	// A fresh code with the wrong verifier fails, and is spent.
	allowed = b.do("POST", "/authorize", url.Values{"query": {query}, "decision": {"allow"}}.Encode(), same, true)
	location, _ = url.Parse(allowed.Header().Get("Location"))
	if bad := exchange(location.Query().Get("code"), "wrong-verifier"); bad.Code != 400 {
		t.Fatalf("bad verifier: %d", bad.Code)
	}
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	got := b.do("POST", "/mcp", body, map[string]string{"Authorization": "Bearer " + issued.AccessToken, "Content-Type": "application/json", "Accept": "application/json, text/event-stream"}, false)
	if got.Code != 200 || !strings.Contains(got.Body.String(), "aragonite-alexandria") {
		t.Fatalf("MCP with OAuth token: %d %s", got.Code, got.Body)
	}
}

func TestDevicesPageListsAndRevokes(t *testing.T) {
	r, db, _ := fixture(t)
	b := browser{t, r}
	_, hash := deviceKey("web-device")
	site := "0000000000000000000000000W"
	if got := enroll(r, `{"site_id":"`+site+`","token_hash":"`+hash+`"}`, nil); got.Code != 204 {
		t.Fatal(got.Code)
	}
	got := b.do("GET", "/settings/devices", "", nil, true)
	if got.Code != 200 || !strings.Contains(got.Body.String(), site) || !strings.Contains(got.Body.String(), "Active") {
		t.Fatalf("devices: %d %s", got.Code, got.Body)
	}
	if got := b.do("POST", "/settings/devices/revoke", "site_id="+site, sameSite, true); got.Code != 303 {
		t.Fatalf("revoke: %d", got.Code)
	}
	var revoked bool
	if err := db.QueryRow(`SELECT revoked FROM sync_device_identity WHERE site_id=$1`, site).Scan(&revoked); err != nil || !revoked {
		t.Fatal("not revoked", err)
	}
	if got := b.do("GET", "/settings/devices", "", nil, true); !strings.Contains(got.Body.String(), "Revoked") {
		t.Fatal("revoked state not shown")
	}
}
