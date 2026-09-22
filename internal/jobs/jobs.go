package jobs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var ErrLeaseLost = errors.New("job lease lost")

type Job struct {
	ID             uuid.UUID
	Type           string
	Payload        json.RawMessage
	IdempotencyKey string
	Attempts       int
	MaxAttempts    int
}

type DB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type Store struct{ db DB }

func NewStore(db DB) *Store { return &Store{db: db} }

func (s *Store) Enqueue(ctx context.Context, jobType, idempotencyKey string, payload any, maxAttempts int) (uuid.UUID, bool, error) {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("encode job payload: %w", err)
	}
	id := uuid.New()
	var stored uuid.UUID
	err = s.db.QueryRowContext(ctx, `
		INSERT INTO alexandria_jobs(id, job_type, payload, idempotency_key, max_attempts)
		VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key
		RETURNING id`, id, jobType, body, idempotencyKey, maxAttempts).Scan(&stored)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("enqueue job: %w", err)
	}
	return stored, stored == id, nil
}

func (s *Store) Claim(ctx context.Context, owner string, lease time.Duration) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT id FROM alexandria_jobs
			WHERE status='pending' AND available_at <= now() AND attempts < max_attempts
			ORDER BY available_at, created_at
			FOR UPDATE SKIP LOCKED LIMIT 1
		)
		UPDATE alexandria_jobs j SET
			status='leased', lease_owner=$1, lease_until=now()+$2::interval,
			attempts=j.attempts+1, started_at=COALESCE(j.started_at, now())
		FROM candidate WHERE j.id=candidate.id
		RETURNING j.id,j.job_type,j.payload,j.idempotency_key,j.attempts,j.max_attempts`, owner, interval(lease))
	var job Job
	if err := row.Scan(&job.ID, &job.Type, &job.Payload, &job.IdempotencyKey, &job.Attempts, &job.MaxAttempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("claim job: %w", err)
	}
	return &job, nil
}

// ClaimID is used by event-launched workers. It remains safe if the launcher
// delivers the same job more than once: only one invocation can take the lease.
func (s *Store) ClaimID(ctx context.Context, id uuid.UUID, owner string, lease time.Duration) (*Job, error) {
	row := s.db.QueryRowContext(ctx, `
		UPDATE alexandria_jobs SET status='leased', lease_owner=$2, lease_until=now()+$3::interval,
			attempts=attempts+1, started_at=COALESCE(started_at, now())
		WHERE id=$1 AND status='pending' AND available_at <= now() AND attempts < max_attempts
		RETURNING id,job_type,payload,idempotency_key,attempts,max_attempts`, id, owner, interval(lease))
	var job Job
	if err := row.Scan(&job.ID, &job.Type, &job.Payload, &job.IdempotencyKey, &job.Attempts, &job.MaxAttempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("claim job %s: %w", id, err)
	}
	return &job, nil
}

func (s *Store) Complete(ctx context.Context, id uuid.UUID, owner string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE alexandria_jobs SET status='done',finished_at=now(),lease_owner=NULL,lease_until=NULL
		WHERE id=$1 AND status='leased' AND lease_owner=$2`, id, owner)
	return leaseResult(result, err)
}

func (s *Store) Fail(ctx context.Context, job Job, owner string, cause error) error {
	delay := time.Minute << min(job.Attempts-1, 5)
	status := "pending"
	if job.Attempts >= job.MaxAttempts {
		status = "failed"
	}
	result, err := s.db.ExecContext(ctx, `UPDATE alexandria_jobs SET status=$3::alexandria_job_status,last_error=$4,
		available_at=CASE WHEN $3='pending' THEN now()+$5::interval ELSE available_at END,
		finished_at=CASE WHEN $3='failed' THEN now() ELSE NULL END,lease_owner=NULL,lease_until=NULL
		WHERE id=$1 AND status='leased' AND lease_owner=$2`, job.ID, owner, status, cause.Error(), interval(delay))
	return leaseResult(result, err)
}

func (s *Store) RequeueExpired(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE alexandria_jobs SET status=CASE WHEN attempts>=max_attempts THEN 'failed'::alexandria_job_status ELSE 'pending'::alexandria_job_status END,
		lease_owner=NULL,lease_until=NULL,last_error='worker lease expired',
		finished_at=CASE WHEN attempts>=max_attempts THEN now() ELSE NULL END
		WHERE status='leased' AND lease_until < now()`)
	if err != nil {
		return 0, fmt.Errorf("requeue expired jobs: %w", err)
	}
	return result.RowsAffected()
}

func leaseResult(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}

func interval(d time.Duration) string { return fmt.Sprintf("%f seconds", d.Seconds()) }
