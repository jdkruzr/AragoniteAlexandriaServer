package boox

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
)

type managementGateway struct {
	mu         sync.Mutex
	docs       map[string]map[string]any
	writes     int
	failDelete bool
}

func (g *managementGateway) serve(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	id := strings.TrimPrefix(r.URL.Path, "/neocloud/")
	if id == "_changes" {
		var results []map[string]any
		for id, b := range g.docs {
			results = append(results, map[string]any{"id": id, "changes": []map[string]any{{"rev": b["_rev"]}}})
		}
		reply(w, 200, map[string]any{"results": results, "last_seq": g.writes + 1})
		return
	}
	b, ok := g.docs[id]
	switch r.Method {
	case "GET":
		if !ok || b["_deleted"] == true && r.URL.Query().Get("rev") == "" {
			reply(w, 404, map[string]any{})
			return
		}
		reply(w, 200, b)
	case "PUT":
		var in map[string]any
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			http.Error(w, "json", 400)
			return
		}
		if ok && in["_rev"] != b["_rev"] {
			http.Error(w, "conflict", 409)
			return
		}
		g.writes++
		in["_rev"] = fmt.Sprintf("2-%d", g.writes)
		g.docs[id] = in
		reply(w, 201, map[string]any{"rev": in["_rev"]})
	case "DELETE":
		if g.failDelete {
			http.Error(w, "synthetic unavailable", 503)
			return
		}
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("rev") != b["_rev"] {
			http.Error(w, "conflict", 409)
			return
		}
		g.writes++
		g.docs[id] = map[string]any{"_id": id, "_rev": fmt.Sprintf("3-%d", g.writes), "_deleted": true}
		reply(w, 200, map[string]any{"ok": true})
	default:
		http.Error(w, "method", 405)
	}
}
func managementFixture(t *testing.T) (Service, *managementGateway) {
	t.Helper()
	s := testService(t)
	g := &managementGateway{docs: map[string]map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(g.serve))
	t.Cleanup(server.Close)
	s.Config.GatewayAdminURL = server.URL
	return s, g
}
func managementQueue(t *testing.T, s Service, action string, c ...notebookChoice) string {
	t.Helper()
	id := uuid.NewString()
	if action == "purge" {
		for i := range c {
			var e error
			c[i].AssetsVersion, e = notebookAssetVersion(context.Background(), s.DB, s.uid(), c[i].ID)
			if e != nil {
				t.Fatal(e)
			}
		}
	}
	if e := s.queueNotebookManagement(context.Background(), notebookIntent{Token: id, Action: action, Return: "/boox?view=folders&folder=f&sort=name", Choices: c}); e != nil {
		t.Fatal(e)
	}
	return id
}
func managementPublish(t *testing.T, s Service) {
	t.Helper()
	if work, e := s.PublishOne(context.Background()); e != nil || !work {
		t.Fatal(work, e)
	}
}
func managementObserve(t *testing.T, s Service) {
	t.Helper()
	if _, e := s.Observe(context.Background()); e != nil {
		t.Fatal(e)
	}
}
func managementStateFor(t *testing.T, s Service, batch string) string {
	t.Helper()
	var state string
	if e := s.DB.QueryRowContext(context.Background(), `SELECT state FROM boox_operation WHERE payload->>'managementBatch'=$1`, batch).Scan(&state); e != nil {
		t.Fatal(e)
	}
	return state
}

func TestNotebookManagementLifecycle(t *testing.T) {
	s, g := managementFixture(t)
	ctx := context.Background()
	g.docs["n"] = cleanupFixture(t, s, "n", "Original", nil)
	g.docs["f"] = cleanupFixture(t, s, "f", "Folder", map[string]any{"type": 0})
	for _, action := range []string{"rename", "move", "trash", "restore"} {
		items, e := s.managementItems(ctx, []notebookChoice{{ID: "n"}})
		if e != nil {
			t.Fatal(e)
		}
		c := items[0].notebookChoice
		if action == "rename" {
			c.Title = "Renamed <&>"
		}
		if action == "move" {
			c.Parent = "f"
			c.ParentRevision = "1-original"
		}
		batch := managementQueue(t, s, action, c)
		managementPublish(t, s)
		managementObserve(t, s)
		if got := managementStateFor(t, s, batch); got != "published" {
			t.Fatal(action, got)
		}
		b := g.docs["n"]
		if b["uniqueId"] != "n" || b["opaque"] != "preserve-me" {
			t.Fatal("lost identity or opaque fields")
		}
		if action == "rename" && b["title"] != c.Title {
			t.Fatal(b)
		}
		if action == "move" && b["parentUniqueId"] != "f" {
			t.Fatal(b)
		}
		if action == "trash" && fmt.Sprint(b["status"]) != "0" {
			t.Fatal(b)
		}
		if action == "restore" && (fmt.Sprint(b["status"]) != "1" || b["parentUniqueId"] != "f") {
			t.Fatal(b)
		}
	}
	// Missing original destination is explicit in the restore review.
	_, e := s.DB.ExecContext(ctx, `UPDATE boox_projection SET body=jsonb_set(body,'{status}','0') WHERE document_id='n'; DELETE FROM boox_projection WHERE document_id='f'`)
	if e != nil {
		t.Fatal(e)
	}
	items, e := s.managementItems(ctx, []notebookChoice{{ID: "n"}})
	if e != nil || !items[0].RootFallback || items[0].Parent != "" {
		t.Fatal(items, e)
	}
}
func TestNotebookManagementStaleAndReplay(t *testing.T) {
	s, g := managementFixture(t)
	ctx := context.Background()
	g.docs["a"] = cleanupFixture(t, s, "a", "A", nil)
	g.docs["b"] = cleanupFixture(t, s, "b", "B", nil)
	in := notebookIntent{Token: uuid.NewString(), Action: "trash", Choices: []notebookChoice{{ID: "a", Revision: "1-original"}, {ID: "b", Revision: "wrong"}}}
	if e := s.queueNotebookManagement(ctx, in); !errors.Is(e, errNotebookSelection) {
		t.Fatal(e)
	}
	var n int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_operation`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	in.Choices[1].Revision = "1-original"
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			copy := in
			copy.Choices = append([]notebookChoice(nil), in.Choices...)
			results <- s.queueNotebookManagement(ctx, copy)
		}()
	}
	for i := 0; i < 4; i++ {
		if e := <-results; e != nil {
			t.Fatal(e)
		}
	}
	altered := in
	altered.Action = "rename"
	if e := s.queueNotebookManagement(ctx, altered); e == nil {
		t.Fatal("repurposed token")
	}
	g.docs["b"]["_rev"] = "2-native-change"
	managementPublish(t, s)
	managementPublish(t, s)
	var published, blocked int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE state='published'),count(*) FILTER(WHERE state='blocked') FROM boox_operation`).Scan(&published, &blocked); e != nil || published != 1 || blocked != 1 {
		t.Fatal(published, blocked, e)
	}
}
func TestNotebookManagementDestinationFence(t *testing.T) {
	s, g := managementFixture(t)
	g.docs["n"] = cleanupFixture(t, s, "n", "Note", nil)
	g.docs["f"] = cleanupFixture(t, s, "f", "Folder", map[string]any{"type": 0})
	batch := managementQueue(t, s, "move", notebookChoice{ID: "n", Revision: "1-original", Parent: "f", ParentRevision: "1-original"})
	g.docs["f"]["status"] = 0
	managementPublish(t, s)
	if managementStateFor(t, s, batch) != "blocked" || g.docs["n"]["parentUniqueId"] != "nested-folder" {
		t.Fatal("moved into a deleted destination")
	}
}

func TestNotebookPermanentRemovalAndLateArrivals(t *testing.T) {
	s, g := managementFixture(t)
	ctx := context.Background()
	c := getSTS(t, s, enrollTest(t, s, uuid.NewString()))
	g.docs["gone"] = cleanupFixture(t, s, "gone", "Ghost", map[string]any{"status": 0})
	g.docs["keep"] = cleanupFixture(t, s, "keep", "Independent", nil)
	g.docs["commit"] = map[string]any{"_id": "commit", "_rev": "1-a", "uniqueId": "commit", "user": s.uid(), "dbId": s.uid() + "-NOTE_TREE", "commitType": 1, "documentUniqueId": "gone"}
	g.docs["tag"] = map[string]any{"_id": "tag", "_rev": "1-a", "uniqueId": "tag", "user": s.uid(), "dbId": s.uid() + "-NOTE_TREE", "modeType": 1, "content": "private tag", "documentId": "gone"}
	managementObserve(t, s)
	shared := []byte("shared synthetic attachment")
	unique := []byte("exclusive synthetic attachment")
	for key, body := range map[string][]byte{s.uid() + "/note/gone/resource/a": shared, s.uid() + "/note/keep/resource/a": shared, s.uid() + "/note/gone/resource/b": unique, s.uid() + "/note/gone.png": []byte("synthetic thumbnail")} {
		if w := signed(t, s, c, "PUT", key, "", body, false); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_recognition_job(id,source,notebook_id,page_id,state) VALUES($1,'boox','gone','p','blocked')`, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_note_action_item(source,notebook_id,page_id,color,fingerprint,kind,item_id) VALUES('boox','gone','p','red','f','task','independent')`); e != nil {
		t.Fatal(e)
	}
	// independent task has no foreign key ownership by its originating notebook.
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO alexandria_tasks(task_id,title,created_at,updated_at) VALUES('independent','Keep task',1,1)`); e != nil {
		t.Fatal(e)
	}
	batch := managementQueue(t, s, "purge", notebookChoice{ID: "gone", Revision: "1-original"})
	for i := 0; i < 8 && managementStateFor(t, s, batch) != "complete"; i++ {
		managementPublish(t, s)
		managementObserve(t, s)
	}
	if managementStateFor(t, s, batch) != "complete" {
		t.Fatal("purge incomplete")
	}
	var refs int
	if e := s.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM boox_projection WHERE document_id IN ('gone','tag','commit'))+(SELECT count(*) FROM boox_revision WHERE document_id IN ('gone','tag','commit'))+(SELECT count(*) FROM boox_blob_version WHERE starts_with(native_key,$1) OR native_key=rtrim($1,'/')||'.png')+(SELECT count(*) FROM alexandria_recognition_job WHERE notebook_id='gone')+(SELECT count(*) FROM alexandria_note_action_item WHERE notebook_id='gone')`, s.uid()+"/note/gone/").Scan(&refs); e != nil || refs != 0 {
		t.Fatal(refs, e)
	}
	if _, e := s.Objects.Stat(ctx, "boox/bodies/"+hash(unique)); e == nil {
		t.Fatal("exclusive body retained")
	}
	if _, e := s.Objects.Stat(ctx, "boox/bodies/"+hash(shared)); e != nil {
		t.Fatal("shared body removed", e)
	}
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_tasks WHERE task_id='independent'`).Scan(&refs); e != nil || refs != 1 {
		t.Fatal("independent task removed", refs, e)
	}
	if w := signed(t, s, c, "PUT", s.uid()+"/note/gone/resource/late", "", []byte("late"), false); w.Code != 410 {
		t.Fatal(w.Code)
	}
	if _, e := s.Objects.Stat(ctx, "boox/bodies/"+hash([]byte("synthetic thumbnail"))); e == nil {
		t.Fatal("thumbnail body retained")
	}
	if w := signed(t, s, c, "PUT", s.uid()+"/note/gone.png", "", []byte("late thumbnail"), false); w.Code != 410 {
		t.Fatal("thumbnail upload accepted", w.Code)
	}
	key := s.previewKey("gone", "stale", 1)
	storePreview(key, previewResult{PNG: []byte("late preview")})
	if _, ok := cachedPreview(key); ok {
		t.Fatal("in-flight preview recreated removed cache")
	}
	// Old recognition snapshots and retries cannot resurrect any queue/history.
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_page_index(notebook_id,page_id,page_number,state) VALUES('gone','late',1,'queued')`); e != nil {
		t.Fatal(e)
	}
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_page_index WHERE notebook_id='gone'`).Scan(&refs); e != nil || refs != 0 {
		t.Fatal(refs, e)
	}
	// Offline client reappears with an edited live record; observe never archives it.
	g.docs["gone"] = map[string]any{"_id": "gone", "_rev": "9-offline", "uniqueId": "gone", "user": s.uid(), "dbId": s.uid() + "-NOTE_TREE", "type": 1, "status": 1, "title": "Resurrected private text"}
	managementObserve(t, s)
	for i := 0; i < 6 && managementStateFor(t, s, batch) != "complete"; i++ {
		managementPublish(t, s)
		managementObserve(t, s)
	}
	if g.docs["gone"]["_deleted"] != true {
		t.Fatal("native resurrection remains")
	}
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM boox_revision WHERE document_id='gone'`).Scan(&refs); e != nil || refs != 0 {
		t.Fatal("late content archived", refs, e)
	}
}

type failPurgeDelete struct{ blob.Store }

func (s failPurgeDelete) Delete(ctx context.Context, key string) error {
	return errors.New("synthetic object deletion outage")
}
func TestNotebookPurgeRetryAndCancellation(t *testing.T) {
	s, g := managementFixture(t)
	ctx := context.Background()
	g.docs["n"] = cleanupFixture(t, s, "n", "Gone", map[string]any{"status": 0})
	managementObserve(t, s)
	batch := managementQueue(t, s, "purge", notebookChoice{ID: "n", Revision: "1-original"})
	managementPublish(t, s)
	g.failDelete = true
	if _, e := s.PublishOne(ctx); e == nil {
		t.Fatal("expected native failure")
	}
	if managementStateFor(t, s, batch) != "purging_native" {
		t.Fatal("lost retry stage")
	}
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_operation SET next_attempt_at=now()`); e != nil {
		t.Fatal(e)
	}
	g.failDelete = false
	managementPublish(t, s)
	managementPublish(t, s)
	original := s.Objects
	s.Objects = failPurgeDelete{original}
	if _, e := s.PublishOne(ctx); e == nil {
		t.Fatal("expected object failure")
	}
	if managementStateFor(t, s, batch) != "purging_objects" {
		t.Fatal("marked incomplete cleanup complete")
	}
	if _, e := s.DB.ExecContext(ctx, `UPDATE boox_operation SET next_attempt_at=now()`); e != nil {
		t.Fatal(e)
	}
	s.Objects = original
	managementPublish(t, s)
	if managementStateFor(t, s, batch) != "complete" {
		t.Fatal("retry failed")
	}
}

func managementHTTP(t *testing.T, s Service, method string, v url.Values, origin bool) *httptest.ResponseRecorder {
	t.Helper()
	path := "/boox/manage"
	var body *strings.Reader
	if method == "GET" {
		path += "?" + v.Encode()
		body = strings.NewReader("")
	} else {
		body = strings.NewReader(v.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin {
		r.Header.Set("Origin", s.Config.PublicURL)
	}
	w := httptest.NewRecorder()
	s.Admin(w, r)
	return w
}
func TestNotebookManagementForms(t *testing.T) {
	s, _ := managementFixture(t)
	cleanupFixture(t, s, "n", "<unsafe & title>", nil)
	w := managementHTTP(t, s, "GET", url.Values{"id": {"n"}, "action": {"rename"}, "return": {"/boox?sort=name&order=asc&offset=60"}}, false)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "&lt;unsafe &amp; title&gt;") || !strings.Contains(w.Body.String(), "sort=name&amp;order=asc&amp;offset=60") {
		t.Fatal(w.Code, w.Body.String())
	}
	b, _ := json.Marshal(notebookChoice{ID: "n", Revision: "1-original"})
	form := url.Values{"step": {"queue"}, "action": {"rename"}, "token": {uuid.NewString()}, "return": {"/boox?sort=name"}, "selected": {string(b)}, "title": {"New name"}, "confirm": {"on"}}
	if w := managementHTTP(t, s, "POST", form, false); w.Code != 403 {
		t.Fatal("missing origin accepted")
	}
	if w := managementHTTP(t, s, "POST", form, true); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	status := managementHTTP(t, s, "GET", url.Values{"batch": {form.Get("token")}}, false)
	if status.Code != 200 || !strings.Contains(status.Body.String(), "Cancel queued operations") {
		t.Fatal(status.Code, status.Body.String())
	}
	if w := managementHTTP(t, s, "POST", url.Values{"batch": {form.Get("token")}, "step": {"cancel"}}, true); w.Code != 303 {
		t.Fatal(w.Code)
	}
	if managementStateFor(t, s, form.Get("token")) != "cancelled" {
		t.Fatal("cancel failed")
	}
	if notebookReturn("//evil.example/path") != "/boox" || notebookReturn("https://evil.example/boox") != "/boox" {
		t.Fatal("unsafe return")
	}
	if validNotebookTitle("\x00bad") || validNotebookTitle(strings.Repeat("é", 513)) {
		t.Fatal("invalid title accepted")
	}
	// GETs and review POSTs are read-only, even for permanent-removal selections.
	var n int
	if e := s.DB.QueryRowContext(context.Background(), `SELECT count(*) FROM boox_notebook_purged`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
}

func TestNotebookObjectBytesPreparedBeforeBinding(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	raw := []byte("synthetic staging data")
	asset, e := s.stagePublicationAsset(ctx, raw, "text/plain")
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	if e = notebookWriteLock(ctx, tx); e != nil {
		t.Fatal(e)
	}
	if e = s.bindPublicationAsset(ctx, tx, s.uid()+"/note/synthetic/resource/file", asset); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	r, _, e := s.Objects.Get(ctx, "boox/bodies/"+asset.SHA256)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	var b bytes.Buffer
	b.ReadFrom(r)
	if !bytes.Equal(raw, b.Bytes()) {
		t.Fatal("content changed")
	}
}

func TestNotebookPurgeAdmissionAndSingleConnection(t *testing.T) {
	s, g := managementFixture(t)
	ctx := context.Background()
	g.docs["n"] = cleanupFixture(t, s, "n", "Gone", map[string]any{"status": 0})
	items, e := s.managementItems(ctx, []notebookChoice{{ID: "n"}})
	if e != nil {
		t.Fatal(e)
	}
	// An attachment arriving after review invalidates the selection even when the
	// notebook's native revision has not changed.
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO boox_blob_version(native_key,deleted) VALUES($1,true)`, s.uid()+"/note/n/resource/new"); e != nil {
		t.Fatal(e)
	}
	if e = s.queueNotebookManagement(ctx, notebookIntent{Token: uuid.NewString(), Action: "purge", Choices: []notebookChoice{items[0].notebookChoice}}); !errors.Is(e, errNotebookSelection) {
		t.Fatal(e)
	}
	// Runtime admission often owns exactly one DB connection.
	db := s.DB.(*sql.DB)
	db.SetMaxOpenConns(1)
	batch := managementQueue(t, s, "purge", notebookChoice{ID: "n", Revision: "1-original"})
	deadline, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	for i := 0; i < 6 && managementStateFor(t, s, batch) != "complete"; i++ {
		if _, e = s.PublishOne(deadline); e != nil {
			t.Fatal(e)
		}
	}
	if managementStateFor(t, s, batch) != "complete" {
		t.Fatal("single-connection purge did not complete")
	}
}

type blockedPurgeDelete struct {
	blob.Store
	entered, release chan struct{}
}

func (s blockedPurgeDelete) Delete(ctx context.Context, key string) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.Store.Delete(ctx, key)
}
func TestNotebookGarbageCollectionVersusUpload(t *testing.T) {
	s, g := managementFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := getSTS(t, s, enrollTest(t, s, uuid.NewString()))
	g.docs["n"] = cleanupFixture(t, s, "n", "Gone", map[string]any{"status": 0})
	body := []byte("synthetic identical content arriving during collection")
	if w := signed(t, s, c, "PUT", s.uid()+"/note/n/resource/a", "", body, false); w.Code != 200 {
		t.Fatal(w.Code)
	}
	batch := managementQueue(t, s, "purge", notebookChoice{ID: "n", Revision: "1-original"})
	managementPublish(t, s)
	managementPublish(t, s)
	managementPublish(t, s)
	blocker := blockedPurgeDelete{Store: s.Objects, entered: make(chan struct{}, 1), release: make(chan struct{})}
	s.Objects = blocker
	collected := make(chan error, 1)
	go func() { _, e := s.PublishOne(ctx); collected <- e }()
	select {
	case <-blocker.entered:
	case <-ctx.Done():
		t.Fatal("collector did not reach object")
	}
	uploaded := make(chan int, 1)
	go func() { uploaded <- signed(t, s, c, "PUT", s.uid()+"/note/other/resource/a", "", body, false).Code }()
	select {
	case code := <-uploaded:
		t.Fatalf("upload bypassed GC lock: %d", code)
	case <-time.After(50 * time.Millisecond):
	}
	close(blocker.release)
	if e := <-collected; e != nil {
		t.Fatal(e)
	}
	if code := <-uploaded; code != 200 {
		t.Fatal(code)
	}
	if managementStateFor(t, s, batch) != "complete" {
		t.Fatal("cleanup incomplete")
	}
	if w := signed(t, s, c, "GET", s.uid()+"/note/other/resource/a", "", nil, false); w.Code != 200 || !bytes.Equal(w.Body.Bytes(), body) {
		t.Fatal("concurrent upload lost", w.Code)
	}
}

func TestNotebookStartedOperationCannotCancel(t *testing.T) {
	s, g := managementFixture(t)
	ctx := context.Background()
	g.docs["n"] = cleanupFixture(t, s, "n", "Gone", map[string]any{"status": 0})
	batch := managementQueue(t, s, "purge", notebookChoice{ID: "n", Revision: "1-original"})
	managementPublish(t, s)
	w := managementHTTP(t, s, "POST", url.Values{"batch": {batch}, "step": {"cancel"}}, true)
	if w.Code != 303 {
		t.Fatal(w.Code)
	}
	if managementStateFor(t, s, batch) != "purging_native" {
		t.Fatal("cancelled a started removal")
	}
	var started bool
	if e := s.DB.QueryRowContext(ctx, `SELECT started_at IS NOT NULL FROM boox_operation WHERE payload->>'managementBatch'=$1`, batch).Scan(&started); e != nil || !started {
		t.Fatal(started, e)
	}
}
