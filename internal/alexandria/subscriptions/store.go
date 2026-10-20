package subscriptions

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

var ErrChanged = errors.New("subscription changed or removed")
var ErrLimit = errors.New("subscription limit reached")
var ErrInvalid = errors.New("invalid subscription")
var Colors = []string{"blue", "green", "purple", "orange", "gray"}

type Subscription struct {
	ID, Name, Color                      string
	Enabled                              bool
	Revision                             int64
	LastSuccess, LastAttempt, LeaseUntil *time.Time
	NextAt                               time.Time
	ErrorCode                            string
	feedURL, etag, modified, token       string
	failures                             int
}

func (s Subscription) Status() string {
	if !s.Enabled {
		return "Paused"
	}
	if s.LeaseUntil != nil && s.LeaseUntil.After(time.Now()) {
		return "Refreshing calendar…"
	}
	if s.ErrorCode != "" {
		return Problem(s.ErrorCode)
	}
	if s.LastSuccess == nil {
		return "Waiting for the first calendar refresh."
	}
	return "Calendar is up to date."
}
func Problem(code string) string {
	switch code {
	case "invalid_url":
		return "Enter a public HTTPS iCalendar feed URL (port 443)."
	case "too_large":
		return "This calendar exceeds the subscription import limits."
	case "invalid_feed":
		return "The source did not return a complete, valid iCalendar feed."
	case "http_error":
		return "The calendar source returned an HTTP error. Check that the feed is public."
	default:
		return "The calendar could not be fetched. Check its public URL and try again."
	}
}
func valid(name, color string) bool {
	if len(name) == 0 || len(name) > 120 {
		return false
	}
	for _, c := range Colors {
		if c == color {
			return true
		}
	}
	return false
}

type Store struct{ DB pg.DB }

const columns = `id,name,color,enabled,revision,next_at,lease_until,last_success,last_attempt,error_code,feed_url,etag,modified,lease_token,failures`

func scan(row interface{ Scan(...any) error }) (Subscription, error) {
	var s Subscription
	err := row.Scan(&s.ID, &s.Name, &s.Color, &s.Enabled, &s.Revision, &s.NextAt, &s.LeaseUntil, &s.LastSuccess, &s.LastAttempt, &s.ErrorCode, &s.feedURL, &s.etag, &s.modified, &s.token, &s.failures)
	return s, err
}
func (s Store) List(ctx context.Context) ([]Subscription, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+columns+` FROM alexandria_calendar_subscription ORDER BY lower(name),id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s Store) Get(ctx context.Context, id string) (Subscription, error) {
	return scan(s.DB.QueryRowContext(ctx, `SELECT `+columns+` FROM alexandria_calendar_subscription WHERE id=$1`, id))
}
func (s Store) Create(ctx context.Context, name, color, raw string) (string, error) {
	name, raw = strings.TrimSpace(name), strings.TrimSpace(raw)
	if !valid(name, color) {
		return "", ErrInvalid
	}
	if err := ValidateURL(raw); err != nil {
		return "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(74085435421282)`); err != nil {
		return "", err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM alexandria_calendar_subscription`).Scan(&n); err != nil {
		return "", err
	}
	if n >= MaxSources {
		return "", ErrLimit
	}
	id := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `INSERT INTO alexandria_calendar_subscription(id,name,color,feed_url) VALUES($1,$2,$3,$4)`, id, name, color, raw); err != nil {
		return "", err
	}
	return id, tx.Commit()
}
func changed(r sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n == 0 {
		return ErrChanged
	}
	return err
}
func (s Store) Update(ctx context.Context, id string, rev int64, name, color string, enabled bool) error {
	name = strings.TrimSpace(name)
	if !valid(name, color) {
		return ErrInvalid
	}
	return changed(s.DB.ExecContext(ctx, `UPDATE alexandria_calendar_subscription SET name=$3,color=$4,enabled=$5,revision=revision+1,lease_token='',lease_until=NULL,next_at=now() WHERE id=$1 AND revision=$2`, id, rev, name, color, enabled))
}
func (s Store) Remove(ctx context.Context, id string, rev int64) error {
	return changed(s.DB.ExecContext(ctx, `DELETE FROM alexandria_calendar_subscription WHERE id=$1 AND revision=$2`, id, rev))
}
func (s Store) Refresh(ctx context.Context, id string) error {
	// An in-flight attempt already satisfies the request; never start duplicates.
	_, err := s.DB.ExecContext(ctx, `UPDATE alexandria_calendar_subscription SET next_at=greatest(now(),coalesce(last_attempt+interval '30 seconds',now())) WHERE id=$1 AND enabled AND (lease_until IS NULL OR lease_until<now())`, id)
	return err
}
func (s Store) claim(ctx context.Context) (Subscription, error) {
	token := uuid.NewString()
	return scan(s.DB.QueryRowContext(ctx, `WITH due AS (SELECT id FROM alexandria_calendar_subscription WHERE enabled AND next_at<=now() AND (lease_until IS NULL OR lease_until<now()) ORDER BY next_at,id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE alexandria_calendar_subscription SET lease_token=$1,lease_until=now()+interval '2 minutes',last_attempt=now() WHERE id=(SELECT id FROM due) RETURNING `+columns, token))
}

// Process fetches one due source outside a transaction. The lease token and
// configuration revision fence publication against retries, pause and unsubscribe.
// client is injectable for synthetic fixtures; nil always uses the public client.
func (s Store) Process(ctx context.Context, client *http.Client) (bool, error) {
	sub, err := s.claim(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if client == nil {
		client = PublicClient()
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	f, problem := fetch(fetchCtx, client, sub)
	var series []Series
	if problem == "" && !f.unchanged {
		series, err = Parse(fetchCtx, f.raw)
		if err != nil {
			problem = err.Error()
			if problem != "too_large" {
				problem = "invalid_feed"
			}
		}
	}
	if ctx.Err() != nil {
		return true, ctx.Err()
	} // durable lease expires after interruption
	return true, s.finish(ctx, sub, f, series, problem)
}
func (s Store) finish(ctx context.Context, sub Subscription, f fetched, series []Series, problem string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT id FROM alexandria_calendar_subscription WHERE id=$1 AND revision=$2 AND lease_token=$3 AND enabled AND lease_until>now() FOR UPDATE`, sub.ID, sub.Revision, sub.token).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if problem != "" {
		delay := min(120, 5*(1<<min(sub.failures, 5)))
		_, err = tx.ExecContext(ctx, `UPDATE alexandria_calendar_subscription SET lease_token='',lease_until=NULL,failures=least(failures+1,20),error_code=$2,next_at=now()+$3::interval WHERE id=$1`, sub.ID, problem, (time.Duration(delay) * time.Minute).String())
	} else {
		if !f.unchanged {
			if _, err = tx.ExecContext(ctx, `DELETE FROM alexandria_calendar_import WHERE subscription_id=$1`, sub.ID); err != nil {
				return err
			}
			for _, v := range series {
				if _, err = tx.ExecContext(ctx, `INSERT INTO alexandria_calendar_import(subscription_id,uid,series_key,ical_blob) VALUES($1,$2,$3,$4)`, sub.ID, v.UID, v.Key, v.Blob); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `UPDATE alexandria_calendar_subscription SET original_feed=$2,etag=$3,modified=$4 WHERE id=$1`, sub.ID, string(f.raw), f.etag, f.modified); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE alexandria_calendar_subscription SET lease_token='',lease_until=NULL,failures=0,error_code='',last_success=now(),next_at=now()+interval '30 minutes' WHERE id=$1`, sub.ID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

type Imported struct {
	Event                            events.Event
	SubscriptionID, Key, Name, Color string
}

func EventID(source, key string) string { return "subscription:" + source + ":" + key }
func (s Store) Read(ctx context.Context, ids []string, loc *time.Location) ([]Imported, []events.Incomplete, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT s.id,s.name,s.color,i.series_key,i.ical_blob FROM alexandria_calendar_import i JOIN alexandria_calendar_subscription s ON s.id=i.subscription_id WHERE s.enabled AND s.id=ANY($1) ORDER BY s.id,i.series_key`, ids)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []Imported{}
	issues := []events.Incomplete{}
	for rows.Next() {
		var v Imported
		var blob string
		if err = rows.Scan(&v.SubscriptionID, &v.Name, &v.Color, &v.Key, &blob); err != nil {
			return nil, nil, err
		}
		e, err := events.FromSeries(EventID(v.SubscriptionID, v.Key), blob, loc)
		if err != nil {
			issues = append(issues, events.Incomplete{ID: e.ID, Title: v.Name + ": " + e.Title, Reason: "unsupported"})
			continue
		}
		v.Event = e
		out = append(out, v)
	}
	return out, issues, rows.Err()
}
