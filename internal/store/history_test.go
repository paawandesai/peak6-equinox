package store_test

import (
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func TestListRoundsSince(t *testing.T) {
	s := newStore(t)
	for i := 0; i < 4; i++ {
		r, err := s.CreateRound(ctx, t0.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if i < 3 {
			if err := s.FinishRound(ctx, r.ID, store.RoundOK, nil, t0.Add(time.Duration(i)*time.Hour+time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := s.ListRoundsSince(ctx, t0.Add(90*time.Minute), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Status != store.RoundRunning || got[1].Status != store.RoundOK {
		t.Fatalf("rounds since +90m = %+v, want the running one then the 2h one", got)
	}
	if got[0].ID < got[1].ID {
		t.Error("rounds should be newest first")
	}
	got, err = s.ListRoundsSince(ctx, t0, 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("limit 2: %v, %d rounds", err, len(got))
	}
	got, err = s.ListRoundsSince(ctx, t0.Add(24*time.Hour), 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("future since: %v, %d rounds", err, len(got))
	}
}

func TestQuoteHistory(t *testing.T) {
	s := newStore(t)
	const a, b, c = "kalshi:HIST-A", "polymarket:11112222", "kalshi:HIST-C"
	var rounds []store.Round
	for i := 0; i < 3; i++ {
		r, err := s.CreateRound(ctx, t0.Add(time.Duration(i)*5*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		rounds = append(rounds, r)
		for _, id := range []string{a, b, c} {
			if err := s.UpsertInstrument(ctx, r.ID, instrument(id)); err != nil {
				t.Fatal(err)
			}
			q := canonical.Quote{InstrumentID: id, FetchedAt: r.StartedAt,
				YesBids: []canonical.Level{{Price: d("0.4"), Size: d("10")}}, YesAsks: []canonical.Level{{Price: d("0.6"), Size: d("10")}}}
			if i == 2 && id == a {
				q.YesBids = nil // one-sided: no mid
			}
			if err := s.InsertQuote(ctx, r.ID, q); err != nil {
				t.Fatal(err)
			}
		}
	}
	pts, err := s.QuoteHistory(ctx, []string{a, b}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 6 {
		t.Fatalf("got %d points, want 6 (3 rounds x 2 instruments; C excluded)", len(pts))
	}
	for i := 1; i < len(pts); i++ {
		if pts[i].FetchedAt.Before(pts[i-1].FetchedAt) {
			t.Errorf("points not oldest first at %d: %v", i, pts[i].FetchedAt)
		}
	}
	if pts[0].RoundID != rounds[0].ID || pts[0].InstrumentID != a || pts[5].InstrumentID != b || pts[5].RoundID != rounds[2].ID {
		t.Errorf("ordering: first %+v, last %+v", pts[0], pts[5])
	}
	last := pts[4]
	if last.InstrumentID != a || last.Mid.Valid || last.BestBid.Valid || !last.BestAsk.Valid || last.BestAsk.Decimal.String() != "0.6" {
		t.Errorf("one-sided point = %+v", last)
	}
	if !pts[0].Mid.Valid || pts[0].Mid.Decimal.String() != "0.5" {
		t.Errorf("mid = %+v", pts[0].Mid)
	}

	// The limit keeps the most recent points, still returned oldest first.
	pts, err = s.QuoteHistory(ctx, []string{a, b}, 2)
	if err != nil || len(pts) != 2 || pts[0].RoundID != rounds[2].ID {
		t.Fatalf("limit 2: %v, %+v", err, pts)
	}
	for _, ids := range [][]string{nil, {}, {a, b, c}, {"kalshi:bad\x00id"}} {
		pts, err := s.QuoteHistory(ctx, ids, 0)
		if err != nil || pts != nil {
			t.Errorf("QuoteHistory(%q) = %v, %v; want nil, nil", ids, pts, err)
		}
	}
}
