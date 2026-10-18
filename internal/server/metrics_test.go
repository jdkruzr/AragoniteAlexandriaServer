package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/testenv"
)

// /metrics needs the owner's login, like the API, so a proxy that forwards
// every path doesn't publish it.
func TestMetricsRequireLogin(t *testing.T) {
	lib := testenv.Database(t)
	ctx := context.Background()
	store := auth.NewStore(lib.DB)
	if err := store.SetUser(ctx, "owner", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateToken(ctx, "prometheus")
	if err != nil {
		t.Fatal(err)
	}
	g := NewRuntimeGateway(lib.DB, ":0", ":0", time.Second, http.NotFoundHandler(), slog.Default())
	get := func(set func(*http.Request)) int {
		r := httptest.NewRequest("GET", "/metrics", nil)
		set(r)
		w := httptest.NewRecorder()
		g.main.Handler.ServeHTTP(w, r)
		return w.Code
	}
	cases := map[string]struct {
		set  func(*http.Request)
		want int
	}{
		"anonymous":      {func(*http.Request) {}, 401},
		"wrong password": {func(r *http.Request) { r.SetBasicAuth("owner", "nope") }, 401},
		"bad token":      {func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, 401},
		"password":       {func(r *http.Request) { r.SetBasicAuth("owner", "correct horse battery") }, 200},
		"token":          {func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }, 200},
	}
	for name, c := range cases {
		if got := get(c.set); got != c.want {
			t.Errorf("%s: %d, want %d", name, got, c.want)
		}
	}
}
