package kalshi

import (
	"context"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestFetchQuotesCoverageContextCancellation tests the context cancellation
// path in FetchQuotes (kalshi.go:196-199). This path is nondeterministically
// covered in concurrent tests but needs deterministic coverage for the gate.
// M2-04 follow-up, round 9.
func TestFetchQuotesCoverageContextCancellation(t *testing.T) {
	fs := newFixtureServer(t)
	a := newTestAdapter(t, fs.URL, nil)

	// Create an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately before FetchQuotes

	ins := []canonical.Instrument{
		{
			ID:            "kalshi:KXFED-26OCT-T4.00",
			VenueMarketID: "KXFED-26OCT-T4.00",
			Category:      canonical.CategoryFed,
		},
	}

	quotes, _, err := a.FetchQuotes(ctx, ins)
	// Should return error for cancelled context
	if err == nil {
		t.Error("expected error from cancelled context")
	}
	// When context is cancelled immediately, quotes may be empty or contain no_book entries
	// The important thing is the cancellation path (196-199) is covered
	t.Logf("got %d quotes with cancelled context", len(quotes))
}

// TestFetchQuoteCoverageZeroFetchedAt tests the zero FetchedAt path in
// fetchQuote (kalshi.go:243-244). This path uses Now() when raw.FetchedAt
// is zero, and is nondeterministically covered depending on response timing.
// M2-04 follow-up, round 9.
func TestFetchQuoteCoverageZeroFetchedAt(t *testing.T) {
	nowCalled := false
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	fs := newFixtureServer(t)
	a := newTestAdapter(t, fs.URL, func(o *Options) {
		o.Now = func() time.Time {
			nowCalled = true
			return now
		}
	})

	ins := canonical.Instrument{
		ID:            "kalshi:KXFED-26OCT-T4.00",
		VenueMarketID: "KXFED-26OCT-T4.00",
		Category:      canonical.CategoryFed,
	}

	ctx := context.Background()
	quote, raw, err := a.fetchQuote(ctx, ins)

	// Should use Now() when raw.FetchedAt is zero (fixture responses don't set it)
	if !nowCalled {
		t.Error("expected Now() to be called when FetchedAt is zero")
	}
	if !quote.FetchedAt.Equal(now) {
		t.Errorf("expected FetchedAt=%v, got %v", now, quote.FetchedAt)
	}
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if raw.Body == nil {
		t.Error("expected raw body")
	}
}
