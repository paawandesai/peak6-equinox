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
	ReserveLLMCall(ctx context.Context, day time.Time, dailyCap int) (bool, error)
	RecordLLMTokens(ctx context.Context, day time.Time, inputTokens, outputTokens int64) error
}

// TokenSource exposes token usage from the last API call.
type TokenSource interface {
	LastUsage() Usage
}

// BudgetedVerifier wraps a Verifier with a daily call cap. When the cap is
// reached, Verify returns ErrBudgetExhausted and the caller leaves the rules
// verdict unchanged.
type BudgetedVerifier struct {
	verifier    Verifier
	store       UsageStore
	tokenSource TokenSource // nil for mocks that don't expose usage
	dailyCap    int
	nowFunc     func() time.Time
}

// NewBudgetedVerifier creates a verifier that enforces a daily call cap.
// tokenSource may be nil for test mocks; if non-nil, token usage is recorded.
func NewBudgetedVerifier(v Verifier, store UsageStore, dailyCap int, tokenSource TokenSource) *BudgetedVerifier {
	return &BudgetedVerifier{
		verifier:    v,
		store:       store,
		tokenSource: tokenSource,
		dailyCap:    dailyCap,
		nowFunc:     time.Now,
	}
}

// Verify atomically reserves a call slot, calls the wrapped verifier if the
// reservation succeeded, then records token usage. If the cap is reached,
// returns ErrBudgetExhausted (AO review issue #1: atomic reserve prevents
// concurrent overshoots).
func (bv *BudgetedVerifier) Verify(ctx context.Context, a, b canonical.Instrument, r canonical.Relationship) (canonical.LLMVerdict, error) {
	today := bv.nowFunc().UTC()

	// Atomically reserve a call slot (AO review issue #1)
	reserved, err := bv.store.ReserveLLMCall(ctx, today, bv.dailyCap)
	if err != nil {
		return canonical.LLMVerdict{}, fmt.Errorf("reserving llm call: %w", err)
	}
	if !reserved {
		return canonical.LLMVerdict{}, ErrBudgetExhausted
	}

	verdict, err := bv.verifier.Verify(ctx, a, b, r)
	if err != nil {
		// Call was reserved but failed; the reserved slot is kept (failed calls
		// still count toward the cap to prevent hammering a failing API).
		return canonical.LLMVerdict{}, err
	}

	// Record token usage if available (AO review issue #5)
	if bv.tokenSource != nil {
		usage := bv.tokenSource.LastUsage()
		if err := bv.store.RecordLLMTokens(ctx, today, usage.InputTokens, usage.OutputTokens); err != nil {
			// AO review issue #2: log the error but return the verdict (don't discard paid work)
			fmt.Printf("WARNING: failed to record token usage: %v\n", err)
		}
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
