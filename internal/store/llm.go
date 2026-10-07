package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// GetLLMUsage returns the usage record for a given UTC date, or zero values if
// no record exists yet.
func (s *Store) GetLLMUsage(ctx context.Context, day time.Time) (calls, inputTokens, outputTokens int64, err error) {
	d := day.UTC().Truncate(24 * time.Hour).Format("2006-01-02")
	row := s.pool.QueryRow(ctx, `SELECT calls, input_tokens, output_tokens FROM llm_usage WHERE day = $1`, d)
	err = row.Scan(&calls, &inputTokens, &outputTokens)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, nil
	}
	if err != nil {
		return 0, 0, 0, fmt.Errorf("getting llm_usage for %s: %w", d, err)
	}
	return calls, inputTokens, outputTokens, nil
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
	if errors.Is(err, sql.ErrNoRows) {
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
