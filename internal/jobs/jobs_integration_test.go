package jobs

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/aragonite-loom/internal/database"
)

func TestConcurrentClaimsIntegration(t *testing.T) {
	databaseURL := os.Getenv("LOOM_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("LOOM_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db, slog.Default()); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	prefix := uuid.NewString()
	var ids []uuid.UUID
	for i := 0; i < 12; i++ {
		id, _, err := store.Enqueue(ctx, "test", prefix+uuid.NewString(), map[string]int{"n": i}, 3)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM loom_jobs WHERE idempotency_key LIKE $1`, prefix+"%")
		_ = db.Close()
	})

	claimed := make(chan uuid.UUID, len(ids))
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			owner := "worker-" + uuid.NewString()
			for attempt := 0; attempt < 20; attempt++ {
				job, err := store.Claim(ctx, owner, time.Minute)
				if err != nil {
					t.Errorf("claim %d: %v", i, err)
					return
				}
				if job != nil {
					claimed <- job.ID
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}
	wg.Wait()
	close(claimed)
	seen := map[uuid.UUID]bool{}
	for id := range claimed {
		if seen[id] {
			t.Fatalf("job %s claimed twice", id)
		}
		seen[id] = true
	}
	if len(seen) != len(ids) {
		t.Fatalf("claimed %d jobs, want %d", len(seen), len(ids))
	}
}
