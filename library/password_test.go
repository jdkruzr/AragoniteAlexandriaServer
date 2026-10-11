package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
)

func TestSettingsPasswordChange(t *testing.T) {
	r, db, _ := fixture(t)
	const old = "a-long-test-password"
	const next = "a-different-test-password"
	request := func(method, password, current, proposed, confirmation, origin, token string) *httptest.ResponseRecorder {
		form := url.Values{"current_password": {current}, "new_password": {proposed}, "confirm_password": {confirmation}, "username": {"somebody-else"}}
		req := httptest.NewRequest(method, "http://library.test/settings/password", strings.NewReader(form.Encode()))
		if method == "GET" {
			req.URL.Path = "/settings"
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", origin)
		if password != "" {
			req.SetBasicAuth("author", password)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		return out
	}
	if got := request("GET", old, "", "", "", "", ""); got.Code != 200 || !strings.Contains(got.Body.String(), "Change password") {
		t.Fatalf("form: %d", got.Code)
	}
	token, err := auth.NewStore(db).CreateToken(context.Background(), "password test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, password, current, new, confirm, origin, token string
		status                                               int
	}{
		{"anonymous", "", old, next, next, "http://library.test", "", 401},
		{"cross site", old, old, next, next, "https://evil.example", "", 403},
		{"token", "", old, next, next, "http://library.test", token, 403},
		{"wrong current", old, "wrong", next, next, "http://library.test", "", 400},
		{"mismatch", old, old, next, next + "x", "http://library.test", "", 400},
		{"short", old, old, "short", "short", "http://library.test", "", 400},
		{"long", old, old, strings.Repeat("a", 73), strings.Repeat("a", 73), "http://library.test", "", 400},
		{"unicode short", old, old, "界界界界界", "界界界界界", "http://library.test", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := request("POST", tc.password, tc.current, tc.new, tc.confirm, tc.origin, tc.token)
			if got.Code != tc.status {
				t.Fatalf("status %d want %d", got.Code, tc.status)
			}
		})
	}
	got := request("POST", old, old, next, next, "http://library.test", "")
	if got.Code != 200 || !strings.Contains(got.Body.String(), "Password changed.") {
		t.Fatalf("change: %d %s", got.Code, got.Body)
	}
	if strings.Contains(got.Body.String(), next) || strings.Contains(got.Body.String(), old) || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("secret reflected or response cacheable")
	}
	if got := request("GET", old, "", "", "", "", ""); got.Code != 401 {
		t.Fatalf("old password: %d", got.Code)
	}
	if got := request("GET", next, "", "", "", "", ""); got.Code != 200 {
		t.Fatalf("new password: %d", got.Code)
	}
	if got := request("GET", "", "", "", "", "", token); got.Code != 200 {
		t.Fatalf("token invalidated: %d", got.Code)
	}
	if got := request("POST", next, old, old, old, "http://library.test", ""); got.Code != 400 {
		t.Fatalf("stale current: %d", got.Code)
	}
}

func TestSettingsPasswordExternalAuthentication(t *testing.T) {
	r, _, _ := fixture(t)
	r.cfg.Authenticate = func(context.Context, *http.Request, string) error { return nil }
	b := browser{t, r}
	if got := b.do("GET", "/settings", "", nil, true); got.Code != 200 || strings.Contains(got.Body.String(), "Change password") {
		t.Fatalf("external settings: %d", got.Code)
	}
	if got := b.do("POST", "/settings/password", "current_password=ignored&new_password=ignored&confirm_password=ignored", sameSite, true); got.Code != 403 {
		t.Fatalf("external change: %d", got.Code)
	}
}
