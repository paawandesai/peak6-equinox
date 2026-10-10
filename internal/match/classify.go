package match

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// Confidence is the matcher's confidence in the type it returns, in [0,1].
// The numbers are deliberate and conservative (D-020); they are not learned.
//
//	equivalent              starts at 0.99 (rules can never prove two texts
//	                        identical) and loses 0.01 per full hour of deadline
//	                        gap (up to 24 h), 0.02 for an unknown unit or
//	                        unknown edge rules on either side, and 0.05 per
//	                        deadline that was defaulted from the close time.
//	equivalent_given_facts  starts at 0.97: it rests on a reviewed fact as well
//	                        as on the texts. The meeting fact has confirmed
//	                        that both deadlines sit on one FOMC decision, so
//	                        the gap and defaulted-deadline deductions do not
//	                        apply (their residual is scored as basis risk);
//	                        the unit and edge-rule deductions do.
//	complement              starts at 0.97: one further inference (a partition
//	                        or a polarity flip) on top of the equal terms.
//	implies                 starts at 0.93: a containment inference between
//	                        two different thresholds. Both derived types take
//	                        the equivalent deductions (or the given-facts ones
//	                        when facts placed them) and lose a further 0.02
//	                        when a fact took part.
//	related                 0.80 when every veto is a known difference; 0.50
//	                        when any veto comes from an unknown term (marked
//	                        uncertain for the LLM).
//	unrelated               0.95 when blocked on a known difference (category,
//	                        subject, deadline gap); 0.50 when blocked because a
//	                        term was unknown.
//
// Nothing goes below 0.5: a long list of caveats never reads as "no confidence".
var (
	confEquivalentBase  = decimal.RequireFromString("0.99")
	confGivenFactsBase  = decimal.RequireFromString("0.97")
	confComplementBase  = decimal.RequireFromString("0.97")
	confImpliesBase     = decimal.RequireFromString("0.93")
	confPerHourGap      = decimal.RequireFromString("0.01")
	confUnknownMinor    = decimal.RequireFromString("0.02")
	confDefaultedDeadl  = decimal.RequireFromString("0.05")
	confFactApplied     = decimal.RequireFromString("0.02")
	confFloor           = decimal.RequireFromString("0.5")
	confRelatedKnown    = decimal.RequireFromString("0.8")
	confUncertain       = decimal.RequireFromString("0.5")
	confUnrelatedKnown  = decimal.RequireFromString("0.95")
	confUnrelatedUnsure = confUncertain
)

// verdict is a classification before it is copied into a Relationship.
type verdict struct {
	typ         canonical.RelationshipType
	confidence  decimal.Decimal
	explanation string
	facts       []canonical.FactUse
	derived     *derivation // set for derived types; nil otherwise
}

// classify picks the type, confidence and explanation (D-017, D-019). The
// explanation always names the fields, and the facts, that decided the verdict.
func classify(a, b canonical.Instrument, blocked *blockReason, vetoes []canonical.Veto, f facts.Set) verdict {
	switch {
	case blocked != nil:
		conf := confUnrelatedKnown
		if strings.Contains(blocked.detail, "unknown") {
			conf = confUnrelatedUnsure
		}
		return verdict{typ: canonical.RelUnrelated, confidence: conf,
			explanation: fmt.Sprintf("unrelated: blocked on %s (%s).", blocked.field, blocked.detail)}
	case len(vetoes) == 0:
		// D-109: try Fed same-metric pairs first, even when there are no vetoes.
		// Exact matches on the Fed grid should report equivalent_given_facts to document
		// that the grid constraint was checked.
		if a.Category == canonical.CategoryFed && b.Category == canonical.CategoryFed &&
			a.Terms.Metric == b.Terms.Metric && a.Terms.Metric != canonical.Unknown {
			if d, _ := deriveSameMetricFed(a, b, vetoes, f); d != nil {
				return verdict{typ: d.typ, confidence: derivedConfidence(a, b, d), explanation: derivedExplanation(a, b, vetoes, d),
					facts: d.facts, derived: d}
			}
		}
		return verdict{typ: canonical.RelEquivalent, confidence: equivalentConfidence(a, b), explanation: equivalentExplanation(a, b)}
	}
	d, note := derive(a, b, vetoes, f)
	if d == nil {
		return verdict{typ: canonical.RelRelated, confidence: relatedConfidence(vetoes),
			explanation: relatedExplanation(a, b, vetoes, note), facts: note.facts}
	}
	return verdict{typ: d.typ, confidence: derivedConfidence(a, b, d), explanation: derivedExplanation(a, b, vetoes, d),
		facts: d.facts, derived: d}
}

func relatedConfidence(vetoes []canonical.Veto) decimal.Decimal {
	if uncertain(vetoes) {
		return confUncertain
	}
	return confRelatedKnown
}

func vetoList(vetoes []canonical.Veto) string {
	parts := make([]string, 0, len(vetoes))
	for _, v := range vetoes {
		parts = append(parts, fmt.Sprintf("%s (%s)", v.Field, v.Detail))
	}
	return strings.Join(parts, ", ")
}

func relatedExplanation(a, b canonical.Instrument, vetoes []canonical.Veto, note derivationNote) string {
	s := fmt.Sprintf("related: same subject %s with deadlines %s apart (within %s), but vetoed on %s.",
		a.Terms.Subject, deadlineGap(a, b).Round(time.Minute), BlockWindow, vetoList(vetoes))
	if note.reason != "" {
		s += " Facts: " + note.reason + "."
	}
	if uncertain(vetoes) {
		s += " Uncertain: at least one vetoed term is unknown, so the pair cannot be equivalent until a human or the LLM verifier resolves it."
	}
	return s
}

// derivedConfidence applies the table above to a derived verdict.
func derivedConfidence(a, b canonical.Instrument, d *derivation) decimal.Decimal {
	var conf decimal.Decimal
	switch d.typ {
	case canonical.RelEquivalentGivenFacts:
		conf = confGivenFactsBase
	case canonical.RelComplement:
		conf = confComplementBase
	default:
		conf = confImpliesBase
	}
	conf = conf.Sub(minorDeductions(a, b))
	if !d.meeting {
		conf = conf.Sub(deadlineDeductions(a, b))
	}
	if len(d.facts) > 0 && d.typ != canonical.RelEquivalentGivenFacts {
		conf = conf.Sub(confFactApplied)
	}
	return clampConfidence(conf)
}

func derivedExplanation(a, b canonical.Instrument, vetoes []canonical.Veto, d *derivation) string {
	t := a.Terms
	var head string
	switch d.typ {
	case canonical.RelEquivalentGivenFacts:
		head = "equivalent_given_facts: the same payout once the facts are applied"
	case canonical.RelComplement:
		head = fmt.Sprintf("complement: YES on %s pays exactly when NO on %s pays", a.ID, b.ID)
	default:
		from, to := a.ID, b.ID
		if !d.forward {
			from, to = b.ID, a.ID
		}
		head = fmt.Sprintf("implies: YES on %s forces YES on %s, not the reverse", from, to)
	}
	s := fmt.Sprintf("%s. Shared: subject %s, source %s, unit %s; deadlines %s and %s are %s apart. Differs on %s. Reasoning: %s.",
		head, t.Subject, t.ResolutionSource, t.Unit,
		t.Deadline.UTC().Format(time.RFC3339), b.Terms.Deadline.UTC().Format(time.RFC3339), deadlineGap(a, b).Round(time.Minute),
		vetoList(vetoes), d.detail)
	if len(d.facts) > 0 {
		ids := make([]string, 0, len(d.facts))
		for _, fu := range d.facts {
			ids = append(ids, fmt.Sprintf("%s=%s (as of %s)", fu.Key, fu.Value, fu.AsOf.Format(facts.DateLayout)))
		}
		s += " Facts used: " + strings.Join(ids, ", ") + "."
	}
	if c := caveats(a, b); len(c) > 0 {
		s += " Caveats: " + strings.Join(c, "; ") + "."
	}
	return s
}

// equivalentConfidence applies the equivalent row of the table above.
func equivalentConfidence(a, b canonical.Instrument) decimal.Decimal {
	return clampConfidence(confEquivalentBase.Sub(minorDeductions(a, b)).Sub(deadlineDeductions(a, b)))
}

// minorDeductions: 0.02 per side for an unknown unit and for unknown edge rules.
func minorDeductions(a, b canonical.Instrument) decimal.Decimal {
	d := decimal.Zero
	for _, in := range []canonical.Instrument{a, b} {
		if in.Terms.Unit == canonical.Unknown {
			d = d.Add(confUnknownMinor)
		}
		if in.Terms.EdgeRules == canonical.Unknown {
			d = d.Add(confUnknownMinor)
		}
	}
	return d
}

// deadlineDeductions: 0.01 per full hour of gap and 0.05 per defaulted deadline.
func deadlineDeductions(a, b canonical.Instrument) decimal.Decimal {
	hours := int64(deadlineGap(a, b) / time.Hour)
	d := confPerHourGap.Mul(decimal.NewFromInt(hours))
	for _, in := range []canonical.Instrument{a, b} {
		if in.Quality.Has(canonical.QualityDeadlineDefaulted) {
			d = d.Add(confDefaultedDeadl)
		}
	}
	return d
}

// clampConfidence applies the floor so a long list of caveats never reads as "no confidence".
func clampConfidence(conf decimal.Decimal) decimal.Decimal {
	if conf.LessThan(confFloor) {
		return confFloor
	}
	return conf
}

func equivalentExplanation(a, b canonical.Instrument) string {
	t := a.Terms
	threshold := termValue(t.Threshold)
	if t.Comparator == canonical.ComparatorBetween {
		threshold += " and " + termValue(t.ThresholdHigh)
	}
	s := fmt.Sprintf("equivalent: both resolve on %s %s %s %s %s per %s with %s polarity; deadlines %s and %s are %s apart (within %s).",
		t.Subject, t.Metric, t.Comparator, threshold, t.Unit, t.ResolutionSource, t.Polarity,
		t.Deadline.UTC().Format(time.RFC3339), b.Terms.Deadline.UTC().Format(time.RFC3339),
		deadlineGap(a, b).Round(time.Minute), DeadlineTolerance)
	if c := caveats(a, b); len(c) > 0 {
		s += " Caveats: " + strings.Join(c, "; ") + "."
	}
	return s
}

// caveats lists the uncertainties the confidence deductions stand for.
func caveats(a, b canonical.Instrument) []string {
	var c []string
	if a.Terms.Unit == canonical.Unknown || b.Terms.Unit == canonical.Unknown {
		c = append(c, "unit unknown")
	}
	if a.Terms.EdgeRules == canonical.Unknown || b.Terms.EdgeRules == canonical.Unknown {
		c = append(c, "edge rules not compared")
	}
	if a.Quality.Has(canonical.QualityDeadlineDefaulted) || b.Quality.Has(canonical.QualityDeadlineDefaulted) {
		c = append(c, "a deadline was taken from the close time")
	}
	return c
}

// buildEvidence constructs the MatcherEvidence for a verdict (UI-20, D-115).
// It records the prior, per-term contributions, total, and for near misses
// (pairs with vetoes), the pre-veto score and required score.
func buildEvidence(a, b canonical.Instrument, v verdict, vetoes []canonical.Veto) canonical.MatcherEvidence {
	// Determine the prior based on the verdict type
	var prior decimal.Decimal
	switch v.typ {
	case canonical.RelEquivalent:
		prior = confEquivalentBase
	case canonical.RelEquivalentGivenFacts:
		prior = confGivenFactsBase
	case canonical.RelComplement:
		prior = confComplementBase
	case canonical.RelImplies:
		prior = confImpliesBase
	case canonical.RelRelated:
		prior = confRelatedKnown
		if uncertain(vetoes) {
			prior = confUncertain
		}
	case canonical.RelUnrelated:
		// For unrelated, check if it's due to unknown
		prior = confUnrelatedKnown
		// The blocked reason would contain "unknown" if uncertain
		// We don't have direct access to blocked here, so we use the confidence
		if v.confidence.Equal(confUncertain) {
			prior = confUnrelatedUnsure
		}
	}

	contributions := []canonical.ContributionTerm{
		{Term: "prior", Value: prior},
	}

	// Add minor deductions (unknown unit, unknown edge rules)
	if a.Terms.Unit == canonical.Unknown {
		contributions = append(contributions, canonical.ContributionTerm{
			Term:  "unknown_unit_a",
			Value: confUnknownMinor.Neg(),
		})
	}
	if b.Terms.Unit == canonical.Unknown {
		contributions = append(contributions, canonical.ContributionTerm{
			Term:  "unknown_unit_b",
			Value: confUnknownMinor.Neg(),
		})
	}
	if a.Terms.EdgeRules == canonical.Unknown {
		contributions = append(contributions, canonical.ContributionTerm{
			Term:  "unknown_edge_rules_a",
			Value: confUnknownMinor.Neg(),
		})
	}
	if b.Terms.EdgeRules == canonical.Unknown {
		contributions = append(contributions, canonical.ContributionTerm{
			Term:  "unknown_edge_rules_b",
			Value: confUnknownMinor.Neg(),
		})
	}

	// Add deadline deductions (only for equivalent and derived types where meeting=false)
	applyDeadlineDeductions := v.typ == canonical.RelEquivalent ||
		(v.derived != nil && !v.derived.meeting)

	if applyDeadlineDeductions {
		hours := int64(deadlineGap(a, b) / time.Hour)
		if hours > 0 {
			contributions = append(contributions, canonical.ContributionTerm{
				Term:  "deadline_gap_hours",
				Value: confPerHourGap.Mul(decimal.NewFromInt(hours)).Neg(),
			})
		}
		if a.Quality.Has(canonical.QualityDeadlineDefaulted) {
			contributions = append(contributions, canonical.ContributionTerm{
				Term:  "deadline_defaulted_a",
				Value: confDefaultedDeadl.Neg(),
			})
		}
		if b.Quality.Has(canonical.QualityDeadlineDefaulted) {
			contributions = append(contributions, canonical.ContributionTerm{
				Term:  "deadline_defaulted_b",
				Value: confDefaultedDeadl.Neg(),
			})
		}
	}

	// Add fact-applied deduction for complement/implies
	if v.derived != nil && len(v.facts) > 0 && v.typ != canonical.RelEquivalentGivenFacts {
		contributions = append(contributions, canonical.ContributionTerm{
			Term:  "fact_applied",
			Value: confFactApplied.Neg(),
		})
	}

	// Build the evidence
	evidence := canonical.MatcherEvidence{
		Prior:         prior,
		Contributions: contributions,
		Total:         v.confidence,
	}

	// For near misses (pairs with vetoes that aren't unrelated), compute pre-veto score
	// A near miss is a pair that has vetoes but could theoretically be equivalent if the vetoes didn't exist
	if len(vetoes) > 0 && v.typ != canonical.RelUnrelated {
		preVetoScore := equivalentConfidence(a, b)
		requiredScore := decimal.RequireFromString("0.9") // MIN_ROUTE_CONFIDENCE
		evidence.PreVetoScore = &preVetoScore
		evidence.RequiredScore = &requiredScore
	}

	return evidence
}
