package web

import (
	"context"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

type fakeLiveQuotes struct {
	latest int64
	quotes map[int64][]store.QuoteRecord
}

func (f fakeLiveQuotes) ListQuotes(_ context.Context, id int64) ([]store.QuoteRecord, error) {
	return append([]store.QuoteRecord(nil), f.quotes[id]...), nil
}

func (f fakeLiveQuotes) LatestRound(context.Context) (store.Round, error) {
	if f.latest == 0 {
		return store.Round{}, store.ErrNotFound
	}
	return store.Round{ID: f.latest}, nil
}

func qr(round int64, id string, at time.Time) store.QuoteRecord {
	return store.QuoteRecord{RoundID: round, Quote: canonical.Quote{InstrumentID: id, FetchedAt: at}}
}

func TestLiveQuotesOverlaysNewerRound(t *testing.T) {
	old := time.Date(2026, 10, 10, 4, 21, 0, 0, time.UTC)
	now := old.Add(6 * time.Hour)
	f := fakeLiveQuotes{latest: 950, quotes: map[int64][]store.QuoteRecord{
		904: {qr(904, "a", old), qr(904, "b", old)},
		950: {qr(950, "a", now), qr(950, "c", now)},
	}}
	got, err := liveQuotes(context.Background(), f, 904)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (instrument c must not be added)", len(got))
	}
	if got[0].RoundID != 950 || !got[0].FetchedAt.Equal(now) {
		t.Errorf("a not refreshed: %+v", got[0])
	}
	if got[1].RoundID != 904 {
		t.Errorf("b without newer quote must keep the pinned round's: %+v", got[1])
	}
}

func TestLiveQuotesSameOrNoLatestRound(t *testing.T) {
	at := time.Date(2026, 10, 10, 4, 21, 0, 0, time.UTC)
	for _, latest := range []int64{0, 904} {
		f := fakeLiveQuotes{latest: latest, quotes: map[int64][]store.QuoteRecord{904: {qr(904, "a", at)}}}
		got, err := liveQuotes(context.Background(), f, 904)
		if err != nil || len(got) != 1 || got[0].RoundID != 904 {
			t.Errorf("latest=%d: got %+v, %v", latest, got, err)
		}
	}
}
