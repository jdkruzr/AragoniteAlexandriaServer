package web

import (
	"context"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/status"
)

type SourceSummary = status.SourceSummary

func (d Deps) clientSummary(ctx context.Context, needsRestore *int) SourceSummary {
	s := SourceSummary{Notebooks: -1, Books: -1, Devices: -1, ActivityLabel: "Last sync"}
	if d.DB == nil {
		return s
	}
	_ = d.DB.QueryRowContext(ctx, `SELECT count(*) FROM fn_notebook WHERE deleted_at IS NULL`).Scan(&s.Notebooks)
	_ = d.DB.QueryRowContext(ctx, `SELECT count(*) FROM fn_reader_book b WHERE NOT EXISTS (SELECT 1 FROM fn_reader_book_lifecycle bl WHERE bl.id=b.id AND bl.deleted=1)`).Scan(&s.Books)
	if devices, err := (relay.Store{DB: d.DB}).ListDevices(ctx); err == nil {
		s.Devices = 0
		for _, dev := range devices {
			if dev.Enrolled && !dev.Revoked {
				s.Devices++
			}
			if dev.NeedsAdoption && !dev.Revoked {
				*needsRestore++
			}
			if seen := time.UnixMilli(dev.LastSeenMs); dev.LastSeenMs > 0 && seen.After(s.LastActivity) {
				s.LastActivity = seen
			}
		}
	}
	return s
}
