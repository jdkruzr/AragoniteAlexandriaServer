package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"image/png"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// previewFixture makes a synthetic folder, a two-page notebook in it with
// blank-template pages, and a deleted notebook whose folder still exists.
func previewFixture(t *testing.T, s Service) {
	t.Helper()
	ctx := context.Background()
	cleanupFixture(t, s, "work", "Synthetic work folder", map[string]any{"type": 0, "parentUniqueId": ""})
	cleanupFixture(t, s, "n", "Synthetic preview note", map[string]any{"parentUniqueId": "work",
		"pageNameList": map[string]any{"pageNameList": []string{"p1", "p2"}},
		"notePageInfo": map[string]any{"pageInfoMap": map[string]any{"p1": map[string]any{"width": 1000, "height": 1200}, "p2": map[string]any{"width": 1000, "height": 1200}}}})
	cleanupFixture(t, s, "d", "Synthetic deleted note", map[string]any{"status": 0, "parentUniqueId": "work"})
	body := []byte(`{"properties":{"layoutType":"LayoutBlank"}}`)
	sha := hash(body)
	if _, e := s.Objects.Put(ctx, "boox/bodies/"+sha, "application/json", bytes.NewReader(body), int64(len(body))); e != nil {
		t.Fatal(e)
	}
	for _, page := range []string{"p1", "p2"} {
		key := s.uid() + "/note/n/template/json/" + page + ".template_json"
		var vid int64
		if e := s.DB.QueryRowContext(ctx, `INSERT INTO boox_blob_version(native_key,sha256,md5,bytes) VALUES($1,$2,$3,$4) RETURNING id`, key, sha, strings.Repeat("0", 32), len(body)).Scan(&vid); e != nil {
			t.Fatal(e)
		}
		if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_blob_live(native_key,version_id) VALUES($1,$2)`, key, vid); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO boox_page_index(notebook_id,page_id,page_number,state,text,input_version) VALUES('n','p1',1,'ready','Synthetic text','v1'),('n','gone',3,'ready','A removed page','v1')`); e != nil {
		t.Fatal(e)
	}
}

func libraryGet(t *testing.T, s Service, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.Admin(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestLibraryNotebookPreview(t *testing.T) {
	s, _ := managementFixture(t)
	previewFixture(t, s)
	w := libraryGet(t, s, "/boox?selected=n")
	page := html.UnescapeString(w.Body.String())
	if w.Code != 200 {
		t.Fatal(w.Code, page)
	}
	for _, want := range []string{"Synthetic preview note", "Synthetic work folder", "1 of 2 pages recognized", "/boox/thumb.png?id=n&page=2&version=", "Deleted (1)", `aria-current="true"`, `name="review" value="rename"`} {
		if !strings.Contains(page, want) {
			t.Errorf("preview lacks %q", want)
		}
	}
	// A list row shows its folder.
	if !strings.Contains(page, "Handwritten note · Synthetic work folder") {
		t.Error("row lacks its folder")
	}
	if w := libraryGet(t, s, "/boox?selected=no-such-note"); w.Code != 200 || strings.Contains(w.Body.String(), `id="preview"`) {
		t.Fatal("unknown selection must show no preview", w.Code)
	}
	// The deleted notebook's preview offers permanent removal, pinned to the
	// stored content it was shown with.
	dead := html.UnescapeString(libraryGet(t, s, "/boox?show=deleted&selected=d").Body.String())
	version, e := notebookAssetVersion(context.Background(), s.DB, s.uid(), "d")
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(dead, `name="action" value="purge"`) || !strings.Contains(dead, `"AssetsVersion":"`+version+`"`) {
		t.Fatal("deleted preview lacks a pinned purge form")
	}
}

func TestLibraryInlineQueue(t *testing.T) {
	s, _ := managementFixture(t)
	previewFixture(t, s)
	choice := func(id string) string {
		b, _ := json.Marshal(notebookChoice{ID: id, Revision: "1-original"})
		return string(b)
	}
	post := func(v url.Values) *httptest.ResponseRecorder {
		t.Helper()
		v.Set("inline", "1")
		if v.Get("token") == "" {
			v.Set("token", uuid.NewString())
		}
		return managementHTTP(t, s, "POST", v, true)
	}
	// Moving needs no ticked box and comes back to the Library.
	move := url.Values{"action": {"move"}, "selected": {choice("n")}, "folder": {`{"ID":"","Revision":""}`}, "return": {"/boox?selected=n&queued=old"}}
	w := post(move)
	if w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/boox?selected=n&queued=old&queued=") {
		// The page's own return drops the old notice; a hand-made one keeps it.
		if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "queued="+move.Get("token")) {
			t.Fatal(w.Code, w.Header().Get("Location"), w.Body.String())
		}
	}
	after := html.UnescapeString(libraryGet(t, s, "/boox?selected=n&queued="+move.Get("token")).Body.String())
	if !strings.Contains(after, "View progress") || !strings.Contains(after, "Move queued") {
		t.Fatal("queued move not shown")
	}
	// Trash always needs its box.
	if w := post(url.Values{"action": {"trash"}, "selected": {choice("n")}, "return": {"/boox"}}); w.Code != 409 {
		t.Fatal("unconfirmed trash queued", w.Code)
	}
	// Restore goes back to the notebook's folder, found on the server.
	restore := url.Values{"action": {"restore"}, "selected": {choice("d")}, "return": {"/boox?show=deleted"}}
	if w := post(restore); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	var parent string
	if e := s.DB.QueryRowContext(context.Background(), `SELECT payload->>'parent' FROM boox_operation WHERE payload->>'managementBatch'=$1`, restore.Get("token")).Scan(&parent); e != nil || parent != "work" {
		t.Fatal(parent, e)
	}
	// Renaming from the selection bar opens the review page instead.
	review := managementHTTP(t, s, "POST", url.Values{"review": {"rename"}, "inline": {"1"}, "selected": {choice("n")}, "return": {"/boox"}, "token": {uuid.NewString()}}, true)
	if review.Code != 200 || !strings.Contains(review.Body.String(), `name="title"`) {
		t.Fatal(review.Code, review.Body.String())
	}
}

func TestLibraryThumbnails(t *testing.T) {
	s := testService(t)
	previewFixture(t, s)
	ctx := context.Background()
	n, e := s.notebookSnapshot(ctx, "n")
	if e != nil {
		t.Fatal(e)
	}
	thumb := func(page, version string) *httptest.ResponseRecorder {
		return libraryGet(t, s, "/boox/thumb.png?id=n&page="+page+"&version="+version)
	}
	w := thumb("1", n.Version)
	if w.Code != 200 || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatal(w.Code, w.Body.String())
	}
	img, e := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if e != nil || img.Bounds().Dx() != ThumbWidth || img.Bounds().Dy() != 288 {
		t.Fatal("thumbnail size", e)
	}
	var kept int
	if e = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_page_thumb WHERE source='boox' AND notebook_id='n' AND version=$1`, n.Version).Scan(&kept); e != nil || kept != 1 {
		t.Fatal("thumbnail not kept", kept, e)
	}
	if w := thumb("1", "stale"); w.Code != 409 {
		t.Fatal("stale version served", w.Code)
	}
	// With the slot taken and nothing cached, a kept page is still served
	// and one never drawn waits, then gives up.
	thumbSlots <- struct{}{}
	wait := thumbWait
	thumbWait = 50 * time.Millisecond
	forgetPreviews(t)
	if w := thumb("1", n.Version); w.Code != 200 {
		t.Fatal("kept thumbnail needed drawing", w.Code)
	}
	if w := thumb("2", n.Version); w.Code != 503 {
		t.Fatal("busy slot", w.Code)
	}
	<-thumbSlots
	thumbWait = wait
	// After an edit the earlier drawing is shown, not cached, and redrawn.
	if _, e = s.DB.ExecContext(ctx, `UPDATE alexandria_page_thumb SET version='earlier' WHERE notebook_id='n'`); e != nil {
		t.Fatal(e)
	}
	if w := thumb("1", n.Version); w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatal("earlier drawing", w.Code, w.Header().Get("Cache-Control"))
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var v string
		_ = s.DB.QueryRowContext(ctx, `SELECT version FROM alexandria_page_thumb WHERE notebook_id='n' AND page=1`).Scan(&v)
		if v == n.Version {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not redrawn")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func forgetPreviews(t *testing.T) {
	t.Helper()
	previews.Lock()
	defer previews.Unlock()
	previews.Items, previews.Bytes = map[string]previewResult{}, 0
}
