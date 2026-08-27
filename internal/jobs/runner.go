package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

type Handler func(context.Context, Job) error

type Runner struct {
	Store    *Store
	Owner    string
	Lease    time.Duration
	Poll     time.Duration
	Handlers map[string]Handler
	Logger   *slog.Logger
}

// RunOne executes exactly the durable job named by an external scheduler.
// A nil result is success: another delivery already owns or completed it.
func (r *Runner) RunOne(ctx context.Context, id uuid.UUID) error {
	job, err := r.Store.ClaimID(ctx, id, r.Owner, r.Lease)
	if err != nil || job == nil {
		return err
	}
	handler := r.Handlers[job.Type]
	if handler == nil {
		err = fmt.Errorf("unknown job type %q", job.Type)
	} else {
		err = handler(ctx, *job)
	}
	if err != nil {
		if failErr := r.Store.Fail(ctx, *job, r.Owner, err); failErr != nil {
			return fmt.Errorf("job failed: %v; record failure: %w", err, failErr)
		}
		return err
	}
	return r.Store.Complete(ctx, job.ID, r.Owner)
}

func (r *Runner) Run(ctx context.Context) error {
	if r.Store == nil || r.Owner == "" {
		return fmt.Errorf("job runner requires a store and owner")
	}
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	if r.Poll <= 0 {
		r.Poll = 5 * time.Second
	}
	if r.Lease <= 0 {
		r.Lease = 15 * time.Minute
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		job, err := r.Store.Claim(ctx, r.Owner, r.Lease)
		if err != nil {
			r.Logger.Error("claim job", "error", err)
			timer.Reset(r.Poll)
			continue
		}
		if job == nil {
			timer.Reset(r.Poll)
			continue
		}
		handler := r.Handlers[job.Type]
		if handler == nil {
			err = fmt.Errorf("unknown job type %q", job.Type)
		} else {
			err = handler(ctx, *job)
		}
		if err != nil {
			r.Logger.Warn("job failed", "job_id", job.ID, "job_type", job.Type, "attempt", job.Attempts, "error", err)
			if failErr := r.Store.Fail(ctx, *job, r.Owner, err); failErr != nil {
				r.Logger.Error("record job failure", "job_id", job.ID, "error", failErr)
			}
		} else if err := r.Store.Complete(ctx, job.ID, r.Owner); err != nil {
			r.Logger.Error("complete job", "job_id", job.ID, "error", err)
		}
		timer.Reset(0)
	}
}
