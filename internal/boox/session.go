package boox

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"
)

// Production policy, not the lab's accelerated-expiry controls. The stock SDK
// uses six-month account validity and a one-month refresh interval. The device
// gets no Gateway expiry field, so only the facade rotates backend cookies.
const nativeAccountTTL = 180 * 24 * time.Hour
const nativeSessionIdleTTL = 180 * 24 * time.Hour
const nativeBackendSessionTTL = 24 * time.Hour

func (s Service) createBackendSession(ctx context.Context, d device) (string, time.Time, error) {
	var grant struct {
		CookieName string    `json:"cookie_name"`
		SessionID  string    `json:"session_id"`
		Expires    time.Time `json:"expires"`
	}
	principal := "ps_" + strings.ReplaceAll(d.ID, "-", "")
	if e := s.Config.gateway(ctx, "POST", "/_session", map[string]any{"name": principal, "ttl": int(nativeBackendSessionTTL.Seconds())}, &grant); e != nil {
		return "", time.Time{}, e
	}
	if grant.CookieName != "SyncGatewaySession" || grant.SessionID == "" || !grant.Expires.After(time.Now()) {
		return "", time.Time{}, errors.New("invalid gateway session")
	}
	return grant.SessionID, grant.Expires, nil
}
func (s Service) session(ctx context.Context, d device) (any, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	channels, e := s.lockDeviceChannels(ctx, tx, d)
	if e != nil {
		return nil, e
	}
	if e = s.applyGatewayChannels(ctx, d, channels); e != nil {
		return nil, e
	}
	backend, expires, e := s.createBackendSession(ctx, d)
	if e != nil {
		return nil, e
	}
	front := "PSG" + token()
	_, e = tx.ExecContext(ctx, `INSERT INTO boox_grant(id,device_id,kind,secret,expires_at,backend_session_id,backend_expires_at) VALUES($1,$2,'session','',$3,$4,$5)`, front, d.ID, time.Now().Add(nativeSessionIdleTTL), backend, expires)
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		_ = s.Config.gateway(ctx, "DELETE", "/_session/"+url.PathEscape(backend), nil, nil)
		return nil, e
	}
	return map[string]string{"cookie_name": "SyncGatewaySession", "session_id": front}, nil
}

// expectedBackend requests repair after an upstream 401. A concurrent repair
// wins without issuing another cookie. Expired/revoked frontend handles never renew.
func (s Service) sessionBackend(ctx context.Context, front, expectedBackend string) (string, error) {
	var d device
	e := s.DB.QueryRowContext(ctx, `SELECT d.id::text,d.account,coalesce(d.native_fingerprint,''),d.generation FROM boox_grant a JOIN boox_device d ON d.id=a.device_id JOIN sync_library_generation g ON g.generation=d.generation WHERE g.id=1 AND a.id=$1 AND a.kind='session' AND a.expires_at>now() AND NOT d.revoked`, front).Scan(&d.ID, &d.Account, &d.Fingerprint, &d.Generation)
	if errors.Is(e, sql.ErrNoRows) {
		return "", errAuth
	}
	if e != nil {
		return "", e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	channels, e := s.lockDeviceChannels(ctx, tx, d)
	if e != nil {
		return "", e
	}
	var backend sql.NullString
	var expires sql.NullTime
	e = tx.QueryRowContext(ctx, `SELECT backend_session_id,backend_expires_at FROM boox_grant WHERE id=$1 AND device_id=$2 AND kind='session' AND expires_at>now() FOR UPDATE`, front, d.ID).Scan(&backend, &expires)
	if errors.Is(e, sql.ErrNoRows) {
		return "", errAuth
	}
	if e != nil {
		return "", e
	}
	if !backend.Valid || !expires.Valid || expires.Time.Before(time.Now().Add(5*time.Minute)) || (expectedBackend != "" && expectedBackend == backend.String) {
		if e = s.applyGatewayChannels(ctx, d, channels); e != nil {
			return "", e
		}
		fresh, deadline, err := s.createBackendSession(ctx, d)
		if err != nil {
			return "", err
		}
		backend.String = fresh
		backend.Valid = true
		expires.Time = deadline
		expires.Valid = true
	}
	_, e = tx.ExecContext(ctx, `UPDATE boox_grant SET backend_session_id=$2,backend_expires_at=$3,expires_at=$4 WHERE id=$1`, front, backend.String, expires.Time, time.Now().Add(nativeSessionIdleTTL))
	if e == nil {
		e = tx.Commit()
	}
	return backend.String, e
}
func (s Service) keepSessionActive(ctx context.Context, front string) error {
	// Limit durable writes to approximately hourly while checking revocation every second.
	_, e := s.DB.ExecContext(ctx, `UPDATE boox_grant a SET expires_at=$2 FROM boox_device d,sync_library_generation g WHERE a.device_id=d.id AND g.id=1 AND d.generation=g.generation AND NOT d.revoked AND a.id=$1 AND a.kind='session' AND a.expires_at>now() AND a.expires_at<$3`, front, time.Now().Add(nativeSessionIdleTTL), time.Now().Add(nativeSessionIdleTTL-time.Hour))
	return e
}
