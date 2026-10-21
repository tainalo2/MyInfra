package store

import (
	"context"
	"time"
)

// leadLock is held by the copy that drives the machines while several run
// against the same database: the two pods of MyInfra in a cluster.
const leadLock = 842017

// Lead waits until this process holds the lead, and keeps it as long as the
// process lives: the lock goes with its connection. It returns false when
// the context ends first.
func (s *Store) Lead(ctx context.Context) bool {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false
	}
	for {
		var held bool
		if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, leadLock).Scan(&held); err == nil && held {
			// The connection leaves the pool: given back, it would keep the
			// lock for whoever takes it next. Closed, it frees the lock.
			own := conn.Hijack()
			go func() {
				<-ctx.Done()
				_ = own.Close(context.Background())
			}()
			return true
		}
		select {
		case <-ctx.Done():
			conn.Release()
			return false
		case <-time.After(5 * time.Second):
		}
	}
}
