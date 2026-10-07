// Package host assembles the Alexandria shared-library protocol for one
// admitted library connection. Selectively ported from UltraBridge
// (internal/libraryhost) under Apache-2.0.
//
// Account credentials authorize enrollment only; a device key binds every
// other protocol request. There is no account bypass for rows or assets.
package host

import (
	"net/http"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/rhizome/server-go/bounded"
)

// Host holds request-independent state. Serve runs per admitted request.
type Host struct {
	caps http.Handler
}

func New() (*Host, error) {
	// assets-v1 is advertised only once the S3 asset store is mounted (P3).
	caps, err := bounded.CapabilityHandler(bounded.Defaults(), []string{contract.CandidateCombined().SchemaHash()}, false)
	if err != nil {
		return nil, err
	}
	return &Host{caps: caps}, nil
}

// Owns reports whether a path belongs to the protocol, which authenticates
// itself and must bypass the generic API middleware.
func Owns(path string) bool { return strings.HasPrefix(path, "/sync/") }

// Serve answers one protocol request over the admitted connection db. Patterns
// are exact, so nothing on /sync/* is ever redirected.
func (h *Host) Serve(db pg.DB, account identity.AccountCheck, w http.ResponseWriter, r *http.Request) {
	// Context cancellation alone does not interrupt a blocked body read.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Second))
	store := identity.Store{DB: db}
	if strings.HasPrefix(r.URL.Path, "/sync/devices/v1/") {
		store.AdminHandler(account).ServeHTTP(w, r)
		return
	}
	store.Bind(func(site string, w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sync/capabilities":
			h.caps.ServeHTTP(w, r)
		case "/sync/v1":
			relay.Store{DB: db}.Handler(site, nil).ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}).ServeHTTP(w, r)
}
