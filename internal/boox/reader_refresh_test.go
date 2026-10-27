package boox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func refreshFixture(t *testing.T) (Service, string, map[string]any) {
	t.Helper()
	s := testService(t)
	ctx := context.Background()
	if _, err := s.DB.ExecContext(ctx, `UPDATE boox_identity SET native_uid=$1 WHERE id=1`, s.uid()); err != nil {
		t.Fatal(err)
	}
	doc := s.uid() + "#refresh-book"
	b := map[string]any{"_id": doc, "_rev": "1-baseline", "uniqueId": "refresh-book", "user": s.uid(), "dbId": s.uid() + "-READER_LIBRARY", "modeType": 4, "status": 0, "title": "Synthetic refresh book", "progress": "2/4", "readingStatus": 1, "extraAttributes": `{"backend":{"user_doc_data_update_time":1,"current_page_position_v3":"opaque-anchor","opaque":9007199254740993},"keep":"exact"}`}
	raw, _ := json.Marshal(b)
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,'1-baseline','reading','',$2,$3)`, doc, s.uid(), raw); err != nil {
		t.Fatal(err)
	}
	return s, doc, b
}

func TestReaderRefreshPublicationAndFences(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(fmt.Sprint("stale=", stale), func(t *testing.T) {
			s, doc, b := refreshFixture(t)
			ctx := context.Background()
			key := uuid.NewString()
			id, err := s.QueueReaderRefresh(ctx, doc, "1-baseline", key)
			if err != nil {
				t.Fatal(err)
			}
			replay, err := s.QueueReaderRefresh(ctx, doc, "1-baseline", key)
			if err != nil || replay != id {
				t.Fatal("duplicate request", replay, err)
			}
			if _, err = s.QueueReaderRefresh(ctx, doc, "0-old", uuid.NewString()); !errors.Is(err, errReaderRefreshStale) {
				t.Fatal("stale preview accepted", err)
			}
			if _, err = s.QueueReaderRefresh(ctx, doc+"wrong", "1-baseline", key); !errors.Is(err, errReaderRefreshStale) {
				t.Fatal("cross-document idempotency replay", err)
			}
			docs := map[string]map[string]any{doc: b}
			recordWrites := 0
			failMessage := !stale
			if stale {
				b["_rev"] = "2-device-edit"
				b["progress"] = "4/4"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				name := strings.TrimPrefix(r.URL.Path, "/neocloud/")
				if r.Method == "GET" {
					v, ok := docs[name]
					if !ok {
						http.NotFound(w, r)
						return
					}
					json.NewEncoder(w).Encode(v)
					return
				}
				var v map[string]any
				d := json.NewDecoder(r.Body)
				d.UseNumber()
				if d.Decode(&v) != nil {
					w.WriteHeader(400)
					return
				}
				if v["msgType"] != nil && failMessage {
					w.WriteHeader(503)
					return
				}
				if name == doc {
					recordWrites++
				}
				v["_rev"] = "2-server"
				docs[name] = v
				json.NewEncoder(w).Encode(map[string]string{"rev": "2-server"})
			}))
			defer server.Close()
			s.Config.GatewayAdminURL = server.URL
			_, err = s.PublishOne(ctx)
			if stale {
				if err != nil {
					t.Fatal(err)
				}
				var state, code string
				if err = s.DB.QueryRowContext(ctx, `SELECT state,error_code FROM boox_operation WHERE id=$1`, id).Scan(&state, &code); err != nil {
					t.Fatal(err)
				}
				if state != "blocked" || code != "stale_preview" || recordWrites != 0 || docs[doc]["progress"] != "4/4" {
					t.Fatal("native edit not fenced", state, code)
				}
				return
			}
			if err == nil {
				t.Fatal("expected failed message acknowledgment")
			}
			failMessage = false
			if _, err = s.PublishOne(ctx); err != nil {
				t.Fatal(err)
			}
			if recordWrites != 1 {
				t.Fatal("retry rewrote metadata", recordWrites)
			}
			got := docs[doc]
			if got["progress"] != "2/4" || got["uniqueId"] != "refresh-book" || got["alexandriaOperationId"] != id {
				t.Fatal("changed reading fields")
			}
			var extra map[string]any
			d := json.NewDecoder(strings.NewReader(got["extraAttributes"].(string)))
			d.UseNumber()
			if d.Decode(&extra) != nil {
				t.Fatal("extra JSON")
			}
			backend := extra["backend"].(map[string]any)
			if backend["opaque"] != json.Number("9007199254740993") || backend["current_page_position_v3"] != "opaque-anchor" || backend["user_doc_data_update_time"] == json.Number("1") || extra["keep"] != "exact" {
				t.Fatal("freshness or opaque fields changed incorrectly")
			}
			var state string
			if err = s.DB.QueryRowContext(ctx, `SELECT state FROM boox_operation WHERE id=$1`, id).Scan(&state); err != nil || state != "published" {
				t.Fatal(state, err)
			}
			if worked, err := s.PublishOne(ctx); err != nil || worked {
				t.Fatal("duplicate queued work", worked, err)
			}
		})
	}
}

func TestReaderRefreshHTTPGuards(t *testing.T) {
	s, doc, b := refreshFixture(t)
	form := url.Values{"documentId": {doc}, "expectedRevision": {"1-baseline"}, "idempotencyKey": {uuid.NewString()}}
	call := func(method, path string, origin bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin {
			r.Header.Set("Origin", s.Config.PublicURL)
		}
		w := httptest.NewRecorder()
		s.Admin(w, r)
		return w
	}
	if w := call("GET", "/boox/reading/refresh?record="+url.QueryEscape(doc), false); w.Code != 200 || !strings.Contains(w.Body.String(), "Request refresh") {
		t.Fatal("refresh form", w.Code)
	}
	if w := call("POST", "/boox/reading/refresh", false); w.Code != 403 {
		t.Fatal("origin not required", w.Code)
	}
	w := call("POST", "/boox/reading/refresh", true)
	if w.Code != 303 {
		t.Fatal("queue", w.Code, w.Body.String())
	}
	if w = call("GET", w.Header().Get("Location"), false); w.Code != 200 || !strings.Contains(w.Body.String(), "not verified") {
		t.Fatal("truthful operation status", w.Code)
	}
	b["status"] = 1
	raw, _ := json.Marshal(b)
	if _, err := s.DB.ExecContext(context.Background(), `UPDATE boox_projection SET body=$1 WHERE document_id=$2`, raw, doc); err != nil {
		t.Fatal(err)
	}
	form.Set("idempotencyKey", uuid.NewString())
	if w = call("POST", "/boox/reading/refresh", true); w.Code != 409 {
		t.Fatal("deleted book refresh accepted", w.Code)
	}
	b["status"] = 0
	b["modeType"] = 1
	raw, _ = json.Marshal(b)
	if _, err := s.DB.ExecContext(context.Background(), `UPDATE boox_projection SET body=$1 WHERE document_id=$2`, raw, doc); err != nil {
		t.Fatal(err)
	}
	if w = call("GET", "/boox/reading/refresh?record="+url.QueryEscape(doc), false); w.Code != 404 {
		t.Fatal("annotation accepted as book", w.Code)
	}
}

// A stored book that replaced cloud-account records can take over one of their
// reading positions, keeping its own identity and reader settings.
func TestReaderCopyTakesOverAnEarlierCopysPosition(t *testing.T) {
	s, doc, b := refreshFixture(t)
	ctx := context.Background()
	put := func(id, rev string, body map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,native_uid,body) VALUES($1,$2,'reading','',$3,$4) ON CONFLICT(document_id) DO UPDATE SET revision=EXCLUDED.revision,body=EXCLUDED.body`, s.uid()+"#"+id, rev, s.uid(), raw); err != nil {
			t.Fatal(err)
		}
	}
	b["hashTag"], b["fetchSource"] = "synthetic-fingerprint", 0
	put("refresh-book", "1-baseline", b)
	copyOf := func(id string, fields map[string]any) map[string]any {
		c := map[string]any{"_id": s.uid() + "#" + id, "uniqueId": id, "user": s.uid(), "dbId": s.uid() + "-READER_LIBRARY", "modeType": 4, "status": 1, "fetchSource": 19, "hashTag": "synthetic-fingerprint", "progress": "652/661", "readingStatus": 1, "lastAccess": 1742321494000, "extraAttributes": `{"backend":{"user_doc_data_update_time":1,"current_page_position_v3":"chapter-50"},"fontSize":40}`}
		for k, v := range fields {
			c[k] = v
		}
		return c
	}
	put("old-copy", "2-retired", copyOf("old-copy", nil))
	put("unread-copy", "1-cloud", copyOf("unread-copy", map[string]any{"progress": nil}))
	put("native-twin", "1-native", copyOf("native-twin", map[string]any{"fetchSource": 0}))
	put("other-book", "1-cloud", copyOf("other-book", map[string]any{"hashTag": "another-fingerprint"}))
	copies, err := s.readerCopies(ctx, s.DB, doc, b)
	if err != nil || len(copies) != 1 || copies[0].Document != s.uid()+"#old-copy" || copies[0].Progress != "652/661" {
		t.Fatal("copies", copies, err)
	}
	copyDoc := s.uid() + "#old-copy"
	key := uuid.NewString()
	id, err := s.QueueReaderCopy(ctx, doc, "1-baseline", copyDoc, "2-retired", key)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.QueueReaderCopy(ctx, doc, "1-baseline", copyDoc, "2-retired", key); err != nil || replay != id {
		t.Fatal("replay", replay, err)
	}
	for name, c := range map[string][2]string{"changed copy": {copyDoc, "1-older"}, "native twin": {s.uid() + "#native-twin", "1-native"}, "unread copy": {s.uid() + "#unread-copy", "1-cloud"}, "other book": {s.uid() + "#other-book", "1-cloud"}} {
		if _, err := s.QueueReaderCopy(ctx, doc, "1-baseline", c[0], c[1], uuid.NewString()); !errors.Is(err, errReaderRefreshStale) {
			t.Error(name, err)
		}
	}
	if _, err := s.QueueReaderCopy(ctx, doc, "1-baseline", s.uid()+"#native-twin", "1-native", key); !errors.Is(err, errReaderRefreshStale) {
		t.Fatal("a key reused for another copy", err)
	}
	docs := map[string]map[string]any{doc: b}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/neocloud/")
		if r.Method == "GET" {
			if v, ok := docs[name]; ok {
				json.NewEncoder(w).Encode(v)
				return
			}
			http.NotFound(w, r)
			return
		}
		var v map[string]any
		d := json.NewDecoder(r.Body)
		d.UseNumber()
		if d.Decode(&v) != nil {
			w.WriteHeader(400)
			return
		}
		v["_rev"] = "2-server"
		docs[name] = v
		json.NewEncoder(w).Encode(map[string]string{"rev": "2-server"})
	}))
	defer server.Close()
	s.Config.GatewayAdminURL = server.URL
	for i := 0; i < 5; i++ {
		if worked, err := s.PublishOne(ctx); err != nil {
			t.Fatal(err)
		} else if !worked {
			break
		}
	}
	got := docs[doc]
	if got["progress"] != "652/661" || got["uniqueId"] != "refresh-book" || got["alexandriaOperationId"] != id || fmt.Sprint(got["readingStatus"]) != "1" {
		t.Fatal("reading position not taken over", got["progress"], got["uniqueId"])
	}
	var extra map[string]any
	if json.Unmarshal([]byte(got["extraAttributes"].(string)), &extra) != nil {
		t.Fatal("extra JSON")
	}
	if extra["backend"].(map[string]any)["current_page_position_v3"] != "chapter-50" || extra["keep"] != "exact" || extra["fontSize"] != nil {
		t.Fatal("position or settings", extra)
	}

	// The page offers the copy and reports the request.
	form := url.Values{"documentId": {doc}, "expectedRevision": {"2-server"}, "copy": {copyDoc}, "copyRevision": {"2-retired"}, "idempotencyKey": {uuid.NewString()}}
	if _, err := s.DB.ExecContext(ctx, `UPDATE boox_projection SET revision='2-server' WHERE document_id=$1`, doc); err != nil {
		t.Fatal(err)
	}
	call := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewBufferString(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", s.Config.PublicURL)
		w := httptest.NewRecorder()
		s.Admin(w, r)
		return w
	}
	if w := call("GET", "/boox/reading/refresh?record="+url.QueryEscape(doc)); w.Code != 200 || !strings.Contains(w.Body.String(), "Use this reading position") || !strings.Contains(w.Body.String(), "652/661") {
		t.Fatal("copy offered", w.Code)
	}
	w := call("POST", "/boox/reading/refresh")
	if w.Code != 303 {
		t.Fatal("queue", w.Code, w.Body.String())
	}
	if w = call("GET", w.Header().Get("Location")); w.Code != 200 || !strings.Contains(w.Body.String(), "not verified") {
		t.Fatal("status", w.Code)
	}
}
