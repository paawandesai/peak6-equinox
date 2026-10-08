package route

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestComplementCheaperThanDirect: complement route wins when it has lower all-in cost.
func TestComplementCheaperThanDirect(t *testing.T) {
	// Target: Kalshi YES @ 0.52
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.52", "100")}, nil)

	// Polymarket: equivalent to target, YES @ 0.51 (more expensive than Kalshi)
	equivPartner := cand(polymarketID, []canonical.Level{lv("0.49", "100")}, []canonical.Level{lv("0.51", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	equivPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	// OtherID: complement to target, NO @ 0.48 (cheaper than Kalshi YES @ 0.52 and Polymarket YES @ 0.51)
	// When buying YES on Kalshi, buying NO on OtherID is the complement route.
	// NO @ 0.48 means we're buying at the complement (1 - YES bid) = 1 - 0.52 = 0.48
	compPartner := cand(otherID, []canonical.Level{lv("0.52", "100")}, []canonical.Level{lv("0.48", "100")},
		rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{equivPartner, compPartner})

	// The complement route should win (NO @ 0.48 on otherID is cheaper)
	if len(dec.Chosen) != 1 {
		t.Fatalf("chosen = %+v, want 1 leg", dec.Chosen)
	}
	chosen := dec.Chosen[0]
	if chosen.InstrumentID != otherID {
		t.Errorf("chosen instrument = %s, want %s (complement)", chosen.InstrumentID, otherID)
	}
	if chosen.Route != RouteComplement {
		t.Errorf("chosen route = %s, want %s", chosen.Route, RouteComplement)
	}
	if chosen.Outcome != canonical.OutcomeNo {
		t.Errorf("chosen outcome = %s, want NO (complement of YES)", chosen.Outcome)
	}
	if !chosen.Quantity.Equal(d("100")) {
		t.Errorf("chosen quantity = %s, want 100", chosen.Quantity)
	}

	// Explanation should mention complement
	if !strings.Contains(dec.Explanation, "complement") {
		t.Errorf("explanation lacks 'complement': %s", dec.Explanation)
	}
	if !strings.Contains(dec.Explanation, strings.ToUpper(string(canonical.OutcomeNo))) {
		t.Errorf("explanation lacks NO outcome: %s", dec.Explanation)
	}
}

// TestDirectCheaperThanComplement: direct route wins when it has lower all-in cost.
func TestDirectCheaperThanComplement(t *testing.T) {
	// Target: Kalshi YES @ 0.50
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.50", "100")}, nil)

	// Polymarket: equivalent, YES @ 0.49 (cheaper than target)
	equivPartner := cand(polymarketID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.49", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	equivPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	// OtherID: complement, NO @ 0.52 (more expensive than Polymarket YES @ 0.49)
	compPartner := cand(otherID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.52", "100")},
		rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{equivPartner, compPartner})

	// The direct route should win (Polymarket YES @ 0.49 is cheaper)
	if len(dec.Chosen) != 1 {
		t.Fatalf("chosen = %+v, want 1 leg", dec.Chosen)
	}
	chosen := dec.Chosen[0]
	if chosen.InstrumentID != polymarketID {
		t.Errorf("chosen instrument = %s, want %s (direct)", chosen.InstrumentID, polymarketID)
	}
	if chosen.Route != RouteDirect {
		t.Errorf("chosen route = %s, want %s", chosen.Route, RouteDirect)
	}
	if chosen.Outcome != canonical.OutcomeYes {
		t.Errorf("chosen outcome = %s, want YES (direct)", chosen.Outcome)
	}

	// Check alternatives: complement should be ranked
	foundComplement := false
	for _, alt := range dec.Alternatives {
		if len(alt.Legs) == 1 && alt.Legs[0].InstrumentID == otherID {
			foundComplement = true
			if alt.Legs[0].Route != RouteComplement {
				t.Errorf("complement alternative route = %s, want %s", alt.Legs[0].Route, RouteComplement)
			}
		}
	}
	if !foundComplement {
		t.Errorf("complement alternative not found in alternatives: %+v", dec.Alternatives)
	}
}

// TestMixEquivalentAndComplement: both equivalent and complement candidates are considered.
func TestMixEquivalentAndComplement(t *testing.T) {
	// Target: Kalshi YES @ 0.52
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.52", "100")}, nil)

	// Polymarket: equivalent, YES @ 0.51
	equivPartner := cand(polymarketID, []canonical.Level{lv("0.49", "100")}, []canonical.Level{lv("0.51", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	equivPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	// OtherID: complement, NO @ 0.50 (cheaper than both target and equiv)
	compPartner := cand(otherID, []canonical.Level{lv("0.50", "100")}, []canonical.Level{lv("0.50", "100")},
		rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{equivPartner, compPartner})

	// All three candidates should be in alternatives (target, equiv, complement)
	if len(dec.Alternatives) < 3 {
		t.Errorf("alternatives = %d, want at least 3 (target, equiv, complement)", len(dec.Alternatives))
	}

	// Complement should win
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != otherID {
		t.Errorf("chosen = %+v, want complement %s", dec.Chosen, otherID)
	}

	// Count route types in alternatives
	directCount := 0
	complementCount := 0
	for _, alt := range dec.Alternatives {
		if len(alt.Legs) == 1 {
			if alt.Legs[0].Route == RouteDirect {
				directCount++
			} else if alt.Legs[0].Route == RouteComplement {
				complementCount++
			}
		}
	}
	if directCount < 2 {
		t.Errorf("direct alternatives = %d, want at least 2 (target + equiv)", directCount)
	}
	if complementCount < 1 {
		t.Errorf("complement alternatives = %d, want at least 1", complementCount)
	}
}

// TestComplementPartialFill: IOC complement route with partial fill.
func TestComplementPartialFill(t *testing.T) {
	// Target: Kalshi YES @ 0.52, 100 available
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.52", "100")}, nil)

	// OtherID: complement, NO @ 0.50, only 60 available (partial)
	// For buying NO, we need YES bids (1-p), so to get NO @ 0.50, we need YES bid @ 0.50
	// The NO ask price is 1 - YES bid, so YES bid @ 0.50 gives NO ask @ 0.50
	compPartner := cand(otherID, []canonical.Level{lv("0.50", "60")}, []canonical.Level{lv("0.55", "100")},
		rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	o := buy("100") // IOC by default
	dec := mustRoute(t, o, target, []canonical.Candidate{compPartner})

	// Greedy sweep should fill: 60 @ complement (cheaper), 40 @ target
	if len(dec.Chosen) != 2 {
		t.Fatalf("chosen = %+v, want 2 legs (sweep)", dec.Chosen)
	}

	// First leg should be complement (60 available, cheaper)
	if dec.Chosen[0].InstrumentID != otherID {
		t.Errorf("leg 0 instrument = %s, want %s (complement)", dec.Chosen[0].InstrumentID, otherID)
	}
	if !dec.Chosen[0].Quantity.Equal(d("60")) {
		t.Errorf("leg 0 quantity = %s, want 60", dec.Chosen[0].Quantity)
	}
	if dec.Chosen[0].Route != RouteComplement {
		t.Errorf("leg 0 route = %s, want %s", dec.Chosen[0].Route, RouteComplement)
	}

	// Second leg should be target (fills remaining 40)
	if !dec.Chosen[1].Quantity.Equal(d("40")) {
		t.Errorf("leg 1 quantity = %s, want 40", dec.Chosen[1].Quantity)
	}
}

// TestComplementWithBasisPenalty: complement route with basis-risk penalty.
func TestComplementWithBasisPenalty(t *testing.T) {
	// Target: Kalshi YES @ 0.55 (expensive)
	target := cand(kalshiID, []canonical.Level{lv("0.45", "100")}, []canonical.Level{lv("0.55", "100")}, nil)

	// OtherID: complement, NO @ 0.45 (cheaper even with basis penalty)
	// For buying NO @ 0.45, we need YES bid @ 0.55, which gives NO ask @ 1-0.55 = 0.45
	compPartner := cand(otherID, []canonical.Level{lv("0.55", "100")}, []canonical.Level{lv("0.60", "100")},
		rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
	// Basis penalty 2.0 × 0.01 = 0.02, so all-in is ~0.45 + fee + 0.02 ≈ 0.49, still cheaper than target 0.55 + fee ≈ 0.57
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("2.0"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{compPartner})

	// Complement should be chosen despite basis penalty
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != otherID {
		t.Fatalf("chosen = %+v, want complement %s", dec.Chosen, otherID)
	}

	// Basis penalty should be applied (2.0 × 0.01 = 0.02)
	expectedBasis := d("0.02")
	if !dec.Chosen[0].BasisPenaltyPerContract.Equal(expectedBasis) {
		t.Errorf("basis penalty = %s, want %s", dec.Chosen[0].BasisPenaltyPerContract, expectedBasis)
	}

	// All-in should include basis penalty
	if dec.Chosen[0].BasisPenaltyPerContract.IsZero() {
		t.Errorf("basis penalty is zero, should be %s", expectedBasis)
	}
}

// TestComplementSweepMultiVenue: complement route in a multi-venue sweep.
func TestComplementSweepMultiVenue(t *testing.T) {
	// Order for 150 contracts
	// Target: Kalshi YES @ 0.52, 100 available
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.52", "100")}, nil)

	// OtherID: complement, NO @ 0.50, 60 available (cheaper, fills first)
	// For buying NO @ 0.50, we need YES bid @ 0.50, which gives NO ask @ 1-0.50 = 0.50
	compPartner := cand(otherID, []canonical.Level{lv("0.50", "60")}, []canonical.Level{lv("0.55", "100")},
		rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("150"), target, []canonical.Candidate{compPartner})

	// Should have 2 legs: 60 @ complement, 90 @ target
	if len(dec.Chosen) != 2 {
		t.Fatalf("chosen = %+v, want 2 legs (sweep)", dec.Chosen)
	}

	// First leg should be complement (cheaper, 60 available)
	if dec.Chosen[0].InstrumentID != otherID || dec.Chosen[0].Route != RouteComplement {
		t.Errorf("leg 0 = %+v, want complement %s", dec.Chosen[0], otherID)
	}
	if !dec.Chosen[0].Quantity.Equal(d("60")) {
		t.Errorf("leg 0 quantity = %s, want 60", dec.Chosen[0].Quantity)
	}

	// Second leg should be target (direct, fills remaining 90)
	if dec.Chosen[1].InstrumentID != kalshiID || dec.Chosen[1].Route != RouteDirect {
		t.Errorf("leg 1 = %+v, want direct %s", dec.Chosen[1], kalshiID)
	}
	if !dec.Chosen[1].Quantity.Equal(d("90")) {
		t.Errorf("leg 1 quantity = %s, want 90", dec.Chosen[1].Quantity)
	}

	// Explanation should mention both routes
	if !strings.Contains(dec.Explanation, "complement") {
		t.Errorf("explanation lacks 'complement': %s", dec.Explanation)
	}
}

// TestComplementPropertyInverse: flipping the order's outcome should produce the inverse route.
func TestComplementPropertyInverse(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random price for the complement NO side
		noPrice := rapid.Float64Range(0.40, 0.60).Draw(rt, "noPrice")
		noPriceStr := fmt.Sprintf("%.4f", noPrice)

		// Target: Kalshi YES
		yesPrice := 1.0 - noPrice
		yesPriceStr := fmt.Sprintf("%.4f", yesPrice)
		target := cand(kalshiID, []canonical.Level{lv(yesPriceStr, "100")}, []canonical.Level{lv(yesPriceStr, "100")}, nil)

		// Complement: OtherID NO (complement of target YES)
		compPartner := cand(otherID, []canonical.Level{lv(noPriceStr, "100")}, []canonical.Level{lv(noPriceStr, "100")},
			rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
		compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

		// Route 1: buy YES on target
		dec1, err := Route(buy("50"), target, []canonical.Candidate{compPartner}, cfg, t0)
		if err != nil {
			rt.Fatalf("Route 1: %v", err)
		}

		// Route 2: buy NO on target (flip outcome)
		// When buying NO on target, the complement is buying YES on compPartner
		// But we need to create the reverse scenario: target is now OtherID, partner is Kalshi
		targetFlip := cand(otherID, []canonical.Level{lv(noPriceStr, "100")}, []canonical.Level{lv(noPriceStr, "100")}, nil)
		compPartnerFlip := cand(kalshiID, []canonical.Level{lv(yesPriceStr, "100")}, []canonical.Level{lv(yesPriceStr, "100")},
			rel(otherID, kalshiID, canonical.RelComplement, "0.98"))
		compPartnerFlip.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

		o2 := canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeNo, Quantity: ptr("50"), Type: canonical.OrderMarket, TIF: canonical.TIFIOC}
		dec2, err := Route(o2, targetFlip, []canonical.Candidate{compPartnerFlip}, cfg, t0)
		if err != nil {
			rt.Fatalf("Route 2: %v", err)
		}

		// Both should have chosen legs
		if len(dec1.Chosen) == 0 || len(dec2.Chosen) == 0 {
			return // skip if no fill
		}

		// Property: the all-in costs should be equal (within cost tolerance 0.0001)
		cost1 := dec1.Chosen[0].AllInPerContract
		cost2 := dec2.Chosen[0].AllInPerContract
		diff := cost1.Sub(cost2).Abs()
		tolerance := d("0.0001")
		if diff.GreaterThan(tolerance) {
			rt.Fatalf("cost mismatch: YES route all-in %s, NO route all-in %s, diff %s > tolerance %s",
				cost1, cost2, diff, tolerance)
		}
	})
}

// TestComplementNoBookExclusion: complement with no NO book is excluded.
func TestComplementNoBookExclusion(t *testing.T) {
	// Target: Kalshi YES @ 0.52
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.52", "100")}, nil)

	// OtherID: complement, but no NO book (no YES bids to convert to NO asks for buying)
	// For buying NO, we need YES bids. If there are no YES bids, there's no NO book for buying.
	compPartner := cand(otherID, []canonical.Level{}, []canonical.Level{lv("0.50", "100")},
		rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	dec := mustRoute(t, buy("100"), target, []canonical.Candidate{compPartner})

	// Complement should be excluded (no book or insufficient depth for NO side)
	foundExclusion := false
	for _, ex := range dec.Exclusions {
		if ex.InstrumentID == otherID && (ex.Reason == ReasonNoBook || ex.Reason == ReasonInsufficientDepth) {
			foundExclusion = true
		}
	}
	if !foundExclusion {
		t.Errorf("complement not excluded; exclusions = %+v", dec.Exclusions)
	}

	// Target should be chosen
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != kalshiID {
		t.Errorf("chosen = %+v, want target %s", dec.Chosen, kalshiID)
	}
}

// TestComplementFOKFullFill: FOK fills completely using complement route.
func TestComplementFOKFullFill(t *testing.T) {
	// Target: Kalshi YES @ 0.52, 100 available
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.52", "100")}, nil)

	// OtherID: complement, NO @ 0.50, 120 available (can fill FOK)
	compPartner := cand(otherID, []canonical.Level{lv("0.50", "120")}, []canonical.Level{lv("0.50", "120")},
		rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	o := buy("100")
	o.TIF = canonical.TIFFOK
	dec := mustRoute(t, o, target, []canonical.Candidate{compPartner})

	// Complement should fill completely
	if len(dec.Chosen) != 1 {
		t.Fatalf("chosen = %+v, want 1 leg (FOK)", dec.Chosen)
	}
	if !dec.Chosen[0].Quantity.Equal(d("100")) {
		t.Errorf("chosen quantity = %s, want 100 (FOK)", dec.Chosen[0].Quantity)
	}
	if dec.Chosen[0].Route != RouteComplement {
		t.Errorf("chosen route = %s, want %s", dec.Chosen[0].Route, RouteComplement)
	}
}

// TestComplementFOKInsufficientDepth: FOK rejects complement when insufficient liquidity.
func TestComplementFOKInsufficientDepth(t *testing.T) {
	// Target: Kalshi YES @ 0.52, only 50 available (not enough for FOK)
	target := cand(kalshiID, []canonical.Level{lv("0.48", "50")}, []canonical.Level{lv("0.52", "50")}, nil)

	// OtherID: complement, NO @ 0.50, only 60 available (cheaper but also not enough alone)
	// Total: 50 + 60 = 110, but FOK needs 100 from a single venue (greedy fills to 110 across both)
	// Actually, greedy sweep fills across venues, so 50+60=110 > 100 should succeed.
	// To test FOK rejection, we need combined liquidity < 100
	// Let's use 50 + 40 = 90 < 100
	compPartner := cand(otherID, []canonical.Level{lv("0.50", "40")}, []canonical.Level{lv("0.55", "40")},
		rel(kalshiID, otherID, canonical.RelComplement, "0.98"))
	compPartner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	o := buy("100")
	o.TIF = canonical.TIFFOK
	dec := mustRoute(t, o, target, []canonical.Candidate{compPartner})

	// FOK should reject partial (no chosen) because total liquidity 50+40=90 < 100
	if len(dec.Chosen) != 0 {
		t.Errorf("FOK partial should be rejected, got chosen = %+v", dec.Chosen)
	}

	// Both candidates should be excluded for insufficient depth
	if len(dec.Exclusions) != 2 {
		t.Errorf("FOK should exclude both candidates, got %d exclusions: %+v", len(dec.Exclusions), dec.Exclusions)
	}
}
