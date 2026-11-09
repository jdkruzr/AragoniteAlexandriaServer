package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct{ db DB }

func NewStore(db DB) *Store { return &Store{db: db} }

func (s *Store) SetUser(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || len(password) < 12 {
		return fmt.Errorf("username is required and password must contain at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO alexandria_users(username,password_hash) VALUES($1,$2)
		ON CONFLICT(username) DO UPDATE SET password_hash=EXCLUDED.password_hash,updated_at=now()`, username, string(hash))
	return err
}

// ChangePassword requires the current secret and compare-and-swaps the hash so
// a simultaneous operator reset cannot be overwritten by a stale form.
func (s *Store) ChangePassword(ctx context.Context, username, current, next string) error {
	if utf8.RuneCountInString(next) < 12 || len(next) > 72 {
		return ErrPasswordPolicy
	}
	var old string
	if err := s.db.QueryRowContext(ctx, "SELECT password_hash FROM alexandria_users WHERE username=$1", username).Scan(&old); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCurrentPassword
		}
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(old), []byte(current)) != nil {
		return ErrCurrentPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, "UPDATE alexandria_users SET password_hash=$1,updated_at=now() WHERE username=$2 AND password_hash=$3", string(hash), username, old)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrCurrentPassword
	}
	return nil
}

// ErrInvalidToken is a bearer token that is unknown or revoked.
var ErrInvalidToken = errors.New("invalid bearer token")

var ErrPasswordPolicy = errors.New("Use at least 12 characters and at most 72 UTF-8 bytes.")
var ErrCurrentPassword = errors.New("Current password is incorrect or has changed. Reload and try again.")

// Scope is what a request may do: ScopeFull changes things, ScopeRead only
// looks. Password sign-ins are always full.
type Scope string

const (
	ScopeFull Scope = "full"
	ScopeRead Scope = "read"
)

// ParseScope accepts "read" or "full"; anything else is full.
func ParseScope(v string) Scope {
	if strings.TrimSpace(v) == string(ScopeRead) {
		return ScopeRead
	}
	return ScopeFull
}

// CreateToken issues a full-access token.
func (s *Store) CreateToken(ctx context.Context, label string) (string, error) {
	return s.CreateScopedToken(ctx, label, ScopeFull, "")
}

// CreateScopedToken issues a token with a scope; oauthClient names the
// registered OAuth client that asked for it ("" for tokens made by hand).
func (s *Store) CreateScopedToken(ctx context.Context, label string, scope Scope, oauthClient string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", fmt.Errorf("token label is required")
	}
	if scope != ScopeRead {
		scope = ScopeFull
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := "alexandria_" + base64.RawURLEncoding.EncodeToString(raw)
	_, err := s.db.ExecContext(ctx, `INSERT INTO alexandria_api_tokens(token_hash,label,scope,oauth_client_id) VALUES($1,$2,$3,NULLIF($4,''))`,
		tokenHash(token), label, string(scope), oauthClient)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) Authenticate(ctx context.Context, username, password, bearer string) (string, error) {
	session, err := s.AuthenticateScoped(ctx, username, password, bearer)
	return session.Identity, err
}

// Session is who a request is: an account, or "token" with the token's
// scope and label.
type Session struct {
	Identity string
	Scope    Scope
	Label    string
}

// AuthenticateScoped checks a bearer token or a password.
func (s *Store) AuthenticateScoped(ctx context.Context, username, password, bearer string) (Session, error) {
	if bearer != "" {
		var scope, label string
		err := s.db.QueryRowContext(ctx, `UPDATE alexandria_api_tokens SET last_used_at=now()
			WHERE token_hash=$1 AND revoked_at IS NULL RETURNING scope,label`, tokenHash(bearer)).Scan(&scope, &label)
		if errors.Is(err, sql.ErrNoRows) {
			return Session{}, ErrInvalidToken
		}
		if err != nil {
			return Session{}, err
		}
		return Session{Identity: "token", Scope: ParseScope(scope), Label: label}, nil
	}
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM alexandria_users WHERE username=$1`, username).Scan(&hash); err != nil {
		return Session{}, errors.New("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return Session{}, errors.New("invalid credentials")
	}
	return Session{Identity: username, Scope: ScopeFull}, nil
}

// With binds a verified session to a request's context.
func (v Session) With(ctx context.Context) context.Context {
	ctx = WithScope(WithIdentity(ctx, v.Identity), v.Scope)
	if v.Label != "" {
		ctx = context.WithValue(ctx, labelKey{}, v.Label)
	}
	return ctx
}

type labelKey struct{}

// TokenLabel names the token a request came with ("" for none).
func TokenLabel(ctx context.Context) string {
	label, _ := ctx.Value(labelKey{}).(string)
	return label
}

// Bearer returns the request's bearer token, or "".
func Bearer(r *http.Request) string {
	if value := r.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
	}
	return ""
}

func (s *Store) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var username, password string
		bearer := Bearer(r)
		if bearer == "" {
			username, password, _ = r.BasicAuth()
		}
		session, err := s.AuthenticateScoped(r.Context(), username, password, bearer)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Aragonite Alexandria Server"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(session.With(r.Context())))
	})
}

// SafeMethod reports a method that only reads.
func SafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, "PROPFIND", "REPORT":
		return true
	}
	return false
}

// ReadOnlyGuard refuses changes from a read-scoped request. exempt names
// paths that enforce the scope themselves (MCP registers no write tools).
func ReadOnlyGuard(next http.Handler, exempt func(path string) bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ScopeOf(r.Context()) == ScopeRead && !SafeMethod(r.Method) && (exempt == nil || !exempt(r.URL.Path)) {
			http.Error(w, "This token can only read.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Identity(ctx context.Context) string {
	identity, _ := ctx.Value(identityKey{}).(string)
	return identity
}

type identityKey struct{}

type scopeKey struct{}

// WithScope binds the scope of an already verified request.
func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// ScopeOf is the request's scope; requests without one (hosted sign-in,
// owner pages) are full.
func ScopeOf(ctx context.Context) Scope {
	if scope, _ := ctx.Value(scopeKey{}).(Scope); scope == ScopeRead {
		return ScopeRead
	}
	return ScopeFull
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// WithIdentity binds an identity already verified for this request and library.
// It is not an authentication operation and must not be populated from input.
func WithIdentity(ctx context.Context, identity string) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}
