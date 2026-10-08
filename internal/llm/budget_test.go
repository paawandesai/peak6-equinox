package llm

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestCacheHitDoesNotIncrementUsage verifies that when the cache is layered
// outside the budget (Cache wraps Budget), cache hits do not burn the daily cap
// (Cursor review issue #3).
func TestCacheHitDoesNotIncrementUsage(t *testing.T) {
	ctx := context.Background()

	// Mock usage store that tracks increments
	var incrementCalls int
	mockStore := &mockUsageStore{
		getFunc: func(context.Context, time.Time) (int64, int64, int64, error) {
			return 0, 0, 0, nil // under cap
		},
		incrementFunc: func(context.Context, time.Time, int, int64, int64) error {
			incrementCalls++
			return nil
		},
	}

	// Build verifier stack: Model -> Budget -> Cache
	mockModel := &ScriptedModel{Responses: []string{HonestResponse}}
	verifier := NewVerifier(mockModel)
	budgetedVerifier := NewBudgetedVerifier(verifier, mockStore, 10)

	var memCache MemoryCache
	cachedVerifier := NewCachedVerifier(budgetedVerifier, &memCache, "test-model", PromptVersion)

	a, b := InjectionCase{Field: "title", VenueText: "test"}.Instruments()
	rel := canonical.Relationship{
		RoundID:     1,
		AID:         a.ID,
		BID:         b.ID,
		Type:        canonical.RelRelated, // Match what HonestResponse returns
		Method:      canonical.MethodRules,
		Confidence:  decimal.NewFromInt(1),
		Explanation: "test",
	}

	// First call: cache miss, should hit budget and model
	_, err := cachedVerifier.Verify(ctx, a, b, rel)
	if err != nil {
		t.Fatal(err)
	}
	if incrementCalls != 1 {
		t.Errorf("first call (cache miss): got %d increment calls, want 1", incrementCalls)
	}

	// Second call: cache hit, should NOT increment usage
	incrementCalls = 0
	_, err = cachedVerifier.Verify(ctx, a, b, rel)
	if err != nil {
		t.Fatal(err)
	}
	if incrementCalls != 0 {
		t.Errorf("second call (cache hit): got %d increment calls, want 0", incrementCalls)
	}

	// Verify the model was only called once (cache hit didn't reach it)
	if len(mockModel.Prompts()) != 1 {
		t.Errorf("model was called %d times, want 1 (cache should prevent second call)", len(mockModel.Prompts()))
	}

	// Verify the cached verdict is returned (HonestResponse returns "related")
	cached, ok, err := memCache.Get(ctx, CacheKey(a, b, PromptVersion, "test-model"))
	if err != nil || !ok {
		t.Fatalf("cache.Get: ok=%v err=%v", ok, err)
	}
	if cached.Relationship != canonical.RelRelated {
		t.Errorf("cached verdict: got %s, want related", cached.Relationship)
	}
}

// mockUsageStore is a test double for UsageStore.
type mockUsageStore struct {
	getFunc       func(context.Context, time.Time) (int64, int64, int64, error)
	incrementFunc func(context.Context, time.Time, int, int64, int64) error
}

func (m *mockUsageStore) GetLLMUsage(ctx context.Context, day time.Time) (int64, int64, int64, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, day)
	}
	return 0, 0, 0, nil
}

func (m *mockUsageStore) IncrementLLMUsage(ctx context.Context, day time.Time, calls int, inTokens, outTokens int64) error {
	if m.incrementFunc != nil {
		return m.incrementFunc(ctx, day, calls, inTokens, outTokens)
	}
	return nil
}
