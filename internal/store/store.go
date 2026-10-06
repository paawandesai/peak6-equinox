// Package store is Equinox's Postgres access layer. Every query is a constant
// SQL string with $n placeholders; caller values are only ever passed as
// parameters (NFR-14, AB-08). Times are written and returned in UTC.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("not found")

// Store wraps a pgx connection pool.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to the database at dsn and checks the connection.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// New wraps an existing pool. The caller keeps ownership of the pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Close closes the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping checks the database is reachable.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// notFound maps pgx.ErrNoRows to ErrNotFound and wraps other errors.
func notFound(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	return fmt.Errorf("%s: %w", what, err)
}

// unstorable reports whether any value contains a NUL byte or invalid UTF-8.
// Postgres text cannot hold such a value, so a lookup on it can match nothing;
// lookups return no rows instead of surfacing a database error (AB-08).
func unstorable(vals ...string) bool {
	for _, v := range vals {
		if strings.IndexByte(v, 0) >= 0 || !utf8.ValidString(v) {
			return true
		}
	}
	return false
}

// jsonParam marshals v for a jsonb parameter, mapping a nil slice to [].
func jsonParam(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding json: %w", err)
	}
	if string(b) == "null" {
		return []byte("[]"), nil
	}
	return b, nil
}

// utcPtr returns nil for a zero time, else t in UTC.
func utcPtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	u := t.UTC()
	return &u
}

// fromPtr returns the zero time for nil, else *t in UTC.
func fromPtr(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

// clampLimit bounds a list limit to [1, maxLimit], defaulting to def.
func clampLimit(limit, def, maxLimit int) int {
	switch {
	case limit <= 0:
		return def
	case limit > maxLimit:
		return maxLimit
	}
	return limit
}

const (
	defaultListLimit = 100
	maxListLimit     = 10000
)
