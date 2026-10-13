package boox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func cleanupFixture(t *testing.T, s Service, id, title string, overrides map[string]any) map[string]any {
	t.Helper()
	b := map[string]any{"_id": id, "_rev": "1-original", "uniqueId": id, "user": s.uid(), "dbId": s.uid() + "-NOTE_TREE", "type": 1, "status": 1, "title": title, "parentUniqueId": "nested-folder", "opaque": "preserve-me", "pageNameList": map[string]any{"pageNameList": []string{"page"}}}
	for k, v := range overrides {
		b[k] = v
	}
	raw, _ := json.Marshal(b)
	if _, e := s.DB.ExecContext(context.Background(), `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-original','notebook','1',$2,$3)`, id, s.uid(), raw); e != nil {
		t.Fatal(e)
	}
	return b
}
func cleanupForm(t *testing.T, s Service, v url.Values, origin bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "/boox/cleanup", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin {
		r.Header.Set("Origin", s.Config.PublicURL)
	}
	w := httptest.NewRecorder()
	s.Admin(w, r)
	return w
}
func TestCleanupExactMatchingAndReview(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	cleanupFixture(t, s, "yes", "Notebook-001", nil)
	for i, name := range []string{"Notebook-", "Notebook-1 keep", "Notebook-1\n", "notebook-1", "My Notebook-1", "Notebook-１２", "Notebook--1"} {
		cleanupFixture(t, s, fmt.Sprint("no", i), name, nil)
	}
	for key, b := range map[string]map[string]any{"folder": {"type": 0}, "deleted": {"status": 0}, "foreign": {"user": "foreign"}, "reader": {"dbId": s.uid() + "-READER_LIBRARY"}, "commit": {"commitType": 1}, "mismatch": {"uniqueId": "different"}} {
		cleanupFixture(t, s, key, "Notebook-2", b)
	}
	d, e := s.cleanupCandidates(ctx)
	if e != nil || d.Total != 1 || d.Items[0].ID != "yes" {
		t.Fatal(d, e)
	}
	w := httptest.NewRecorder()
	s.Admin(w, httptest.NewRequest("GET", "/boox/cleanup", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Notebook-001") || !strings.Contains(w.Body.String(), "Created (UTC)") {
		t.Fatal(w.Code, w.Body.String())
	}
	var n int
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_operation`).Scan(&n); e != nil || n != 0 {
		t.Fatal("GET created operations", n, e)
	}
	form := url.Values{"action": {"queue"}, "token": {d.Token}, "selected": {d.Items[0].Choice}, "confirm": {"on"}}
	if w = cleanupForm(t, s, form, false); w.Code != 403 {
		t.Fatal("missing CSRF protection", w.Code)
	}
	form.Del("confirm")
	if w = cleanupForm(t, s, form, true); w.Code != 400 {
		t.Fatal("missing confirmation", w.Code)
	}
	form.Set("confirm", "on")
	if w = cleanupForm(t, s, form, true); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = cleanupForm(t, s, form, true); w.Code != 303 {
		t.Fatal("replay", w.Code)
	}
	d, e = s.cleanupStatus(ctx, d.Token)
	if e != nil || d.Total != 1 || d.Pending != 1 {
		t.Fatal(d, e)
	}
	if w = cleanupForm(t, s, url.Values{"action": {"cancel"}, "token": {d.Batch}}, true); w.Code != 303 {
		t.Fatal(w.Code)
	}
	d, e = s.cleanupStatus(ctx, d.Batch)
	if e != nil || d.Cancelled != 1 {
		t.Fatal(d, e)
	}
}
func TestCleanupAtomicStaleSelectionAndConcurrentReplay(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	cleanupFixture(t, s, "a", "Notebook-1", nil)
	cleanupFixture(t, s, "b", "Notebook-2", nil)
	choices := []cleanupChoice{{"a", "1-original"}, {"b", "wrong"}}
	if e := s.queueCleanup(ctx, uuid.NewString(), choices); !errors.Is(e, errCleanupSelection) {
		t.Fatal(e)
	}
	var n int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_operation`).Scan(&n); e != nil || n != 0 {
		t.Fatal("partial selection committed", n, e)
	}
	for _, invalid := range [][]cleanupChoice{nil, {{"a", "1-original"}, {"a", "1-original"}}, {{"unknown", "1-original"}}} {
		if e := s.queueCleanup(ctx, uuid.NewString(), invalid); !errors.Is(e, errCleanupSelection) {
			t.Fatal(e)
		}
	}
	token := uuid.NewString()
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			results <- s.queueCleanup(ctx, token, []cleanupChoice{{"a", "1-original"}, {"b", "1-original"}})
		}()
	}
	for i := 0; i < 4; i++ {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	d, e := s.cleanupStatus(ctx, token)
	if e != nil || d.Total != 2 {
		t.Fatal(d, e)
	}
}
func TestCleanupNativePublicationAndStaleFence(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	docs := map[string]map[string]any{"note": cleanupFixture(t, s, "note", "Notebook-7", nil), "changed": cleanupFixture(t, s, "changed", "Notebook-8", nil)}
	docs["changed"]["_rev"] = "2-newer"
	docs["changed"]["title"] = "Keep this renamed note"
	var mu sync.Mutex
	puts := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		id := strings.TrimPrefix(r.URL.Path, "/neocloud/")
		if id == "_changes" {
			results := []map[string]any{}
			for key, b := range docs {
				results = append(results, map[string]any{"id": key, "changes": []map[string]any{{"rev": b["_rev"]}}})
			}
			reply(w, 200, map[string]any{"last_seq": 1, "results": results})
			return
		}
		switch r.Method {
		case "GET":
			if b, ok := docs[id]; ok {
				reply(w, 200, b)
			} else {
				reply(w, 404, map[string]string{})
			}
		case "PUT":
			var b map[string]any
			if json.NewDecoder(r.Body).Decode(&b) != nil {
				http.Error(w, "bad json", 400)
				return
			}
			if old, ok := docs[id]; ok && b["_rev"] != old["_rev"] {
				http.Error(w, "conflict", 409)
				return
			}
			puts++
			b["_rev"] = fmt.Sprintf("2-%d", puts)
			docs[id] = b
			reply(w, 201, map[string]any{"rev": b["_rev"]})
		default:
			http.Error(w, "unsupported", 405)
		}
	}))
	defer gateway.Close()
	s.Config.GatewayAdminURL = gateway.URL
	token := uuid.NewString()
	if e := s.queueCleanup(ctx, token, []cleanupChoice{{"note", "1-original"}, {"changed", "1-original"}}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if _, e := s.PublishOne(ctx); e != nil {
			t.Fatal(e)
		}
	}
	d, e := s.cleanupStatus(ctx, token)
	if e != nil || d.Published != 1 || d.Blocked != 1 {
		t.Fatal(d, e)
	}
	mu.Lock()
	published := docs["note"]
	count := puts
	mu.Unlock()
	if fmt.Sprint(published["status"]) != "0" || published["opaque"] != "preserve-me" || published["parentUniqueId"] != "nested-folder" || count != 3 {
		t.Fatal("publication must preserve metadata and include commit and MESSAGE", published, count)
	}
	// Replay a lost acknowledgement using the existing stable operation marker.
	if _, e = s.DB.ExecContext(ctx, `UPDATE boox_operation SET state='queued' WHERE document_id='note'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishOne(ctx); e != nil {
		t.Fatal(e)
	}
	mu.Lock()
	after := puts
	mu.Unlock()
	if after != count {
		t.Fatal("duplicate publication", count, after)
	}
	if _, e = s.Observe(ctx); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(searchHTML(t, s, "Notebook-7"), ">Notebook-7<") {
		t.Fatal("deleted notebook remained searchable")
	}
	var history int
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_revision WHERE document_id='note'`).Scan(&history); e != nil || history == 0 {
		t.Fatal("missing retained revision", history, e)
	}
}
