package route

import (
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
// explanation contains the exclusion, and inputs show BestAsk=0.999 (re-priced),
// enabling correct slippage calculation (≈ 0 bps, not old bug's 9980 bps).
func TestRegressionRound735Decision71(t *testing.T) {
	// Scenario matching issue #73: buy YES on target, complement offers NO
	// at a price that costs >= $1 all-in. Live decision #71 had all-in 1.0019,
	// quoted 0.0010 (YES ask on complement), slippage showed '(9980) bps'.
	//
	// After fix: inputs store re-priced NO levels, slippage compares like to like.

	// Target: direct YES @ 0.98
	target := cand(kalshiID, []canonical.Level{lv("0.97", "100")}, []canonical.Level{lv("0.98", "100")}, nil)

	// Complement: YES bid @ 0.001 → NO ask @ 1 - 0.001 = 0.999
	// With basis penalty (0.213 × 0.01 = 0.00213), all-in ≈ 0.999 + 0.00007 + 0.00213 = 1.0012
	// (closer to live 1.0019; Polymarket fee: 100 × 0.07 × 0.999 × 0.001 ≈ 0.007 → 0.00007/contract)
	compPartner := cand(otherID,
		[]canonical.Level{lv("0.001", "100")}, // YES bids (used for buying NO)
		[]canonical.Level{lv("0.999", "100")}, // YES asks (quoted ref for old slippage bug)
		rel(kalshiID, otherID, canonical.RelComplement, "0.95"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0.213"), Components: []canonical.BasisComponent{}}

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
	var exDetail string
	for _, ex := range dec.Exclusions {
		if ex.InstrumentID == otherID {
			foundExclusion = true
			exDetail = ex.Detail
			if ex.Reason != ReasonSureLoss {
				t.Errorf("exclusion reason = %s, want %s", ex.Reason, ReasonSureLoss)
			}
		}
	}
	if !foundExclusion {
		t.Errorf("complement should be excluded, but exclusions = %+v", dec.Exclusions)
	}

	// Pin the corrected input snapshot: BestAsk for NO should be re-priced (Cursor round 2 #1)
	// YES bid 0.001 → NO ask 1 - 0.001 = 0.999
	compInput := findInput(t, dec.Inputs, otherID)
	if compInput.OutcomeSide != canonical.OutcomeNo {
		t.Errorf("complement input outcome = %s, want NO", compInput.OutcomeSide)
	}
	if !compInput.BestAsk.Equal(d("0.999")) {
		t.Errorf("complement input BestAsk = %s, want 0.999 (1 - 0.001, re-priced)", compInput.BestAsk)
	}

	// Pin slippage calculation fix (Cursor round 3 #1): if complement were viable,
	// handlers_route.go would compute slippage = (VWAP - BestAsk) × 10000
	// = (0.999 - 0.999) × 10000 = 0 bps (not old bug's 9980 bps from 0.999 vs 0.001)
	// Since complement is excluded, verify via a separate viable complement scenario below.

	// Pin explanation (Cursor round 3 #1): must contain exclusion with sure_loss detail
	if !strings.Contains(dec.Explanation, "Excluded:") {
		t.Errorf("explanation should contain 'Excluded:', got: %s", dec.Explanation)
	}
	if !strings.Contains(dec.Explanation, otherID) {
		t.Errorf("explanation should mention excluded instrument %s, got: %s", otherID, dec.Explanation)
	}
	if !strings.Contains(dec.Explanation, "sure_loss") {
		t.Errorf("explanation should mention sure_loss reason, got: %s", dec.Explanation)
	}
	// Detail should mention the all-in cost
	if !strings.Contains(exDetail, "$1 payout") && !strings.Contains(exDetail, "guaranteed loss") {
		t.Errorf("exclusion detail should mention $1 payout or guaranteed loss: %s", exDetail)
	}
}

// TestComplementSlippageCalculation: verify that after the buildInput fix,
// slippage for complement routes is calculated correctly (VWAP vs re-priced BestAsk).
// This shows handlers_route.go's formula (VWAP - BestAsk) × 10000 now gets correct inputs.
func TestComplementSlippageCalculation(t *testing.T) {
	// Scenario: buy YES on target, complement offers cheaper NO route
	// Target: Kalshi YES @ 0.54 (all-in 0.5574)
	target := cand("kalshi:A", []canonical.Level{lv("0.46", "100")}, []canonical.Level{lv("0.54", "100")}, nil)

	// Complement: Polymarket YES bid @ 0.52 → NO ask @ 1 - 0.52 = 0.48
	// All-in: 0.48 + fee 0.017472 + basis 0.003 = 0.500472 (cheaper than target)
	compPartner := cand("polymarket:B",
		[]canonical.Level{lv("0.52", "100")}, // YES bids (for buying NO)
		[]canonical.Level{lv("0.48", "100")}, // YES asks
		rel("kalshi:A", "polymarket:B", canonical.RelComplement, "0.95"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0.3"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{compPartner})

	// Complement should be chosen (cheaper)
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != "polymarket:B" {
		t.Fatalf("chosen = %+v, want complement polymarket:B", dec.Chosen)
	}

	// Verify input snapshot has re-priced NO levels
	compInput := findInput(t, dec.Inputs, "polymarket:B")
	if compInput.OutcomeSide != canonical.OutcomeNo {
		t.Errorf("complement input outcome = %s, want NO", compInput.OutcomeSide)
	}
	// YES bid 0.52 → NO ask 1 - 0.52 = 0.48
	if !compInput.BestAsk.Equal(d("0.48")) {
		t.Errorf("complement input BestAsk = %s, want 0.48 (1 - 0.52, re-priced)", compInput.BestAsk)
	}

	// Slippage calculation (if handlers_route.go were invoked with this decision):
	// VWAP 0.48 - BestAsk 0.48 = 0, so slippage = 0 × 10000 = 0 bps ✓
	// Old bug would have: VWAP 0.48 - raw YES bid 0.52 = -0.04, slippage = -400 bps ✗
	//
	// Since we're in the route package (no web handlers), verify the inputs are correct:
	chosen := dec.Chosen[0]
	slippageBps := chosen.VWAP.Sub(compInput.BestAsk).Mul(decimal.NewFromInt(10000))
	if slippageBps.Abs().GreaterThan(d("1")) { // Allow ±1 bps rounding
		t.Errorf("slippage = %s bps (VWAP %s - BestAsk %s), want ≈ 0 bps",
			slippageBps, chosen.VWAP, compInput.BestAsk)
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
