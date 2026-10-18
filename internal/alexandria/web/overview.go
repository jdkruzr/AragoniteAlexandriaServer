package web

import (
	"context"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/diagnostics"
	"net/http"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/recognition"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/relay"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/status"
)

type SourceSummary = status.SourceSummary

type overviewData struct {
	NativeBOOX         bool
	Client             SourceSummary
	BOOX               *SourceSummary
	OCR, Semantic      bool
	Active, Failed     int
	Blocked, Paused    int
	JobsKnown          bool
	ClientNeedsRestore int
}

func (d Deps) overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	v := overviewData{NativeBOOX: d.Status.NativeBOOX, OCR: d.Status.OCR != "", Semantic: d.Status.Embedding != ""}
	v.Client = d.clientSummary(ctx, &v.ClientNeedsRestore)
	if d.BOOXSummary != nil {
		if s, err := d.BOOXSummary(ctx); err == nil {
			v.BOOX = &s
		} else if d.Logger != nil {
			d.Logger.Warn("BOOX overview summary unavailable", "code", diagnostics.Code(err))
		}
	}
	if jobs, err := (recognition.Store{DB: d.DB}).List(ctx, recognition.Filter{}); err == nil {
		v.JobsKnown = true
		v.Active = jobs.Counts["queued"] + jobs.Counts["processing"]
		v.Failed, v.Blocked, v.Paused = jobs.Counts["failed"], jobs.Counts["blocked"], jobs.Counts["paused"]
	}
	d.render(w, r, "overview", "Library", "overview", v)
}

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
