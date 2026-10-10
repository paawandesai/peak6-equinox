package web

import (
	"context"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// liveQuoteStore is the slice of the store that liveQuotes needs.
type liveQuoteStore interface {
	ListQuotes(ctx context.Context, roundID int64) ([]store.QuoteRecord, error)
	LatestRound(ctx context.Context) (store.Round, error)
	GetQuote(ctx context.Context, roundID int64, instrumentID string) (store.QuoteRecord, error)
}

// liveQuote is liveQuotes for one instrument: the newest finished round's
// quote when it is newer than roundID's, else roundID's own. Routing, the pair
// page and the matches list all use this rule, so the price shown and the
// book routed are the same one. Not-found is returned only when neither round
// has a quote.
func liveQuote(ctx context.Context, st liveQuoteStore, roundID int64, id string) (store.QuoteRecord, error) {
	own, ownErr := st.GetQuote(ctx, roundID, id)
	if ownErr != nil && !isNotFound(ownErr) {
		return store.QuoteRecord{}, ownErr
	}
	latest, err := st.LatestRound(ctx)
	if err != nil || latest.ID == roundID {
		return own, ownErr //nolint:nilerr // no newer round: the round's own quote stands
	}
	newer, err := st.GetQuote(ctx, latest.ID, id)
	switch {
	case isNotFound(err):
		return own, ownErr
	case err != nil:
		return store.QuoteRecord{}, err
	}
	if ownErr != nil || newer.FetchedAt.After(own.FetchedAt) {
		return newer, nil
	}
	return own, nil
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

// quoteSnapshot is the snapshot time the router measures quote age against.
// liveQuote can hand the router a quote from a newer round than the pinned
// one, so ages are taken from the newest finished round's snapshot when it is
// later; otherwise a newer quote would show a negative age and pass the
// staleness check however old it really is.
func quoteSnapshot(ctx context.Context, st liveQuoteStore, r store.Round) time.Time {
	snap := r.SnapshotAt()
	latest, err := st.LatestRound(ctx)
	if err != nil || latest.ID == r.ID {
		return snap
	}
	if ls := latest.SnapshotAt(); ls.After(snap) {
		return ls
	}
	return snap
}

// decisionSnapshot is the "as of" time for a stored decision's inputs: the
// round's snapshot, or the newest input fetch when the router read live quotes
// from a later round. Ages on the trace are measured from it, so they are
// never negative and match the badges on the pair and matches pages.
func decisionSnapshot(d canonical.Decision, roundSnapshot time.Time) time.Time {
	snap := roundSnapshot
	for _, in := range d.Inputs {
		if in.FetchedAt.After(snap) {
			snap = in.FetchedAt
		}
	}
	return snap
}
