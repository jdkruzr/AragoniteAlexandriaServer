package restore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jdkruzr/rhizome/server-go/assets"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/assetstore"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/identity"
)

// Handler serves /sync/restore/v1/*. Publication requires the library
// owner's account. Discovery, baseline download and adoption accept a valid
// OLD device key without granting normal sync access.
func (s Service) Handler(account identity.AccountCheck) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			http.Error(w, "native_restore_only", 403)
			return
		}
		if r.URL.Path == "/sync/restore/v1/publish" {
			if account == nil || account(r) != nil {
				w.Header().Set("WWW-Authenticate", `Basic realm="Alexandria"`)
				http.Error(w, "admin_auth_required", 401)
				return
			}
			var req struct {
				ID        string `json:"request_id"`
				Expected  string `json:"expected_generation"`
				Snapshot  string `json:"snapshot"`
				Publisher string `json:"publisher"`
			}
			if !decode(w, r, &req) {
				return
			}
			// Publication may ingest a large library; ordinary routes keep the
			// host's short deadlines. Cancellation still rolls everything back.
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(20 * time.Minute))
			b, err := s.Publish(r.Context(), generation.Request{ID: req.ID, Expected: req.Expected, SnapshotHash: req.Snapshot, Publisher: req.Publisher})
			respond(w, b, err)
			return
		}
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") {
			http.Error(w, "device_key_required", 401)
			return
		}
		site, err := identity.Store{DB: s.DB}.Resolve(r.Context(), strings.TrimPrefix(authz, "Bearer "))
		if err != nil {
			if errors.Is(err, identity.ErrInvalid) {
				http.Error(w, "invalid_device_key", 401)
			} else {
				respond(w, nil, err)
			}
			return
		}
		switch r.URL.Path {
		case "/sync/restore/v1/state":
			if r.Method != http.MethodGet {
				http.Error(w, "method_not_allowed", 405)
				return
			}
			current, err := generation.Current(r.Context(), s.DB)
			if err != nil {
				respond(w, nil, err)
				return
			}
			b, e := s.Baseline(r.Context())
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				respond(w, nil, e)
				return
			}
			var base *Baseline
			if e == nil {
				base = &b
			}
			_, _, admission := generation.Admit(r.Context(), s.DB, site)
			if admission != nil && !errors.Is(admission, generation.ErrReplaced) {
				respond(w, nil, admission)
				return
			}
			respond(w, map[string]any{"generation": current, "needs_adoption": errors.Is(admission, generation.ErrReplaced), "baseline": base}, nil)
		case "/sync/restore/v1/adopt":
			var a Adoption
			if !decode(w, r, &a) {
				return
			}
			b, err := s.Adopt(r.Context(), site, a)
			respond(w, b, err)
		default:
			if strings.HasPrefix(r.URL.Path, "/sync/restore/v1/publications/") {
				if r.Method != http.MethodGet {
					http.Error(w, "method_not_allowed", 405)
					return
				}
				b, e := s.Receipt(r.Context(), site, strings.TrimPrefix(r.URL.Path, "/sync/restore/v1/publications/"))
				if errors.Is(e, sql.ErrNoRows) {
					http.NotFound(w, r)
					return
				}
				respond(w, b, e)
				return
			}
			// Only the current baseline is downloadable through this exception.
			// A stale key cannot list arbitrary assets or write even one chunk.
			if r.Method != http.MethodGet {
				http.Error(w, "method_not_allowed", 405)
				return
			}
			b, e := s.Baseline(r.Context())
			if e != nil {
				if errors.Is(e, sql.ErrNoRows) {
					http.NotFound(w, r)
				} else {
					respond(w, nil, e)
				}
				return
			}
			prefix := "/sync/restore/v1/assets/" + b.Snapshot
			if r.URL.Path != prefix && !strings.HasPrefix(r.URL.Path, prefix+"/") {
				http.NotFound(w, r)
				return
			}
			if r.URL.Query().Get("generation") != b.Generation {
				respond(w, nil, generation.ErrConflict)
				return
			}
			clone := r.Clone(r.Context())
			u := *r.URL
			u.Path = "/sync/assets/v1/" + strings.TrimPrefix(r.URL.Path, "/sync/restore/v1/assets/")
			clone.URL = &u
			assets.NewHandler(assetstore.Store{DB: s.DB, Objects: s.Objects}).ServeHTTP(w, clone)
		}
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "method_not_allowed", 405)
		return false
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "json_required", 415)
		return false
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(v) != nil || d.Decode(&extra) != io.EOF {
		http.Error(w, "invalid_restore_request", 400)
		return false
	}
	return true
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		status := 503
		code := "restore_unavailable"
		switch {
		case errors.Is(err, generation.ErrConflict), errors.Is(err, generation.ErrReplaced):
			status = 409
			code = err.Error()
		case errors.Is(err, ErrSnapshot), errors.Is(err, generation.ErrInvalid):
			status = 422
			code = "invalid_library_snapshot"
		}
		http.Error(w, code, status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
