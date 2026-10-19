package web

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

func TestRecognitionSizeFormsAndOrigin(t *testing.T) {
	db := testenv.Database(t).DB
	ctx := context.Background()
	if _, e := db.ExecContext(ctx, `INSERT INTO boox_projection(document_id,revision,domain,native_type,body) SELECT 'large','1-a','notebook','1',jsonb_build_object('uniqueId','large','type',1,'status',1,'title','Synthetic giant','pageNameList',(SELECT jsonb_agg('p'||i) FROM generate_series(1,101) i))`); e != nil {
		t.Fatal(e)
	}
	d := Deps{DB: db, PublicURL: "https://size.example.test", RecognitionCatalog: func(context.Context, string, string) (recognition.Notebook, error) {
		return recognition.Notebook{ID: "large", Title: "Synthetic giant", Pages: make([]recognition.Page, 101)}, nil
	}}
	h := Handler(d)
	call := func(method, path, origin, lang string, v url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		r.Header.Set("Accept-Language", lang)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, lang := range []string{"en", "es"} {
		for _, path := range []string{"/jobs/size", "/jobs/new?source=boox&notebook=large"} {
			w := call("GET", path, "", lang, nil)
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Synthetic giant") {
				t.Fatal(path, w.Code, w.Body.String())
			}
		}
	}
	v := url.Values{"source": {"boox"}, "notebook": {"large"}, "allow": {"1"}}
	if w := call("POST", "/jobs/size", "https://evil.example", "en", v); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := call("POST", "/jobs/size", d.PublicURL, "en", v); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	p, e := (recognition.Store{DB: db}).SizePolicy(ctx, "boox", "large", 0)
	if e != nil || !p.AlwaysAllow {
		t.Fatal(p, e)
	}
	if w := call("POST", "/settings/recognition-limit", d.PublicURL, "en", url.Values{"limit": {"200"}, "expected": {"100"}}); w.Code != 303 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := call("POST", "/settings/recognition-limit", "https://evil.example", "en", url.Values{"limit": {"999"}, "expected": {"200"}}); w.Code != 403 {
		t.Fatal(w.Code)
	}
	p, e = (recognition.Store{DB: db}).SizePolicy(ctx, "boox", "large", 0)
	if e != nil || p.Limit != 200 {
		t.Fatal(p, e)
	}
}
