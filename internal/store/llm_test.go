package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// TestGetLLMUsageEmptyTable verifies that reading usage from an empty table
// returns zero values, not an error (Cursor review: pgx.ErrNoRows case).
func TestGetLLMUsageEmptyTable(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	day := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	calls, inTokens, outTokens, err := st.GetLLMUsage(ctx, day)
	if err != nil {
		t.Fatalf("GetLLMUsage on empty table: %v", err)
	}
	if calls != 0 || inTokens != 0 || outTokens != 0 {
		t.Errorf("got (%d, %d, %d), want (0, 0, 0)", calls, inTokens, outTokens)
	}
}

// TestIncrementLLMUsage verifies usage tracking and retrieval.
func TestIncrementLLMUsage(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	day := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

	// Increment once
	if err := st.IncrementLLMUsage(ctx, day, 1, 100, 50); err != nil {
		t.Fatal(err)
	}

	calls, inTokens, outTokens, err := st.GetLLMUsage(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || inTokens != 100 || outTokens != 50 {
		t.Errorf("after first increment: got (%d, %d, %d), want (1, 100, 50)", calls, inTokens, outTokens)
	}

	// Increment again (should add to existing)
	if err := st.IncrementLLMUsage(ctx, day, 2, 200, 100); err != nil {
		t.Fatal(err)
	}

	calls, inTokens, outTokens, err = st.GetLLMUsage(ctx, day)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 || inTokens != 300 || outTokens != 150 {
		t.Errorf("after second increment: got (%d, %d, %d), want (3, 300, 150)", calls, inTokens, outTokens)
	}
}

// TestGetLLMVerdictCacheMiss verifies that a cache miss returns false, not an
// error (Cursor review: pgx.ErrNoRows case).
func TestGetLLMVerdictCacheMiss(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	verdict, found, err := st.GetLLMVerdict(ctx, "nonexistent-key")
	if err != nil {
		t.Fatalf("GetLLMVerdict on cache miss: %v", err)
	}
	if found {
		t.Errorf("cache miss returned found=true")
	}
	if verdict.Relationship != "" {
		t.Errorf("cache miss returned non-empty verdict: %+v", verdict)
	}
}

// TestLLMVerdictCacheRoundTrip verifies verdict storage and retrieval.
func TestLLMVerdictCacheRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	// Cache key must be a 64-character hex string (SHA-256 hash)
	// Synthetic value to avoid gitleaks false positive
	key := strings.Repeat("ab", 32)
	model := "claude-haiku-4-5-20251001"
	promptVersion := "v1"
	verdict := canonical.LLMVerdict{
		Relationship:    canonical.RelEquivalent,
		Confidence:      decimal.NewFromFloat(0.95),
		DifferingFields: []string{"deadline"},
		Evidence:        []string{"both resolve on election day"},
	}

	// Store
	if err := st.PutLLMVerdict(ctx, key, model, promptVersion, verdict); err != nil {
		t.Fatal(err)
	}

	// Retrieve
	got, found, err := st.GetLLMVerdict(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("cache hit returned found=false")
	}
	if got.Relationship != verdict.Relationship {
		t.Errorf("relationship: got %s, want %s", got.Relationship, verdict.Relationship)
	}
	if !got.Confidence.Equal(verdict.Confidence) {
		t.Errorf("confidence: got %s, want %s", got.Confidence, verdict.Confidence)
	}

	// Update (same key, different verdict)
	verdict2 := canonical.LLMVerdict{
		Relationship: canonical.RelRelated,
		Confidence:   decimal.NewFromFloat(0.8),
	}
	if err := st.PutLLMVerdict(ctx, key, model, promptVersion, verdict2); err != nil {
		t.Fatal(err)
	}

	got2, found, err := st.GetLLMVerdict(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("second cache hit returned found=false")
	}
	if got2.Relationship != canonical.RelRelated {
		t.Errorf("after update: got %s, want related", got2.Relationship)
	}
}

// TestGetLLMVerifyLastRunEmpty verifies that reading from an empty table
// returns false, not an error (UI-08 follow-up: issue #40).
func TestGetLLMVerifyLastRunEmpty(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	_, found, err := st.GetLLMVerifyLastRun(ctx)
	if err != nil {
		t.Fatalf("GetLLMVerifyLastRun on empty table: %v", err)
	}
	if found {
		t.Errorf("empty table returned found=true")
	}
}

// TestLLMVerifyLastRunRoundTrip verifies storing and retrieving the last run
// summary (UI-08 follow-up: issue #40).
func TestLLMVerifyLastRunRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	now := time.Date(2026, 10, 8, 12, 34, 56, 0, time.UTC)
	run := store.LLMVerifyLastRun{
		Verified:   14,
		Downgraded: 2,
		CacheHits:  9,
		RanAt:      now,
	}

	// Store
	if err := st.PutLLMVerifyLastRun(ctx, run); err != nil {
		t.Fatal(err)
	}

	// Retrieve
	got, found, err := st.GetLLMVerifyLastRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("stored run not found")
	}
	if got.Verified != run.Verified {
		t.Errorf("verified: got %d, want %d", got.Verified, run.Verified)
	}
	if got.Downgraded != run.Downgraded {
		t.Errorf("downgraded: got %d, want %d", got.Downgraded, run.Downgraded)
	}
	if got.CacheHits != run.CacheHits {
		t.Errorf("cache_hits: got %d, want %d", got.CacheHits, run.CacheHits)
	}
	if !got.RanAt.Equal(run.RanAt) {
		t.Errorf("ran_at: got %v, want %v", got.RanAt, run.RanAt)
	}

	// Update (should replace)
	run2 := store.LLMVerifyLastRun{
		Verified:   20,
		Downgraded: 5,
		CacheHits:  15,
		RanAt:      now.Add(time.Hour),
	}
	if err := st.PutLLMVerifyLastRun(ctx, run2); err != nil {
		t.Fatal(err)
	}

	got2, found, err := st.GetLLMVerifyLastRun(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("updated run not found")
	}
	if got2.Verified != run2.Verified {
		t.Errorf("after update: verified got %d, want %d", got2.Verified, run2.Verified)
	}
	if got2.Downgraded != run2.Downgraded {
		t.Errorf("after update: downgraded got %d, want %d", got2.Downgraded, run2.Downgraded)
	}
	if got2.CacheHits != run2.CacheHits {
		t.Errorf("after update: cache_hits got %d, want %d", got2.CacheHits, run2.CacheHits)
	}
}

// TestLLMVerifyRunsRoundTrip verifies storing, finishing and retrieving verification runs (UI-22).
func TestLLMVerifyRunsRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	model := "claude-haiku-4-5-20251001"
	promptVersion := "v1"

	// Insert a run
	runID, err := st.InsertLLMVerifyRun(ctx, model, promptVersion, 100)
	if err != nil {
		t.Fatal(err)
	}
	if runID <= 0 {
		t.Fatalf("InsertLLMVerifyRun returned id %d, want > 0", runID)
	}

	// Finish the run
	if err := st.FinishLLMVerifyRun(ctx, runID, 10, 2, 5, 5, 1000, 500, "ok"); err != nil {
		t.Fatal(err)
	}

	// Retrieve runs
	runs, err := st.GetLLMVerifyRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}

	got := runs[0]
	if got.ID != runID {
		t.Errorf("id: got %d, want %d", got.ID, runID)
	}
	if got.Model != model {
		t.Errorf("model: got %s, want %s", got.Model, model)
	}
	if got.PromptVersion != promptVersion {
		t.Errorf("prompt_version: got %s, want %s", got.PromptVersion, promptVersion)
	}
	if got.Verified != 10 {
		t.Errorf("verified: got %d, want 10", got.Verified)
	}
	if got.Downgraded != 2 {
		t.Errorf("downgraded: got %d, want 2", got.Downgraded)
	}
	if got.CacheHits != 5 {
		t.Errorf("cache_hits: got %d, want 5", got.CacheHits)
	}
	if got.CallsMade != 5 {
		t.Errorf("calls_made: got %d, want 5", got.CallsMade)
	}
	if got.InputTokens != 1000 {
		t.Errorf("input_tokens: got %d, want 1000", got.InputTokens)
	}
	if got.OutputTokens != 500 {
		t.Errorf("output_tokens: got %d, want 500", got.OutputTokens)
	}
	if got.Outcome != "ok" {
		t.Errorf("outcome: got %s, want ok", got.Outcome)
	}
	if got.FinishedAt == nil {
		t.Error("finished_at is nil, want non-nil")
	}
	if got.StartedAt.IsZero() {
		t.Error("started_at is zero, want non-zero")
	}
}

// M5-02: GetLLMVerifyRun retrieves a single run by ID instead of scanning all runs.
func TestGetLLMVerifyRun(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	model := "claude-haiku-4-5-20251001"
	promptVersion := "v1"

	// Insert and finish a run
	runID, err := st.InsertLLMVerifyRun(ctx, model, promptVersion, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishLLMVerifyRun(ctx, runID, 10, 2, 5, 5, 1000, 500, "ok"); err != nil {
		t.Fatal(err)
	}

	// Retrieve by ID
	got, found, err := st.GetLLMVerifyRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("run not found")
	}

	if got.ID != runID {
		t.Errorf("id: got %d, want %d", got.ID, runID)
	}
	if got.Model != model {
		t.Errorf("model: got %s, want %s", got.Model, model)
	}
	if got.PromptVersion != promptVersion {
		t.Errorf("prompt_version: got %s, want %s", got.PromptVersion, promptVersion)
	}
	if got.Verified != 10 {
		t.Errorf("verified: got %d, want 10", got.Verified)
	}
	if got.Downgraded != 2 {
		t.Errorf("downgraded: got %d, want 2", got.Downgraded)
	}
	if got.CacheHits != 5 {
		t.Errorf("cache_hits: got %d, want 5", got.CacheHits)
	}
	if got.CallsMade != 5 {
		t.Errorf("calls_made: got %d, want 5", got.CallsMade)
	}
	if got.InputTokens != 1000 {
		t.Errorf("input_tokens: got %d, want 1000", got.InputTokens)
	}
	if got.OutputTokens != 500 {
		t.Errorf("output_tokens: got %d, want 500", got.OutputTokens)
	}
	if got.Outcome != "ok" {
		t.Errorf("outcome: got %s, want ok", got.Outcome)
	}
	if got.FinishedAt == nil {
		t.Error("finished_at is nil, want non-nil")
	}

	// Query for non-existent run
	_, found, err = st.GetLLMVerifyRun(ctx, runID+9999)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("non-existent run should not be found")
	}
}

// TestLLMVerifyRunsPruning verifies that old runs are pruned when inserting new ones (UI-22).
func TestLLMVerifyRunsPruning(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	model := "claude-haiku-4-5-20251001"
	promptVersion := "v1"

	// Insert 5 runs with a max of 3
	var ids []int64
	for i := 0; i < 5; i++ {
		id, err := st.InsertLLMVerifyRun(ctx, model, promptVersion, 3)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		// Sleep a tiny bit to ensure different timestamps
		time.Sleep(2 * time.Millisecond)
	}

	// Should have only the latest 3 runs
	runs, err := st.GetLLMVerifyRuns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d runs after pruning, want 3", len(runs))
	}

	// The 3 latest runs should be kept (ids[2], ids[3], ids[4])
	gotIDs := make(map[int64]bool)
	for _, r := range runs {
		gotIDs[r.ID] = true
	}
	for i := 2; i < 5; i++ {
		if !gotIDs[ids[i]] {
			t.Errorf("expected run %d to be kept", ids[i])
		}
	}
	// The first 2 should be pruned
	for i := 0; i < 2; i++ {
		if gotIDs[ids[i]] {
			t.Errorf("expected run %d to be pruned", ids[i])
		}
	}
}

// TestLinkRelationshipToVerdict verifies linking relationships to LLM verdicts (UI-22).
func TestLinkRelationshipToVerdict(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	// Create a round
	round, err := st.CreateRound(ctx, time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	// Create two instruments
	instA := newTestInstrument("kalshi:TESTM1")
	instB := newTestInstrument("polymarket:TESTM2")
	if err := st.UpsertInstrument(ctx, round.ID, instA); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertInstrument(ctx, round.ID, instB); err != nil {
		t.Fatal(err)
	}

	// Create a relationship
	rel := canonical.Relationship{
		RoundID:     round.ID,
		AID:         instA.ID,
		BID:         instB.ID,
		Type:        canonical.RelEquivalent,
		Confidence:  decimal.NewFromFloat(0.95),
		Method:      canonical.MethodRules,
		Explanation: "test relationship",
	}
	relID, err := st.UpsertRelationship(ctx, rel)
	if err != nil {
		t.Fatal(err)
	}

	// Create a verify run
	runID, err := st.InsertLLMVerifyRun(ctx, "claude-haiku-4-5-20251001", "v1", 100)
	if err != nil {
		t.Fatal(err)
	}

	// Link the relationship to a verdict
	cacheKey := strings.Repeat("cd", 32)
	if err := st.LinkRelationshipToVerdict(ctx, relID, cacheKey, runID); err != nil {
		t.Fatal(err)
	}

	// Verify the link was created by querying the relationship
	var gotCacheKey *string
	var gotRunID *int64
	var gotVerifiedAt *time.Time
	err = pool.QueryRow(ctx, `
		SELECT llm_cache_key, llm_run_id, llm_verified_at
		FROM relationships
		WHERE id = $1
	`, relID).Scan(&gotCacheKey, &gotRunID, &gotVerifiedAt)
	if err != nil {
		t.Fatal(err)
	}

	if gotCacheKey == nil || *gotCacheKey != cacheKey {
		t.Errorf("llm_cache_key: got %v, want %s", gotCacheKey, cacheKey)
	}
	if gotRunID == nil || *gotRunID != runID {
		t.Errorf("llm_run_id: got %v, want %d", gotRunID, runID)
	}
	if gotVerifiedAt == nil {
		t.Error("llm_verified_at is nil, want non-nil")
	}
}

// newTestInstrument creates a minimal instrument for testing.
func newTestInstrument(id string) canonical.Instrument {
	v := canonical.VenueOf(id)
	key := strings.TrimPrefix(id, string(v)+":")
	t0 := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	return canonical.Instrument{
		ID:            id,
		Venue:         v,
		VenueMarketID: key,
		VenueEventID:  "EVT",
		Category:      canonical.CategoryPolitics,
		Title:         "Test Market",
		RulesText:     "Test rules",
		Terms:         canonical.UnknownTerms(),
		CloseTime:     t0.Add(time.Hour),
		Status:        canonical.StatusOpen,
		FeeSchedule:   feeFor(v),
		Quality:       canonical.Quality{},
	}
}

// feeFor returns a valid per-venue fee schedule (Instrument.Validate rejects an empty id).
func feeFor(v canonical.Venue) canonical.FeeSchedule {
	if v == canonical.VenuePolymarket {
		return canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1}
	}
	return canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": decimal.RequireFromString("0.07")}}
}
