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

// LLMVerifyLastRun holds the summary of the last LLM verification run.
type LLMVerifyLastRun struct {
	Verified   int
	Downgraded int
	CacheHits  int
	RanAt      time.Time
}

// GetLLMVerifyLastRun retrieves the last LLM verify run summary, or returns
// (empty, false, nil) if no run has been recorded yet.
func (s *Store) GetLLMVerifyLastRun(ctx context.Context) (LLMVerifyLastRun, bool, error) {
	var run LLMVerifyLastRun
	row := s.pool.QueryRow(ctx, `SELECT verified, downgraded, cache_hits, ran_at FROM llm_verify_last_run WHERE id = true`)
	err := row.Scan(&run.Verified, &run.Downgraded, &run.CacheHits, &run.RanAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return LLMVerifyLastRun{}, false, nil
	}
	if err != nil {
		return LLMVerifyLastRun{}, false, fmt.Errorf("reading llm_verify_last_run: %w", err)
	}
	run.RanAt = run.RanAt.UTC()
	return run, true, nil
}

// PutLLMVerifyLastRun stores the last LLM verify run summary, replacing any
// existing entry.
func (s *Store) PutLLMVerifyLastRun(ctx context.Context, run LLMVerifyLastRun) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO llm_verify_last_run (id, verified, downgraded, cache_hits, ran_at)
		VALUES (true, $1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE
		SET verified = EXCLUDED.verified,
		    downgraded = EXCLUDED.downgraded,
		    cache_hits = EXCLUDED.cache_hits,
		    ran_at = EXCLUDED.ran_at
	`, run.Verified, run.Downgraded, run.CacheHits, run.RanAt.UTC())
	if err != nil {
		return fmt.Errorf("storing llm_verify_last_run: %w", err)
	}
	return nil
}

// LLMVerifyRun holds one verification run record.
type LLMVerifyRun struct {
	ID            int64
	StartedAt     time.Time
	FinishedAt    *time.Time
	Model         string
	PromptVersion string
	Verified      int
	Downgraded    int
	CacheHits     int
	CallsMade     int
	InputTokens   int64
	OutputTokens  int64
	Outcome       string // "ok", "cap_reached", "error"
}

// InsertLLMVerifyRun creates a new verification run record and prunes old runs
// to keep at most maxRuns (e.g. 100). Returns the new run's ID.
func (s *Store) InsertLLMVerifyRun(ctx context.Context, model, promptVersion string, maxRuns int) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO llm_verify_runs (started_at, model, prompt_version, outcome)
		VALUES (now(), $1, $2, 'ok')
		RETURNING id
	`, model, promptVersion).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("inserting llm_verify_run: %w", err)
	}

	// Prune old runs in the same transaction context
	if err := s.PruneOldLLMVerifyRuns(ctx, maxRuns); err != nil {
		return 0, fmt.Errorf("pruning old llm_verify_runs: %w", err)
	}

	return id, nil
}

// FinishLLMVerifyRun updates a run with its final statistics and outcome.
func (s *Store) FinishLLMVerifyRun(ctx context.Context, id int64, verified, downgraded, cacheHits, calls int, inputTokens, outputTokens int64, outcome string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE llm_verify_runs
		SET finished_at = now(),
		    verified = $2,
		    downgraded = $3,
		    cache_hits = $4,
		    calls_made = $5,
		    input_tokens = $6,
		    output_tokens = $7,
		    outcome = $8
		WHERE id = $1
	`, id, verified, downgraded, cacheHits, calls, inputTokens, outputTokens, outcome)
	if err != nil {
		return fmt.Errorf("finishing llm_verify_run %d: %w", id, err)
	}
	return nil
}

// GetLLMVerifyRun retrieves a single verification run by ID, or returns
// (empty, false, nil) if the run does not exist.
func (s *Store) GetLLMVerifyRun(ctx context.Context, id int64) (LLMVerifyRun, bool, error) {
	var r LLMVerifyRun
	var finishedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, started_at, finished_at, model, prompt_version,
		       verified, downgraded, cache_hits, calls_made,
		       input_tokens, output_tokens, outcome
		FROM llm_verify_runs
		WHERE id = $1
	`, id).Scan(&r.ID, &r.StartedAt, &finishedAt, &r.Model, &r.PromptVersion,
		&r.Verified, &r.Downgraded, &r.CacheHits, &r.CallsMade,
		&r.InputTokens, &r.OutputTokens, &r.Outcome)
	if errors.Is(err, pgx.ErrNoRows) {
		return LLMVerifyRun{}, false, nil
	}
	if err != nil {
		return LLMVerifyRun{}, false, fmt.Errorf("querying llm_verify_run %d: %w", id, err)
	}
	r.StartedAt = r.StartedAt.UTC()
	if finishedAt != nil {
		t := finishedAt.UTC()
		r.FinishedAt = &t
	}
	return r, true, nil
}

// GetLLMVerifyRuns returns the latest N verification runs, newest first.
func (s *Store) GetLLMVerifyRuns(ctx context.Context, limit int) ([]LLMVerifyRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, started_at, finished_at, model, prompt_version,
		       verified, downgraded, cache_hits, calls_made,
		       input_tokens, output_tokens, outcome
		FROM llm_verify_runs
		ORDER BY started_at DESC, id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("querying llm_verify_runs: %w", err)
	}
	defer rows.Close()

	var runs []LLMVerifyRun
	for rows.Next() {
		var r LLMVerifyRun
		var finishedAt *time.Time
		if err := rows.Scan(&r.ID, &r.StartedAt, &finishedAt, &r.Model, &r.PromptVersion,
			&r.Verified, &r.Downgraded, &r.CacheHits, &r.CallsMade,
			&r.InputTokens, &r.OutputTokens, &r.Outcome); err != nil {
			return nil, fmt.Errorf("scanning llm_verify_run: %w", err)
		}
		r.StartedAt = r.StartedAt.UTC()
		if finishedAt != nil {
			t := finishedAt.UTC()
			r.FinishedAt = &t
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating llm_verify_runs: %w", err)
	}
	return runs, nil
}

// PruneOldLLMVerifyRuns deletes all but the latest maxRuns verification runs.
func (s *Store) PruneOldLLMVerifyRuns(ctx context.Context, maxRuns int) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM llm_verify_runs
		WHERE id NOT IN (
			SELECT id FROM llm_verify_runs
			ORDER BY started_at DESC, id DESC
			LIMIT $1
		)
	`, maxRuns)
	if err != nil {
		return fmt.Errorf("pruning llm_verify_runs: %w", err)
	}
	return nil
}

// LinkRelationshipToVerdict updates a relationship to link it to an LLM verdict.
func (s *Store) LinkRelationshipToVerdict(ctx context.Context, relationshipID int64, cacheKey string, runID int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE relationships
		SET llm_cache_key = $2,
		    llm_run_id = $3,
		    llm_verified_at = now()
		WHERE id = $1
	`, relationshipID, cacheKey, runID)
	if err != nil {
		return fmt.Errorf("linking relationship %d to verdict %q: %w", relationshipID, cacheKey, err)
	}
	return nil
}

// RelationshipLLMData holds LLM verification data for a relationship (UI-22).
type RelationshipLLMData struct {
	CacheKey   string
	RunID      int64
	VerifiedAt time.Time
	Model      string
	Evidence   []string // Rationale from the verdict
}

// GetRelationshipLLMData fetches LLM verification data for a relationship, if it was verified.
// Returns (data, false, nil) if the relationship was never verified.
func (s *Store) GetRelationshipLLMData(ctx context.Context, relationshipID int64) (RelationshipLLMData, bool, error) {
	var cacheKey *string
	var runID *int64
	var verifiedAt *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT llm_cache_key, llm_run_id, llm_verified_at
		FROM relationships
		WHERE id = $1
	`, relationshipID).Scan(&cacheKey, &runID, &verifiedAt)
	if err != nil {
		return RelationshipLLMData{}, false, fmt.Errorf("reading LLM data for relationship %d: %w", relationshipID, err)
	}

	// Not verified if any field is null
	if cacheKey == nil || runID == nil || verifiedAt == nil {
		return RelationshipLLMData{}, false, nil
	}

	// Fetch the verdict to get the model and evidence (rationale)
	verdict, found, err := s.GetLLMVerdict(ctx, *cacheKey)
	if err != nil {
		return RelationshipLLMData{}, false, fmt.Errorf("reading LLM verdict for relationship %d: %w", relationshipID, err)
	}
	if !found {
		// Cache may have been pruned; return what we have without evidence
		return RelationshipLLMData{
			CacheKey:   *cacheKey,
			RunID:      *runID,
			VerifiedAt: verifiedAt.UTC(),
			Evidence:   []string{},
		}, true, nil
	}

	// Fetch the run to get the model (M5-02: direct lookup instead of scanning 1000 runs)
	run, found, err := s.GetLLMVerifyRun(ctx, *runID)
	if err != nil {
		return RelationshipLLMData{}, false, fmt.Errorf("reading LLM run %d for relationship %d: %w", *runID, relationshipID, err)
	}

	model := "unknown"
	if found {
		model = run.Model
	}

	return RelationshipLLMData{
		CacheKey:   *cacheKey,
		RunID:      *runID,
		VerifiedAt: verifiedAt.UTC(),
		Model:      model,
		Evidence:   verdict.Evidence,
	}, true, nil
}
