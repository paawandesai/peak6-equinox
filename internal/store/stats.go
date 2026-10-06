package store

import (
	"context"
	"fmt"
)

// DatabaseSize returns the on-disk size in bytes of the connected database
// (pg_database_size), for the admin page.
func (s *Store) DatabaseSize(ctx context.Context) (int64, error) {
	var size int64
	if err := s.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&size); err != nil {
		return 0, fmt.Errorf("reading database size: %w", err)
	}
	return size, nil
}
