package eval

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// Lookback is how far back each category's settled markets are fetched.
// Categories settle at very different rates: crypto ladders settle hourly
// (hundreds of markets a day), Fed markets once per meeting.
type Lookback map[canonical.Category]time.Duration

const day = 24 * time.Hour

// DefaultLookback matches the windows the fixtures were recorded with
// (crypto one day, Fed and politics ninety days).
func DefaultLookback() Lookback {
	return Lookback{
		canonical.CategoryCrypto:   1 * day,
		canonical.CategoryFed:      90 * day,
		canonical.CategoryPolitics: 90 * day,
	}
}

// Since is the earliest settlement to fetch for cat, relative to asOf. A
// category with no lookback gets the zero time (no lower bound).
func (l Lookback) Since(cat canonical.Category, asOf time.Time) time.Time {
	d, ok := l[cat]
	if !ok || d <= 0 {
		return time.Time{}
	}
	return asOf.Add(-d).UTC()
}

// FetchSettled asks every adapter for the settled markets of each configured
// category, one call per (venue, category) so each category gets its own
// window. Adapters return what they could read alongside an error, so a
// failed series or tag is logged and reported and the rest still counts:
// the back-test must never silently shrink its sample. The returned errors
// are one per failed (venue, category), nil when all succeeded.
func FetchSettled(ctx context.Context, adapters []venues.Adapter, us venues.Universes, lb Lookback, asOf time.Time, logger *slog.Logger) ([]canonical.Settled, []error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	var (
		out  []canonical.Settled
		errs []error
	)
	for _, a := range adapters {
		if a == nil {
			continue
		}
		full := us.For(a.Venue())
		for _, cat := range canonical.Categories() {
			sel, ok := full.Categories[cat]
			if !ok || (len(sel.Series) == 0 && len(sel.TagSlugs) == 0) {
				continue
			}
			if err := ctx.Err(); err != nil {
				return out, append(errs, fmt.Errorf("fetching settled markets: %w", err))
			}
			u := venues.Universe{Venue: a.Venue(), Categories: map[canonical.Category]venues.Selector{cat: sel}}
			since := lb.Since(cat, asOf)
			settled, _, err := a.FetchSettled(ctx, u, since)
			logger.Info("fetched settled markets", "venue", a.Venue(), "category", cat, "since", since, "count", len(settled), "failed", err != nil)
			out = append(out, settled...)
			if err != nil {
				logger.Warn("settled fetch incomplete", "venue", a.Venue(), "category", cat, "error", err.Error())
				errs = append(errs, fmt.Errorf("%s %s: %w", a.Venue(), cat, err))
			}
		}
	}
	return out, errs
}

// ErrNoSettled is returned by callers when nothing at all was fetched.
var ErrNoSettled = errors.New("no settled markets fetched")
