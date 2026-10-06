package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// RoundStatus is the state of a fetch round.
type RoundStatus string

// Round statuses. A round is running until FinishRound records its result.
const (
	RoundRunning RoundStatus = "running"
	RoundOK      RoundStatus = "ok"
	RoundPartial RoundStatus = "partial" // at least one venue failed
	RoundFailed  RoundStatus = "failed"
)

func (s RoundStatus) finished() bool { return s == RoundOK || s == RoundPartial || s == RoundFailed }

// VenueStatus is one venue's result within a round.
type VenueStatus struct {
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
	Instruments int    `json:"instruments"`
	Quotes      int    `json:"quotes"`
	DurationMS  int64  `json:"duration_ms"`
}

// Round is a fetch_rounds row.
type Round struct {
	ID          int64
	StartedAt   time.Time
	FinishedAt  time.Time // zero while running
	Status      RoundStatus
	VenueStatus map[canonical.Venue]VenueStatus
	Pinned      bool
}

const roundCols = `id, started_at, finished_at, status, venue_status, pinned`

func scanRound(row pgx.Row) (Round, error) {
	var (
		r        Round
		finished *time.Time
	)
	if err := row.Scan(&r.ID, &r.StartedAt, &finished, &r.Status, &r.VenueStatus, &r.Pinned); err != nil {
		return Round{}, err
	}
	r.StartedAt = r.StartedAt.UTC()
	r.FinishedAt = fromPtr(finished)
	if r.VenueStatus == nil {
		r.VenueStatus = map[canonical.Venue]VenueStatus{}
	}
	return r, nil
}

// CreateRound starts a new round in status running.
func (s *Store) CreateRound(ctx context.Context, startedAt time.Time) (Round, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO fetch_rounds (started_at) VALUES ($1) RETURNING `+roundCols, startedAt.UTC())
	r, err := scanRound(row)
	if err != nil {
		return Round{}, fmt.Errorf("creating round: %w", err)
	}
	return r, nil
}

// FinishRound records a running round's final status and per-venue results.
func (s *Store) FinishRound(ctx context.Context, id int64, status RoundStatus, venues map[canonical.Venue]VenueStatus, finishedAt time.Time) error {
	if !status.finished() {
		return fmt.Errorf("finishing round %d: status %q is not ok, partial or failed", id, status)
	}
	if venues == nil {
		venues = map[canonical.Venue]VenueStatus{}
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE fetch_rounds SET status = $2, venue_status = $3, finished_at = $4 WHERE id = $1`,
		id, status, venues, finishedAt.UTC())
	if err != nil {
		return fmt.Errorf("finishing round %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("finishing round %d: %w", id, ErrNotFound)
	}
	return nil
}

// GetRound returns one round.
func (s *Store) GetRound(ctx context.Context, id int64) (Round, error) {
	r, err := scanRound(s.pool.QueryRow(ctx, `SELECT `+roundCols+` FROM fetch_rounds WHERE id = $1`, id))
	if err != nil {
		return Round{}, notFound(err, fmt.Sprintf("getting round %d", id))
	}
	return r, nil
}

// ListRounds returns the most recent rounds, newest first.
func (s *Store) ListRounds(ctx context.Context, limit int) ([]Round, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+roundCols+` FROM fetch_rounds ORDER BY id DESC LIMIT $1`, clampLimit(limit, 20, 1000))
	if err != nil {
		return nil, fmt.Errorf("listing rounds: %w", err)
	}
	defer rows.Close()
	var out []Round
	for rows.Next() {
		r, err := scanRound(rows)
		if err != nil {
			return nil, fmt.Errorf("listing rounds: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing rounds: %w", err)
	}
	return out, nil
}

// LatestRound returns the newest round with status ok or partial.
func (s *Store) LatestRound(ctx context.Context) (Round, error) {
	r, err := scanRound(s.pool.QueryRow(ctx,
		`SELECT `+roundCols+` FROM fetch_rounds WHERE status IN ('ok', 'partial') ORDER BY id DESC LIMIT 1`))
	if err != nil {
		return Round{}, notFound(err, "getting latest round")
	}
	return r, nil
}

// PinnedRound returns the pinned round.
func (s *Store) PinnedRound(ctx context.Context) (Round, error) {
	r, err := scanRound(s.pool.QueryRow(ctx, `SELECT `+roundCols+` FROM fetch_rounds WHERE pinned`))
	if err != nil {
		return Round{}, notFound(err, "getting pinned round")
	}
	return r, nil
}

// CurrentRound returns the round pages read from: the pinned round if one is
// set, else the latest ok or partial round.
func (s *Store) CurrentRound(ctx context.Context) (Round, error) {
	r, err := s.PinnedRound(ctx)
	if errors.Is(err, ErrNotFound) {
		return s.LatestRound(ctx)
	}
	return r, err
}

// PinRound makes id the only pinned round. Only an ok or partial round can be pinned.
func (s *Store) PinRound(ctx context.Context, id int64) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE fetch_rounds SET pinned = false WHERE pinned AND id <> $1`, id); err != nil {
			return fmt.Errorf("pinning round %d: %w", id, err)
		}
		tag, err := tx.Exec(ctx,
			`UPDATE fetch_rounds SET pinned = true WHERE id = $1 AND status IN ('ok', 'partial')`, id)
		if err != nil {
			return fmt.Errorf("pinning round %d: %w", id, err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("pinning round %d (must exist with status ok or partial): %w", id, ErrNotFound)
		}
		return nil
	})
}

// UnpinRounds clears any pinned round.
func (s *Store) UnpinRounds(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `UPDATE fetch_rounds SET pinned = false WHERE pinned`); err != nil {
		return fmt.Errorf("unpinning rounds: %w", err)
	}
	return nil
}
