package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// GetLLMUsage returns the usage record for a given UTC date, or zero values if
// no record exists yet.
func (s *Store) GetLLMUsage(ctx context.Context, day time.Time) (calls, inputTokens, outputTokens int64, err error) {
	d := day.UTC().Truncate(24 * time.Hour).Format("2006-01-02")
	row := s.pool.QueryRow(ctx, `SELECT calls, input_tokens, output_tokens FROM llm_usage WHERE day = $1`, d)
	err = row.Scan(&calls, &inputTokens, &outputTokens)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, 0, nil
	}
	if err != nil {
		return 0, 0, 0, fmt.Errorf("getting llm_usage for %s: %w", d, err)
	}
	return calls, inputTokens, outputTokens, nil
}

// ReserveLLMCall atomically reserves a call slot if under the dailyCap, returning
// true if the reservation succeeded. The cap check and increment happen in one
// transaction to prevent concurrent overshoots (AO review issue #1).
func (s *Store) ReserveLLMCall(ctx context.Context, day time.Time, dailyCap int) (bool, error) {
	d := day.UTC().Truncate(24 * time.Hour).Format("2006-01-02")

	// Try to increment only if current calls < dailyCap
	result, err := s.pool.Exec(ctx, `
		INSERT INTO llm_usage (day, calls, input_tokens, output_tokens)
		VALUES ($1, 1, 0, 0)
		ON CONFLICT (day) DO UPDATE
		SET calls = llm_usage.calls + 1
		WHERE llm_usage.calls < $2
	`, d, dailyCap)
	if err != nil {
		return false, fmt.Errorf("reserving llm call for %s: %w", d, err)
	}

	// If RowsAffected is 0, we hit the cap (WHERE clause failed)
	return result.RowsAffected() > 0, nil
}

// RecordLLMTokens records token usage for a previously reserved call. This is
// called after a successful API response to update the token counts.
func (s *Store) RecordLLMTokens(ctx context.Context, day time.Time, inputTokens, outputTokens int64) error {
	d := day.UTC().Truncate(24 * time.Hour).Format("2006-01-02")
	_, err := s.pool.Exec(ctx, `
		UPDATE llm_usage
		SET input_tokens = input_tokens + $2,
		    output_tokens = output_tokens + $3
		WHERE day = $1
	`, d, inputTokens, outputTokens)
	if err != nil {
		return fmt.Errorf("recording llm tokens for %s: %w", d, err)
	}
	return nil
}

// IncrementLLMUsage atomically increments the counters for a given UTC day,
// creating the record if it does not exist.
func (s *Store) IncrementLLMUsage(ctx context.Context, day time.Time, calls int, inputTokens, outputTokens int64) error {
	d := day.UTC().Truncate(24 * time.Hour).Format("2006-01-02")
	_, err := s.pool.Exec(ctx, `
		INSERT INTO llm_usage (day, calls, input_tokens, output_tokens)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (day) DO UPDATE
		SET calls = llm_usage.calls + EXCLUDED.calls,
		    input_tokens = llm_usage.input_tokens + EXCLUDED.input_tokens,
		    output_tokens = llm_usage.output_tokens + EXCLUDED.output_tokens
	`, d, calls, inputTokens, outputTokens)
	if err != nil {
		return fmt.Errorf("incrementing llm_usage for %s: %w", d, err)
	}
	return nil
}

// GetLLMVerdict retrieves a cached verdict by cache key, or returns
// (empty, false, nil) if no entry exists.
func (s *Store) GetLLMVerdict(ctx context.Context, cacheKey string) (canonical.LLMVerdict, bool, error) {
	var verdictJSON []byte
	row := s.pool.QueryRow(ctx, `SELECT verdict FROM llm_verdicts WHERE cache_key = $1`, cacheKey)
	err := row.Scan(&verdictJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return canonical.LLMVerdict{}, false, nil
	}
	if err != nil {
		return canonical.LLMVerdict{}, false, fmt.Errorf("reading llm_verdicts cache_key %q: %w", cacheKey, err)
	}
	var v canonical.LLMVerdict
	if err := json.Unmarshal(verdictJSON, &v); err != nil {
		return canonical.LLMVerdict{}, false, fmt.Errorf("unmarshaling llm_verdicts cache_key %q: %w", cacheKey, err)
	}
	return v, true, nil
}

// PutLLMVerdict stores a verdict in llm_verdicts, replacing any existing entry
// with the same cache key.
func (s *Store) PutLLMVerdict(ctx context.Context, cacheKey, model, promptVersion string, v canonical.LLMVerdict) error {
	verdictJSON, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshaling llm verdict: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO llm_verdicts (cache_key, model, prompt_version, verdict, created_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (cache_key) DO UPDATE
		SET model = EXCLUDED.model,
		    prompt_version = EXCLUDED.prompt_version,
		    verdict = EXCLUDED.verdict,
		    created_at = EXCLUDED.created_at
	`, cacheKey, model, promptVersion, verdictJSON)
	if err != nil {
		return fmt.Errorf("storing llm_verdicts cache_key %q: %w", cacheKey, err)
	}
	return nil
}
