package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// MatchRunOutcome is the result of a match run.
type MatchRunOutcome string

// Match run outcomes.
const (
	MatchRunOK     MatchRunOutcome = "ok"
	MatchRunFailed MatchRunOutcome = "failed"
)

// MatchRun is a match_runs row, recording one execution of the matcher.
type MatchRun struct {
	ID                   int64
	RoundID              int64
	StartedAt            time.Time
	FinishedAt           time.Time
	DurationMS           int
	PairsCompared        int
	RelationshipsStored  int
	Outcome              MatchRunOutcome
}

const matchRunCols = `id, round_id, started_at, finished_at, duration_ms, pairs_compared, relationships_stored, outcome`

func scanMatchRun(row pgx.Row) (MatchRun, error) {
	var m MatchRun
	if err := row.Scan(&m.ID, &m.RoundID, &m.StartedAt, &m.FinishedAt, &m.DurationMS, &m.PairsCompared, &m.RelationshipsStored, &m.Outcome); err != nil {
		return MatchRun{}, err
	}
	m.StartedAt = m.StartedAt.UTC()
	m.FinishedAt = m.FinishedAt.UTC()
	return m, nil
}

// InsertMatchRun records one match run and prunes old runs to maintain the retention cap.
// The retention cap is 500 rows; older runs are deleted in the same transaction.
func (s *Store) InsertMatchRun(ctx context.Context, m MatchRun) (int64, error) {
	const retentionCap = 500
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("inserting match run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id int64
	err = tx.QueryRow(ctx,
		`INSERT INTO match_runs (round_id, started_at, finished_at, duration_ms, pairs_compared, relationships_stored, outcome)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		m.RoundID, m.StartedAt.UTC(), m.FinishedAt.UTC(), m.DurationMS, m.PairsCompared, m.RelationshipsStored, m.Outcome).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("inserting match run: %w", err)
	}

	// Prune old runs beyond the retention cap.
	_, err = tx.Exec(ctx,
		`DELETE FROM match_runs WHERE id IN (
			SELECT id FROM match_runs ORDER BY finished_at DESC OFFSET $1
		)`, retentionCap)
	if err != nil {
		return 0, fmt.Errorf("pruning old match runs: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("inserting match run: %w", err)
	}
	return id, nil
}

// GetLatestMatchRun returns the most recent match run by finished_at, or ErrNotFound if none.
func (s *Store) GetLatestMatchRun(ctx context.Context) (MatchRun, error) {
	m, err := scanMatchRun(s.pool.QueryRow(ctx,
		`SELECT `+matchRunCols+` FROM match_runs ORDER BY finished_at DESC LIMIT 1`))
	if err != nil {
		return MatchRun{}, notFound(err, "latest match run")
	}
	return m, nil
}

// ListMatchRuns returns the most recent match runs, ordered by finished_at descending.
// Limit is clamped to [1, 500] (matches the retention cap).
func (s *Store) ListMatchRuns(ctx context.Context, limit int) ([]MatchRun, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+matchRunCols+` FROM match_runs ORDER BY finished_at DESC LIMIT $1`,
		clampLimit(limit, 1, 500))
	if err != nil {
		return nil, fmt.Errorf("listing match runs: %w", err)
	}
	defer rows.Close()
	var out []MatchRun
	for rows.Next() {
		m, err := scanMatchRun(rows)
		if err != nil {
			return nil, fmt.Errorf("listing match runs: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing match runs: %w", err)
	}
	return out, nil
}
