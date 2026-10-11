package boox

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGatewayReadinessRequiresOnlineDatabaseAndReadableViews(t *testing.T) {
	for _, tc := range []struct {
		name, info, docs string
		status           int
		want             bool
	}{
		{"ready", `{"db_name":"neocloud","state":"Online"}`, `{"rows":[{"id":"synthetic"}]}`, 200, true},
		{"empty", `{"db_name":"neocloud","state":"Online"}`, `{"rows":[]}`, 200, true},
		{"offline", `{"db_name":"neocloud","state":"Offline"}`, `{"rows":[]}`, 200, false},
		{"wrong_database", `{"db_name":"other","state":"Online"}`, `{"rows":[]}`, 200, false},
		{"listener_only", `{}`, `{"rows":[]}`, 200, false},
		{"view_failure", `{"db_name":"neocloud","state":"Online"}`, `{"error":"private upstream detail"}`, 500, false},
		{"malformed_view", `{"db_name":"neocloud","state":"Online"}`, `invalid private body`, 200, false},
		{"missing_rows", `{"db_name":"neocloud","state":"Online"}`, `{}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				if !ok || user != "operator" || pass != "private-test-secret" || r.Method != "GET" {
					t.Error("incorrect private probe")
				}
				switch r.URL.Path {
				case "/neocloud/":
					fmt.Fprint(w, tc.info)
				case "/neocloud/_all_docs":
					if r.URL.RawQuery != "limit=1" {
						t.Error("unbounded view probe")
					}
					w.WriteHeader(tc.status)
					fmt.Fprint(w, tc.docs)
				default:
					t.Error("unexpected route")
					w.WriteHeader(404)
				}
			}))
			defer upstream.Close()
			cfg := Config{GatewayAdminURL: upstream.URL, Database: "neocloud", GatewayUsername: "operator", GatewayPassword: "private-test-secret"}
			err := cfg.CheckReady(context.Background())
			if (err == nil) != tc.want {
				t.Fatalf("ready=%v, want=%v", err == nil, tc.want)
			}
			if err != nil && (strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), upstream.URL)) {
				t.Fatal("upstream detail leaked")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if cfg.CheckReady(ctx) == nil {
				t.Fatal("canceled probe reported ready")
			}
		})
	}
}
