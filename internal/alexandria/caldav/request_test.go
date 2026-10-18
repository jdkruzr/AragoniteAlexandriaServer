package caldav

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestXMLBodyBoundsAndSyntax(t *testing.T) {
	for _, method := range []string{"REPORT", "PROPFIND", "PROPPATCH"} {
		for _, tc := range []struct {
			name, body string
			want       int
		}{
			{"valid", `<D:propfind xmlns:D="DAV:"><D:allprop/></D:propfind>`, 200},
			{"truncated", `<D:propfind xmlns:D="DAV:">`, 400},
			{"multiple roots", `<one/><two/>`, 400},
			{"trailing text", `<one/>bad`, 400},
			{"at limit", `<a>` + strings.Repeat(" ", maxXMLBody-7) + `</a>`, 200},
			{"over limit", `<a>` + strings.Repeat(" ", maxXMLBody-6) + `</a>`, 413},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				r := httptest.NewRequest(method, "/", strings.NewReader(tc.body))
				w := httptest.NewRecorder()
				b, err := readXMLBody(w, r)
				if w.Code != tc.want {
					t.Fatalf("status=%d want=%d err=%v", w.Code, tc.want, err)
				}
				if tc.want == 200 && (err != nil || string(b) != tc.body) {
					t.Fatal("body changed", err)
				}
				if tc.want != 200 && err == nil {
					t.Fatal("invalid body accepted")
				}
			})
		}
	}
}
func TestXMLWrappersRejectOversizeBeforeDelegating(t *testing.T) {
	for _, method := range []string{"REPORT", "PROPFIND", "PROPPATCH"} {
		t.Run(method, func(t *testing.T) {
			called := false
			next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
			var handler http.Handler
			switch method {
			case "REPORT":
				handler = SyncCollectionSource(next, nil)
			case "PROPPATCH":
				handler = ProppatchStub(next, ProppatchOptions{OnDisplayName: func(string) error { called = true; return nil }})
			case "PROPFIND":
				handler = SyncPropsStub(next, bodyProbeStore{called: &called}, nil)
			}
			r := httptest.NewRequest(method, "/", io.NopCloser(bytes.NewReader(bytes.Repeat([]byte("x"), maxXMLBody+1))))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != 413 || called {
				t.Fatalf("status=%d delegated=%v", w.Code, called)
			}
		})
	}
}

type bodyProbeStore struct{ called *bool }

func (s bodyProbeStore) MaxUpdatedAtAll(context.Context) (int64, error) {
	*s.called = true
	return 0, nil
}
