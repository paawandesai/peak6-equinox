package route

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestSweepSingleVenue: single venue fills the entire order.
func TestSweepSingleVenue(t *testing.T) {
	o := buy("100")
	cand1 := cand(kalshiID, []canonical.Level{lv("0.48", "200")}, []canonical.Level{lv("0.52", "150")}, nil)
	legs := Sweep(o, []canonical.Candidate{cand1}, cfg)

	if len(legs) != 1 {
		t.Fatalf("expected 1 leg, got %d: %+v", len(legs), legs)
	}
	if !legs[0].Quantity.Equal(d("100")) {
		t.Errorf("leg quantity = %s, want 100", legs[0].Quantity)
	}
	if legs[0].InstrumentID != kalshiID {
		t.Errorf("leg instrument = %s, want %s", legs[0].InstrumentID, kalshiID)
	}
}

// TestSweepTwoVenueSplit: venue A fills 60%, venue B fills 40%.
func TestSweepTwoVenueSplit(t *testing.T) {
	o := buy("100")
	// Kalshi: cheaper all-in, but only has 60 contracts
	cand1 := cand(kalshiID, nil, []canonical.Level{lv("0.50", "60")}, nil)
	// Polymarket: more expensive, but has 100 contracts
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.52", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)

	if len(legs) != 2 {
		t.Fatalf("expected 2 legs, got %d: %+v", len(legs), legs)
	}

	// First leg should be Kalshi (cheaper, fills 60)
	if legs[0].InstrumentID != kalshiID || !legs[0].Quantity.Equal(d("60")) {
		t.Errorf("leg 0 = %+v, want Kalshi 60", legs[0])
	}

	// Second leg should be Polymarket (fills remaining 40)
	if legs[1].InstrumentID != polymarketID || !legs[1].Quantity.Equal(d("40")) {
		t.Errorf("leg 1 = %+v, want Polymarket 40", legs[1])
	}

	totalQty := legs[0].Quantity.Add(legs[1].Quantity)
	if !totalQty.Equal(d("100")) {
		t.Errorf("total filled = %s, want 100", totalQty)
	}
}

// TestSweepIOCPartial: IOC order larger than available liquidity, partial fill succeeds.
func TestSweepIOCPartial(t *testing.T) {
	o := buy("200") // IOC by default
	// Kalshi has 60, Polymarket has 80 → total 140, short of 200
	cand1 := cand(kalshiID, nil, []canonical.Level{lv("0.50", "60")}, nil)
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.52", "80")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)

	if len(legs) != 2 {
		t.Fatalf("IOC partial: expected 2 legs, got %d: %+v", len(legs), legs)
	}

	totalQty := legs[0].Quantity.Add(legs[1].Quantity)
	if !totalQty.Equal(d("140")) {
		t.Errorf("IOC partial: total filled = %s, want 140", totalQty)
	}

	// Shortfall is 60 (200 - 140)
	shortfall := d("200").Sub(totalQty)
	if !shortfall.Equal(d("60")) {
		t.Errorf("IOC partial: shortfall = %s, want 60", shortfall)
	}
}

// TestSweepFOKPartialRejected: FOK order with insufficient liquidity returns no fill.
func TestSweepFOKPartialRejected(t *testing.T) {
	o := fok(buy("200"))
	// Total liquidity: 140, but FOK needs all 200
	cand1 := cand(kalshiID, nil, []canonical.Level{lv("0.50", "60")}, nil)
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.52", "80")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)

	if len(legs) != 0 {
		t.Errorf("FOK partial: expected no fill, got %d legs: %+v", len(legs), legs)
	}
}

// TestSweepFOKFullFill: FOK order with sufficient liquidity fills completely.
func TestSweepFOKFullFill(t *testing.T) {
	o := fok(buy("100"))
	cand1 := cand(kalshiID, nil, []canonical.Level{lv("0.50", "60")}, nil)
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.52", "80")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)

	if len(legs) != 2 {
		t.Fatalf("FOK full: expected 2 legs, got %d: %+v", len(legs), legs)
	}

	totalQty := legs[0].Quantity.Add(legs[1].Quantity)
	if !totalQty.Equal(d("100")) {
		t.Errorf("FOK full: total filled = %s, want 100", totalQty)
	}
}

// TestSweepAllIneligible: all candidates excluded returns empty slice.
func TestSweepAllIneligible(t *testing.T) {
	o := buy("100")
	// Both instruments are closed (ineligible)
	cand1 := cand(kalshiID, nil, []canonical.Level{lv("0.50", "100")}, nil)
	cand1.Instrument.Status = canonical.StatusClosed
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.52", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	cand2.Instrument.Status = canonical.StatusSettled

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)

	if len(legs) != 0 {
		t.Errorf("all ineligible: expected no fill, got %d legs: %+v", len(legs), legs)
	}
}

// TestSweepNoCandidates: no candidates returns empty slice.
func TestSweepNoCandidates(t *testing.T) {
	o := buy("100")
	legs := Sweep(o, []canonical.Candidate{}, cfg)

	if legs != nil {
		t.Errorf("no candidates: expected nil, got %+v", legs)
	}
}

// TestSweepRanksByCost: cheaper venue fills first even if it has less depth.
func TestSweepRanksByCost(t *testing.T) {
	o := buy("100")
	// Kalshi: cheaper (0.50), but only 30 contracts
	cand1 := cand(kalshiID, nil, []canonical.Level{lv("0.50", "30")}, nil)
	// Polymarket: more expensive (0.52), has 100 contracts
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.52", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)

	if len(legs) != 2 {
		t.Fatalf("expected 2 legs, got %d: %+v", len(legs), legs)
	}

	// First leg must be cheaper Kalshi
	if legs[0].InstrumentID != kalshiID {
		t.Errorf("leg 0 instrument = %s, want %s (cheaper venue first)", legs[0].InstrumentID, kalshiID)
	}
	if !legs[0].Quantity.Equal(d("30")) {
		t.Errorf("leg 0 quantity = %s, want 30", legs[0].Quantity)
	}

	// Second leg fills remaining from Polymarket
	if legs[1].InstrumentID != polymarketID {
		t.Errorf("leg 1 instrument = %s, want %s", legs[1].InstrumentID, polymarketID)
	}
	if !legs[1].Quantity.Equal(d("70")) {
		t.Errorf("leg 1 quantity = %s, want 70", legs[1].Quantity)
	}
}

// TestSweepBookWalkMultipleLevels: sweep correctly walks multiple price levels per venue.
func TestSweepBookWalkMultipleLevels(t *testing.T) {
	o := buy("150")
	// Kalshi: 50 @ 0.50, 30 @ 0.51, 40 @ 0.52
	cand1 := cand(kalshiID, nil, []canonical.Level{
		lv("0.50", "50"),
		lv("0.51", "30"),
		lv("0.52", "40"),
	}, nil)
	// Polymarket: 100 @ 0.53
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.53", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)

	if len(legs) != 2 {
		t.Fatalf("expected 2 legs, got %d: %+v", len(legs), legs)
	}

	// Kalshi should fill 120 contracts across 3 levels
	if !legs[0].Quantity.Equal(d("120")) {
		t.Errorf("leg 0 quantity = %s, want 120", legs[0].Quantity)
	}

	// VWAP should be (50*0.50 + 30*0.51 + 40*0.52) / 120
	expectedVWAP := d("0.50").Mul(d("50")).
		Add(d("0.51").Mul(d("30"))).
		Add(d("0.52").Mul(d("40"))).
		Div(d("120"))
	if !legs[0].VWAP.Equal(expectedVWAP) {
		t.Errorf("leg 0 VWAP = %s, want %s", legs[0].VWAP, expectedVWAP)
	}

	// Polymarket fills remaining 30
	if !legs[1].Quantity.Equal(d("30")) {
		t.Errorf("leg 1 quantity = %s, want 30", legs[1].Quantity)
	}
}

// TestSweepWithLimit: limit order respects limit price across venues.
func TestSweepWithLimit(t *testing.T) {
	o := limit(buy("100"), "0.515")
	// Kalshi: 60 @ 0.51 (within limit)
	cand1 := cand(kalshiID, nil, []canonical.Level{lv("0.51", "60")}, nil)
	// Polymarket: 100 @ 0.52 (outside limit)
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.52", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)

	// Only Kalshi fills (60 contracts), Polymarket excluded by limit
	if len(legs) != 1 {
		t.Fatalf("expected 1 leg, got %d: %+v", len(legs), legs)
	}
	if !legs[0].Quantity.Equal(d("60")) {
		t.Errorf("leg quantity = %s, want 60", legs[0].Quantity)
	}
}

// TestRouteIOCPartialSweep: IOC partial sweep across venues (Route-level).
func TestRouteIOCPartialSweep(t *testing.T) {
	// 200 order, 60 @ kalshi, 80 @ polymarket = 140 total
	target := cand(kalshiID, nil, []canonical.Level{lv("0.50", "60")}, nil)
	partner := cand(polymarketID, nil, []canonical.Level{lv("0.52", "80")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	partner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	dec, err := Route(buy("200"), target, []canonical.Candidate{partner}, cfg, t0)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	// Should have 2 legs (greedy sweep)
	if len(dec.Chosen) != 2 {
		t.Fatalf("chosen = %+v (want 2 legs)", dec.Chosen)
	}

	totalFilled := dec.Chosen[0].Quantity.Add(dec.Chosen[1].Quantity)
	if !totalFilled.Equal(d("140")) {
		t.Errorf("total filled = %s, want 140", totalFilled)
	}

	// Check warnings include partial fill
	foundPartial := false
	for _, w := range dec.Warnings {
		if strings.Contains(w, "fills 140 of 200") {
			foundPartial = true
		}
	}
	if !foundPartial {
		t.Errorf("warnings missing partial fill: %v", dec.Warnings)
	}

	// Finding #5: Check explainSweep includes total cost, "filled N of M", and shortfall
	if !strings.Contains(dec.Explanation, "140 of 200") {
		t.Errorf("explanation missing 'filled N of M': %s", dec.Explanation)
	}
	if !strings.Contains(dec.Explanation, "total cost") {
		t.Errorf("explanation missing 'total cost': %s", dec.Explanation)
	}
	if !strings.Contains(dec.Explanation, "Shortfall: 60") {
		t.Errorf("explanation missing shortfall: %s", dec.Explanation)
	}
}

// TestRouteFOKInsufficientDepth: FOK rejects partial with empty Chosen.
func TestRouteFOKInsufficientDepth(t *testing.T) {
	// 200 order FOK, but only 140 available total
	target := cand(kalshiID, nil, []canonical.Level{lv("0.50", "60")}, nil)
	partner := cand(polymarketID, nil, []canonical.Level{lv("0.52", "80")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	partner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	o := buy("200")
	o.TIF = canonical.TIFFOK
	dec, err := Route(o, target, []canonical.Candidate{partner}, cfg, t0)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	// FOK should reject partial (no chosen)
	if len(dec.Chosen) != 0 {
		t.Errorf("FOK partial should be rejected, got chosen = %+v", dec.Chosen)
	}

	// Finding #5: FOK should have exclusions with ReasonInsufficientDepth
	if len(dec.Exclusions) != 2 {
		t.Errorf("FOK should exclude both candidates, got %d exclusions: %+v", len(dec.Exclusions), dec.Exclusions)
	}
	for _, ex := range dec.Exclusions {
		if ex.Reason != ReasonInsufficientDepth {
			t.Errorf("exclusion reason = %s, want %s: %+v", ex.Reason, ReasonInsufficientDepth, ex)
		}
		if !strings.Contains(ex.Detail, "FOK requires 200") {
			t.Errorf("exclusion detail missing 'FOK requires 200': %s", ex.Detail)
		}
	}
}

// TestRouteFOKMultiVenueFullFill: FOK fills completely from multiple venues.
// Round 3 #1: verify chosen instruments are not in Exclusions.
func TestRouteFOKMultiVenueFullFill(t *testing.T) {
	// FOK 100: kalshi 60, polymarket 80 -> fills 60+40=100 from both venues
	target := cand(kalshiID, nil, []canonical.Level{lv("0.50", "60")}, nil)
	partner := cand(polymarketID, nil, []canonical.Level{lv("0.52", "80")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	partner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	o := buy("100")
	o.TIF = canonical.TIFFOK
	dec, err := Route(o, target, []canonical.Candidate{partner}, cfg, t0)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	// FOK should fill completely from both venues
	if len(dec.Chosen) != 2 {
		t.Fatalf("FOK multi-venue fill should have 2 legs, got %d: %+v", len(dec.Chosen), dec.Chosen)
	}

	totalFilled := dec.Chosen[0].Quantity.Add(dec.Chosen[1].Quantity)
	if !totalFilled.Equal(d("100")) {
		t.Errorf("total filled = %s, want 100", totalFilled)
	}

	// Round 3 #1: verify no chosen instrument is in Exclusions
	chosenIDs := make(map[string]bool)
	for _, leg := range dec.Chosen {
		chosenIDs[leg.InstrumentID] = true
	}
	for _, ex := range dec.Exclusions {
		if chosenIDs[ex.InstrumentID] {
			t.Errorf("chosen instrument %s appears in Exclusions: %+v", ex.InstrumentID, ex)
		}
	}
}

// TestRouteSellSideGreedySweep: sell-side greedy sweep (M3-03 #7: sell test).
func TestRouteSellSideGreedySweep(t *testing.T) {
	// Sell 100: kalshi bids 0.48 (100), polymarket bids 0.49 (80)
	// Greedy: sell to highest bidder first (0.49, then 0.48)
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, nil, nil)
	partner := cand(polymarketID, []canonical.Level{lv("0.49", "80")}, nil,
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	partner.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}

	o := canonical.Order{Side: canonical.SideSell, Outcome: canonical.OutcomeYes,
		Quantity: ptr("100"), Type: canonical.OrderMarket, TIF: canonical.TIFIOC}
	dec, err := Route(o, target, []canonical.Candidate{partner}, cfg, t0)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	// Should have 2 legs: 80 @ polymarket (0.49), 20 @ kalshi (0.48)
	if len(dec.Chosen) != 2 {
		t.Fatalf("sell sweep should have 2 legs, got %d: %+v", len(dec.Chosen), dec.Chosen)
	}

	// First leg should be polymarket (higher bid)
	if dec.Chosen[0].InstrumentID != polymarketID || !dec.Chosen[0].Quantity.Equal(d("80")) {
		t.Errorf("first leg = %+v, want polymarket 80", dec.Chosen[0])
	}

	// Second leg should be kalshi (remaining 20)
	if dec.Chosen[1].InstrumentID != kalshiID || !dec.Chosen[1].Quantity.Equal(d("20")) {
		t.Errorf("second leg = %+v, want kalshi 20", dec.Chosen[1])
	}

	// Finding #5: Verify cross-venue cheapest-first ordering (sell: highest proceeds first)
	firstProceeds := dec.Chosen[0].AllInPerContract
	secondProceeds := dec.Chosen[1].AllInPerContract
	if firstProceeds.LessThan(secondProceeds) {
		t.Errorf("sell-side ordering: first leg proceeds %s < second leg proceeds %s (should sell to highest bidder first)",
			firstProceeds, secondProceeds)
	}
}

// TestSweepGreedyProperty: greedy never leaves a cheaper unfilled contract when
// a more expensive one was filled (M3-03 #7: non-vacuous property test).
// M3-04: fixed flaky test when random prices round to same 4-decimal value.
func TestSweepGreedyProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		qty := rapid.IntRange(1, 200).Draw(rt, "qty")

		// Generate two price levels at different prices (after rounding to 4 dp)
		p1 := rapid.Float64Range(0.01, 0.99).Draw(rt, "p1")
		p2 := rapid.Float64Range(0.01, 0.99).Draw(rt, "p2")

		// Format to 4 dp immediately to check for ties
		p1Str := fmt.Sprintf("%.4f", p1)
		p2Str := fmt.Sprintf("%.4f", p2)

		// If they round to the same value, skip this iteration
		if p1Str == p2Str {
			rt.Skip("prices round to same 4-decimal value")
		}

		// Ensure p1 < p2 after formatting
		p1Dec := d(p1Str)
		p2Dec := d(p2Str)
		if p1Dec.GreaterThanOrEqual(p2Dec) {
			p1Str, p2Str = p2Str, p1Str
			p1Dec = d(p1Str) // Reload after swap
		}

		sz1 := rapid.IntRange(1, 100).Draw(rt, "sz1")
		sz2 := rapid.IntRange(1, 100).Draw(rt, "sz2")

		o := buy(fmt.Sprintf("%d", qty))
		cand := cand(kalshiID, nil, []canonical.Level{
			lv(p1Str, fmt.Sprintf("%d", sz1)),
			lv(p2Str, fmt.Sprintf("%d", sz2)),
		}, nil)

		legs := Sweep(o, []canonical.Candidate{cand}, cfg)

		if len(legs) == 0 {
			return // no fill
		}

		// Property: if filled > sz1, VWAP must reflect both levels
		totalFilled := legs[0].Quantity
		availableAtP1 := d(fmt.Sprintf("%d", sz1))
		if totalFilled.GreaterThan(availableAtP1) {
			vwap := legs[0].VWAP
			// VWAP must be > p1 (consumed into more expensive p2)
			if vwap.LessThanOrEqual(p1Dec) {
				rt.Fatalf("filled %s (> %s), but VWAP %s <= p1 %s",
					totalFilled, availableAtP1, vwap, p1Str)
			}
		}

		// Finding #5: Cross-venue cheapest-first ordering is tested in TestRouteSellSideGreedySweep
	})
}
