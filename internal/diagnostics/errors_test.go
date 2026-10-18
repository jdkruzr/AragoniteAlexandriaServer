package diagnostics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providerhttp"
)

func TestSafeErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "cancelled"}, {context.DeadlineExceeded, "deadline_exceeded"}, {sql.ErrNoRows, "not_found"},
		{&pgconn.PgError{Code: "23505", Detail: "private-secret"}, "postgres_23505"},
		{&pgconn.PgError{Code: "private-secret"}, "postgres_error"},
		{&providerhttp.HTTPError{Status: 429}, "provider_http_429"}, {errors.New("private-secret"), "operation_failed"},
	} {
		if got := Code(fmt.Errorf("private-secret: %w", tc.err)); got != tc.want || strings.Contains(got, "private-secret") {
			t.Fatal(got)
		}
	}
}
