package route

import (
	"fmt"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestRejectSureLossComplement: the router must never pick a leg with all-in cost >= $1
// per $1 payout, even if it's the cheapest option (Issue #73, decision #71).
//
// Scenario: buy YES on target at 0.99, but complement offers NO at all-in >= 1.0
// (a guaranteed loss). The router must exclude the complement leg and choose the direct.
func TestRejectSureLossComplement(t *testing.T) {
	// Target: Kalshi YES @ 0.99 (all-in 0.99 + fee ≈ 0.997)
	target := cand(kalshiID, []canonical.Level{lv("0.98", "100")}, []canonical.Level{lv("0.99", "100")}, nil)

	// Complement: Polymarket
	// To buy NO on complement, we consume YES bids, re-priced as 1 - p
	// YES bid @ 0.001 → NO ask @ 1 - 0.001 = 0.999
	// But with a high basis penalty (score 0.3 × multiplier 0.01 = 0.003)
	// and fees, the all-in can exceed $1
	// Fee: 0.07 × 0.999 × 0.001 ≈ 0.00007 (very small)
	// All-in: 0.999 + 0.00007 + 0.003 = 1.00207 (> $1, guaranteed loss)
	compPartner := cand(polymarketID, []canonical.Level{lv("0.001", "100")}, []canonical.Level{lv("0.999", "100")},
		rel(kalshiID, polymarketID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0.3"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{compPartner})

	// The complement must be excluded (all-in >= $1), and the target should be chosen
	if len(dec.Chosen) != 1 {
		t.Fatalf("chosen = %+v, want 1 leg (target)", dec.Chosen)
	}
	chosen := dec.Chosen[0]
	if chosen.InstrumentID != kalshiID {
		t.Errorf("chosen instrument = %s, want %s (target, not complement)", chosen.InstrumentID, kalshiID)
	}
	if chosen.Route != RouteDirect {
		t.Errorf("chosen route = %s, want %s", chosen.Route, RouteDirect)
	}

	// The complement must appear in exclusions with a "sure loss" or cost-related reason
	foundExclusion := false
	for _, ex := range dec.Exclusions {
		if ex.InstrumentID == polymarketID {
			foundExclusion = true
			// The reason should indicate the all-in cost is invalid
			if !strings.Contains(ex.Reason, "cost") && !strings.Contains(ex.Detail, "cost") &&
				!strings.Contains(ex.Detail, "$1") && !strings.Contains(ex.Detail, "loss") {
				t.Errorf("exclusion reason/detail should mention cost or loss: reason=%s, detail=%s",
					ex.Reason, ex.Detail)
			}
		}
	}
	if !foundExclusion {
		t.Errorf("complement %s should be excluded for sure-loss cost, but exclusions = %+v",
			polymarketID, dec.Exclusions)
	}
}

// TestRejectComplementCostlierThanDirect: never pick a complement leg costlier than the direct leg.
func TestRejectComplementCostlierThanDirect(t *testing.T) {
	// Target: Kalshi YES @ 0.50 (all-in ≈ 0.507)
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.50", "100")}, nil)

	// Complement: Polymarket, YES bid @ 0.40 → NO ask @ 0.60 (all-in ≈ 0.607)
	// This is worse than the direct route (0.507), so it should not be chosen.
	compPartner := cand(polymarketID, []canonical.Level{lv("0.40", "100")}, []canonical.Level{lv("0.60", "100")},
		rel(kalshiID, polymarketID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{compPartner})

	// The direct route should be chosen (cheaper)
	if len(dec.Chosen) != 1 {
		t.Fatalf("chosen = %+v, want 1 leg (target)", dec.Chosen)
	}
	chosen := dec.Chosen[0]
	if chosen.InstrumentID != kalshiID {
		t.Errorf("chosen instrument = %s, want %s (target, not complement)", chosen.InstrumentID, kalshiID)
	}
	if chosen.Route != RouteDirect {
		t.Errorf("chosen route = %s, want %s", chosen.Route, RouteDirect)
	}

	// The complement should appear in alternatives ranked lower
	foundComplement := false
	for _, alt := range dec.Alternatives {
		if len(alt.Legs) == 1 && alt.Legs[0].InstrumentID == polymarketID {
			foundComplement = true
			if alt.Rank < 2 {
				t.Errorf("complement should not be rank 1 when it's costlier: rank=%d", alt.Rank)
			}
		}
	}
	if !foundComplement {
		t.Errorf("complement should appear in alternatives: %+v", dec.Alternatives)
	}
}

// TestRegressionRound735Decision71: regression test for issue #73, decision #71.
//
// This test pins the specific scenario from round 735 where a complement route
// picked a sure-loss leg (all-in 1.0019) and showed incorrect slippage.
func TestRegressionRound735Decision71(t *testing.T) {
	// Recreate the scenario from decision #71:
	// - Buy YES on target
	// - Complement offers NO at a price that costs >= $1 all-in
	// - Quoted reference should be for the NO side, not YES

	// Target: direct YES @ 0.98 (reasonable)
	target := cand(kalshiID, []canonical.Level{lv("0.97", "100")}, []canonical.Level{lv("0.98", "100")}, nil)

	// Complement: YES bid @ 0.001 → NO ask @ 1 - 0.001 = 0.999
	// With high basis penalty (0.5 × 0.01 = 0.005), all-in ≈ 0.999 + 0.0001 + 0.005 = 1.0041
	compPartner := cand(otherID,
		[]canonical.Level{lv("0.001", "100")}, // YES bids (used for buying NO)
		[]canonical.Level{lv("0.999", "100")}, // YES asks
		rel(kalshiID, otherID, canonical.RelComplement, "0.95"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0.5"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{compPartner})

	// The complement must be excluded, and the target should be chosen
	if len(dec.Chosen) != 1 {
		t.Fatalf("chosen = %+v, want 1 leg (target)", dec.Chosen)
	}
	chosen := dec.Chosen[0]
	if chosen.InstrumentID != kalshiID {
		t.Errorf("chosen instrument = %s, want %s (target)", chosen.InstrumentID, kalshiID)
	}
	if chosen.AllInPerContract.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		t.Errorf("chosen all-in = %s, must be < $1 per $1 payout", chosen.AllInPerContract)
	}

	// The complement must be excluded
	foundExclusion := false
	for _, ex := range dec.Exclusions {
		if ex.InstrumentID == otherID {
			foundExclusion = true
		}
	}
	if !foundExclusion {
		t.Errorf("complement should be excluded, but exclusions = %+v", dec.Exclusions)
	}

	// Explanation should NOT mention choosing a complement route
	if strings.Contains(dec.Explanation, fmt.Sprintf("Chose NO on %s", otherID)) {
		t.Errorf("explanation should not claim the complement was chosen: %s", dec.Explanation)
	}
}
