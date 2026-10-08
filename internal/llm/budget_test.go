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

	// Mock usage store that tracks reservations
	var reserveCalls int
	mockStore := &mockUsageStore{
		reserveFunc: func(context.Context, time.Time, int) (bool, error) {
			reserveCalls++
			return true, nil // always under cap
		},
	}

	// Build verifier stack: Model -> Budget -> Cache
	mockModel := &ScriptedModel{Responses: []string{HonestResponse}}
	verifier := NewVerifier(mockModel)
	budgetedVerifier := NewBudgetedVerifier(verifier, mockStore, 10, nil, nil)

	var memCache MemoryCache
	cachedVerifier := NewCachedVerifier(budgetedVerifier, &memCache, "test-model", PromptVersion, nil)

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

	// First call: cache miss, should reserve and call model
	_, err := cachedVerifier.Verify(ctx, a, b, rel)
	if err != nil {
		t.Fatal(err)
	}
	if reserveCalls != 1 {
		t.Errorf("first call (cache miss): got %d reserve calls, want 1", reserveCalls)
	}

	// Second call: cache hit, should NOT reserve
	reserveCalls = 0
	_, err = cachedVerifier.Verify(ctx, a, b, rel)
	if err != nil {
		t.Fatal(err)
	}
	if reserveCalls != 0 {
		t.Errorf("second call (cache hit): got %d reserve calls, want 0", reserveCalls)
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
	reserveFunc      func(context.Context, time.Time, int) (bool, error)
	recordTokensFunc func(context.Context, time.Time, int64, int64) error
}

func (m *mockUsageStore) ReserveLLMCall(ctx context.Context, day time.Time, dailyCap int) (bool, error) {
	if m.reserveFunc != nil {
		return m.reserveFunc(ctx, day, dailyCap)
	}
	return true, nil
}

func (m *mockUsageStore) RecordLLMTokens(ctx context.Context, day time.Time, inTokens, outTokens int64) error {
	if m.recordTokensFunc != nil {
		return m.recordTokensFunc(ctx, day, inTokens, outTokens)
	}
	return nil
}
