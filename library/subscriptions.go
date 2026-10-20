package library

import (
	"context"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/subscriptions"
)

// ProcessCalendarSubscriptions refreshes bounded public ICS sources. Hosts can
// schedule this independently of recognition and indexing; each source is leased
// in PostgreSQL, so multiple workers cannot publish competing snapshots.
func (r *Runtime) ProcessCalendarSubscriptions(ctx context.Context, max int) (int, error) {
	conn, done, err := r.admit(ctx, Process)
	if err != nil {
		return 0, err
	}
	defer done()
	store := subscriptions.Store{DB: conn}
	n := 0
	for n < max && ctx.Err() == nil {
		found, err := store.Process(ctx, nil)
		if err != nil {
			return n, err
		}
		if !found {
			break
		}
		n++
	}
	return n, ctx.Err()
}
