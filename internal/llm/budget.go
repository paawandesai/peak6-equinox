package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// BudgetedVerifier wraps a Verifier with a daily call cap. When the cap is
// reached, Verify returns ErrBudgetExhausted and the caller leaves the rules
// verdict unchanged.
type BudgetedVerifier struct {
	verifier Verifier
	store    UsageStore
	log      *slog.Logger // nil is replaced with a discard handler
	dailyCap int
	nowFunc  func() time.Time
}

// NewBudgetedVerifier creates a verifier that enforces a daily call cap.
// log may be nil, in which case warnings are discarded.
func NewBudgetedVerifier(v Verifier, store UsageStore, dailyCap int, log *slog.Logger) *BudgetedVerifier {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &BudgetedVerifier{
		verifier: v,
		store:    store,
		log:      log,
		dailyCap: dailyCap,
		nowFunc:  time.Now,
	}
}

// Verify atomically reserves a call slot, calls the wrapped verifier if the
// reservation succeeded, then records per-call token usage. If the cap is reached,
// returns ErrBudgetExhausted. Cursor: record tokens even when the call fails after
// successful HTTP (decode error case), so we track all billable API usage.
func (bv *BudgetedVerifier) Verify(ctx context.Context, a, b canonical.Instrument, r canonical.Relationship) (VerifyResult, error) {
	today := bv.nowFunc().UTC()

	// Atomically reserve a call slot (AO review issue #1: atomic reserve prevents concurrent overshoots)
	reserved, err := bv.store.ReserveLLMCall(ctx, today, bv.dailyCap)
	if err != nil {
		return VerifyResult{}, fmt.Errorf("reserving llm call: %w", err)
	}
	if !reserved {
		return VerifyResult{}, ErrBudgetExhausted
	}

	result, err := bv.verifier.Verify(ctx, a, b, r)
	// Cursor: record tokens even when the call fails (e.g., decode error after successful HTTP)
	// The result.Usage is set even on decode errors (see verifier.Verify).
	if result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 {
		if recordErr := bv.store.RecordLLMTokens(ctx, today, result.Usage.InputTokens, result.Usage.OutputTokens); recordErr != nil {
			// Log the error but return the verdict (don't discard paid work)
			bv.log.WarnContext(ctx, "failed to record token usage", "err", recordErr, "input_tokens", result.Usage.InputTokens, "output_tokens", result.Usage.OutputTokens)
		}
	}

	if err != nil {
		// Call was reserved but failed; the reserved slot is kept (failed calls
		// still count toward the cap to prevent hammering a failing API).
		return result, err
	}

	return result, nil
}

// CachedVerifier wraps a Verifier with a verdict cache. On a cache hit, it
// returns the cached verdict without calling the underlying verifier. On a
// miss, it calls the verifier and stores the result.
type CachedVerifier struct {
	verifier      Verifier
	cache         VerdictCache
	log           *slog.Logger // nil is replaced with a discard handler
	model         string
	promptVersion string
}

// NewCachedVerifier creates a verifier that caches verdicts.
// log may be nil, in which case warnings are discarded.
func NewCachedVerifier(v Verifier, cache VerdictCache, model, promptVersion string, log *slog.Logger) *CachedVerifier {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &CachedVerifier{
		verifier:      v,
		cache:         cache,
		log:           log,
		model:         model,
		promptVersion: promptVersion,
	}
}

// Verify checks the cache first, then calls the wrapped verifier on a miss and
// stores the result. Cache hits return zero usage (no API call made).
func (c *CachedVerifier) Verify(ctx context.Context, a, b canonical.Instrument, r canonical.Relationship) (VerifyResult, error) {
	key := CacheKey(a, b, c.promptVersion, c.model)
	if cached, ok, err := c.cache.Get(ctx, key); err != nil {
		return VerifyResult{}, fmt.Errorf("reading verdict cache: %w", err)
	} else if ok {
		// Cache hit: return the verdict with zero usage (no API call was made)
		return VerifyResult{Verdict: cached, Usage: Usage{}}, nil
	}

	result, err := c.verifier.Verify(ctx, a, b, r)
	if err != nil {
		return result, err
	}

	// AO review: log cache Put failures but return the verdict (don't discard paid work)
	if err := c.cache.Put(ctx, key, result.Verdict); err != nil {
		c.log.WarnContext(ctx, "failed to cache verdict", "err", err, "cache_key", key)
	}

	return result, nil
}
