package library

import (
	"context"
	"testing"
)

func TestCalendarSubscriptionWorkerAdmission(t *testing.T) {
	r, db, id := fixture(t)
	ctx := context.Background()
	if n, err := r.ProcessCalendarSubscriptions(ctx, 1); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	for _, mode := range []string{"read_only", "maintenance"} {
		if err := SetMode(ctx, db, id, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := r.ProcessCalendarSubscriptions(ctx, 1); err == nil {
			t.Fatalf("worker admitted in %s", mode)
		}
	}
}
