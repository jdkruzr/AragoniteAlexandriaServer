package relay

import (
	"context"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/wire"
)

// Device is one known device for the management page: its enrollment, its
// relay cursor (if it has synced) and derived health. Ported from UltraBridge
// (internal/syncstore/devices.go) without compaction, which this server does
// not run, and with enrollment state, which UltraBridge's view lacked.
type Device struct {
	SiteID string
	Name   string // as the device reports it ("" if never sent)
	// Label is the operator's own name, in a column the sync path never writes.
	Label         string
	FirstSeenMs   int64 // from the site ULID's timestamp
	LastSeenMs    int64 // last sync; 0 if it never synced
	LastPullSeq   int64
	AckedOpSeq    int64
	PendingOps    int64 // relay ops it has not pulled yet (excluding its own)
	Enrolled      bool
	Revoked       bool
	NeedsAdoption bool // bound to a generation an authoritative restore replaced
}

// DisplayName prefers the operator's label, then the reported name.
func (d Device) DisplayName() string {
	if d.Label != "" {
		return d.Label
	}
	if d.Name != "" {
		return d.Name
	}
	return "Unnamed device"
}

// ListDevices returns every enrolled or syncing device, most recent first.
func (s Store) ListDevices(ctx context.Context) ([]Device, error) {
	rows, err := s.DB.QueryContext(ctx, `WITH sites AS (
			SELECT site_id FROM sync_device_identity UNION SELECT site_id FROM sync_cursors)
		SELECT s.site_id, COALESCE(c.device_name,''), COALESCE(c.operator_label,''), COALESCE(c.updated_at,0),
			COALESCE(c.last_pull_seq,0), COALESCE(c.acked_op_seq,0),
			CASE WHEN c.site_id IS NULL THEN 0 ELSE (SELECT count(*) FROM sync_ops o WHERE o.seq > c.last_pull_seq AND o.site_id <> c.site_id) END,
			i.site_id IS NOT NULL, COALESCE(i.revoked,false),
			COALESCE(g.generation <> (SELECT generation FROM sync_library_generation WHERE id=1), false)
		FROM sites s
		LEFT JOIN sync_cursors c ON c.site_id = s.site_id
		LEFT JOIN sync_device_identity i ON i.site_id = s.site_id
		LEFT JOIN sync_device_generation g ON g.site_id = s.site_id
		ORDER BY COALESCE(c.updated_at,0) DESC, s.site_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.SiteID, &d.Name, &d.Label, &d.LastSeenMs, &d.LastPullSeq, &d.AckedOpSeq, &d.PendingOps,
			&d.Enrolled, &d.Revoked, &d.NeedsAdoption); err != nil {
			return nil, err
		}
		d.FirstSeenMs, _ = wire.ULIDTime(d.SiteID)
		out = append(out, d)
	}
	return out, rows.Err()
}

// SetDeviceLabel sets (or clears) the operator's label. UPDATE-only: it never
// conjures a cursor row for a device that has not synced. Reports existence.
func (s Store) SetDeviceLabel(ctx context.Context, siteID, label string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE sync_cursors SET operator_label=$1 WHERE site_id=$2`, pgText(label), siteID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// PruneDevice removes a device's relay cursor. Cleanup only: its authored ops
// and enrollment stay, and a live device re-registers on its next sync.
func (s Store) PruneDevice(ctx context.Context, siteID string) (bool, error) {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM sync_cursors WHERE site_id=$1`, siteID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
