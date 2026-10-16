// Package events stores calendar events in alexandria_events, served over
// CalDAV as VEVENT beside the Tasks collection, on the Calendar page and over
// MCP. Deletion follows tasks: a soft delete leaves a tombstone that
// sync-collection reports, and a purged UID can never be recreated.
package events

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("event not found")
	ErrDeleted  = errors.New("event is deleted")
	ErrPurged   = errors.New("event was permanently deleted")
	ErrInvalid  = errors.New("invalid event")
)

// Event is one calendar entry. For an all-day event Start and End are
// midnight UTC of the first day and of the day after the last.
type Event struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Detail   string    `json:"detail,omitempty"`
	Location string    `json:"location,omitempty"`
	URL      string    `json:"url,omitempty"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	AllDay   bool      `json:"all_day,omitempty"`
	// TZID is the IANA zone a timed event is shown in ("" = UTC).
	TZID string `json:"tzid,omitempty"`
	// Blob is the iCalendar text a client sent, kept for round trips.
	Blob      string `json:"-"`
	Deleted   bool   `json:"deleted,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// Zone is the event's time zone, falling back to UTC.
func (e Event) Zone() *time.Location {
	if e.TZID != "" {
		if loc, err := time.LoadLocation(e.TZID); err == nil {
			return loc
		}
	}
	return time.UTC
}

// LastDay is the final day of an all-day event (End is exclusive).
func (e Event) LastDay() time.Time { return e.End.AddDate(0, 0, -1) }

// Normalize checks an event before it is stored.
func (e *Event) Normalize() error {
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" {
		return fmt.Errorf("%w: a title is required", ErrInvalid)
	}
	if e.Start.IsZero() {
		return fmt.Errorf("%w: a start is required", ErrInvalid)
	}
	if e.AllDay {
		e.Start = day(e.Start)
		if e.End.IsZero() || !e.End.After(e.Start) {
			e.End = e.Start.AddDate(0, 0, 1)
		}
		e.End = day(e.End)
	} else if e.End.IsZero() {
		e.End = e.Start.Add(time.Hour)
	} else if e.End.Before(e.Start) {
		return fmt.Errorf("%w: the end is before the start", ErrInvalid)
	}
	if e.TZID != "" {
		if _, err := time.LoadLocation(e.TZID); err != nil {
			return fmt.Errorf("%w: unknown time zone %q", ErrInvalid, e.TZID)
		}
	}
	return nil
}

// day is midnight UTC of t's calendar date (as written, ignoring its zone).
func day(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// NewID makes a UID for an event created in Alexandria.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:]) + "@alexandria"
}

// DB is satisfied by *sql.DB and *sql.Conn.
type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

// Execer is a DB or a transaction, for writes inside a caller's transaction.
type Execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct{ db DB }

func NewStore(db DB) *Store { return &Store{db: db} }

const columns = `event_id,title,detail,location,url,start_at,end_at,all_day,tzid,ical_blob,deleted,created_at,updated_at`

func scan(s interface{ Scan(...any) error }) (Event, error) {
	var e Event
	err := s.Scan(&e.ID, &e.Title, &e.Detail, &e.Location, &e.URL, &e.Start, &e.End, &e.AllDay, &e.TZID, &e.Blob, &e.Deleted, &e.CreatedAt, &e.UpdatedAt)
	e.Start, e.End = e.Start.UTC(), e.End.UTC()
	return e, err
}

func (s *Store) list(ctx context.Context, q string, args ...any) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+" FROM alexandria_events "+q, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// List is every live event, by start.
func (s *Store) List(ctx context.Context) ([]Event, error) {
	return s.list(ctx, "WHERE NOT deleted ORDER BY start_at,event_id")
}

// Range is the live events overlapping [from, to); a zero bound is open.
func (s *Store) Range(ctx context.Context, from, to time.Time) ([]Event, error) {
	q, args := "WHERE NOT deleted", []any{}
	if !from.IsZero() {
		args = append(args, from)
		q += fmt.Sprintf(" AND end_at > $%d", len(args))
	}
	if !to.IsZero() {
		args = append(args, to)
		q += fmt.Sprintf(" AND start_at < $%d", len(args))
	}
	return s.list(ctx, q+" ORDER BY start_at,event_id", args...)
}

// Deleted is the trash, newest deletion first; purged events are not shown.
func (s *Store) Deleted(ctx context.Context) ([]Event, error) {
	return s.list(ctx, "e WHERE deleted AND NOT EXISTS (SELECT 1 FROM alexandria_event_purged p WHERE p.event_id=e.event_id) ORDER BY updated_at DESC")
}

func (s *Store) Get(ctx context.Context, id string) (Event, error) {
	e, err := scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM alexandria_events WHERE event_id=$1 AND NOT deleted", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Event{}, ErrNotFound
	}
	return e, err
}

// Create stores a new event, assigning a UID when it has none. A UID that is
// in the trash or was purged is refused, so a device that missed a deletion
// can't bring the event back.
func (s *Store) Create(ctx context.Context, e *Event) error { return Create(ctx, s.db, e) }

// Create is Store.Create inside a caller's transaction.
func Create(ctx context.Context, db Execer, e *Event) error {
	if err := e.Normalize(); err != nil {
		return err
	}
	if e.ID == "" {
		e.ID = NewID()
	}
	var deleted, purged bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM alexandria_events WHERE event_id=$1 AND deleted),
		EXISTS (SELECT 1 FROM alexandria_event_purged WHERE event_id=$1)`, e.ID).Scan(&deleted, &purged); err != nil {
		return fmt.Errorf("check event: %w", err)
	}
	if purged {
		return ErrPurged
	}
	if deleted {
		return ErrDeleted
	}
	now := time.Now().UnixMilli()
	e.CreatedAt, e.UpdatedAt = now, now
	_, err := db.ExecContext(ctx, `INSERT INTO alexandria_events(`+columns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,false,$11,$11)`,
		e.ID, text(e.Title), text(e.Detail), text(e.Location), text(e.URL), e.Start, e.End, e.AllDay, e.TZID, text(e.Blob), now)
	if err != nil {
		return fmt.Errorf("create event: %w", err)
	}
	return nil
}

// Update replaces a live event's fields.
func (s *Store) Update(ctx context.Context, e *Event) error {
	if err := e.Normalize(); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE alexandria_events SET title=$2,detail=$3,location=$4,url=$5,start_at=$6,end_at=$7,
		all_day=$8,tzid=$9,ical_blob=$10,updated_at=$11 WHERE event_id=$1 AND NOT deleted`,
		e.ID, text(e.Title), text(e.Detail), text(e.Location), text(e.URL), e.Start, e.End, e.AllDay, e.TZID, text(e.Blob), now)
	if err != nil {
		return fmt.Errorf("update event: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	e.UpdatedAt = now
	return nil
}

// Delete moves an event to the trash.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE alexandria_events SET deleted=true,updated_at=$2 WHERE event_id=$1 AND NOT deleted`, id, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("delete event: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Restore takes an event out of the trash.
func (s *Store) Restore(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE alexandria_events SET deleted=false,updated_at=$2 WHERE event_id=$1 AND deleted
		AND NOT EXISTS (SELECT 1 FROM alexandria_event_purged WHERE event_id=$1)`, id, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("restore event: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var purged bool
		if s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM alexandria_event_purged WHERE event_id=$1)`, id).Scan(&purged) == nil && purged {
			return ErrPurged
		}
		return ErrNotFound
	}
	return nil
}

// Purge permanently deletes events already in the trash: content is erased,
// the tombstone stays for sync-collection, and the UID is reserved.
func (s *Store) Purge(ctx context.Context, ids []string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	res, err := tx.ExecContext(ctx, `WITH gone AS (UPDATE alexandria_events SET title='',detail='',location='',url='',ical_blob='',updated_at=$2
		WHERE event_id=ANY($1) AND deleted AND NOT EXISTS (SELECT 1 FROM alexandria_event_purged p WHERE p.event_id=alexandria_events.event_id)
		RETURNING event_id)
		INSERT INTO alexandria_event_purged(event_id,purged_at) SELECT event_id,$2 FROM gone ON CONFLICT DO NOTHING`, ids, now)
	if err != nil {
		return 0, fmt.Errorf("purge events: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, tx.Commit()
}

// MaxUpdatedAtAll is the collection change token, tombstones included.
func (s *Store) MaxUpdatedAtAll(ctx context.Context) (int64, error) {
	var max sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(updated_at) FROM alexandria_events`).Scan(&max)
	return max.Int64, err
}

// ListChangedSince is every row written at or after sinceMs, tombstones
// included, in write order (inclusive for the reason taskdb gives).
func (s *Store) ListChangedSince(ctx context.Context, sinceMs int64) ([]Event, error) {
	return s.list(ctx, "WHERE updated_at >= $1 ORDER BY updated_at,event_id", sinceMs)
}

// SyncFloor is 0: event tombstones are never pruned, so every token is answerable.
func (s *Store) SyncFloor(context.Context) (int64, error) { return 0, nil }

// text keeps a value storable in PostgreSQL text (no U+0000).
func text(s string) string { return strings.ReplaceAll(s, "\x00", "�") }
