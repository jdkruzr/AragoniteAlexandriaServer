package taskdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskstore"
)

func TestConcurrentUpsertDeletePurge(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	for i := 0; i < 15; i++ {
		id := fmt.Sprintf("concurrent-%d", i)
		task := taskstore.Task{TaskID: id}
		if err := s.Create(ctx, &task); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			v := task
			err := s.Upsert(ctx, &v)
			if errors.Is(err, taskstore.ErrDeleted) || errors.Is(err, taskstore.ErrPurged) {
				err = nil
			}
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			err := s.Delete(ctx, id)
			if err == nil {
				_, err = s.Purge(ctx, []string{id})
			}
			errs <- err
		}()
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Get(ctx, id); !errors.Is(err, taskstore.ErrNotFound) {
			t.Fatal("task resurrected", err)
		}
		if err := s.Upsert(ctx, &task); !errors.Is(err, taskstore.ErrPurged) {
			t.Fatal(err)
		}
	}
}
func TestImportRespectsReservations(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	v := taskstore.Task{TaskID: "import", CreatedAt: 10, UpdatedAt: 20, LastModified: sql.NullInt64{Int64: 15, Valid: true}}
	if err := s.Import(ctx, &v); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Get(ctx, v.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.CreatedAt != 10 || saved.UpdatedAt != 20 || saved.LastModified.Int64 != 15 {
		t.Fatal("historical timestamps lost")
	}
	if err = s.Delete(ctx, v.TaskID); err != nil {
		t.Fatal(err)
	}
	if err = s.Import(ctx, &v); !errors.Is(err, taskstore.ErrDeleted) {
		t.Fatal(err)
	}
	if _, err = s.Purge(ctx, []string{v.TaskID}); err != nil {
		t.Fatal(err)
	}
	if err = s.Import(ctx, &v); !errors.Is(err, taskstore.ErrPurged) {
		t.Fatal(err)
	}
}
