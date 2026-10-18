package boox

import (
	"context"
	"database/sql"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/status"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxvisibility"
)

// Summary reports the native library for the server overview, using the same
// visibility rules as the notebook and Reading browsers. Last upload is the
// newest resource a device sent; it is not proof of a complete sync.
func (s Service) Summary(ctx context.Context) (status.SourceSummary, error) {
	out := status.SourceSummary{ActivityLabel: "Last upload"}
	var last sql.NullTime
	e := s.DB.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM boox_projection WHERE `+notebookPredicate+` AND native_uid=$1 AND body->>'status'='1' AND body->>'type'='1'),
 (SELECT count(*) FROM boox_projection p WHERE `+booxvisibility.Reading("p")+` AND p.native_uid=$1 AND p.body->>'modeType'='4'),
 (SELECT count(*) FROM boox_device WHERE NOT revoked),
 (SELECT max(created_at) FROM boox_blob_version WHERE device_id IS NOT NULL)`, s.uid()).Scan(&out.Notebooks, &out.Books, &out.Devices, &last)
	if last.Valid {
		out.LastActivity = last.Time
	}
	return out, e
}
