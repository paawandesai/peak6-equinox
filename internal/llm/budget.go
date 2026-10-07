package llm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// ErrBudgetExhausted is returned when the daily call cap is reached.
var ErrBudgetExhausted = errors.New("llm daily call cap reached")

// UsageStore is the subset of store.Store needed for budget tracking.
type UsageStore interface {
	GetLLMUsage(ctx context.Context, day time.Time) (calls, inputTokens, outputTokens int64, err error)
	IncrementLLMUsage(ctx context.Context, day time.Time, calls int, inputTokens, outputTokens int64) error
}

// BudgetedVerifier wraps a Verifier with a daily call cap. When the cap is
// reached, Verify returns ErrBudgetExhausted and the caller leaves the rules
// verdict unchanged.
type BudgetedVerifier struct {
	verifier Verifier
	store    UsageStore
	dailyCap int
	nowFunc  func() time.Time
}

// NewBudgetedVerifier creates a verifier that enforces a daily call cap.
func NewBudgetedVerifier(v Verifier, store UsageStore, dailyCap int) *BudgetedVerifier {
	return &BudgetedVerifier{
		verifier: v,
		store:    store,
		dailyCap: dailyCap,
		nowFunc:  time.Now,
	}
}

// Verify checks the budget, calls the wrapped verifier if under the cap, then
// increments usage. If the cap is reached, returns ErrBudgetExhausted.
func (bv *BudgetedVerifier) Verify(ctx context.Context, a, b canonical.Instrument, r canonical.Relationship) (canonical.LLMVerdict, error) {
	today := bv.nowFunc().UTC()
	calls, _, _, err := bv.store.GetLLMUsage(ctx, today)
	if err != nil {
		return canonical.LLMVerdict{}, fmt.Errorf("checking llm budget: %w", err)
	}
	if calls >= int64(bv.dailyCap) {
		return canonical.LLMVerdict{}, ErrBudgetExhausted
	}

	verdict, err := bv.verifier.Verify(ctx, a, b, r)
	if err != nil {
		return canonical.LLMVerdict{}, err
	}

	// Increment usage. Token counts are approximations; the real count comes
	// from the Anthropic API response, but we don't expose that in the Model
	// interface for M3-06. The cap is on calls, not tokens.
	if err := bv.store.IncrementLLMUsage(ctx, today, 1, 0, 0); err != nil {
		return canonical.LLMVerdict{}, fmt.Errorf("recording llm usage: %w", err)
	}

	return verdict, nil
}

// CachedVerifier wraps a Verifier with a verdict cache. On a cache hit, it
// returns the cached verdict without calling the underlying verifier. On a
// miss, it calls the verifier and stores the result.
type CachedVerifier struct {
	verifier      Verifier
	cache         VerdictCache
	model         string
	promptVersion string
}

// NewCachedVerifier creates a verifier that caches verdicts.
func NewCachedVerifier(v Verifier, cache VerdictCache, model, promptVersion string) *CachedVerifier {
	return &CachedVerifier{
		verifier:      v,
		cache:         cache,
		model:         model,
		promptVersion: promptVersion,
	}
}

// Verify checks the cache first, then calls the wrapped verifier on a miss and
// stores the result.
func (c *CachedVerifier) Verify(ctx context.Context, a, b canonical.Instrument, r canonical.Relationship) (canonical.LLMVerdict, error) {
	key := CacheKey(a, b, c.promptVersion, c.model)
	if cached, ok, err := c.cache.Get(ctx, key); err != nil {
		return canonical.LLMVerdict{}, fmt.Errorf("reading verdict cache: %w", err)
	} else if ok {
		return cached, nil
	}

	verdict, err := c.verifier.Verify(ctx, a, b, r)
	if err != nil {
		return canonical.LLMVerdict{}, err
	}

	if err := c.cache.Put(ctx, key, verdict); err != nil {
		return canonical.LLMVerdict{}, fmt.Errorf("storing verdict in cache: %w", err)
	}

	return verdict, nil
}
