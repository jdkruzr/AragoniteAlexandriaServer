package relay

import (
	"context"
	"net/http"

	"github.com/jdkruzr/rhizome/server-go/bounded"
)

// Handler serves POST /sync/v1 for one verified device. Only the bounded
// protocol exists here: a request without the bounded header is a client from
// before shared libraries and gets the same 409 UltraBridge returns.
// changed is called after commit with pages whose render input changed; it
// must not block.
func (s Store) Handler(site string, changed func(context.Context, []TablePK)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, exists := r.Header[http.CanonicalHeaderKey(bounded.Header)]; !exists {
			bounded.WriteError(w, bounded.Fail(409, "schema_mismatch"))
			return
		}
		req, limits, err := bounded.ReadRequest(w, r, bounded.Defaults())
		var body []byte
		var pages []TablePK
		if err == nil {
			body, pages, err = s.Exchange(r.Context(), req, limits, site)
		}
		if err != nil {
			bounded.WriteError(w, err)
			return
		}
		if changed != nil && len(pages) > 0 {
			changed(r.Context(), pages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}
