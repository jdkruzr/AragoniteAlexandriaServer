// Package oauth lets an MCP client (Claude Web) obtain an API token through
// OAuth 2.1: discovery metadata, dynamic registration of public clients, an
// account-authenticated consent step, and a PKCE (S256) code exchange.
// Validation follows UltraBridge (internal/web OAuth handlers, Apache-2.0).
// Differences: the user sees and approves a consent page instead of an
// immediate redirect, and codes live in PostgreSQL rather than memory.
package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/auth"
)

// TokenLabel marks API tokens issued through OAuth.
const TokenLabel = "Claude-OAuth"

var ErrInvalid = errors.New("invalid authorization request")

// BaseURL is the public origin: configured, else derived from the request
// (X-Forwarded-Proto from the operator's TLS proxy).
func BaseURL(public string, r *http.Request) string {
	if public != "" {
		return strings.TrimRight(public, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// Public reports whether a path is an unauthenticated OAuth endpoint.
func Public(path string) bool {
	return strings.HasPrefix(path, "/.well-known/oauth-") || path == "/register" || path == "/token"
}

// PublicHandler serves discovery, registration and the token exchange.
func PublicHandler(db pg.DB, public string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := BaseURL(public, r)
		switch {
		case r.Method == http.MethodGet && (r.URL.Path == "/.well-known/oauth-protected-resource" || r.URL.Path == "/.well-known/oauth-protected-resource/mcp"):
			writeJSON(w, 200, map[string]any{"resource": base + "/mcp", "authorization_servers": []string{base}})
		case r.Method == http.MethodGet && r.URL.Path == "/.well-known/oauth-authorization-server":
			writeJSON(w, 200, map[string]any{
				"issuer": base, "authorization_endpoint": base + "/authorize", "token_endpoint": base + "/token",
				"registration_endpoint": base + "/register", "response_types_supported": []string{"code"},
				"grant_types_supported": []string{"authorization_code"}, "token_endpoint_auth_methods_supported": []string{"none"},
				"code_challenge_methods_supported": []string{"S256"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/register":
			register(db, w, r)
		case r.Method == http.MethodPost && r.URL.Path == "/token":
			token(db, w, r)
		default:
			http.NotFound(w, r)
		}
	})
}

// Challenge answers an unauthenticated /mcp request so MCP clients can
// discover the authorization server.
func Challenge(public string, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+BaseURL(public, r)+`/.well-known/oauth-protected-resource/mcp"`)
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

type registration struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	Scope                   string   `json:"scope"`
	ApplicationType         string   `json:"application_type"`
}

func register(db pg.DB, w http.ResponseWriter, r *http.Request) {
	var req registration
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		oauthError(w, 400, "invalid_client_metadata", "invalid registration document")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 16 {
		oauthError(w, 400, "invalid_redirect_uri", "redirect_uris is required")
		return
	}
	for _, uri := range req.RedirectURIs {
		if !ValidRedirectURI(uri) {
			oauthError(w, 400, "invalid_redirect_uri", "redirect URI must use HTTPS (HTTP is allowed for loopback hosts)")
			return
		}
	}
	if req.TokenEndpointAuthMethod == "" {
		req.TokenEndpointAuthMethod = "none"
	}
	if req.TokenEndpointAuthMethod != "none" {
		oauthError(w, 400, "invalid_client_metadata", "only public clients are supported")
		return
	}
	name := strings.TrimSpace(req.ClientName)
	if len(name) > 200 {
		name = name[:200]
	}
	clientID := randomToken()
	uris, _ := json.Marshal(req.RedirectURIs)
	var issued time.Time
	if err := db.QueryRowContext(r.Context(), `INSERT INTO alexandria_oauth_clients(client_id,client_name,redirect_uris) VALUES($1,$2,$3) RETURNING created_at`,
		clientID, strings.ToValidUTF8(strings.ReplaceAll(name, "\x00", ""), ""), string(uris)).Scan(&issued); err != nil {
		oauthError(w, 500, "server_error", "client registration failed")
		return
	}
	response := map[string]any{"client_id": clientID, "client_id_issued_at": issued.Unix(), "client_name": name,
		"redirect_uris": req.RedirectURIs, "token_endpoint_auth_method": "none",
		"grant_types": []string{"authorization_code"}, "response_types": []string{"code"}}
	if req.Scope != "" {
		response["scope"] = req.Scope
	}
	if req.ApplicationType != "" {
		response["application_type"] = req.ApplicationType
	}
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, http.StatusCreated, response)
}

// Request is a validated authorization request awaiting consent.
type Request struct {
	ClientID, ClientName, RedirectURI, State, Challenge string
}

// Validate checks an authorization request: response_type=code, a registered
// client with an exactly matching redirect URI, and PKCE S256.
func Validate(ctx context.Context, db pg.Querier, q url.Values) (Request, error) {
	req := Request{ClientID: q.Get("client_id"), RedirectURI: q.Get("redirect_uri"), State: q.Get("state"), Challenge: q.Get("code_challenge")}
	if req.RedirectURI == "" || req.ClientID == "" || q.Get("response_type") != "code" ||
		q.Get("code_challenge_method") != "S256" || req.Challenge == "" || len(req.Challenge) > 128 {
		return req, ErrInvalid
	}
	var raw string
	var uris []string
	err := db.QueryRowContext(ctx, `SELECT client_name, redirect_uris FROM alexandria_oauth_clients WHERE client_id=$1`, req.ClientID).Scan(&req.ClientName, &raw)
	if err != nil || json.Unmarshal([]byte(raw), &uris) != nil {
		return req, ErrInvalid
	}
	for _, uri := range uris {
		if uri == req.RedirectURI {
			return req, nil
		}
	}
	return req, ErrInvalid
}

// Approve issues a single-use code and returns the client redirect URL.
func Approve(ctx context.Context, db pg.Querier, req Request) (string, error) {
	code := randomToken()
	sum := sha256.Sum256([]byte(code))
	if _, err := db.ExecContext(ctx, `DELETE FROM alexandria_oauth_codes WHERE expires_at < now()`); err != nil {
		return "", err
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO alexandria_oauth_codes(code_hash,client_id,redirect_uri,code_challenge,expires_at) VALUES($1,$2,$3,$4,now()+$5::interval)`,
		hex.EncodeToString(sum[:]), req.ClientID, req.RedirectURI, req.Challenge, "5 minutes"); err != nil {
		return "", err
	}
	return redirect(req, url.Values{"code": {code}}), nil
}

// Deny returns the client redirect URL carrying access_denied.
func Deny(req Request) string { return redirect(req, url.Values{"error": {"access_denied"}}) }

func redirect(req Request, params url.Values) string {
	target, _ := url.Parse(req.RedirectURI)
	q := target.Query()
	for k, v := range params {
		q[k] = v
	}
	if req.State != "" {
		q.Set("state", req.State)
	}
	target.RawQuery = q.Encode()
	return target.String()
}

func token(db pg.DB, w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := r.ParseForm(); err != nil {
		oauthError(w, 400, "invalid_request", "bad request")
		return
	}
	if r.FormValue("grant_type") != "authorization_code" {
		oauthError(w, 400, "unsupported_grant_type", "grant_type must be authorization_code")
		return
	}
	sum := sha256.Sum256([]byte(r.FormValue("code")))
	var clientID, redirectURI, challenge string
	var live bool
	// Single use: the code is deleted whether or not the rest checks out.
	err := db.QueryRowContext(r.Context(), `DELETE FROM alexandria_oauth_codes WHERE code_hash=$1 RETURNING client_id, redirect_uri, code_challenge, expires_at > now()`,
		hex.EncodeToString(sum[:])).Scan(&clientID, &redirectURI, &challenge, &live)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !live) {
		oauthError(w, 400, "invalid_grant", "invalid or expired authorization code")
		return
	}
	if err != nil {
		oauthError(w, 500, "server_error", "token exchange failed")
		return
	}
	if clientID != r.FormValue("client_id") || redirectURI != r.FormValue("redirect_uri") || !ValidVerifier(r.FormValue("code_verifier"), challenge) {
		oauthError(w, 400, "invalid_grant", "authorization code binding or PKCE verification failed")
		return
	}
	issued, err := auth.NewStore(db).CreateToken(r.Context(), TokenLabel)
	if err != nil {
		oauthError(w, 500, "server_error", "token exchange failed")
		return
	}
	w.Header().Set("Pragma", "no-cache")
	writeJSON(w, 200, map[string]any{"access_token": issued, "token_type": "Bearer", "expires_in": 315360000})
}

// ValidRedirectURI allows HTTPS, or HTTP on a loopback host, without fragments.
func ValidRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Fragment != "" || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	host := u.Hostname()
	return u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")
}

// ValidVerifier checks PKCE S256 in constant time.
func ValidVerifier(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func oauthError(w http.ResponseWriter, status int, code, description string) {
	writeJSON(w, status, map[string]string{"error": code, "error_description": description})
}
