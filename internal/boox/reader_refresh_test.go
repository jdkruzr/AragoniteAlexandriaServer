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
