package boox

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestShortEnrollmentCodeLifecycle(t *testing.T) {
	s := testService(t)
	ctx := context.Background()
	code, err := s.IssueCode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 8 || strings.Trim(code, enrollmentAlphabet) != "" {
		t.Fatal("code is not eight unambiguous alphanumerics")
	}
	var minutes float64
	if err = s.DB.QueryRowContext(ctx, `SELECT extract(epoch from (expires_at-now()))/60 FROM boox_enrollment WHERE code_hash=$1`, tokenHash(code)).Scan(&minutes); err != nil {
		t.Fatal(err)
	}
	if minutes < 9 || minutes > 10 {
		t.Fatalf("unexpected validity: %v", minutes)
	}
	payload := map[string]string{"code": "  " + strings.ToLower(code) + "  ", "installationId": uuid.NewString()}
	if w := request(t, s, "POST", "/api/v1/boox/enroll", payload, nil); w.Code != 200 {
		t.Fatalf("lowercase code: %d", w.Code)
	}
	if w := request(t, s, "POST", "/api/v1/boox/enroll", payload, nil); w.Code != 401 {
		t.Fatalf("reused code: %d", w.Code)
	}
	code, err = s.IssueCode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE boox_enrollment SET expires_at=now()-interval '1 second' WHERE code_hash=$1`, tokenHash(code)); err != nil {
		t.Fatal(err)
	}
	payload["code"] = code
	if w := request(t, s, "POST", "/api/v1/boox/enroll", payload, nil); w.Code != 401 {
		t.Fatalf("expired code: %d", w.Code)
	}
	legacy := token()
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO boox_enrollment(code_hash,expires_at) VALUES($1,now()+interval '10 minutes')`, tokenHash(legacy)); err != nil {
		t.Fatal(err)
	}
	payload["code"] = legacy
	if w := request(t, s, "POST", "/api/v1/boox/enroll", payload, nil); w.Code != 200 {
		t.Fatalf("legacy code: %d", w.Code)
	}
}
