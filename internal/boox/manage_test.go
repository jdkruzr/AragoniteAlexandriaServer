package boox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigureAbsentDatabasePermissionGate(t *testing.T) {
	for _, tc := range []struct {
		name, list string
		status     int
		created    bool
	}{
		{"absent", `[]`, 200, true},
		{"existing denied", `["neocloud"]`, 200, false},
		{"listing denied", ``, 403, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				user, pass, ok := r.BasicAuth()
				if !ok || user != "private" || pass != "private" {
					t.Error("missing management authentication")
					w.WriteHeader(401)
					return
				}
				switch {
				case r.URL.Path == "/neocloud/_config":
					w.WriteHeader(403)
				case r.URL.Path == "/_all_dbs":
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.list))
				case r.URL.Path == "/neocloud/" && r.Method == "PUT":
					created = true
					w.WriteHeader(201)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			c := Config{PublicURL: "https://boox.example", GatewayPublicURL: server.URL, GatewayAdminURL: server.URL, GatewayUsername: "private", GatewayPassword: "private", Database: "neocloud"}
			err := c.Configure(context.Background(), "01234567-89ab-cdef-0123-456789abcdef")
			if created != tc.created || (err == nil) != tc.created {
				t.Fatalf("created=%v err=%v", created, err)
			}
		})
	}
}
