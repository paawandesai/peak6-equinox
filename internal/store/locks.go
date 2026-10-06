package store

import (
	"context"
	"fmt"
	"time"
)

// unlockTimeout bounds the unlock statement when a lock is released.
const unlockTimeout = 10 * time.Second

// TryLockIngest takes the advisory lock that serialises fetch rounds across
// every process sharing this database schema (several Cloud Run instances,
// Cloud Scheduler plus a manual trigger). The lock is session-level on one
// pooled connection, which release gives back after unlocking. acquired is
// false, with no error, when another holder has it. The key is derived from
// the current schema, so test packages (one schema each) never block each
// other while production, with one schema, is serialised.
func (s *Store) TryLockIngest(ctx context.Context) (release func(), acquired bool, err error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("acquiring ingest lock: %w", err)
	}
	err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('equinox-ingest:' || current_schema()))`).Scan(&acquired)
	if err != nil {
		conn.Release()
		return nil, false, fmt.Errorf("acquiring ingest lock: %w", err)
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	release = func() {
		// Unlock on a fresh context: the caller's may already be cancelled.
		uctx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
		defer cancel()
		_, _ = conn.Exec(uctx, `SELECT pg_advisory_unlock(hashtext('equinox-ingest:' || current_schema()))`)
		conn.Release()
	}
	return release, true, nil
}
