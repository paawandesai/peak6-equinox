package route

import (
	"encoding/json"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestDocsExample1SingleVenueFill is the fixture test for docs/routing.md Example 1.
// It asserts the exact numbers and explanation text shown in the documentation.
func TestDocsExample1SingleVenueFill(t *testing.T) {
	// Scenario: Buy 100 YES on Kalshi target, Polymarket partner is cheaper.
	// Kalshi: 100 @ 0.52, Polymarket: 50 @ 0.50 + 50 @ 0.53 (VWAP 0.515)
	target, partners := scenario()
	dec := mustRoute(t, buy("100"), target, partners)

	// Chosen: Polymarket (cheaper all-in)
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID {
		t.Fatalf("chosen = %+v, want polymarket", dec.Chosen)
	}
	leg := dec.Chosen[0]
	// VWAP: (50×0.50 + 50×0.53) / 100 = 0.515
	if !leg.VWAP.Equal(d("0.515")) {
		t.Errorf("VWAP = %s, want 0.515", leg.VWAP)
	}
	// Fee: 50 × 0.07 × 0.50 × 0.50 = 0.875; 50 × 0.07 × 0.53 × 0.47 = 0.87185 → 1.74685 / 100
	if !leg.FeePerContract.Equal(d("0.0174685")) {
		t.Errorf("FeePerContract = %s, want 0.0174685", leg.FeePerContract)
	}
	// All-in: 0.515 + 0.0174685 + 0 = 0.5324685
	if !leg.AllInPerContract.Equal(d("0.5324685")) {
		t.Errorf("AllInPerContract = %s, want 0.5324685", leg.AllInPerContract)
	}

	// Alternatives: rank 1 (Polymarket chosen), rank 2 (Kalshi)
	if len(dec.Alternatives) != 2 {
		t.Fatalf("alternatives = %d, want 2", len(dec.Alternatives))
	}
	if dec.Alternatives[0].Rank != 1 || dec.Alternatives[0].Note != NoteChosen {
		t.Errorf("alternative[0] = %+v, want rank 1 chosen", dec.Alternatives[0])
	}
	second := dec.Alternatives[1]
	if second.Rank != 2 || second.Legs[0].InstrumentID != kalshiID {
		t.Errorf("alternative[1] = %+v, want rank 2 kalshi", second)
	}
	// Kalshi: 100 × 0.07 × 0.52 × 0.48 = 1.7472 → ceil → 1.75 → 0.0175/contract
	if !second.AllInPerContract.Equal(d("0.5375")) {
		t.Errorf("alternative[1] all-in = %s, want 0.5375", second.AllInPerContract)
	}

	// Explanation: verify key phrases
	for _, want := range []string{
		"Best route: polymarket",
		"fills all 100 contracts",
		"VWAP of 0.5150",
		"fee of 0.0175",
		"all-in cost of 0.5325",
		"kalshi (kalshi:KXBTCD-26OCT0617-T120000) ranks #2 at 0.5375",
		"0.0050 worse",
		"Fees are verified for polymarket but approximate for kalshi",
		"Basis-risk penalty is applied as score × 0.01",
		"partners below a relationship confidence of 0.9 are excluded",
	} {
		if !strings.Contains(dec.Explanation, want) {
			t.Errorf("explanation missing %q\nGot: %s", want, dec.Explanation)
		}
	}

	// Warnings: Kalshi is unverified in scenario()
	if !hasWarning(dec, "fees: approximate for kalshi") {
		t.Errorf("warnings = %v, want 'fees: approximate for kalshi'", dec.Warnings)
	}

	t.Logf("Example 1 Decision:\n%s", dec.Explanation)
	t.Logf("Warnings: %v", dec.Warnings)
}

// TestDocsExample2TwoVenueSweep is the fixture test for docs/routing.md Example 2.
// It asserts the exact numbers and explanation text for a greedy sweep.
func TestDocsExample2TwoVenueSweep(t *testing.T) {
	// Scenario: Buy 500 YES (more than any single book can fill)
	// Polymarket: 150 total (50 @ 0.50, 100 @ 0.53)
	// Kalshi: 100 @ 0.52
	target, partners := scenario()
	dec := mustRoute(t, buy("500"), target, partners)

	// Chosen: multi-venue fill (250 total)
	if len(dec.Chosen) != 2 {
		t.Fatalf("chosen legs = %d, want 2 (multi-venue)", len(dec.Chosen))
	}

	// Leg 1: Polymarket 150 contracts
	leg1 := dec.Chosen[0]
	if leg1.InstrumentID != polymarketID || !leg1.Quantity.Equal(d("150")) {
		t.Errorf("leg[0] = %+v, want polymarket 150", leg1)
	}
	// Polymarket 150: 50 @ 0.50 + 100 @ 0.53 → VWAP 0.52
	if !leg1.VWAP.Equal(d("0.52")) {
		t.Errorf("leg[0] VWAP = %s, want 0.52", leg1.VWAP)
	}
	// Fee: 50 × 0.07 × 0.50 × 0.50 = 0.875; 100 × 0.07 × 0.53 × 0.47 = 3.4874 → 4.3624 / 150
	// But wait, the reviewer said 0.017458, let me check: 100×0.07×0.53×0.47 = 1.7437 per 100, not per level!
	// Actually for the second level we have 100 contracts at 0.53, so fee is 100 × 0.07 × 0.53 × (1-0.53) = 100 × 0.07 × 0.53 × 0.47 = 1.7437
	// Total fee: 0.875 + 1.7437 = 2.6187 for 150 contracts → 0.017458/contract
	if !leg1.FeePerContract.Equal(d("0.017458")) {
		t.Errorf("leg[0] FeePerContract = %s, want 0.017458", leg1.FeePerContract)
	}
	// All-in: 0.52 + 0.017458 + 0 = 0.537458
	if !leg1.AllInPerContract.Equal(d("0.537458")) {
		t.Errorf("leg[0] AllInPerContract = %s, want 0.537458", leg1.AllInPerContract)
	}

	// Leg 2: Kalshi 100 contracts
	leg2 := dec.Chosen[1]
	if leg2.InstrumentID != kalshiID || !leg2.Quantity.Equal(d("100")) {
		t.Errorf("leg[1] = %+v, want kalshi 100", leg2)
	}
	// Kalshi: 100 @ 0.52, fee 1.75 → 0.0175/contract, all-in 0.5375
	if !leg2.AllInPerContract.Equal(d("0.5375")) {
		t.Errorf("leg[1] AllInPerContract = %s, want 0.5375", leg2.AllInPerContract)
	}

	// Total: 250 contracts, average all-in
	totalQty := leg1.Quantity.Add(leg2.Quantity)
	if !totalQty.Equal(d("250")) {
		t.Errorf("total quantity = %s, want 250", totalQty)
	}
	totalCost := leg1.Quantity.Mul(leg1.AllInPerContract).Add(leg2.Quantity.Mul(leg2.AllInPerContract))
	// 150 × 0.537458 + 100 × 0.5375 = 80.6187 + 53.75 = 134.3687
	if !totalCost.Equal(d("134.3687")) {
		t.Errorf("total cost = %s, want 134.3687", totalCost)
	}
	avgAllIn := totalCost.Div(totalQty)
	// 134.3687 / 250 = 0.5374748, rounds to 0.5375
	if !avgAllIn.Round(4).Equal(d("0.5375")) {
		t.Errorf("average all-in = %s, want ~0.5375", avgAllIn)
	}

	// Explanation: verify key phrases (they tie at 0.5375, so ranks #2/#3 show 0.0000 worse)
	for _, want := range []string{
		"250 of 500 fill across 2 venues",
		"250 contracts at average all-in 0.5375",
		"total cost 134.3687",
		"polymarket (polymarket:1234567890): 150 contracts",
		"kalshi (kalshi:KXBTCD-26OCT0617-T120000): 100 contracts",
		"Shortfall: 250 contracts (IOC)",
	} {
		if !strings.Contains(dec.Explanation, want) {
			t.Errorf("explanation missing %q\nGot: %s", want, dec.Explanation)
		}
	}

	// Warnings: partial fill
	if !hasWarning(dec, "partial fill: chosen route fills 250 of 500 contracts (IOC)") {
		t.Errorf("warnings = %v, want partial fill warning", dec.Warnings)
	}

	t.Logf("Example 2 Decision:\n%s", dec.Explanation)
	t.Logf("Warnings: %v", dec.Warnings)
}

// TestDocsExample3ComplementRoute is the fixture test for docs/routing.md Example 3.
// It tests a complement route where buying NO on B is cheaper than buying YES on A.
func TestDocsExample3ComplementRoute(t *testing.T) {
	// Scenario: Buy 100 YES on Kalshi A; Polymarket B is a complement.
	// Direct (YES on A at 0.54) vs Complement (NO on B via YES bids at 0.52 → 1-0.52 = 0.48)
	kalshiA := cand("kalshi:A", []canonical.Level{lv("0.46", "100")}, []canonical.Level{lv("0.54", "100")}, nil)
	polymarketB := cand("polymarket:B", []canonical.Level{lv("0.52", "100")}, []canonical.Level{lv("0.48", "100")},
		&canonical.Relationship{
			RoundID: 7, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelComplement, Confidence: d("0.95"),
			Method: canonical.MethodRules, Explanation: "test complement",
			BasisRisk: &canonical.BasisRisk{Score: d("0.3"), Components: []canonical.BasisComponent{}},
		})

	dec := mustRoute(t, buy("100"), kalshiA, []canonical.Candidate{polymarketB})

	// Chosen: Complement route (NO on B)
	if len(dec.Chosen) != 1 {
		t.Fatalf("chosen legs = %d, want 1", len(dec.Chosen))
	}
	leg := dec.Chosen[0]
	if leg.InstrumentID != "polymarket:B" || leg.Outcome != canonical.OutcomeNo || leg.Route != RouteComplement {
		t.Errorf("chosen = %+v, want polymarket:B NO complement", leg)
	}
	// Complement: buy NO on B via YES bids at 0.52 → 1-0.52 = 0.48
	if !leg.VWAP.Equal(d("0.48")) {
		t.Errorf("VWAP = %s, want 0.48", leg.VWAP)
	}
	// Fee: 100 × 0.07 × 0.48 × 0.52 = 1.7472 (Polymarket rounds total to 5 dp → 1.74720 → 0.017472/contract)
	if !leg.FeePerContract.Equal(d("0.017472")) {
		t.Errorf("FeePerContract = %s, want 0.017472", leg.FeePerContract)
	}
	// Basis penalty: 0.3 × 0.01 = 0.003
	if !leg.BasisPenaltyPerContract.Equal(d("0.003")) {
		t.Errorf("BasisPenaltyPerContract = %s, want 0.003", leg.BasisPenaltyPerContract)
	}
	// All-in: 0.48 + 0.017472 + 0.003 = 0.500472
	if !leg.AllInPerContract.Equal(d("0.500472")) {
		t.Errorf("AllInPerContract = %s, want 0.500472", leg.AllInPerContract)
	}

	// Alternatives: rank 1 (complement chosen), rank 2 (direct)
	if len(dec.Alternatives) != 2 {
		t.Fatalf("alternatives = %d, want 2", len(dec.Alternatives))
	}
	second := dec.Alternatives[1]
	if second.Rank != 2 || second.Legs[0].InstrumentID != "kalshi:A" {
		t.Errorf("alternative[1] = %+v, want rank 2 kalshi:A", second)
	}
	// Direct: 100 @ 0.54, fee ceil(100 × 0.07 × 0.54 × 0.46) = ceil(1.7388) = 1.74 → 0.0174/contract
	// All-in: 0.54 + 0.0174 + 0 = 0.5574
	if !second.AllInPerContract.Equal(d("0.5574")) {
		t.Errorf("alternative[1] all-in = %s, want 0.5574", second.AllInPerContract)
	}

	// Explanation: verify complement explanation
	for _, want := range []string{
		"Chose NO on polymarket:B (complement of kalshi:A)",
		"VWAP of 0.4800",
		"fee of 0.0175",
		"basis penalty of 0.0030",
		"all-in cost of 0.5005",
		"vs 0.5574 for YES on kalshi:A",
		"kalshi (kalshi:A) ranks #2 at 0.5574",
		"0.0569 worse",
	} {
		if !strings.Contains(dec.Explanation, want) {
			t.Errorf("explanation missing %q\nGot: %s", want, dec.Explanation)
		}
	}

	t.Logf("Example 3 Decision:\n%s", dec.Explanation)

	// Generate JSON for the documentation
	jsonBytes, err := json.MarshalIndent(dec, "", "  ")
	if err != nil {
		t.Fatalf("JSON marshal: %v", err)
	}
	t.Logf("Example 3 JSON:\n%s", string(jsonBytes))
}
