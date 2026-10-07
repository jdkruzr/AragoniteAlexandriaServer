// Package host assembles the Alexandria shared-library protocol for one
// admitted library connection. Selectively ported from UltraBridge
// (internal/libraryhost) under Apache-2.0.
//
// Account credentials authorize enrollment only; a device key binds every
// other protocol request. There is no account bypass for rows or assets.
package host

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/assetstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/contract"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/notes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/restore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/blob"
	"github.com/jdkruzr/rhizome/server-go/assets"
	"github.com/jdkruzr/rhizome/server-go/bounded"
)

// Host holds request-independent state. Serve runs per admitted request.
type Host struct {
	caps http.Handler
}

// Library is what one admitted request may touch.
type Library struct {
	DB      pg.DB      // the admitted connection
	Objects blob.Store // scoped to this library
	Account identity.AccountCheck
}

func New() (*Host, error) {
	caps, err := bounded.CapabilityHandler(bounded.Defaults(), []string{contract.CandidateCombined().SchemaHash()}, true)
	if err != nil {
		return nil, err
	}
	return &Host{caps: caps}, nil
}

// Owns reports whether a path belongs to the protocol, which authenticates
// itself and must bypass the generic API middleware.
func Owns(path string) bool { return strings.HasPrefix(path, "/sync/") }

// Assets returns the asset store for an admitted library. Every mutation runs
// the generation fence first, inside its own transaction.
func Assets(lib Library) assetstore.Store {
	return assetstore.Store{DB: lib.DB, Objects: lib.Objects, BeforeWrite: func(ctx context.Context, tx *sql.Tx) error {
		err := generation.CheckRequestTx(ctx, tx)
		if errors.Is(err, generation.ErrReplaced) {
			return assets.Fail(409, "library_replaced")
		}
		return err
	}}
}

// Serve answers one protocol request. Patterns are exact, so nothing on
// /sync/* is ever redirected.
func (h *Host) Serve(lib Library, w http.ResponseWriter, r *http.Request) {
	// Context cancellation alone does not interrupt a blocked body read.
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(30 * time.Second))
	store := identity.Store{DB: lib.DB}
	if strings.HasPrefix(r.URL.Path, "/sync/devices/v1/") {
		store.AdminHandler(lib.Account).ServeHTTP(w, r)
		return
	}
	// Restore authenticates for itself: an OLD device key may discover and
	// adopt a replacement it is fenced out of.
	if strings.HasPrefix(r.URL.Path, "/sync/restore/v1/") {
		restore.Service{DB: lib.DB, Objects: lib.Objects, ReplaceDerived: notes.ReplaceDerived}.Handler(lib.Account).ServeHTTP(w, r)
		return
	}
	store.Bind(func(site string, w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sync/capabilities":
			h.caps.ServeHTTP(w, r)
		case r.URL.Path == "/sync/v1":
			relay.Store{DB: lib.DB}.Handler(site, nil).ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/sync/assets/v1/"):
			assets.NewHandler(Assets(lib)).ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}).ServeHTTP(w, r)
}
