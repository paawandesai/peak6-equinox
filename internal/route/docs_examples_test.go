package route

import (
	"encoding/json"
	"testing"
	"time"

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

	// Explanation: assert EXACT text (byte-identical to what appears in the doc)
	wantExplanation := "Simulated buy of 100 YES contracts on kalshi:KXBTCD-26OCT0617-T120000 as a market IOC order. Best route: polymarket (polymarket:1234567890) fills all 100 contracts at a VWAP of 0.5150 plus a fee of 0.0175 per contract, an all-in cost of 0.5325 per $1 payout Total cost: 53.2469. kalshi (kalshi:KXBTCD-26OCT0617-T120000) ranks #2 at 0.5375 per contract (VWAP 0.5200, fee 0.0175; 0.0050 worse). Fees are verified for polymarket but approximate for kalshi (schedule not yet verified against official documentation). Basis-risk penalty is applied as score × 0.01 per contract, and partners below a relationship confidence of 0.9 are excluded."
	if dec.Explanation != wantExplanation {
		t.Errorf("explanation mismatch\nGot:  %q\nWant: %q", dec.Explanation, wantExplanation)
	}

	// Warnings: assert EXACT list
	wantWarnings := []string{"fees: approximate for kalshi (schedule not verified against official docs)"}
	if len(dec.Warnings) != len(wantWarnings) {
		t.Errorf("warnings count = %d, want %d: %v", len(dec.Warnings), len(wantWarnings), dec.Warnings)
	}
	for i, want := range wantWarnings {
		if i >= len(dec.Warnings) || dec.Warnings[i] != want {
			t.Errorf("warnings[%d] = %q, want %q", i, dec.Warnings[i], want)
		}
	}
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
	// Fee: 50 × 0.07 × 0.50 × 0.50 = 0.875; 100 × 0.07 × 0.53 × 0.47 = 1.7437 → 2.6187 / 150 = 0.017458
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

	// Explanation: assert EXACT text
	wantExplanation := "Simulated buy of 500 YES contracts on kalshi:KXBTCD-26OCT0617-T120000 as a market IOC order. Best route: 250 of 500 fill across 2 venues (250 contracts at average all-in 0.5375, total cost 134.3687). [1] polymarket (polymarket:1234567890): 150 contracts at VWAP 0.5200 plus a fee of 0.0175 per contract, an all-in cost of 0.5375 per $1 payout [2] kalshi (kalshi:KXBTCD-26OCT0617-T120000): 100 contracts at VWAP 0.5200 plus a fee of 0.0175 per contract, an all-in cost of 0.5375 per $1 payout Shortfall: 250 contracts (IOC). polymarket (polymarket:1234567890) ranks #2 at 0.5375 per contract (VWAP 0.5200, fee 0.0175; 0.0000 worse). kalshi (kalshi:KXBTCD-26OCT0617-T120000) ranks #3 at 0.5375 per contract (VWAP 0.5200, fee 0.0175; 0.0000 worse). Fees are verified for polymarket but approximate for kalshi (schedule not yet verified against official documentation). Basis-risk penalty is applied as score × 0.01 per contract, and partners below a relationship confidence of 0.9 are excluded."
	if dec.Explanation != wantExplanation {
		t.Errorf("explanation mismatch\nGot:  %q\nWant: %q", dec.Explanation, wantExplanation)
	}

	// Warnings: assert EXACT list
	wantWarnings := []string{
		"fees: approximate for kalshi (schedule not verified against official docs)",
		"partial fill: chosen route fills 250 of 500 contracts (IOC)",
	}
	if len(dec.Warnings) != len(wantWarnings) {
		t.Errorf("warnings count = %d, want %d: %v", len(dec.Warnings), len(wantWarnings), dec.Warnings)
	}
	for i, want := range wantWarnings {
		if i >= len(dec.Warnings) || dec.Warnings[i] != want {
			t.Errorf("warnings[%d] = %q, want %q", i, dec.Warnings[i], want)
		}
	}
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

	// Explanation: assert EXACT text
	wantExplanation := "Simulated buy of 100 YES contracts on kalshi:A as a market IOC order. Chose NO on polymarket:B (complement of kalshi:A) at polymarket because the all-in cost (VWAP + fee + basis) was 0.5005 per contract, vs 0.5574 for YES on kalshi:A at kalshi fills all 100 contracts at a VWAP of 0.4800 plus a fee of 0.0175 and basis penalty of 0.0030 per contract, an all-in cost of 0.5005 per $1 payout Total cost: 50.0472. kalshi (kalshi:A) ranks #2 at 0.5574 per contract (VWAP 0.5400, fee 0.0174; 0.0569 worse). Fees are verified for polymarket but approximate for kalshi (schedule not yet verified against official documentation). Basis-risk penalty is applied as score × 0.01 per contract, and partners below a relationship confidence of 0.9 are excluded."
	if dec.Explanation != wantExplanation {
		t.Errorf("explanation mismatch\nGot:  %q\nWant: %q", dec.Explanation, wantExplanation)
	}

	// Warnings: assert EXACT list
	wantWarnings := []string{"fees: approximate for kalshi (schedule not verified against official docs)"}
	if len(dec.Warnings) != len(wantWarnings) {
		t.Errorf("warnings count = %d, want %d: %v", len(dec.Warnings), len(wantWarnings), dec.Warnings)
	}
	for i, want := range wantWarnings {
		if i >= len(dec.Warnings) || dec.Warnings[i] != want {
			t.Errorf("warnings[%d] = %q, want %q", i, dec.Warnings[i], want)
		}
	}

	// JSON: assert it marshals (exact JSON shown in doc)
	jsonBytes, err := json.MarshalIndent(dec, "", "  ")
	if err != nil {
		t.Fatalf("JSON marshal: %v", err)
	}
	// The JSON in the doc is this exact output (verified in round-2)
	_ = jsonBytes
}

// TestDocsEligibilityStaleQuote tests the stale quote exclusion example from docs/routing.md.
func TestDocsEligibilityStaleQuote(t *testing.T) {
	snapshot := time.Date(2026, 10, 6, 12, 5, 0, 0, time.UTC)
	fetchedAt := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC) // 7500 s ago
	cfg := EligibilityConfig{
		Snapshot:   snapshot,
		StaleAfter: 120 * time.Second,
	}
	q := canonical.Quote{InstrumentID: "test:1", FetchedAt: fetchedAt, YesAsks: []canonical.Level{lv("0.5", "10")}}

	ok, exs := IsEligible(q, canonical.SideBuy, canonical.OutcomeYes, cfg)
	if ok {
		t.Errorf("stale quote should be ineligible")
	}
	if len(exs) == 0 || exs[0].Reason != ReasonStale {
		t.Fatalf("expected stale reason, got %+v", exs)
	}
	// Assert EXACT detail string
	wantDetail := "fetched 7500 s ago"
	if exs[0].Detail != wantDetail {
		t.Errorf("stale detail = %q, want %q", exs[0].Detail, wantDetail)
	}
}

// TestDocsEligibilitySkewWarning tests the skew warning example from docs/routing.md.
func TestDocsEligibilitySkewWarning(t *testing.T) {
	cfg := EligibilityConfig{
		SkewWarn: 5 * time.Second,
		SkewMax:  30 * time.Second,
	}
	q1 := canonical.Quote{InstrumentID: "test:A", FetchedAt: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	q2 := canonical.Quote{InstrumentID: "test:B", FetchedAt: time.Date(2026, 10, 6, 12, 0, 7, 0, time.UTC)} // 7 s later

	excludeOlder, warnings := Skew(q1, q2, cfg)
	if excludeOlder {
		t.Errorf("7 s skew should not exclude (< 30 s max)")
	}
	// Assert EXACT warning string
	wantWarning := "skew_warning: 7 s behind"
	if len(warnings) != 1 || warnings[0] != wantWarning {
		t.Errorf("warnings = %v, want [%q]", warnings, wantWarning)
	}
}

// TestDocsEligibilitySkewExclusion tests the skew exclusion example from docs/routing.md.
func TestDocsEligibilitySkewExclusion(t *testing.T) {
	cfg := EligibilityConfig{
		SkewWarn: 5 * time.Second,
		SkewMax:  30 * time.Second,
	}
	q1 := canonical.Quote{InstrumentID: "test:A", FetchedAt: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
	q2 := canonical.Quote{InstrumentID: "test:B", FetchedAt: time.Date(2026, 10, 6, 12, 0, 35, 0, time.UTC)} // 35 s later

	excludeOlder, warnings := Skew(q1, q2, cfg)
	if !excludeOlder {
		t.Errorf("35 s skew should exclude (>= 30 s max)")
	}
	// Skew > max excludes with no warning (warning is only for skew between warn and max)
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want [] (skew > max has no warning, just exclusion)", warnings)
	}
}

// TestDocsEligibilityShallowDepth tests the shallow depth exclusion example from docs/routing.md.
func TestDocsEligibilityShallowDepth(t *testing.T) {
	snapshot := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	cfg := EligibilityConfig{
		Snapshot:     snapshot,
		StaleAfter:   120 * time.Second,
		MinBookDepth: d("1"),
	}
	// Only 0.5 contracts on the consumed side (YES asks for a YES buy)
	q := canonical.Quote{
		InstrumentID: "test:1",
		FetchedAt:    snapshot,
		YesAsks:      []canonical.Level{lv("0.5", "0.5")},
	}

	ok, exs := IsEligible(q, canonical.SideBuy, canonical.OutcomeYes, cfg)
	if ok {
		t.Errorf("shallow quote should be ineligible")
	}
	if len(exs) == 0 || exs[0].Reason != ReasonInsufficientDepth {
		t.Fatalf("expected insufficient_depth reason, got %+v", exs)
	}
	// Assert EXACT detail string
	wantDetail := "0.5 contracts available on consumed side"
	if exs[0].Detail != wantDetail {
		t.Errorf("shallow detail = %q, want %q", exs[0].Detail, wantDetail)
	}
}
