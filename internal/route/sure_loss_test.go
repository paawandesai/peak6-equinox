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

	// The complement must be excluded with reason "sure_loss" (Cursor round 1 #3)
	foundExclusion := false
	for _, ex := range dec.Exclusions {
		if ex.InstrumentID == polymarketID {
			foundExclusion = true
			if ex.Reason != ReasonSureLoss {
				t.Errorf("exclusion reason = %s, want %s", ex.Reason, ReasonSureLoss)
			}
			if !strings.Contains(ex.Detail, "$1 payout") && !strings.Contains(ex.Detail, "guaranteed loss") {
				t.Errorf("exclusion detail should mention $1 payout or guaranteed loss: %s", ex.Detail)
			}
		}
	}
	if !foundExclusion {
		t.Errorf("complement %s should be excluded, but exclusions = %+v", polymarketID, dec.Exclusions)
	}
}

// TestRejectComplementCostlierThanDirect: never pick a complement leg costlier than the direct leg.
func TestRejectComplementCostlierThanDirect(t *testing.T) {
	// Target: Kalshi YES @ 0.50
	// Fee: ceil(0.07 × 100 × 0.50 × 0.50) = ceil(1.75) = 1.75 → 0.0175/contract
	// All-in: 0.50 + 0.0175 = 0.5175
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.50", "100")}, nil)

	// Complement: Polymarket, YES bid @ 0.40 → NO ask @ 0.60
	// Fee: 100 × 0.07 × 0.60 × 0.40 = 1.68 → 0.0168/contract
	// All-in: 0.60 + 0.0168 = 0.6168
	// This is worse than the direct route (0.5175), so it should not be chosen.
	compPartner := cand(polymarketID, []canonical.Level{lv("0.40", "100")}, []canonical.Level{lv("0.60", "100")},
		rel(kalshiID, polymarketID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{compPartner})

	// The direct route should be chosen (cheaper: 0.5175 < 0.6168)
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
	// Verify the actual all-in cost (Cursor round 1 #2)
	if !chosen.AllInPerContract.Equal(d("0.5175")) {
		t.Errorf("chosen all-in = %s, want 0.5175", chosen.AllInPerContract)
	}

	// The complement should appear in alternatives ranked lower
	foundComplement := false
	for _, alt := range dec.Alternatives {
		if len(alt.Legs) == 1 && alt.Legs[0].InstrumentID == polymarketID {
			foundComplement = true
			if alt.Rank < 2 {
				t.Errorf("complement should not be rank 1 when it's costlier: rank=%d", alt.Rank)
			}
			// Verify the complement's all-in cost
			if !alt.Legs[0].AllInPerContract.Equal(d("0.6168")) {
				t.Errorf("complement all-in = %s, want 0.6168", alt.Legs[0].AllInPerContract)
			}
		}
	}
	if !foundComplement {
		t.Errorf("complement should appear in alternatives: %+v", dec.Alternatives)
	}
}

// TestRegressionRound735Decision71: regression test for issue #73, decision #71.
//
// Pins the corrected behavior: complement at 0.999 is excluded for sure-loss,
// and if it were viable, inputs would show BestAsk=0.999 (re-priced), slippage ≈ 0 bps.
func TestRegressionRound735Decision71(t *testing.T) {
	// Recreate the scenario from decision #71:
	// - Buy YES on target
	// - Complement offers NO at a price that costs >= $1 all-in
	// - After fix: BestAsk for NO should be re-priced (1 - YES bid), slippage correct

	// Target: direct YES @ 0.98
	target := cand(kalshiID, []canonical.Level{lv("0.97", "100")}, []canonical.Level{lv("0.98", "100")}, nil)

	// Complement: YES bid @ 0.001 → NO ask @ 1 - 0.001 = 0.999
	// With high basis penalty (0.5 × 0.01 = 0.005), all-in ≈ 0.999 + 0.00007 + 0.005 = 1.00407
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

	// The complement must be excluded with reason "sure_loss"
	foundExclusion := false
	for _, ex := range dec.Exclusions {
		if ex.InstrumentID == otherID {
			foundExclusion = true
			if ex.Reason != ReasonSureLoss {
				t.Errorf("exclusion reason = %s, want %s", ex.Reason, ReasonSureLoss)
			}
		}
	}
	if !foundExclusion {
		t.Errorf("complement should be excluded, but exclusions = %+v", dec.Exclusions)
	}

	// Pin the corrected input snapshot (Cursor round 2 #1): BestAsk for NO should be re-priced
	// YES bid 0.001 → NO ask 1 - 0.001 = 0.999
	compInput := findInput(t, dec.Inputs, otherID)
	if compInput.OutcomeSide != canonical.OutcomeNo {
		t.Errorf("complement input outcome = %s, want NO", compInput.OutcomeSide)
	}
	if !compInput.BestAsk.Equal(d("0.999")) {
		t.Errorf("complement input BestAsk = %s, want 0.999 (1 - 0.001, re-priced)", compInput.BestAsk)
	}
	// If the complement were viable (not excluded), slippage would be VWAP - BestAsk ≈ 0
	// (both ≈ 0.999 after re-pricing), not the old bug's 9980 bps from comparing 0.999 vs 0.001

	// Explanation should NOT mention choosing a complement route
	if strings.Contains(dec.Explanation, fmt.Sprintf("Chose NO on %s", otherID)) {
		t.Errorf("explanation should not claim the complement was chosen: %s", dec.Explanation)
	}
}

// TestSellSideSureLoss: sell-side sure-loss guard (net proceeds <= $0).
func TestSellSideSureLoss(t *testing.T) {
	// Sell YES on target at VWAP 0.01
	// Fee: ceil(0.07 × 100 × 0.01 × 0.99) = ceil(0.0693) = 0.07 → 0.0007/contract
	// Basis: 0.01 (score 1.0)
	// Net proceeds: 0.01 - 0.0007 - 0.01 = -0.0007 <= 0 (sure loss)
	target := cand(kalshiID, []canonical.Level{lv("0.01", "100")}, []canonical.Level{lv("0.02", "100")}, nil)
	target.Relationship = &canonical.Relationship{
		AID:        kalshiID,
		BID:        kalshiID,
		Type:       "self",
		Confidence: decimal.NewFromInt(1),
		BasisRisk:  &canonical.BasisRisk{Score: decimal.NewFromInt(1), Components: []canonical.BasisComponent{}},
	}

	dec := mustRoute(t, sell("100", "YES"), target, []canonical.Candidate{})

	// Target should be excluded for sure_loss (net proceeds <= 0)
	if len(dec.Chosen) != 0 {
		t.Errorf("chosen = %+v, want 0 legs (sure-loss)", dec.Chosen)
	}
	foundExclusion := false
	for _, ex := range dec.Exclusions {
		if ex.InstrumentID == kalshiID {
			foundExclusion = true
			if ex.Reason != ReasonSureLoss {
				t.Errorf("exclusion reason = %s, want %s", ex.Reason, ReasonSureLoss)
			}
			if !strings.Contains(ex.Detail, "net proceeds") && !strings.Contains(ex.Detail, "guaranteed loss") {
				t.Errorf("exclusion detail should mention net proceeds or guaranteed loss: %s", ex.Detail)
			}
		}
	}
	if !foundExclusion {
		t.Errorf("target should be excluded for sure-loss, but exclusions = %+v", dec.Exclusions)
	}
}
