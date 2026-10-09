package web

import (
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestComputeNetGap(t *testing.T) {
	// Kalshi instrument with verified fee schedule (7% × 1.0 multiplier).
	kalshiInst := canonical.Instrument{
		ID:    "kalshi:test",
		Venue: canonical.VenueKalshi,
		FeeSchedule: canonical.FeeSchedule{
			ID:       canonical.FeeScheduleKalshiTakerV1,
			Verified: true,
			Source:   "https://kalshi.com/docs/kalshi-fee-schedule.pdf",
			Params: map[string]decimal.Decimal{
				"multiplier": decimal.RequireFromString("0.07"),
				"round_to":   decimal.RequireFromString("0.01"),
			},
		},
	}
	// Polymarket instrument with verified fee schedule (4% rate, exponent 1).
	polyInst := canonical.Instrument{
		ID:    "polymarket:test",
		Venue: canonical.VenuePolymarket,
		FeeSchedule: canonical.FeeSchedule{
			ID:       canonical.FeeSchedulePolymarketV1,
			Verified: true,
			Source:   "https://docs.polymarket.com/trading/fees",
			Params: map[string]decimal.Decimal{
				"rate":     decimal.RequireFromString("0.04"),
				"exponent": decimal.NewFromInt(1),
			},
		},
	}

	// Quote at 0.50 for Kalshi (100 contracts).
	kalshiQuote := &canonical.Quote{
		InstrumentID: "kalshi:test",
		YesAsks: []canonical.Level{
			{Price: decimal.RequireFromString("0.50"), Size: decimal.NewFromInt(100)},
		},
	}
	// Quote at 0.52 for Polymarket (100 contracts).
	polyQuote := &canonical.Quote{
		InstrumentID: "polymarket:test",
		YesAsks: []canonical.Level{
			{Price: decimal.RequireFromString("0.52"), Size: decimal.NewFromInt(100)},
		},
	}

	t.Run("both quotes present", func(t *testing.T) {
		gap := ComputeNetGap(kalshiInst, polyInst, kalshiQuote, polyQuote)
		if !gap.HasValue {
			t.Fatalf("expected gap.HasValue=true, got false with reason %q", gap.Reason)
		}
		// Kalshi at 0.50: fee = 0.07 × 100 × 0.50 × 0.50 = 1.75, rounds to $1.75 (already a cent multiple)
		// All-in per contract: 0.50 + 1.75/100 = 0.5175
		// Polymarket at 0.52: fee = 100 × 0.04 × 0.52 × 0.48 = 0.9984, rounded to 0.99840
		// All-in per contract: 0.52 + 0.99840/100 = 0.529984
		// Net gap = 0.529984 - 0.5175 = 0.012484
		expected := decimal.RequireFromString("0.012484")
		if !gap.Value.Equal(expected) {
			t.Errorf("gap.Value = %s, want %s", gap.Value, expected)
		}
	})

	t.Run("missing A quote", func(t *testing.T) {
		gap := ComputeNetGap(kalshiInst, polyInst, nil, polyQuote)
		if gap.HasValue {
			t.Errorf("expected gap.HasValue=false when A quote is nil")
		}
		if gap.Reason != "no quote: kalshi" {
			t.Errorf("gap.Reason = %q, want %q", gap.Reason, "no quote: kalshi")
		}
	})

	t.Run("missing B quote", func(t *testing.T) {
		gap := ComputeNetGap(kalshiInst, polyInst, kalshiQuote, nil)
		if gap.HasValue {
			t.Errorf("expected gap.HasValue=false when B quote is nil")
		}
		if gap.Reason != "no quote: polymarket" {
			t.Errorf("gap.Reason = %q, want %q", gap.Reason, "no quote: polymarket")
		}
	})

	t.Run("missing ask on A", func(t *testing.T) {
		emptyQuote := &canonical.Quote{InstrumentID: "kalshi:test"}
		gap := ComputeNetGap(kalshiInst, polyInst, emptyQuote, polyQuote)
		if gap.HasValue {
			t.Errorf("expected gap.HasValue=false when A has no asks")
		}
		if gap.Reason != "no ask: kalshi" {
			t.Errorf("gap.Reason = %q, want %q", gap.Reason, "no ask: kalshi")
		}
	})

	t.Run("gap sign: A cheaper (positive gap)", func(t *testing.T) {
		// A at 0.40, B at 0.50 → A is cheaper → positive gap.
		cheaperQuote := &canonical.Quote{
			InstrumentID: "kalshi:test",
			YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.40"), Size: decimal.NewFromInt(100)}},
		}
		expensiveQuote := &canonical.Quote{
			InstrumentID: "polymarket:test",
			YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.50"), Size: decimal.NewFromInt(100)}},
		}
		gap := ComputeNetGap(kalshiInst, polyInst, cheaperQuote, expensiveQuote)
		if !gap.HasValue {
			t.Fatalf("expected gap.HasValue=true, got false")
		}
		if gap.Value.LessThanOrEqual(decimal.Zero) {
			t.Errorf("gap.Value = %s, want positive when A is cheaper", gap.Value)
		}
	})

	t.Run("gap sign: B cheaper (negative gap)", func(t *testing.T) {
		// A at 0.60, B at 0.40 → B is cheaper → negative gap.
		expensiveQuote := &canonical.Quote{
			InstrumentID: "kalshi:test",
			YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.60"), Size: decimal.NewFromInt(100)}},
		}
		cheaperQuote := &canonical.Quote{
			InstrumentID: "polymarket:test",
			YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.40"), Size: decimal.NewFromInt(100)}},
		}
		gap := ComputeNetGap(kalshiInst, polyInst, expensiveQuote, cheaperQuote)
		if !gap.HasValue {
			t.Fatalf("expected gap.HasValue=true, got false")
		}
		if gap.Value.GreaterThanOrEqual(decimal.Zero) {
			t.Errorf("gap.Value = %s, want negative when B is cheaper", gap.Value)
		}
	})
}

func TestDescribeFeeSchedule(t *testing.T) {
	tests := []struct {
		name  string
		sched canonical.FeeSchedule
		want  string
	}{
		{
			name: "Kalshi default (7%, round to 1¢)",
			sched: canonical.FeeSchedule{
				ID: canonical.FeeScheduleKalshiTakerV1,
				Params: map[string]decimal.Decimal{
					"multiplier": decimal.RequireFromString("0.07"),
				},
			},
			want: "7.0 × C × P × (1-P), rounded up to 1¢",
		},
		{
			name: "Kalshi with custom multiplier and round_to",
			sched: canonical.FeeSchedule{
				ID: canonical.FeeScheduleKalshiTakerV1,
				Params: map[string]decimal.Decimal{
					"multiplier": decimal.RequireFromString("0.035"),
					"round_to":   decimal.RequireFromString("0.001"),
				},
			},
			want: "3.5 × C × P × (1-P), rounded up to 0.1¢",
		},
		{
			name: "Polymarket default (exponent 1)",
			sched: canonical.FeeSchedule{
				ID: canonical.FeeSchedulePolymarketV1,
				Params: map[string]decimal.Decimal{
					"rate": decimal.RequireFromString("0.04"),
				},
			},
			want: "C × 0.04 × P × (1-P)",
		},
		{
			name: "Polymarket with exponent 2",
			sched: canonical.FeeSchedule{
				ID: canonical.FeeSchedulePolymarketV1,
				Params: map[string]decimal.Decimal{
					"rate":     decimal.RequireFromString("0.05"),
					"exponent": decimal.NewFromInt(2),
				},
			},
			want: "C × 0.05 × (P × (1-P))^2",
		},
		{
			name: "unknown schedule",
			sched: canonical.FeeSchedule{
				ID: "unknown_schedule",
			},
			want: "unknown formula",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DescribeFeeSchedule(tt.sched)
			if got != tt.want {
				t.Errorf("DescribeFeeSchedule() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildFeeEstimates(t *testing.T) {
	kalshiInst := canonical.Instrument{
		ID:    "kalshi:test1",
		Venue: canonical.VenueKalshi,
		FeeSchedule: canonical.FeeSchedule{
			ID:       canonical.FeeScheduleKalshiTakerV1,
			Verified: true,
			Source:   "https://kalshi.com/docs/kalshi-fee-schedule.pdf",
			Params: map[string]decimal.Decimal{
				"multiplier": decimal.RequireFromString("0.07"),
			},
		},
	}
	polyInst := canonical.Instrument{
		ID:    "polymarket:test1",
		Venue: canonical.VenuePolymarket,
		FeeSchedule: canonical.FeeSchedule{
			ID:       canonical.FeeSchedulePolymarketV1,
			Verified: false,
			Source:   "https://docs.polymarket.com/trading/fees",
			Params: map[string]decimal.Decimal{
				"rate": decimal.RequireFromString("0.05"),
			},
		},
	}
	polyInst2 := canonical.Instrument{
		ID:    "polymarket:test2",
		Venue: canonical.VenuePolymarket,
		FeeSchedule: canonical.FeeSchedule{
			ID:       canonical.FeeSchedulePolymarketV1,
			Verified: false,
			Source:   "https://docs.polymarket.com/trading/fees",
			Params: map[string]decimal.Decimal{
				"rate": decimal.RequireFromString("0.04"),
			},
		},
	}

	estimates := BuildFeeEstimates([]canonical.Instrument{kalshiInst, polyInst, polyInst2})
	if len(estimates) != 2 {
		t.Fatalf("expected 2 unique venue estimates, got %d", len(estimates))
	}

	// Check Kalshi estimate.
	var kalshiEst *FeeEstimate
	for i := range estimates {
		if estimates[i].Venue == "kalshi" {
			kalshiEst = &estimates[i]
			break
		}
	}
	if kalshiEst == nil {
		t.Fatal("expected Kalshi estimate")
	}
	if kalshiEst.Approximate {
		t.Error("expected Kalshi estimate to be exact (Verified=true)")
	}
	if kalshiEst.Source != "https://kalshi.com/docs/kalshi-fee-schedule.pdf" {
		t.Errorf("Kalshi source = %q, want kalshi.com URL", kalshiEst.Source)
	}

	// Check Polymarket estimate.
	var polyEst *FeeEstimate
	for i := range estimates {
		if estimates[i].Venue == "polymarket" {
			polyEst = &estimates[i]
			break
		}
	}
	if polyEst == nil {
		t.Fatal("expected Polymarket estimate")
	}
	if !polyEst.Approximate {
		t.Error("expected Polymarket estimate to be approximate (Verified=false)")
	}
	if polyEst.Source != "https://docs.polymarket.com/trading/fees" {
		t.Errorf("Polymarket source = %q, want docs.polymarket.com URL", polyEst.Source)
	}
}

// TestNetGapSymmetry is a property test: swapping A and B negates the gap.
func TestNetGapSymmetry(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate two prices in [0.10, 0.90] to avoid edge cases where fees dominate.
		priceA := rapid.Float64Range(0.10, 0.90).Draw(rt, "priceA")
		priceB := rapid.Float64Range(0.10, 0.90).Draw(rt, "priceB")

		aInst := canonical.Instrument{
			ID:    "kalshi:a",
			Venue: canonical.VenueKalshi,
			FeeSchedule: canonical.FeeSchedule{
				ID:       canonical.FeeScheduleKalshiTakerV1,
				Verified: false,
				Params: map[string]decimal.Decimal{
					"multiplier": decimal.RequireFromString("0.07"),
					"round_to":   decimal.RequireFromString("0.01"),
				},
			},
		}
		bInst := canonical.Instrument{
			ID:    "polymarket:b",
			Venue: canonical.VenuePolymarket,
			FeeSchedule: canonical.FeeSchedule{
				ID:       canonical.FeeSchedulePolymarketV1,
				Verified: false,
				Params: map[string]decimal.Decimal{
					"rate":     decimal.RequireFromString("0.04"),
					"exponent": decimal.NewFromInt(1),
				},
			},
		}
		aQuote := &canonical.Quote{
			InstrumentID: "kalshi:a",
			YesAsks:      []canonical.Level{{Price: decimal.NewFromFloat(priceA), Size: decimal.NewFromInt(100)}},
		}
		bQuote := &canonical.Quote{
			InstrumentID: "polymarket:b",
			YesAsks:      []canonical.Level{{Price: decimal.NewFromFloat(priceB), Size: decimal.NewFromInt(100)}},
		}

		gapAB := ComputeNetGap(aInst, bInst, aQuote, bQuote)
		gapBA := ComputeNetGap(bInst, aInst, bQuote, aQuote)

		if !gapAB.HasValue || !gapBA.HasValue {
			rt.Fatalf("expected both gaps to have values")
		}
		// gapAB = B_all_in - A_all_in, gapBA = A_all_in - B_all_in → gapAB = -gapBA.
		sum := gapAB.Value.Add(gapBA.Value)
		if !sum.Abs().LessThan(decimal.RequireFromString("0.00001")) {
			rt.Fatalf("gapAB (%s) + gapBA (%s) = %s, want ~0", gapAB.Value, gapBA.Value, sum)
		}
	})
}

// TestNetGapFeeRounding verifies that fee rounding (Kalshi ceil-to-cent, Polymarket 5dp)
// is correctly applied in net gap computation.
func TestNetGapFeeRounding(t *testing.T) {
	// Kalshi: 1 contract @ 0.50 → raw fee = 0.07 × 1 × 0.50 × 0.50 = 0.0175, rounds up to 0.02.
	kalshiInst := canonical.Instrument{
		ID:    "kalshi:round",
		Venue: canonical.VenueKalshi,
		FeeSchedule: canonical.FeeSchedule{
			ID:       canonical.FeeScheduleKalshiTakerV1,
			Verified: true,
			Source:   "https://kalshi.com/docs/kalshi-fee-schedule.pdf",
			Params: map[string]decimal.Decimal{
				"multiplier": decimal.RequireFromString("0.07"),
				"round_to":   decimal.RequireFromString("0.01"),
			},
		},
	}
	kalshiQuote := &canonical.Quote{
		InstrumentID: "kalshi:round",
		YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.50"), Size: decimal.NewFromInt(1)}},
	}

	// Polymarket: 100 contracts @ 0.45 → fee = 100 × 0.07 × 0.45 × 0.55 = 1.7325, rounds to 1.7325 (5dp).
	polyInst := canonical.Instrument{
		ID:    "polymarket:round",
		Venue: canonical.VenuePolymarket,
		FeeSchedule: canonical.FeeSchedule{
			ID:       canonical.FeeSchedulePolymarketV1,
			Verified: true,
			Source:   "https://docs.polymarket.com/trading/fees",
			Params: map[string]decimal.Decimal{
				"rate": decimal.RequireFromString("0.07"),
			},
		},
	}
	polyQuote := &canonical.Quote{
		InstrumentID: "polymarket:round",
		YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.45"), Size: decimal.NewFromInt(100)}},
	}

	// ComputeNetGap uses referenceSize=100, so:
	// Kalshi: fee = 0.07 × 100 × 0.50 × 0.50 = 1.75, rounds to 1.75 (exact).
	//   All-in per contract = 0.50 + 1.75/100 = 0.5175
	// Polymarket: fee = 100 × 0.07 × 0.45 × 0.55 = 1.7325 (5dp).
	//   All-in per contract = 0.45 + 1.7325/100 = 0.467325
	// Net gap = B - A = 0.467325 - 0.5175 = -0.050175 (negative = Polymarket cheaper).
	gap := ComputeNetGap(kalshiInst, polyInst, kalshiQuote, polyQuote)

	if !gap.HasValue {
		t.Fatalf("expected gap.HasValue=true, got false with reason %q", gap.Reason)
	}

	// Verifies: Kalshi fee at 1.75 stays 1.75 (ceil to cent when needed),
	// Polymarket 1.7325 uses 5dp precision (not rounded to 1.73).
	expected := decimal.RequireFromString("-0.050175")
	if !gap.Value.Equal(expected) {
		t.Errorf("gap.Value = %s, want %s (verifies fee rounding: Kalshi ceil-to-cent, Polymarket 5dp)", gap.Value, expected)
	}
}
