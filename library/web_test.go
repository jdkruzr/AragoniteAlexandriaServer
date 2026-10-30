package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

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
		"/library/overview":                                     "<span class=\"stat\">1</span><br><span class=\"stat-label\">notebooks</span>",
		"/files/forestnote/devices":                             "<h2>Connect a Device</h2>",
		"/settings/integrations":                                "/caldav/user/calendars/tasks/",
		"/settings/integrations?events":                         "/caldav/user/calendars/events/",
		"/calendar":                                             "No events.",
		"/calendar?show=deleted":                                "Deleted Events",
		"/help":                                                 "Handwriting recognition",
		"/":                                                     "Nothing to do",
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
	if got := b.do("GET", "/library/overview", "", nil, true); got.Code != 200 || !strings.Contains(got.Body.String(), "Your Library") || !strings.Contains(got.Body.String(), "Alexandria Client") {
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
	// Calendar: add a timed event in the owner's zone, see it, edit it.
	if got := b.do("POST", "/settings/caldav", "collection=Tasks&due_mode=preserve&events_collection=Calendar&time_zone=America%2FChicago", sameSite, true); got.Code != 303 {
		t.Fatalf("save time zone: %d", got.Code)
	}
	if page := b.do("GET", "/settings/integrations", "", nil, true).Body.String(); !strings.Contains(page, `<option value="America/Chicago" selected>Chicago (UTC`) || !strings.Contains(page, `<optgroup label="Europe">`) {
		t.Fatalf("time zone menu:\n%s", page)
	}
	if got := b.do("POST", "/settings/caldav", "collection=Tasks&due_mode=preserve&time_zone=Mars%2FOlympus", sameSite, true); got.Code != 303 || !strings.Contains(got.Header().Get("Location"), "Choose+a+time+zone") {
		t.Fatalf("unknown zone: %d %s", got.Code, got.Header().Get("Location"))
	}
	if got := b.do("POST", "/calendar", "title=Plant+garlic&date=2099-10-22&start=15:00&location=Bed+3", sameSite, true); got.Code != 303 || !strings.Contains(got.Header().Get("Location"), "Event+added") {
		t.Fatalf("add event: %d %s", got.Code, got.Header().Get("Location"))
	}
	landing := b.do("GET", "/calendar", "", nil, true).Body.String()
	if strings.Contains(landing, "Plant garlic") {
		t.Fatal("event in 2099 leaked into the bounded upcoming horizon")
	}
	cal := b.do("GET", "/calendar?date=2099-10-22&view=month", "", nil, true).Body.String()
	if !strings.Contains(cal, "Plant garlic") || !strings.Contains(cal, "2099-10-22 15:00 -0500 – 16:00 -0500") || !strings.Contains(cal, "America/Chicago") {
		t.Fatalf("calendar page:\n%s", cal)
	}
	match := regexp.MustCompile(`href="(/calendar/[^"/]+/edit\?[^"]*)"`).FindStringSubmatch(cal)
	if len(match) != 2 {
		t.Fatal("missing event edit link")
	}
	editLink := html.UnescapeString(match[1])
	if got := b.do("GET", editLink, "", nil, true); got.Code != 200 || !strings.Contains(got.Body.String(), `value="15:00"`) {
		t.Fatalf("edit page: %d", got.Code)
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
	if got := b.do("GET", "/tasks", "", nil, true); !strings.Contains(got.Body.String(), "Show 1 done") {
		t.Fatal("completed task not counted as done")
	}
	if got := b.do("GET", "/?done=show", "", nil, true); !strings.Contains(got.Body.String(), "Clear completed tasks") || !strings.Contains(got.Body.String(), "Save seeds") {
		t.Fatal("completed task not listed when done tasks are shown")
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

func TestClientLibraryNotebooks(t *testing.T) {
	r, db, _ := fixture(t)
	b := browser{t, r}
	ctx := context.Background()
	nb := func(id, name string, folder any, created float64) relay.Op {
		return relay.Op{Table: "notebook", PK: id, Cols: map[string]any{"name": name, "sort_order": float64(0), "created_at": created, "deleted_at": nil, "folder_id": folder, "aspect_long_axis": nil, "page_width": float64(1000), "page_height": float64(1200)}}
	}
	ops := []relay.Op{
		{Table: "folder", PK: "00000000000000000000000FD1", Cols: map[string]any{"name": "Synthetic <Garden>", "parent_folder_id": nil, "sort_order": float64(0), "created_at": float64(1), "deleted_at": nil}},
		nb("00000000000000000000000NB1", "Synthetic seed list", "00000000000000000000000FD1", 2),
		nb("00000000000000000000000NB2", "Synthetic root note", nil, 1),
	}
	for i := 1; i <= 10; i++ {
		ops = append(ops, relay.Op{Table: "page", PK: fmt.Sprintf("0000000000000000000000PG%02d", i), Cols: map[string]any{"notebook_id": "00000000000000000000000NB1", "sort_order": float64(i), "created_at": float64(1), "deleted_at": nil, "template": nil, "template_pitch_mm": nil}})
	}
	if _, err := (relay.Store{DB: db}).AuthorOps(ctx, ops); err != nil {
		t.Fatal(err)
	}
	// Every notebook at once, each with its folder; or one folder at a time.
	all := html.UnescapeString(b.do("GET", "/files/forestnote?sort=created&order=asc", "", nil, true).Body.String())
	if a, c := strings.Index(all, "Synthetic root note"), strings.Index(all, "Synthetic seed list"); a < 0 || c < a || !strings.Contains(all, "Synthetic <Garden> · Partly searchable") || !strings.Contains(all, "Notebook root · ") {
		t.Fatalf("flat list:\n%s", all)
	}
	if folders := b.do("GET", "/files/forestnote?view=folders", "", nil, true).Body.String(); !strings.Contains(folders, "Synthetic &lt;Garden&gt;") || strings.Contains(folders, "Synthetic seed list") {
		t.Fatal("folder view")
	}
	sel := html.UnescapeString(b.do("GET", "/files/forestnote?selected=00000000000000000000000NB1", "", nil, true).Body.String())
	if !strings.Contains(sel, `id="preview"`) || strings.Count(sel, "/files/forestnote/thumb?page=") != 8 || !strings.Contains(sel, "All 10 pages") || !strings.Contains(sel, "0 of 10 pages searchable") {
		t.Fatalf("preview:\n%s", sel)
	}
	thumb := b.do("GET", "/files/forestnote/thumb?page=0000000000000000000000PG01", "", nil, true)
	if thumb.Code != 200 || thumb.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("thumb: %d", thumb.Code)
	}
	if got := b.do("GET", "/files/forestnote/thumb?page=missing", "", nil, true); got.Code != 404 {
		t.Fatalf("missing thumb: %d", got.Code)
	}
	// Deleting from the preview returns to the same list, without the selection.
	got := b.do("POST", "/files/forestnote/delete", "notebook=00000000000000000000000NB1&return="+url.QueryEscape("/files/forestnote?sort=name&selected=00000000000000000000000NB1"), sameSite, true)
	if loc := got.Header().Get("Location"); got.Code != 303 || !strings.HasPrefix(loc, "/files/forestnote?sort=name&notice=") {
		t.Fatalf("delete: %d %s", got.Code, loc)
	}
	if got := b.do("POST", "/files/forestnote/delete", "notebook=00000000000000000000000NB2&return="+url.QueryEscape("https://evil.example/files/forestnote"), sameSite, true); !strings.HasPrefix(got.Header().Get("Location"), "/files/forestnote?notice=") {
		t.Fatalf("unsafe return: %s", got.Header().Get("Location"))
	}
	// The Library opens on the source last shown; without BOOX, the Client.
	if got := b.do("GET", "/library", "", nil, true); got.Code != 303 || got.Header().Get("Location") != "/files/forestnote" {
		t.Fatalf("library home: %d %s", got.Code, got.Header().Get("Location"))
	}
	page := b.do("GET", "/files/forestnote", "", nil, true)
	var remembered bool
	for _, c := range page.Result().Cookies() {
		remembered = remembered || c.Name == "alexandria_library_source" && c.Value == "client"
	}
	if !remembered || strings.Contains(page.Body.String(), `class="source-switch"`) {
		t.Fatal("source not remembered, or a switch with no BOOX source")
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
	if old := b.do("GET", "/settings/devices", "", nil, true); old.Code != 301 || old.Header().Get("Location") != "/files/forestnote/devices" {
		t.Fatalf("old devices path: %d %s", old.Code, old.Header().Get("Location"))
	}
	got := b.do("GET", "/files/forestnote/devices", "", nil, true)
	if got.Code != 200 || !strings.Contains(got.Body.String(), site) || !strings.Contains(got.Body.String(), "Active") {
		t.Fatalf("devices: %d %s", got.Code, got.Body)
	}
	if got := b.do("POST", "/settings/devices/revoke", "site_id="+site, sameSite, true); got.Code != 303 || !strings.HasPrefix(got.Header().Get("Location"), "/files/forestnote/devices") {
		t.Fatalf("revoke: %d", got.Code)
	}
	var revoked bool
	if err := db.QueryRow(`SELECT revoked FROM sync_device_identity WHERE site_id=$1`, site).Scan(&revoked); err != nil || !revoked {
		t.Fatal("not revoked", err)
	}
	if got := b.do("GET", "/files/forestnote/devices", "", nil, true); !strings.Contains(got.Body.String(), "Revoked") {
		t.Fatal("revoked state not shown")
	}
}

func TestDirectBOOXBrowserLinkChallengesForOwnerLogin(t *testing.T) {
	r, _, _ := fixture(t)
	r.cfg.NativeBOOX = &NativeBOOXConfig{PublicURL: "http://library.test", Database: "neocloud"}
	b := browser{t, r}
	for _, path := range []string{"/boox", "/boox/notebook?id=missing"} {
		w := b.do("GET", path, "", nil, false)
		if w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), "Basic realm=") {
			t.Fatalf("%s: %d missing login challenge", path, w.Code)
		}
	}
}

func TestBOOXBookWebDAVRequiresTheOwnerAccount(t *testing.T) {
	r, _, _ := fixture(t)
	r.cfg.NativeBOOX = &NativeBOOXConfig{PublicURL: "http://library.test", Database: "neocloud"}
	b := browser{t, r}
	if w := b.do("PROPFIND", "/dav/books/", "", map[string]string{"Depth": "1"}, false); w.Code != 401 || !strings.Contains(w.Header().Get("WWW-Authenticate"), "Basic realm=") {
		t.Fatalf("unauthenticated WebDAV: %d", w.Code)
	}
	if w := b.do("PROPFIND", "/dav/books/", "", map[string]string{"Depth": "1"}, true); w.Code != 207 || !strings.Contains(w.Body.String(), "/dav/books/.Trash/") {
		t.Fatalf("owner WebDAV: %d %s", w.Code, w.Body.String())
	}
}

// The Tasks home page: bulk actions, restore, and a permanent delete that a
// stale CalDAV client can't undo.
func TestTasksBulkActionsAndPermanentDelete(t *testing.T) {
	r, db, _ := fixture(t)
	b := browser{t, r}
	for _, title := range []string{"Synthetic alpha", "Synthetic beta"} {
		if got := b.do("POST", "/tasks", "title="+url.QueryEscape(title)+"&return=%2F%3Fsort%3Dtitle", sameSite, true); got.Code != 303 || got.Header().Get("Location") != "/?sort=title" {
			t.Fatalf("create %s: %d %s", title, got.Code, got.Header().Get("Location"))
		}
	}
	ids := map[string]string{}
	rows, err := db.Query(`SELECT title, task_id FROM alexandria_tasks`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var title, id string
		if err := rows.Scan(&title, &id); err != nil {
			t.Fatal(err)
		}
		ids[title] = id
	}
	rows.Close()
	alpha, beta := ids["Synthetic alpha"], ids["Synthetic beta"]
	home := b.do("GET", "/?sort=title&dir=desc", "", nil, true).Body.String()
	if i, j := strings.Index(home, "Synthetic beta"), strings.Index(home, "Synthetic alpha"); i < 0 || j < 0 || i > j {
		t.Fatal("title sort, descending: beta should come first")
	}
	if !strings.Contains(home, `value="`+alpha+`" form="task-bulk"`) || !strings.Contains(home, `href="/?dir=desc&amp;edit=`+alpha+`&amp;sort=title#task-`+alpha+`"`) {
		t.Fatal("home page lacks task checkboxes or editor links")
	}
	bulk := func(action string, extra string, ids ...string) *httptest.ResponseRecorder {
		form := "action=" + action + "&return=%2F" + extra
		for _, id := range ids {
			form += "&id=" + url.QueryEscape(id)
		}
		return b.do("POST", "/tasks/bulk", form, sameSite, true)
	}
	if got := bulk("complete", "", alpha, beta); got.Code != 303 || !strings.Contains(got.Header().Get("Location"), "notice_n=2") {
		t.Fatalf("bulk complete: %d %s", got.Code, got.Header().Get("Location"))
	}
	if got := bulk("reopen", "", beta); got.Code != 303 {
		t.Fatalf("bulk reopen: %d", got.Code)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM alexandria_tasks WHERE task_id=$1`, beta).Scan(&status); err != nil || status != "needsAction" {
		t.Fatalf("reopened status %q %v", status, err)
	}
	if got := bulk("delete", "", alpha, beta); got.Code != 303 {
		t.Fatalf("bulk delete: %d", got.Code)
	}
	if got := bulk("restore", "", beta); got.Code != 303 {
		t.Fatalf("bulk restore: %d", got.Code)
	}
	if home := b.do("GET", "/", "", nil, true).Body.String(); !strings.Contains(home, "Deleted tasks (1)") {
		t.Fatal("the open view counts deleted tasks")
	}
	// Permanent delete needs its confirmation box ticked, then purges only what is in the trash.
	if got := bulk("purge", "", alpha, beta); got.Code != 303 || !strings.Contains(got.Header().Get("Location"), "Tick+the+box") {
		t.Fatalf("purge without confirmation: %d %s", got.Code, got.Header().Get("Location"))
	}
	if body := b.do("GET", "/?show=deleted", "", nil, true).Body.String(); !strings.Contains(body, "Synthetic alpha") || !strings.Contains(body, `name="confirm" value="yes" required`) {
		t.Fatal("an unconfirmed purge removed the task, or the trash lacks its confirmation box")
	}
	if got := bulk("purge", "&confirm=yes", alpha); got.Code != 303 || !strings.Contains(got.Header().Get("Location"), "notice_n=1") {
		t.Fatalf("purge: %d %s", got.Code, got.Header().Get("Location"))
	}
	if body := b.do("GET", "/?show=deleted", "", nil, true).Body.String(); strings.Contains(body, "Synthetic alpha") {
		t.Fatal("purged task still in Deleted Tasks")
	}
	if got := bulk("restore", "", alpha); got.Code != 303 || !strings.Contains(got.Header().Get("Location"), "notice_n=0") {
		t.Fatalf("restore after purge: %d %s", got.Code, got.Header().Get("Location"))
	}
	// A device that never heard of the deletion PUTs the task again: refused.
	vtodo := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//test//EN\r\nBEGIN:VTODO\r\nUID:" + alpha + "\r\nSUMMARY:Synthetic alpha\r\n" +
		"DTSTAMP:20261001T000000Z\r\nSTATUS:NEEDS-ACTION\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	if got := b.do("PUT", "/caldav/user/calendars/tasks/"+alpha+".ics", vtodo, map[string]string{"Content-Type": "text/calendar"}, true); got.Code != 412 {
		t.Fatalf("stale PUT of a purged task: %d %s", got.Code, got.Body)
	}
	var title sql.NullString
	if err := db.QueryRow(`SELECT title FROM alexandria_tasks WHERE task_id=$1 AND deleted`, alpha).Scan(&title); err != nil || title.Valid {
		t.Fatalf("purged tombstone: %v %v", title, err)
	}
	// Edit keeps the view it came from.
	if got := b.do("POST", "/tasks/"+beta+"/edit", "title=Synthetic+gamma&due=2026-11-01&detail=notes&return=%2F%3Fsort%3Dupdated", sameSite, true); got.Code != 303 || !strings.HasPrefix(got.Header().Get("Location"), "/?sort=updated") {
		t.Fatalf("edit: %d %s", got.Code, got.Header().Get("Location"))
	}
	// The editor is inline: old edit links land on it, and an empty title returns to it.
	if got := b.do("GET", "/tasks/"+beta+"/edit?return=%2F%3Fsort%3Dupdated", "", nil, true); got.Code != 303 || got.Header().Get("Location") != "/?edit="+beta+"&sort=updated#task-"+beta {
		t.Fatalf("edit link: %d %s", got.Code, got.Header().Get("Location"))
	}
	if got := b.do("GET", "/?edit="+beta, "", nil, true); got.Code != 200 || !strings.Contains(got.Body.String(), `value="2026-11-01"`) || !strings.Contains(got.Body.String(), `formaction="/tasks/`+beta+`/delete"`) {
		t.Fatalf("inline editor: %d", got.Code)
	}
	if got := b.do("POST", "/tasks/"+beta+"/edit", "title=&return=%2F%3Fsort%3Dupdated", sameSite, true); got.Code != 303 || !strings.Contains(got.Header().Get("Location"), "edit="+beta) {
		t.Fatalf("empty title: %d %s", got.Code, got.Header().Get("Location"))
	}
}

// Today shows the next seven days of events, where note-action tasks came
// from (with the page number), and Client devices that need a restore.
func TestTodayEventsSourcesAndAttention(t *testing.T) {
	r, db, _ := fixture(t)
	b := browser{t, r}
	if got := b.do("POST", "/settings/caldav", "collection=Tasks&due_mode=preserve&events_collection=Calendar&time_zone=America%2FChicago", sameSite, true); got.Code != 303 {
		t.Fatalf("save time zone: %d", got.Code)
	}
	chicago, _ := time.LoadLocation("America/Chicago")
	soon, later := time.Now().In(chicago).AddDate(0, 0, 2), time.Now().In(chicago).AddDate(0, 0, 9)
	for title, day := range map[string]time.Time{"Synthetic soon": soon, "Synthetic later": later} {
		if got := b.do("POST", "/calendar", "title="+url.QueryEscape(title)+"&date="+day.Format("2006-01-02")+"&start=14:00", sameSite, true); got.Code != 303 {
			t.Fatalf("add %s: %d", title, got.Code)
		}
	}
	if got := b.do("POST", "/tasks", "title=Synthetic+from+ink", sameSite, true); got.Code != 303 {
		t.Fatalf("create task: %d", got.Code)
	}
	var id string
	if err := db.QueryRow(`SELECT task_id FROM alexandria_tasks WHERE title='Synthetic from ink'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO boox_page_index(notebook_id,page_id,page_number,state,text,input_version) VALUES ('synthnb','synthpage',7,'ready','','synthetic')`,
		`INSERT INTO alexandria_note_action_item(source,notebook_id,page_id,color,fingerprint,kind,item_id) VALUES ('boox','synthnb','synthpage','red','f1','task','` + id + `')`,
		`INSERT INTO sync_library_generation(id,generation) VALUES (1,repeat('a',64)) ON CONFLICT (id) DO NOTHING`,
		`INSERT INTO sync_device_identity(site_id,token_hash,created_at) VALUES ('synthsite1',repeat('1',64),0),('synthsite2',repeat('2',64),0),('synthsite3',repeat('3',64),0)`,
		`UPDATE sync_device_identity SET revoked=true WHERE site_id='synthsite3'`,
		`INSERT INTO sync_device_generation(site_id,generation) SELECT 'synthsite1',generation FROM sync_library_generation`,
		`INSERT INTO sync_device_generation(site_id,generation) VALUES ('synthsite2',repeat('b',64)),('synthsite3',repeat('b',64))`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	home := b.do("GET", "/", "", nil, true).Body.String()
	if !strings.Contains(home, "Synthetic soon") || strings.Contains(home, "Synthetic later") {
		t.Fatal("Next 7 days should list only this week's event")
	}
	// It follows the calendars Calendar's event list hides in this browser, and says so.
	hiddenLocal := b.do("GET", "/", "", map[string]string{"Cookie": "alexandria_calendar_sources_list=local"}, true).Body.String()
	if strings.Contains(hiddenLocal, "Synthetic soon") || !strings.Contains(hiddenLocal, "1 calendar hidden by Calendar&#39;s event filter") {
		t.Fatal("a calendar hidden in Calendar's event list should be left out of Next 7 days, with a note")
	}
	if !strings.Contains(home, "Notebook page, p. 7") {
		t.Fatal("the note-action task lacks its page number")
	}
	if !strings.Contains(home, "1 Client device needs a restore") {
		t.Fatal("the attention notice should count the one unrevoked device on an old generation")
	}
	// The recognition part links to pages that are there: blocked ones when none failed.
	job := func(state string) {
		if _, err := db.Exec(`INSERT INTO alexandria_recognition_job(id,source,notebook_id,page_id,state) VALUES (gen_random_uuid(),'boox','synthnb',gen_random_uuid()::text,$1)`, state); err != nil {
			t.Fatal(err)
		}
	}
	job("blocked")
	if home = b.do("GET", "/", "", nil, true).Body.String(); !strings.Contains(home, `<a href="/jobs?state=blocked">1 page needs attention in Recognition</a>`) {
		t.Fatal("only blocked pages: the notice should open the blocked list")
	}
	job("failed")
	if home = b.do("GET", "/", "", nil, true).Body.String(); !strings.Contains(home, `<a href="/jobs?state=failed">2 pages need attention in Recognition</a>`) || !strings.Contains(home, `nav-count" title="2 pages need attention">2</span>`) {
		t.Fatal("failed and blocked: the notice and badge count both and open the failed list")
	}
}
