package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
)

// Channels requested by a device are advisory subscriptions, never grant
// authority. Only this native library's namespace can be retained.
func (s Service) updateChannels(ctx context.Context, d device, action string, requested []string) ([]string, error) {
	if action == "" {
		action = "set"
	}
	if action != "set" && action != "add" && action != "remove" {
		return nil, errors.New("unsupported channel action")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	previous, e := s.lockDeviceChannels(ctx, tx, d)
	if e != nil {
		return nil, e
	}
	set := map[string]bool{}
	if action != "set" {
		for _, c := range previous {
			set[c] = true
		}
	}
	for _, c := range requested {
		if !strings.HasPrefix(c, s.uid()+"-") {
			continue
		}
		if action == "remove" {
			delete(set, c)
		} else {
			set[c] = true
		}
	}
	out := []string{}
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	// Serialize Gateway updates with session creation and revocation. A failed
	// upstream call does not acknowledge a subscription change to the client.
	if e = s.applyGatewayChannels(ctx, d, out); e != nil {
		return nil, e
	}
	raw, _ := json.Marshal(out)
	_, e = tx.ExecContext(ctx, `UPDATE boox_device SET requested_channels=$2 WHERE id=$1`, d.ID, raw)
	if e == nil {
		e = tx.Commit()
	}
	return out, e
}

// All grant mutations take the generation fence then one exclusive device lock.
func (s Service) lockDeviceChannels(ctx context.Context, tx *sql.Tx, d device) ([]string, error) {
	var generation string
	if e := tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); e != nil {
		return nil, e
	}
	if generation != d.Generation {
		return nil, errAuth
	}
	var raw []byte
	if e := tx.QueryRowContext(ctx, `SELECT requested_channels FROM boox_device WHERE id=$1 AND NOT revoked AND generation=$2 FOR UPDATE`, d.ID, generation).Scan(&raw); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, errAuth
		}
		return nil, e
	}
	var channels []string
	if e := json.Unmarshal(raw, &channels); e != nil {
		return nil, e
	}
	return channels, nil
}
func (s Service) applyGatewayChannels(ctx context.Context, d device, requested []string) error {
	principal := "ps_" + strings.ReplaceAll(d.ID, "-", "")
	path := "/_user/" + url.PathEscape(principal)
	var existing map[string]any
	err := s.Config.gateway(ctx, "GET", path, nil, &existing)
	body := map[string]any{"name": principal, "disabled": false}
	if err != nil {
		upstream, ok := err.(UpstreamError)
		if !ok || upstream.Status != 404 {
			return err
		}
		body["password"] = token() // Do not rotate an existing user's password on every session.
	}
	channels := []string{s.uid()}
	for _, c := range requested {
		if len(c) <= 256 && strings.HasPrefix(c, s.uid()+"-") {
			channels = append(channels, c)
		}
	}
	body["admin_channels"] = channels
	return s.Config.gateway(ctx, "PUT", path, body, nil)
}
