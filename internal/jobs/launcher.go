package jobs

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/aws/aws-sdk-go-v2/service/batch/types"
	"github.com/google/uuid"
)

// Launcher wakes execution capacity after a durable job commits. Launch is
// deliberately at-least-once: the job lease and idempotency key, not the cloud
// scheduler, decide whether work may run.
type Launcher interface {
	Launch(context.Context, uuid.UUID) error
}

type LocalLauncher struct{}

// Local workers poll PostgreSQL and need no external wake-up.
func (LocalLauncher) Launch(context.Context, uuid.UUID) error { return nil }

type batchSubmitter interface {
	SubmitJob(context.Context, *batch.SubmitJobInput, ...func(*batch.Options)) (*batch.SubmitJobOutput, error)
}

type AWSBatchLauncher struct {
	client        batchSubmitter
	queue         string
	jobDefinition string
}

func NewAWSBatchLauncher(client *batch.Client, queue, jobDefinition string) (*AWSBatchLauncher, error) {
	if client == nil || queue == "" || jobDefinition == "" {
		return nil, fmt.Errorf("AWS Batch launcher requires client, queue, and job definition")
	}
	return &AWSBatchLauncher{client: client, queue: queue, jobDefinition: jobDefinition}, nil
}

func (l *AWSBatchLauncher) Launch(ctx context.Context, id uuid.UUID) error {
	_, err := l.client.SubmitJob(ctx, &batch.SubmitJobInput{
		JobName:       aws.String("alexandria-" + id.String()),
		JobQueue:      aws.String(l.queue),
		JobDefinition: aws.String(l.jobDefinition),
		ContainerOverrides: &types.ContainerOverrides{
			Environment: []types.KeyValuePair{{Name: aws.String("ALEXANDRIA_JOB_ID"), Value: aws.String(id.String())}},
		},
	})
	if err != nil {
		return fmt.Errorf("submit AWS Batch job %s: %w", id, err)
	}
	return nil
}

type Service struct {
	Store    *Store
	Launcher Launcher
}

func (s Service) Enqueue(ctx context.Context, jobType, idempotencyKey string, payload any, maxAttempts int) (uuid.UUID, bool, error) {
	id, created, err := s.Store.Enqueue(ctx, jobType, idempotencyKey, payload, maxAttempts)
	if err != nil || !created {
		return id, created, err
	}
	if err := s.Launcher.Launch(ctx, id); err != nil {
		// The row intentionally remains pending. Reconciliation can launch it
		// later, and callers receive the scheduling failure rather than a lie.
		return id, true, err
	}
	return id, true, nil
}
