package store_test

import (
	"context"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func TestGetInstruments(t *testing.T) {
	s := store.New(tdb.Pool(t))
	tdb.Reset(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 6, 17, 0, 0, 0, time.UTC)
	r, err := s.CreateRound(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"kalshi:BYID-A", "polymarket:1", "kalshi:BYID-C"}
	for _, id := range ids {
		in := canonical.Instrument{ID: id, Venue: canonical.VenueOf(id), VenueMarketID: id, Category: canonical.CategoryCrypto,
			Title: "t " + id, Terms: canonical.UnknownTerms(), Status: canonical.StatusOpen,
			FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1}}
		if err := s.UpsertInstrument(ctx, r.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetInstruments(ctx, []string{"kalshi:BYID-A", "kalshi:BYID-C", "kalshi:MISSING", "kalshi:\x00nul", "bad\xff"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["kalshi:BYID-A"].Title != "t kalshi:BYID-A" || got["kalshi:BYID-C"].LastSeenRound != r.ID {
		t.Errorf("got = %+v", got)
	}
	if _, ok := got["kalshi:MISSING"]; ok {
		t.Error("missing id returned a record")
	}
	for _, empty := range [][]string{nil, {}, {"\x00"}} {
		got, err := s.GetInstruments(ctx, empty)
		if err != nil || len(got) != 0 {
			t.Errorf("GetInstruments(%q) = %v, %v", empty, got, err)
		}
	}
}
