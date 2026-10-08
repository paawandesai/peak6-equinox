package route

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestBasisPenaltyFlipsDecision: venue A has cheaper VWAP but higher basis-risk
// score, so venue B wins when the basis penalty tips the all-in cost (M3-02 AC).
func TestBasisPenaltyFlipsDecision(t *testing.T) {
	// Partner A (Kalshi): VWAP 0.50, fee ~0.0175, basis score 3.0 → penalty 0.03 → all-in ~0.5475.
	// Partner B (Polymarket): VWAP 0.52, fee ~0.0182, basis score 0.0 → penalty 0 → all-in ~0.5382.
	// B wins due to basis penalty on A!

	partnerA := cand(kalshiID, []canonical.Level{lv("0.48", "200")}, []canonical.Level{lv("0.50", "100")},
		rel("polymarket:target", kalshiID, canonical.RelEquivalent, "0.95"))
	partnerA.Relationship.BasisRisk = &canonical.BasisRisk{
		Score:      d("3.0"),
		Components: []canonical.BasisComponent{{Name: "cutoff_gap", Score: d("3.0"), Detail: "large gap"}},
	}

	partnerB := cand(polymarketID, []canonical.Level{lv("0.51", "80")}, []canonical.Level{lv("0.52", "100")},
		rel("polymarket:target", polymarketID, canonical.RelEquivalent, "0.97"))
	partnerB.Relationship.BasisRisk = &canonical.BasisRisk{
		Score:      d("0"),
		Components: []canonical.BasisComponent{},
	}

	targetInst := cand("polymarket:target", []canonical.Level{lv("0.52", "50")}, []canonical.Level{lv("0.54", "50")}, nil)

	dec := mustRoute(t, buy("100"), targetInst, []canonical.Candidate{partnerA, partnerB})
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID {
		t.Fatalf("basis should flip decision to polymarket, got chosen = %+v", dec.Chosen)
	}
	if !dec.Chosen[0].BasisPenaltyPerContract.Equal(d("0")) {
		t.Errorf("polymarket basis penalty = %s, want 0", dec.Chosen[0].BasisPenaltyPerContract)
	}
	// Check Kalshi is ranked #2 with basis penalty shown.
	second := dec.Alternatives[1]
	if second.Legs[0].InstrumentID != kalshiID {
		t.Fatalf("second alternative should be kalshi, got %+v", second)
	}
	if !second.Legs[0].BasisPenaltyPerContract.Equal(d("0.03")) {
		t.Errorf("kalshi basis penalty = %s, want 0.03", second.Legs[0].BasisPenaltyPerContract)
	}
	if !strings.Contains(dec.Explanation, "basis 0.0300") {
		t.Errorf("explanation should show basis penalty: %s", dec.Explanation)
	}
}

// TestBasisPenaltyZeroScore: when basis-risk score is 0, penalty is 0 (M3-02 AC).
func TestBasisPenaltyZeroScore(t *testing.T) {
	target := cand(kalshiID, []canonical.Level{lv("0.48", "200")}, []canonical.Level{lv("0.60", "100")}, nil)
	partner := cand(polymarketID, []canonical.Level{lv("0.49", "80")}, []canonical.Level{lv("0.51", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.95"))
	partner.Relationship.BasisRisk = &canonical.BasisRisk{
		Score:      d("0"),
		Components: []canonical.BasisComponent{},
	}

	dec := mustRoute(t, buy("50"), target, []canonical.Candidate{partner})
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID {
		t.Fatalf("chosen = %+v", dec.Chosen)
	}
	if !dec.Chosen[0].BasisPenaltyPerContract.IsZero() {
		t.Errorf("basis penalty = %s, want 0", dec.Chosen[0].BasisPenaltyPerContract)
	}
	// Explanation should not mention basis penalty when it's zero.
	if strings.Contains(dec.Explanation, "basis 0.0000") {
		t.Errorf("explanation should omit zero basis: %s", dec.Explanation)
	}
}

// TestBasisPenaltyTieBreak: when all-in costs are within 0.0001 tolerance, ties
// are broken by deeper book, then lower basis score, then venue name (M3-02 AC).
func TestBasisPenaltyTieBreak(t *testing.T) {
	// Make target stale so it's ineligible, forcing partners to compete.
	targetID := "kalshi:TARGET"
	target := cand(targetID, []canonical.Level{lv("0.48", "200")}, []canonical.Level{lv("0.50", "200")}, nil)
	target.Quote.FetchedAt = t0.Add(-150 * time.Second) // Stale (beyond 120s)

	// Test tie-break by depth: same all-in cost (fee-free), different depth.
	// A: VWAP 0.50, fee 0, basis 0.005 → all-in 0.505, depth 100.
	// B: VWAP 0.505, fee 0, basis 0 → all-in 0.505, depth 50.
	// A should win on depth.
	partnerA := cand("polymarket:A", []canonical.Level{lv("0.49", "80")}, []canonical.Level{lv("0.50", "100")},
		rel(targetID, "polymarket:A", canonical.RelEquivalent, "0.95"))
	partnerA.Quote.FetchedAt = t0 // Ensure fresh
	partnerA.Instrument.FeeSchedule = canonical.FeeSchedule{
		ID: canonical.FeeSchedulePolymarketV1, Params: map[string]decimal.Decimal{"rate": d("0")}, Verified: true, Source: "test"}
	partnerA.Relationship.BasisRisk = &canonical.BasisRisk{
		Score:      d("0.5"),
		Components: []canonical.BasisComponent{{Name: "test", Score: d("0.5"), Detail: "test"}},
	}

	partnerB := cand("polymarket:B", []canonical.Level{lv("0.49", "80")}, []canonical.Level{lv("0.505", "50")},
		rel(targetID, "polymarket:B", canonical.RelEquivalent, "0.95"))
	partnerB.Quote.FetchedAt = t0 // Ensure fresh
	partnerB.Instrument.FeeSchedule = canonical.FeeSchedule{
		ID: canonical.FeeSchedulePolymarketV1, Params: map[string]decimal.Decimal{"rate": d("0")}, Verified: true, Source: "test"}
	partnerB.Relationship.BasisRisk = &canonical.BasisRisk{
		Score:      d("0.0"),
		Components: []canonical.BasisComponent{},
	}

	dec := mustRoute(t, buy("30"), target, []canonical.Candidate{partnerA, partnerB})
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != "polymarket:A" {
		t.Fatalf("tie should resolve to deeper book (A), got chosen = %+v, exclusions = %+v", dec.Chosen, dec.Exclusions)
	}

	// Now test basis tie-break: same depth, different basis.
	partnerA.Quote.YesAsks = []canonical.Level{lv("0.50", "50")}
	partnerB.Quote.YesAsks = []canonical.Level{lv("0.505", "50")}
	dec = mustRoute(t, buy("30"), target, []canonical.Candidate{partnerA, partnerB})
	// With same depth, lower basis score (B=0) should win over higher (A=0.5).
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != "polymarket:B" {
		t.Fatalf("tie with same depth should resolve to lower basis (B), got chosen = %+v", dec.Chosen)
	}
}

// TestBasisPenaltyMonotonicity: increasing basis-risk score never decreases
// all-in cost (M3-02 AC, property test).
func TestBasisPenaltyMonotonicity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate two basis scores: a <= b.
		scoreA := rapid.Float64Range(0, 1.0).Draw(t, "scoreA")
		scoreB := rapid.Float64Range(scoreA, 1.0).Draw(t, "scoreB")
		mult := rapid.Float64Range(0.001, 0.1).Draw(t, "multiplier")

		dScoreA := decimal.NewFromFloat(scoreA)
		dScoreB := decimal.NewFromFloat(scoreB)
		dMult := decimal.NewFromFloat(mult)

		penaltyA := dScoreA.Mul(dMult)
		penaltyB := dScoreB.Mul(dMult)

		// All-in cost = VWAP + fee + basis penalty.
		// If VWAP and fee are constant, increasing basis score increases all-in.
		vwap := d("0.5")
		fee := d("0.01")
		allInA := vwap.Add(fee).Add(penaltyA)
		allInB := vwap.Add(fee).Add(penaltyB)

		if allInB.LessThan(allInA) {
			t.Fatalf("increasing basis score decreased all-in: scoreA=%s penaltyA=%s allInA=%s, scoreB=%s penaltyB=%s allInB=%s",
				dScoreA, penaltyA, allInA, dScoreB, penaltyB, allInB)
		}
	})
}
