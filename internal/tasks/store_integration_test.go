package tasks

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/database"
)

func TestStoreIntegration(t *testing.T) {
	url := os.Getenv("ALEXANDRIA_TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("ALEXANDRIA_REQUIRE_INTEGRATION") == "1" {
			t.Fatal("database is required")
		}
		t.Skip("ALEXANDRIA_TEST_DATABASE_URL not set")
	}
	db, err := database.Open(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db, slog.Default()); err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	id := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM alexandria_tasks WHERE task_id=$1`, id)
		_ = db.Close()
	})
	title := "Weave this"
	now := time.Now().UnixMilli()
	if err := store.Upsert(context.Background(), Task{ID: id, Title: &title, Status: "inProcess", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), id)
	if err != nil || got.Title == nil || *got.Title != title || got.Status != "inProcess" {
		t.Fatalf("Get() = %+v, %v", got, err)
	}
	if err := store.Delete(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), id); err != ErrNotFound {
		t.Fatalf("Get(deleted) error = %v, want ErrNotFound", err)
	}
}
