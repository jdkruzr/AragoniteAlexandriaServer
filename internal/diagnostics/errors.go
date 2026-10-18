// Package diagnostics maps failures to safe codes without retaining error text.
package diagnostics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/providerhttp"
)

func Code(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, sql.ErrNoRows):
		return "not_found"
	}
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) {
		if len(pgerr.Code) == 5 {
			valid := true
			for _, c := range pgerr.Code {
				if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z') {
					valid = false
				}
			}
			if valid {
				return "postgres_" + pgerr.Code
			}
		}
		return "postgres_error"
	}
	var httpErr *providerhttp.HTTPError
	if errors.As(err, &httpErr) && httpErr.Status >= 100 && httpErr.Status <= 599 {
		return fmt.Sprintf("provider_http_%d", httpErr.Status)
	}
	return "operation_failed"
}
