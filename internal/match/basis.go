package match

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// Basis risk (FR-11, domain.md) is the residual risk that two matched
// contracts resolve differently even though the matcher calls them
// equivalent, equivalent_given_facts, complement or implies. It is scored
// from documented components, each in [0,1], summed and capped at 1. The
// weights are judgment, not estimates: they rank pairs, and M3 turns the
// score into a per-contract penalty. Every component carries a detail so a
// reader can see what it measures.
//
//	cutoff_gap            the deadlines differ: 0.20 × gap / 24 h (0 when the
//	                      instants are identical; at most 0.20). A tolerated
//	                      gap means a window in which the measurement can move.
//	cutoff_uncertain      0.05 per deadline taken from the close time rather
//	                      than stated by the rules (quality deadline_defaulted).
//	source_difference     0 when both sides name the same known source. Any
//	                      other case is reported (0.50 different, 0.25 unknown)
//	                      but cannot reach a routable verdict today: a source
//	                      veto blocks it (Q-4: no documented-identical sources
//	                      are allowed yet).
//	edge_rule_difference  0 when both sides' edge rules are known and equal,
//	                      0.05 when either is unknown (not compared), 0.30 when
//	                      both are known and differ.
//	fact_dependency       0.05 per fact the verdict rests on: a fact can go
//	                      stale (an inter-meeting move, a changed step) before
//	                      the contracts resolve.
//	grid_rounding         0.05 when equality relied on the rate grid: an
//	                      off-grid move resolves by each venue's own rounding
//	                      rule, which is not compared.
//
// Pairs that are related or unrelated get no score: they are never routed.
var (
	basisCutoffGapMax  = decimal.RequireFromString("0.20")
	basisCutoffUnsure  = decimal.RequireFromString("0.05")
	basisSourceDiffer  = decimal.RequireFromString("0.50")
	basisSourceUnknown = decimal.RequireFromString("0.25")
	basisEdgeUnknown   = decimal.RequireFromString("0.05")
	basisEdgeDiffer    = decimal.RequireFromString("0.30")
	basisPerFact       = decimal.RequireFromString("0.05")
	basisGrid          = decimal.RequireFromString("0.05")
	basisScale         = decimal.NewFromInt(1)
)

// Basis component names, stable for persistence and the UI.
const (
	BasisCutoffGap       = "cutoff_gap"
	BasisCutoffUncertain = "cutoff_uncertain"
	BasisSourceDiff      = "source_difference"
	BasisEdgeRuleDiff    = "edge_rule_difference"
	BasisFactDependency  = "fact_dependency"
	BasisGridRounding    = "grid_rounding"
)

// basisRisk scores the pair for a routable or implies verdict.
func basisRisk(a, b canonical.Instrument, v verdict) canonical.BasisRisk {
	var comps []canonical.BasisComponent
	add := func(name string, score decimal.Decimal, detail string) {
		comps = append(comps, canonical.BasisComponent{Name: name, Score: score, Detail: detail})
	}

	gap := deadlineGap(a, b)
	gapScore := decimal.Zero
	if gap > 0 {
		gapScore = basisCutoffGapMax.Mul(decimal.NewFromInt(int64(gap))).Div(decimal.NewFromInt(int64(DeadlineTolerance))).Round(3)
		if gapScore.GreaterThan(basisCutoffGapMax) {
			gapScore = basisCutoffGapMax
		}
	}
	add(BasisCutoffGap, gapScore, fmt.Sprintf("deadlines %s apart", gap.Round(time.Minute)))

	var defaulted []string
	for _, in := range []canonical.Instrument{a, b} {
		if in.Quality.Has(canonical.QualityDeadlineDefaulted) {
			defaulted = append(defaulted, in.ID)
		}
	}
	switch len(defaulted) {
	case 0:
		add(BasisCutoffUncertain, decimal.Zero, "both deadlines are stated by the rules")
	default:
		add(BasisCutoffUncertain, basisCutoffUnsure.Mul(decimal.NewFromInt(int64(len(defaulted)))),
			"deadline taken from the close time on "+strings.Join(defaulted, ", "))
	}

	sa, sb := a.Terms.ResolutionSource, b.Terms.ResolutionSource
	switch {
	case sa == canonical.Unknown || sb == canonical.Unknown:
		add(BasisSourceDiff, basisSourceUnknown, fmt.Sprintf("resolution source unknown (%s vs %s)", sa, sb))
	case sa != sb:
		add(BasisSourceDiff, basisSourceDiffer, fmt.Sprintf("different resolution sources (%s vs %s)", sa, sb))
	default:
		add(BasisSourceDiff, decimal.Zero, "both resolve on "+sa)
	}

	ea, eb := a.Terms.EdgeRules, b.Terms.EdgeRules
	switch {
	case ea == canonical.Unknown || eb == canonical.Unknown:
		add(BasisEdgeRuleDiff, basisEdgeUnknown, "edge rules not compared (unknown on at least one side)")
	case ea != eb:
		add(BasisEdgeRuleDiff, basisEdgeDiffer, fmt.Sprintf("edge rules differ (%s vs %s)", ea, eb))
	default:
		add(BasisEdgeRuleDiff, decimal.Zero, "edge rules match ("+ea+")")
	}

	if len(v.facts) > 0 {
		ids := make([]string, 0, len(v.facts))
		for _, fu := range v.facts {
			ids = append(ids, fmt.Sprintf("%s=%s as of %s", fu.Key, fu.Value, fu.AsOf.Format(facts.DateLayout)))
		}
		add(BasisFactDependency, basisPerFact.Mul(decimal.NewFromInt(int64(len(v.facts)))), "rests on "+strings.Join(ids, ", "))
	}
	if v.derived != nil && v.derived.grid {
		add(BasisGridRounding, basisGrid, "equality relies on the rate grid; an off-grid move resolves by each venue's rounding rule")
	}

	score := decimal.Zero
	for _, c := range comps {
		score = score.Add(c.Score)
	}
	if score.GreaterThan(basisScale) {
		score = basisScale
	}
	return canonical.BasisRisk{Score: score, Components: comps}
}
