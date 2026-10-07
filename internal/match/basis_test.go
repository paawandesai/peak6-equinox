package match

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func componentScores(br canonical.BasisRisk) map[string]canonical.BasisComponent {
	out := map[string]canonical.BasisComponent{}
	for _, c := range br.Components {
		out[c.Name] = c
	}
	return out
}

func TestBasisRiskComponents(t *testing.T) {
	a, b := pair()
	br := basisRisk(a, b, verdict{})
	cs := componentScores(br)
	if len(br.Components) != 4 || !br.Score.Equal(basisEdgeUnknown) {
		t.Fatalf("clean pair: score %s, %+v", br.Score, br.Components)
	}
	if !cs[BasisCutoffGap].Score.IsZero() || !cs[BasisCutoffUncertain].Score.IsZero() || !cs[BasisSourceDiff].Score.IsZero() {
		t.Errorf("zero components: %+v", cs)
	}
	if !strings.Contains(cs[BasisSourceDiff].Detail, "cf_benchmarks_brti") || !strings.Contains(cs[BasisEdgeRuleDiff].Detail, "not compared") {
		t.Errorf("details: %+v", cs)
	}

	// A 12 h gap is half the tolerance: 0.10. Two defaulted deadlines: 0.10.
	later := t0.Add(12 * time.Hour)
	b.Terms.Deadline = &later
	a.Quality.Add(canonical.QualityDeadlineDefaulted, "deadline", "x")
	b.Quality.Add(canonical.QualityDeadlineDefaulted, "deadline", "x")
	a.Terms.EdgeRules, b.Terms.EdgeRules = "none", "none"
	cs = componentScores(basisRisk(a, b, verdict{}))
	if !cs[BasisCutoffGap].Score.Equal(decimal.RequireFromString("0.1")) || !strings.Contains(cs[BasisCutoffGap].Detail, "12h0m0s apart") {
		t.Errorf("cutoff gap: %+v", cs[BasisCutoffGap])
	}
	if !cs[BasisCutoffUncertain].Score.Equal(decimal.RequireFromString("0.1")) || !strings.Contains(cs[BasisCutoffUncertain].Detail, "kalshi:A, polymarket:B") {
		t.Errorf("cutoff uncertain: %+v", cs[BasisCutoffUncertain])
	}
	if !cs[BasisEdgeRuleDiff].Score.IsZero() {
		t.Errorf("edge rules match: %+v", cs[BasisEdgeRuleDiff])
	}

	// The gap component is capped at its maximum beyond the tolerance.
	far := t0.Add(48 * time.Hour)
	b.Terms.Deadline = &far
	if cs = componentScores(basisRisk(a, b, verdict{})); !cs[BasisCutoffGap].Score.Equal(basisCutoffGapMax) {
		t.Errorf("capped gap: %+v", cs[BasisCutoffGap])
	}

	// Source and edge-rule differences, and facts.
	b.Terms.ResolutionSource = "binance_btc_usdt"
	b.Terms.EdgeRules = "ties resolve no"
	v := verdict{facts: []canonical.FactUse{{Key: "f1", Value: "1", AsOf: t0}, {Key: "f2", Value: "2", AsOf: t0}}, derived: &derivation{grid: true}}
	br = basisRisk(a, b, v)
	cs = componentScores(br)
	if !cs[BasisSourceDiff].Score.Equal(basisSourceDiffer) || !cs[BasisEdgeRuleDiff].Score.Equal(basisEdgeDiffer) ||
		!cs[BasisFactDependency].Score.Equal(decimal.RequireFromString("0.1")) || !cs[BasisGridRounding].Score.Equal(basisGrid) {
		t.Errorf("components: %+v", br.Components)
	}
	if !strings.Contains(cs[BasisFactDependency].Detail, "f1=1 as of 2026-10-06, f2=2") {
		t.Errorf("fact detail: %q", cs[BasisFactDependency].Detail)
	}
	// 0.20 + 0.10 + 0.50 + 0.30 + 0.10 + 0.05 = 1.25, capped at 1.
	if !br.Score.Equal(decimal.NewFromInt(1)) {
		t.Errorf("score = %s, want 1", br.Score)
	}
	b.Terms.ResolutionSource = canonical.Unknown
	if cs = componentScores(basisRisk(a, b, verdict{})); !cs[BasisSourceDiff].Score.Equal(basisSourceUnknown) {
		t.Errorf("unknown source: %+v", cs[BasisSourceDiff])
	}
}

func TestBasisRiskOnlyForRoutableOrImplies(t *testing.T) {
	a, b := pair()
	if rel := Compare(a, b, fedFacts(t)); rel.BasisRisk == nil {
		t.Error("equivalent without basis risk")
	}
	b.Terms.Polarity = canonical.PolarityNegative
	if rel := Compare(a, b, fedFacts(t)); rel.Type != canonical.RelComplement || rel.BasisRisk == nil {
		t.Errorf("complement: %s %v", rel.Type, rel.BasisRisk)
	}
	b.Terms.Polarity = canonical.PolarityPositive
	b.Terms.Threshold = dec("80000")
	if rel := Compare(a, b, fedFacts(t)); rel.Type != canonical.RelImplies || rel.BasisRisk == nil {
		t.Errorf("implies: %s %v", rel.Type, rel.BasisRisk)
	}
	b.Terms.Subject = "ETH"
	if rel := Compare(a, b, fedFacts(t)); rel.Type != canonical.RelUnrelated || rel.BasisRisk != nil {
		t.Errorf("unrelated: %s %v", rel.Type, rel.BasisRisk)
	}
}
