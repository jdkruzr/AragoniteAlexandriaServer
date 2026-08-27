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

	"golang.org/x/crypto/bcrypt"
)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) SetUser(ctx context.Context, username, password string) error {
	username = strings.TrimSpace(username)
	if username == "" || len(password) < 12 {
		return fmt.Errorf("username is required and password must contain at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO loom_users(username,password_hash) VALUES($1,$2)
		ON CONFLICT(username) DO UPDATE SET password_hash=EXCLUDED.password_hash,updated_at=now()`, username, string(hash))
	return err
}

func (s *Store) CreateToken(ctx context.Context, label string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", fmt.Errorf("token label is required")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := "loom_" + base64.RawURLEncoding.EncodeToString(raw)
	_, err := s.db.ExecContext(ctx, `INSERT INTO loom_api_tokens(token_hash,label) VALUES($1,$2)`, tokenHash(token), label)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) Authenticate(ctx context.Context, username, password, bearer string) (string, error) {
	if bearer != "" {
		result, err := s.db.ExecContext(ctx, `UPDATE loom_api_tokens SET last_used_at=now()
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
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM loom_users WHERE username=$1`, username).Scan(&hash); err != nil {
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
			w.Header().Set("WWW-Authenticate", `Basic realm="Aragonite Loom"`)
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
