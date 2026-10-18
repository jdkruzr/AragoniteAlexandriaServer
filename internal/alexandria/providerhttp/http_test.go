package providerhttp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBoundedJSONAndSafeErrors(t *testing.T) {
	for _, tc := range []struct {
		body  string
		limit int64
		ok    bool
	}{
		{`{"x":1}`, 7, true}, {`{"x":1} `, 7, false}, {`{"x":1}{}`, 20, false}, {`{"private-secret":`, 40, false},
	} {
		var out map[string]any
		err := ReadJSON(strings.NewReader(tc.body), tc.limit, &out)
		if (err == nil) != tc.ok {
			t.Fatal(tc, err)
		}
		if err != nil && strings.Contains(err.Error(), "private-secret") {
			t.Fatal("body leaked")
		}
	}
	for _, status := range []int{400, 401, 408, 429, 500, 503} {
		response := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"private-secret anthropic-workspace-id required"}}`))}
		err := ResponseError(response)
		if err.Status != status || err.WorkspaceRequired != (status == 400) || strings.Contains(err.Error(), "private-secret") {
			t.Fatal(err)
		}
		if Retryable(status) != (status == 408 || status == 429 || status >= 500) {
			t.Fatal(status)
		}
	}
}
func TestClientNeverForwardsCredentialsOnRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer source.Close()
	req, _ := http.NewRequest("POST", source.URL, strings.NewReader("synthetic"))
	req.Header.Set("Authorization", "Bearer synthetic-secret")
	resp, err := Client(0).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if reached || resp.StatusCode != 307 {
		t.Fatal("redirect followed")
	}
}
