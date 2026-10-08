package route

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestRouteRecordsInputs verifies that the router records the input snapshot
// for all candidates (target + partners, eligible and excluded) at decision time
// (UI-25, D-098 call 3).
func TestRouteRecordsInputs(t *testing.T) {
	target, partners := scenario()
	dec := mustRoute(t, buy("100"), target, partners)

	// Should have 2 inputs: target + one partner.
	if len(dec.Inputs) != 2 {
		t.Fatalf("inputs count = %d, want 2", len(dec.Inputs))
	}

	// Check target input.
	targetInput := findInput(t, dec.Inputs, kalshiID)
	if targetInput.Venue != canonical.VenueKalshi {
		t.Errorf("target input venue = %s, want kalshi", targetInput.Venue)
	}
	if targetInput.RelationshipType != "" {
		t.Errorf("target input relationship type = %q, want empty (self)", targetInput.RelationshipType)
	}
	if !targetInput.Confidence.Equal(d("1.0")) {
		t.Errorf("target input confidence = %s, want 1.0", targetInput.Confidence)
	}
	if targetInput.OutcomeSide != canonical.OutcomeYes {
		t.Errorf("target input outcome side = %s, want YES", targetInput.OutcomeSide)
	}
	if !targetInput.BestAsk.Equal(d("0.52")) {
		t.Errorf("target input best ask = %s, want 0.52", targetInput.BestAsk)
	}
	if len(targetInput.AskDepth) != 1 {
		t.Fatalf("target input ask depth count = %d, want 1", len(targetInput.AskDepth))
	}
	if !targetInput.AskDepth[0].Price.Equal(d("0.52")) || !targetInput.AskDepth[0].Size.Equal(d("100")) {
		t.Errorf("target input ask depth[0] = %+v", targetInput.AskDepth[0])
	}
	if !targetInput.FetchedAt.Equal(t0) {
		t.Errorf("target input fetched_at = %v, want %v", targetInput.FetchedAt, t0)
	}
	if targetInput.RoundID != 7 {
		t.Errorf("target input round_id = %d, want 7", targetInput.RoundID)
	}

	// Check partner input.
	partnerInput := findInput(t, dec.Inputs, polymarketID)
	if partnerInput.Venue != canonical.VenuePolymarket {
		t.Errorf("partner input venue = %s, want polymarket", partnerInput.Venue)
	}
	if partnerInput.RelationshipType != canonical.RelEquivalent {
		t.Errorf("partner input relationship type = %s, want equivalent", partnerInput.RelationshipType)
	}
	if !partnerInput.Confidence.Equal(d("0.97")) {
		t.Errorf("partner input confidence = %s, want 0.97", partnerInput.Confidence)
	}
	if partnerInput.OutcomeSide != canonical.OutcomeYes {
		t.Errorf("partner input outcome side = %s, want YES", partnerInput.OutcomeSide)
	}
	if !partnerInput.BestAsk.Equal(d("0.50")) {
		t.Errorf("partner input best ask = %s, want 0.50", partnerInput.BestAsk)
	}
	if len(partnerInput.AskDepth) != 2 {
		t.Fatalf("partner input ask depth count = %d, want 2", len(partnerInput.AskDepth))
	}
	if !partnerInput.AskDepth[0].Price.Equal(d("0.50")) || !partnerInput.AskDepth[0].Size.Equal(d("50")) {
		t.Errorf("partner input ask depth[0] = %+v", partnerInput.AskDepth[0])
	}
	if !partnerInput.AskDepth[1].Price.Equal(d("0.53")) || !partnerInput.AskDepth[1].Size.Equal(d("100")) {
		t.Errorf("partner input ask depth[1] = %+v", partnerInput.AskDepth[1])
	}
}

// TestRouteRecordsInputsForSweep verifies that inputs are recorded for a multi-venue sweep.
func TestRouteRecordsInputsForSweep(t *testing.T) {
	target, partners := scenario()
	// Buy 150: target can fill 100, partner can fill 150, so sweep fills 100 + 50.
	dec := mustRoute(t, buy("150"), target, partners)

	if len(dec.Inputs) != 2 {
		t.Fatalf("sweep inputs count = %d, want 2", len(dec.Inputs))
	}
	// Both candidates should have inputs recorded, regardless of fill distribution.
	findInput(t, dec.Inputs, kalshiID)
	findInput(t, dec.Inputs, polymarketID)
}

// TestRouteRecordsInputsForComplement verifies that complement routes record
// the NO outcome side for the complement candidate.
func TestRouteRecordsInputsForComplement(t *testing.T) {
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.52", "100")}, nil)
	// Complement relationship: buying YES on target ≡ buying NO on partner.
	partner := cand(polymarketID, []canonical.Level{lv("0.49", "100")}, []canonical.Level{lv("0.50", "100")},
		rel(kalshiID, polymarketID, canonical.RelComplement, "0.95"))
	dec := mustRoute(t, buy("50"), target, []canonical.Candidate{partner})

	partnerInput := findInput(t, dec.Inputs, polymarketID)
	if partnerInput.OutcomeSide != canonical.OutcomeNo {
		t.Errorf("complement partner outcome side = %s, want NO", partnerInput.OutcomeSide)
	}
	if partnerInput.RelationshipType != canonical.RelComplement {
		t.Errorf("complement partner relationship type = %s, want complement", partnerInput.RelationshipType)
	}
	// For NO outcome, the asks consumed are YesBids.
	// Partner has YesBids = [0.49:100], so BestAsk should be 0.49.
	if !partnerInput.BestAsk.Equal(d("0.49")) {
		t.Errorf("complement partner best ask = %s, want 0.49 (from bids)", partnerInput.BestAsk)
	}
}

// TestRouteRecordsInputsForExcludedCandidates verifies that excluded candidates
// also have their inputs recorded (UI-25 AC: "one per candidate considered (eligible or not)").
func TestRouteRecordsInputsForExcludedCandidates(t *testing.T) {
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.52", "100")}, nil)
	// Partner with low confidence, so it will be excluded.
	partner := cand(polymarketID, []canonical.Level{lv("0.49", "100")}, []canonical.Level{lv("0.50", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.85")) // below 0.9 floor
	dec := mustRoute(t, buy("50"), target, []canonical.Candidate{partner})

	// Partner should be excluded for low confidence.
	ex := exclusion(t, dec, polymarketID)
	if ex.Reason != ReasonLowConfidence {
		t.Errorf("exclusion reason = %s, want %s", ex.Reason, ReasonLowConfidence)
	}

	// But its input should still be recorded.
	if len(dec.Inputs) != 2 {
		t.Fatalf("inputs count (with excluded) = %d, want 2", len(dec.Inputs))
	}
	partnerInput := findInput(t, dec.Inputs, polymarketID)
	if !partnerInput.Confidence.Equal(d("0.85")) {
		t.Errorf("excluded partner input confidence = %s, want 0.85", partnerInput.Confidence)
	}
}

// TestRouteRecordsInputsForNotRouted verifies that inputs are recorded even when
// no venue can fill the order.
func TestRouteRecordsInputsForNotRouted(t *testing.T) {
	// Target with no book (excluded).
	target := cand(kalshiID, nil, nil, nil)
	target.Quote.Quality = canonical.Quality{{Flag: "no_book"}}
	dec := mustRoute(t, buy("50"), target, nil)

	// Target should be excluded for no book.
	ex := exclusion(t, dec, kalshiID)
	if ex.Reason != ReasonNoBook {
		t.Errorf("exclusion reason = %s, want %s", ex.Reason, ReasonNoBook)
	}

	// But its input should still be recorded.
	if len(dec.Inputs) != 1 {
		t.Fatalf("inputs count (not routed) = %d, want 1", len(dec.Inputs))
	}
	targetInput := findInput(t, dec.Inputs, kalshiID)
	if targetInput.BestAsk.IsPositive() {
		t.Errorf("no-book input best ask = %s, want 0 (no book)", targetInput.BestAsk)
	}
	if len(targetInput.AskDepth) != 0 {
		t.Errorf("no-book input ask depth = %d, want 0", len(targetInput.AskDepth))
	}
}

// TestRouteInputsCapDepth verifies that ask depth is capped at 10 levels (UI-25 AC).
func TestRouteInputsCapDepth(t *testing.T) {
	// Build a book with 15 levels.
	var asks []canonical.Level
	for i := 1; i <= 15; i++ {
		asks = append(asks, lv(d("0.50").Add(d("0.01").Mul(decimal.NewFromInt(int64(i)))).String(), "10"))
	}
	target := cand(kalshiID, []canonical.Level{lv("0.48", "100")}, asks, nil)
	dec := mustRoute(t, buy("50"), target, nil)

	targetInput := findInput(t, dec.Inputs, kalshiID)
	if len(targetInput.AskDepth) != 10 {
		t.Errorf("capped ask depth = %d, want 10 (max cap)", len(targetInput.AskDepth))
	}
	// First level should still be 0.51.
	if !targetInput.AskDepth[0].Price.Equal(d("0.51")) {
		t.Errorf("capped ask depth[0] price = %s, want 0.51", targetInput.AskDepth[0].Price)
	}
	// Last level (9th index) should be 0.60.
	if !targetInput.AskDepth[9].Price.Equal(d("0.60")) {
		t.Errorf("capped ask depth[9] price = %s, want 0.60", targetInput.AskDepth[9].Price)
	}
}

// TestOldDecisionDecodesWithoutInputs verifies that decisions stored before UI-25
// (without inputs) decode successfully with empty Inputs (UI-25 AC: "Old decisions
// decode with empty `Inputs`").
func TestOldDecisionDecodesWithoutInputs(t *testing.T) {
	// Simulate an old decision JSON without inputs or target_type fields.
	oldJSON := `{
		"round_id": 7,
		"target_id": "kalshi:KXBTCD-26OCT0617-T120000",
		"order": {"side": "buy", "outcome": "YES", "quantity": "100", "type": "market", "tif": "IOC"},
		"chosen": [],
		"alternatives": [],
		"exclusions": [],
		"warnings": [],
		"explanation": "test",
		"decided_at": "2026-10-06T12:00:00Z"
	}`

	var dec canonical.Decision
	if err := json.Unmarshal([]byte(oldJSON), &dec); err != nil {
		t.Fatalf("unmarshal old decision: %v", err)
	}

	if len(dec.Inputs) != 0 {
		t.Errorf("old decision inputs = %d, want 0 (empty)", len(dec.Inputs))
	}
	// Other fields should decode as before.
	if dec.RoundID != 7 || dec.TargetID != kalshiID {
		t.Errorf("old decision header = %+v", dec)
	}
}

// TestRouteInputsRoundTrip verifies that inputs survive JSON marshal/unmarshal
// (store round-trip test).
func TestRouteInputsRoundTrip(t *testing.T) {
	target, partners := scenario()
	dec := mustRoute(t, buy("100"), target, partners)

	// Marshal the decision to JSON.
	jsonData, err := json.Marshal(dec)
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}

	// Unmarshal into a new Decision.
	var dec2 canonical.Decision
	if err := json.Unmarshal(jsonData, &dec2); err != nil {
		t.Fatalf("unmarshal decision: %v", err)
	}

	// Inputs should round-trip.
	if len(dec2.Inputs) != len(dec.Inputs) {
		t.Fatalf("round-trip inputs count = %d, want %d", len(dec2.Inputs), len(dec.Inputs))
	}
	for i := range dec.Inputs {
		if dec2.Inputs[i].InstrumentID != dec.Inputs[i].InstrumentID {
			t.Errorf("round-trip input[%d] instrument_id = %s, want %s", i, dec2.Inputs[i].InstrumentID, dec.Inputs[i].InstrumentID)
		}
		if !dec2.Inputs[i].Confidence.Equal(dec.Inputs[i].Confidence) {
			t.Errorf("round-trip input[%d] confidence = %s, want %s", i, dec2.Inputs[i].Confidence, dec.Inputs[i].Confidence)
		}
		if len(dec2.Inputs[i].AskDepth) != len(dec.Inputs[i].AskDepth) {
			t.Errorf("round-trip input[%d] ask depth count = %d, want %d", i, len(dec2.Inputs[i].AskDepth), len(dec.Inputs[i].AskDepth))
		}
	}
}

// findInput returns the input for the given instrument ID, or fails the test.
func findInput(t *testing.T, inputs []canonical.DecisionInput, id string) canonical.DecisionInput {
	t.Helper()
	for _, in := range inputs {
		if in.InstrumentID == id {
			return in
		}
	}
	t.Fatalf("input for %s not found in %d inputs", id, len(inputs))
	return canonical.DecisionInput{}
}
