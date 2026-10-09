package boox

import (
	"context"
	"database/sql"
)

func validNativeUID(uid string) bool {
	if len(uid) < 1 || len(uid) > 128 {
		return false
	}
	for _, c := range uid {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// ResolveIdentity loads the library's native ownership namespace. Credentials
// remain per-device Alexandria capabilities, never Onyx account credentials.
func (s *Service) ResolveIdentity(ctx context.Context) error {
	var uid sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT native_uid FROM boox_identity WHERE id=1`).Scan(&uid); err != nil {
		return err
	}
	s.identity = uid.String
	return nil
}
func (s Service) uid() string {
	if s.identity != "" {
		return s.identity
	}
	return nativeUID(s.LibraryID)
}
