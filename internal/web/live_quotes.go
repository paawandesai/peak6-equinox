package web

import (
	"context"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// liveQuoteStore is the slice of the store that liveQuotes needs.
type liveQuoteStore interface {
	ListQuotes(ctx context.Context, roundID int64) ([]store.QuoteRecord, error)
	LatestRound(ctx context.Context) (store.Round, error)
}

// liveQuotes returns the quotes for roundID's instruments, replaced by the
// latest finished round's quote wherever that one is newer. A pinned round
// keeps its matched pairs, but the collector keeps writing new rounds every
// couple of minutes; without this overlay every price on a pinned page ages
// past STALE_AFTER_SECONDS and shows as stale even though fresh prices exist.
// Instruments only in the latest round are ignored (the pinned round's pairs
// are the page's universe).
func liveQuotes(ctx context.Context, st liveQuoteStore, roundID int64) ([]store.QuoteRecord, error) {
	quotes, err := st.ListQuotes(ctx, roundID)
	if err != nil {
		return nil, err
	}
	latest, err := st.LatestRound(ctx)
	if err != nil || latest.ID == roundID {
		return quotes, nil //nolint:nilerr // no newer round: the round's own quotes stand
	}
	newer, err := st.ListQuotes(ctx, latest.ID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]int, len(quotes))
	for i := range quotes {
		byID[quotes[i].InstrumentID] = i
	}
	for _, q := range newer {
		if i, ok := byID[q.InstrumentID]; ok && q.FetchedAt.After(quotes[i].FetchedAt) {
			quotes[i] = q
		}
	}
	return quotes, nil
}
