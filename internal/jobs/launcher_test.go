package jobs

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/batch"
	"github.com/google/uuid"
)

type fakeBatch struct{ input *batch.SubmitJobInput }

func (f *fakeBatch) SubmitJob(_ context.Context, input *batch.SubmitJobInput, _ ...func(*batch.Options)) (*batch.SubmitJobOutput, error) {
	f.input = input
	return &batch.SubmitJobOutput{}, nil
}

func TestAWSBatchLauncher(t *testing.T) {
	fake := &fakeBatch{}
	launcher := &AWSBatchLauncher{client: fake, queue: "q", jobDefinition: "job"}
	id := uuid.New()
	if err := launcher.Launch(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if fake.input == nil || len(fake.input.ContainerOverrides.Environment) != 1 || *fake.input.ContainerOverrides.Environment[0].Value != id.String() {
		t.Fatalf("wrong Batch input: %+v", fake.input)
	}
}
