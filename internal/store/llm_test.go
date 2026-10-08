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
