// Package settings reads and writes user-facing runtime settings stored in
// alexandria_settings (string values as JSON strings).
package settings

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

const (
	CalDAVCollectionName = "caldav_collection_name"
	DueTimeMode          = "due_time_mode"
	TaskAttachSecret     = "task_attach_secret"
	// CalDAVEventsName is the events calendar's display name.
	CalDAVEventsName = "caldav_events_collection_name"
	// TimeZone is the owner's IANA time zone, for dates typed or written
	// without one ("" = the server's zone).
	TimeZone = "time_zone"
	// NoteActionsSince is when note actions started (RFC 3339): older ink
	// is only acted on from a page's Find actions button.
	NoteActionsSince = "note_actions_since"
	// AutomaticSince is when automatic processing started (RFC 3339): only
	// notebooks that change afterwards are processed automatically.
	AutomaticSince = "automatic_since"
)

// Zone returns the owner's time zone: the saved setting, else the server's
// local zone. An unknown saved name falls back the same way.
func Zone(ctx context.Context, db pg.Querier) *time.Location {
	name, err := Get(ctx, db, TimeZone, "")
	if err == nil && name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.Local
}

// ZoneName is the name Zone's location is shown and stored under.
func ZoneName(loc *time.Location) string {
	if loc == nil || loc.String() == "Local" {
		if tz := os.Getenv("TZ"); tz != "" {
			if _, err := time.LoadLocation(tz); err == nil {
				return tz
			}
		}
		return "UTC"
	}
	return loc.String()
}

// Get returns a string setting, or fallback when unset.
func Get(ctx context.Context, db pg.Querier, key, fallback string) (string, error) {
	var raw []byte
	err := db.QueryRowContext(ctx, `SELECT value FROM alexandria_settings WHERE key=$1`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return "", err
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || value == "" {
		return fallback, nil
	}
	return value, nil
}

// Set stores a string setting.
func Set(ctx context.Context, db pg.Querier, key, value string, secret bool) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO alexandria_settings(key,value,secret,updated_at) VALUES($1,$2,$3,now())
		ON CONFLICT (key) DO UPDATE SET value=EXCLUDED.value, secret=EXCLUDED.secret, updated_at=now()`, key, raw, secret)
	return err
}

// EnsureSecret returns a stable random secret, creating it once. Concurrent
// first calls agree: the first insert wins and everyone reads it back.
func EnsureSecret(ctx context.Context, db pg.Querier, key string) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	raw, _ := json.Marshal(hex.EncodeToString(b[:]))
	if _, err := db.ExecContext(ctx, `INSERT INTO alexandria_settings(key,value,secret) VALUES($1,$2,true) ON CONFLICT (key) DO NOTHING`, key, raw); err != nil {
		return "", err
	}
	secret, err := Get(ctx, db, key, "")
	if err == nil && len(secret) < 32 {
		err = errors.New("stored secret is too short")
	}
	return secret, err
}
