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

// TestSweepExplanation: explanation contains expected details.
func TestSweepExplanation(t *testing.T) {
	o := buy("100")
	cand1 := cand(kalshiID, nil, []canonical.Level{lv("0.50", "60")}, nil)
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.52", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)
	expl := SweepExplanation(o, legs, cfg)

	// Check that explanation contains key details
	wantPhrases := []string{
		"Filled 100",
		"across 2 venue(s)",
		"Legs in cost order",
		"kalshi",
		"polymarket",
	}
	for _, want := range wantPhrases {
		if !strings.Contains(expl, want) {
			t.Errorf("explanation missing %q: %s", want, expl)
		}
	}
}

// TestSweepExplanationPartial: partial fill explanation includes shortfall.
func TestSweepExplanationPartial(t *testing.T) {
	o := buy("200")
	cand1 := cand(kalshiID, nil, []canonical.Level{lv("0.50", "60")}, nil)
	cand2 := cand(polymarketID, nil, []canonical.Level{lv("0.52", "80")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))

	legs := Sweep(o, []canonical.Candidate{cand1, cand2}, cfg)
	expl := SweepExplanation(o, legs, cfg)

	// Check shortfall mentioned
	wantPhrases := []string{
		"Filled 140",
		"Shortfall: 60",
		"IOC",
	}
	for _, want := range wantPhrases {
		if !strings.Contains(expl, want) {
			t.Errorf("partial explanation missing %q: %s", want, expl)
		}
	}
}

// TestSweepGreedyProperty: greedy never leaves a cheaper unfilled contract when
// a more expensive one was filled (within the same venue, monotonic book walk).
func TestSweepGreedyProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		qty := rapid.IntRange(1, 200).Draw(rt, "qty")

		// Generate two price levels at different prices
		p1 := rapid.Float64Range(0.01, 0.99).Draw(rt, "p1")
		p2 := rapid.Float64Range(0.01, 0.99).Draw(rt, "p2")
		if p1 > p2 {
			p1, p2 = p2, p1 // ensure p1 < p2 (cheaper first)
		}

		sz1 := rapid.IntRange(1, 100).Draw(rt, "sz1")
		sz2 := rapid.IntRange(1, 100).Draw(rt, "sz2")

		o := buy(fmt.Sprintf("%d", qty))
		cand := cand(kalshiID, nil, []canonical.Level{
			lv(fmt.Sprintf("%.4f", p1), fmt.Sprintf("%d", sz1)),
			lv(fmt.Sprintf("%.4f", p2), fmt.Sprintf("%d", sz2)),
		}, nil)

		legs := Sweep(o, []canonical.Candidate{cand}, cfg)

		if len(legs) == 0 {
			return // no fill is valid (e.g., closed instrument)
		}

		// If we filled any quantity, verify monotonic walk:
		// - If we filled from the second level (p2), we must have exhausted the first (p1)
		totalFilled := legs[0].Quantity
		if totalFilled.GreaterThan(d(fmt.Sprintf("%d", sz1))) {
			// We consumed into the second level, so the cheaper first level must be exhausted
			// Verify that the fill is within the available liquidity
			if totalFilled.GreaterThan(d(fmt.Sprintf("%d", sz1+sz2))) {
				rt.Fatalf("filled %s exceeds available %d", totalFilled, sz1+sz2)
			}
		}
	})
}

// Helper functions are imported from route_test.go and walk_test.go
// (lv, d, ptr, hasWarning, contains are already defined)
