package web

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestRecognitionCleanupRoutes(t *testing.T) {
	ctx := context.Background()
	db := testenv.Database(t).DB
	d := Deps{DB: db, PublicURL: "https://cleanup.example.test"}
	s := recognition.Store{DB: db}

	if _, _, err := s.Queue(ctx, "boox", recognition.Notebook{ID: "cleanup-demo", Pages: []recognition.Page{{ID: "p", Number: 1}}}, 1, 1, false, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	// Model legacy blocked work; new enqueue requests reject deleted sources.
	if _, err := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,body) VALUES('cleanup-demo','1-a','notebook','1','{"uniqueId":"cleanup-demo","type":1,"status":0,"title":"Synthetic removed notebook"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE boox_page_index SET state='blocked',detail='Page was removed.' WHERE notebook_id='cleanup-demo'`); err != nil {
		t.Fatal(err)
	}
	h := Handler(d)
	call := func(method, path, origin, language string, values url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		r.Header.Set("Accept-Language", language)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, lang := range []string{"en", "es"} {
		w := call("GET", "/jobs/cleanup", "", lang, nil)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "Synthetic removed notebook") || !strings.Contains(w.Body.String(), `method="post" action="/jobs/cleanup"`) {
			t.Fatal(lang, w.Code, w.Body.String())
		}
		if lang == "es" && strings.Contains(w.Body.String(), "Clear these jobs from the list") {
			t.Fatal("untranslated action")
		}
	}
	preview, err := s.PreviewCleanup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	values := url.Values{"token": {preview.Token}}
	if w := call("POST", "/jobs/cleanup", "https://evil.example", "en", values); w.Code != 403 {
		t.Fatal("cross-origin cleanup", w.Code)
	}
	if w := call("POST", "/jobs/cleanup", d.PublicURL, "en", url.Values{"token": {"bad"}}); w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/jobs/cleanup?") {
		t.Fatal("stale cleanup", w.Code, w.Header())
	}
	if w := call("POST", "/jobs/cleanup", d.PublicURL, "en", values); w.Code != 303 || !strings.HasPrefix(w.Header().Get("Location"), "/jobs?") {
		t.Fatal("cleanup", w.Code, w.Body.String())
	}
	if w := call("GET", "/jobs", "", "en", nil); w.Code != 200 || strings.Contains(w.Body.String(), "Synthetic removed notebook") {
		t.Fatal("normal list", w.Code)
	}
	if w := call("GET", "/jobs?history=1", "", "en", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "Cleared from list") {
		t.Fatal("history", w.Code, w.Body.String())
	}
	if w := call("GET", "/jobs/cleanup", "", "en", nil); w.Code != 200 || !strings.Contains(w.Body.String(), "No obsolete blocked jobs to clear.") {
		t.Fatal("empty preview", w.Code)
	}
}
