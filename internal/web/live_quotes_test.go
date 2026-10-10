package web

import (
	"context"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

type fakeLiveQuotes struct {
	latest   int64
	latestAt time.Time // FinishedAt of the latest round
	quotes   map[int64][]store.QuoteRecord
}

func (f fakeLiveQuotes) ListQuotes(_ context.Context, id int64) ([]store.QuoteRecord, error) {
	return append([]store.QuoteRecord(nil), f.quotes[id]...), nil
}

func (f fakeLiveQuotes) LatestRound(context.Context) (store.Round, error) {
	if f.latest == 0 {
		return store.Round{}, store.ErrNotFound
	}
	return store.Round{ID: f.latest, FinishedAt: f.latestAt}, nil
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

func (f fakeLiveQuotes) GetQuote(_ context.Context, round int64, id string) (store.QuoteRecord, error) {
	for _, q := range f.quotes[round] {
		if q.InstrumentID == id {
			return q, nil
		}
	}
	return store.QuoteRecord{}, store.ErrNotFound
}

func TestLiveQuote(t *testing.T) {
	old := time.Date(2026, 10, 10, 4, 21, 0, 0, time.UTC)
	now := old.Add(6 * time.Hour)
	f := fakeLiveQuotes{latest: 950, quotes: map[int64][]store.QuoteRecord{
		904: {qr(904, "a", old), qr(904, "b", old)},
		950: {qr(950, "a", now), qr(950, "c", now)},
	}}
	ctx := context.Background()
	for id, want := range map[string]int64{"a": 950, "b": 904, "c": 950} {
		got, err := liveQuote(ctx, f, 904, id)
		if err != nil || got.RoundID != want {
			t.Errorf("%s: got round %d, %v; want %d", id, got.RoundID, err, want)
		}
	}
	if _, err := liveQuote(ctx, f, 904, "zz"); !isNotFound(err) {
		t.Errorf("missing everywhere: err = %v, want not found", err)
	}
}

// The router must age live quotes against the newest round's snapshot, not the
// pinned round's, or a later quote reads as negative age and is never stale.
func TestQuoteSnapshotUsesNewestRound(t *testing.T) {
	pinned := time.Date(2026, 10, 10, 4, 21, 0, 0, time.UTC)
	latest := pinned.Add(12 * time.Hour)
	r := store.Round{ID: 904, FinishedAt: pinned}
	ctx := context.Background()
	if got := quoteSnapshot(ctx, fakeLiveQuotes{latest: 950, latestAt: latest}, r); !got.Equal(latest) {
		t.Errorf("newer round: got %v, want %v", got, latest)
	}
	if got := quoteSnapshot(ctx, fakeLiveQuotes{latest: 904, latestAt: pinned}, r); !got.Equal(pinned) {
		t.Errorf("pinned is latest: got %v, want %v", got, pinned)
	}
	if got := quoteSnapshot(ctx, fakeLiveQuotes{}, r); !got.Equal(pinned) {
		t.Errorf("no latest round: got %v, want %v", got, pinned)
	}
	if got := quoteSnapshot(ctx, fakeLiveQuotes{latest: 950, latestAt: pinned.Add(-time.Hour)}, r); !got.Equal(pinned) {
		t.Errorf("older latest snapshot: got %v, want %v", got, pinned)
	}
}

func TestDecisionSnapshotNeverBeforeInputs(t *testing.T) {
	pinned := time.Date(2026, 10, 10, 4, 21, 0, 0, time.UTC)
	live := pinned.Add(12 * time.Hour)
	d := canonical.Decision{Inputs: []canonical.DecisionInput{{FetchedAt: pinned.Add(-time.Minute)}, {FetchedAt: live}}}
	if got := decisionSnapshot(d, pinned); !got.Equal(live) {
		t.Errorf("live inputs: got %v, want %v", got, live)
	}
	d.Inputs = d.Inputs[:1]
	if got := decisionSnapshot(d, pinned); !got.Equal(pinned) {
		t.Errorf("pinned inputs: got %v, want %v", got, pinned)
	}
	_, maxAge, _ := buildInputSnapshots(canonical.Decision{Inputs: []canonical.DecisionInput{{FetchedAt: live.Add(-5 * time.Minute)}, {FetchedAt: live}}}, decisionSnapshot(canonical.Decision{Inputs: []canonical.DecisionInput{{FetchedAt: live}}}, pinned))
	if maxAge != 5*time.Minute {
		t.Errorf("max input age = %v, want 5m", maxAge)
	}
}
