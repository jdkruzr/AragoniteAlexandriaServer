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

var ErrPasswordPolicy = errors.New("Use at least 12 characters and at most 72 UTF-8 bytes.")
var ErrCurrentPassword = errors.New("Current password is incorrect or has changed. Reload and try again.")

func (s *Store) CreateToken(ctx context.Context, label string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", fmt.Errorf("token label is required")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := "alexandria_" + base64.RawURLEncoding.EncodeToString(raw)
	_, err := s.db.ExecContext(ctx, `INSERT INTO alexandria_api_tokens(token_hash,label) VALUES($1,$2)`, tokenHash(token), label)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) Authenticate(ctx context.Context, username, password, bearer string) (string, error) {
	if bearer != "" {
		result, err := s.db.ExecContext(ctx, `UPDATE alexandria_api_tokens SET last_used_at=now()
			WHERE token_hash=$1 AND revoked_at IS NULL`, tokenHash(bearer))
		if err != nil {
			return "", err
		}
		if n, _ := result.RowsAffected(); n == 1 {
			return "token", nil
		}
		return "", errors.New("invalid bearer token")
	}
	var hash string
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM alexandria_users WHERE username=$1`, username).Scan(&hash); err != nil {
		return "", errors.New("invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", errors.New("invalid credentials")
	}
	return username, nil
}

func (s *Store) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var username, password, bearer string
		if value := r.Header.Get("Authorization"); strings.HasPrefix(value, "Bearer ") {
			bearer = strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
		} else {
			username, password, _ = r.BasicAuth()
		}
		identity, err := s.Authenticate(r.Context(), username, password, bearer)
		if err != nil {
			w.Header().Set("WWW-Authenticate", `Basic realm="Aragonite Alexandria Server"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, identity)))
	})
}

func Identity(ctx context.Context) string {
	identity, _ := ctx.Value(identityKey{}).(string)
	return identity
}

type identityKey struct{}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
