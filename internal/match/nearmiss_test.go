package match

import (
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestNearMiss(t *testing.T) {
	related := func(status canonical.DiffStatus, a, b string) canonical.Relationship {
		return canonical.Relationship{Type: canonical.RelRelated, Diff: []canonical.FieldDiff{
			{Field: "subject", Status: canonical.DiffMatch},
			{Field: "threshold", A: a, B: b, Status: status},
		}}
	}
	cases := []struct {
		name string
		rel  canonical.Relationship
		want bool
	}{
		{"equal thresholds", related(canonical.DiffMatch, "76599.99", "76599.99"), true},
		{"one cent apart", related(canonical.DiffDiffer, "76599.99", "76600"), true},
		{"next strike", related(canonical.DiffDiffer, "76599.99", "76699.99"), false},
		{"unknown threshold", related(canonical.DiffUnknown, "unknown", "76600"), false},
		{"unparseable", related(canonical.DiffDiffer, "x", "76600"), false},
		{"both zero", related(canonical.DiffDiffer, "0", "0.0"), true},
		{"no threshold field", canonical.Relationship{Type: canonical.RelRelated}, false},
		{"a fact was consulted", canonical.Relationship{Type: canonical.RelRelated, FactsUsed: []canonical.FactUse{{Key: "fed_upper_bound"}}}, true},
		{"equivalent is not a near miss", canonical.Relationship{Type: canonical.RelEquivalent, Diff: []canonical.FieldDiff{{Field: "threshold", Status: canonical.DiffMatch}}}, false},
		{"implies is not a near miss", canonical.Relationship{Type: canonical.RelImplies, FactsUsed: []canonical.FactUse{{Key: "f"}}}, false},
	}
	for _, tc := range cases {
		if got := NearMiss(tc.rel); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}
