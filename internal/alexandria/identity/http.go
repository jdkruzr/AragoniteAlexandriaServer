package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/generation"
)

// AccountCheck verifies that a request carries the library owner's account
// password over HTTP Basic. Bearer tokens (device keys or operator API tokens)
// are not enrollment authority.
type AccountCheck func(r *http.Request) error

// ErrAccount is returned by an AccountCheck that rejects the request.
var ErrAccount = errors.New("account_auth_required")

// AdminHandler serves enroll and revoke. Only fixture and native clients use it:
// browser-origin requests are refused until a settings UI with CSRF protection
// exists. Deploy behind TLS.
func (s Store) AdminHandler(account AccountCheck) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method_not_allowed", 405)
			return
		}
		if account == nil || account(r) != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Alexandria"`)
			http.Error(w, "admin_auth_required", 401)
			return
		}
		if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			http.Error(w, "browser_enrollment_not_enabled", 403)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "json_required", 415)
			return
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2048))
		d.DisallowUnknownFields()
		decode := func(v any) error {
			if d.Decode(v) != nil {
				return ErrInvalid
			}
			var extra any
			if d.Decode(&extra) != io.EOF {
				return ErrInvalid
			}
			return nil
		}
		var err error
		switch r.URL.Path {
		case "/sync/devices/v1/enroll":
			var e Enrollment
			if err = decode(&e); err == nil {
				err = s.Enroll(r.Context(), e)
			}
		case "/sync/devices/v1/revoke":
			var e struct {
				SiteID string `json:"site_id"`
			}
			if err = decode(&e); err == nil {
				err = s.Revoke(r.Context(), e.SiteID)
			}
		default:
			http.NotFound(w, r)
			return
		}
		if err != nil {
			code := http.StatusBadRequest
			message := "invalid_enrollment_request"
			switch {
			case errors.Is(err, ErrConflict), errors.Is(err, ErrAdoption), errors.Is(err, generation.ErrReplaced):
				code = 409
				message = err.Error()
			case errors.Is(err, ErrServerSite):
				code = 403
				message = err.Error()
			case errors.Is(err, ErrInvalid):
				message = err.Error()
			default:
				// Never echo SQL/internal details or request credential material.
				code = 503
				message = "identity_store_unavailable"
			}
			http.Error(w, message, code)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// Bind authorizes each request independently, including capability, asset and
// search reads. No cached identity survives revocation and no Basic fallback.
func (s Store) Bind(next func(site string, w http.ResponseWriter, r *http.Request)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		site, ctx, ok := s.authorize(w, r)
		if ok {
			next(site, w, r.WithContext(ctx))
		}
	})
}

func (s Store) authorize(w http.ResponseWriter, r *http.Request) (string, context.Context, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		unauthorized(w)
		return "", nil, false
	}
	site, err := s.Resolve(r.Context(), parts[1])
	if err != nil {
		if errors.Is(err, ErrInvalid) {
			unauthorized(w)
		} else {
			http.Error(w, "identity_store_unavailable", 503)
		}
		return "", nil, false
	}
	ctx, gen, err := generation.Admit(r.Context(), s.DB, site)
	if gen != "" {
		w.Header().Set(generation.Header, gen)
	}
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		if errors.Is(err, generation.ErrReplaced) {
			http.Error(w, "library_replaced", 409)
		} else {
			http.Error(w, "identity_store_unavailable", 503)
		}
		return "", nil, false
	}
	return site, ctx, true
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="Alexandria Device"`)
	http.Error(w, "device_credential_required", 401)
}
