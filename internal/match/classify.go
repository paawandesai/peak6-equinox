package match

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Confidence is the matcher's confidence in the type it returns, in [0,1].
// The numbers are deliberate and conservative (D-020); they are not learned.
//
//	equivalent  starts at 0.99 (rules can never prove two texts identical) and
//	            loses 0.01 per full hour of deadline gap (up to 24 h), 0.02 for
//	            an unknown unit or unknown edge rules on either side, and 0.05
//	            when a deadline was defaulted from the close time. Never below 0.5.
//	related     0.80 when every veto is a known difference; 0.50 when any veto
//	            comes from an unknown term (marked uncertain for the LLM).
//	unrelated   0.95 when blocked on a known difference (category, subject,
//	            deadline gap); 0.50 when blocked because a term was unknown.
var (
	confEquivalentBase  = decimal.RequireFromString("0.99")
	confPerHourGap      = decimal.RequireFromString("0.01")
	confUnknownMinor    = decimal.RequireFromString("0.02")
	confDefaultedDeadl  = decimal.RequireFromString("0.05")
	confFloor           = decimal.RequireFromString("0.5")
	confRelatedKnown    = decimal.RequireFromString("0.8")
	confUncertain       = decimal.RequireFromString("0.5")
	confUnrelatedKnown  = decimal.RequireFromString("0.95")
	confUnrelatedUnsure = confUncertain
)

// classify picks the type, confidence and explanation (D-017, D-019). The
// explanation always names the fields that decided the verdict.
func classify(a, b canonical.Instrument, blocked *blockReason, vetoes []canonical.Veto) (canonical.RelationshipType, decimal.Decimal, string) {
	switch {
	case blocked != nil:
		conf := confUnrelatedKnown
		if strings.Contains(blocked.detail, "unknown") {
			conf = confUnrelatedUnsure
		}
		return canonical.RelUnrelated, conf, fmt.Sprintf("unrelated: blocked on %s (%s).", blocked.field, blocked.detail)
	case len(vetoes) > 0:
		return canonical.RelRelated, relatedConfidence(vetoes), relatedExplanation(a, b, vetoes)
	}
	return canonical.RelEquivalent, equivalentConfidence(a, b), equivalentExplanation(a, b)
}

func relatedConfidence(vetoes []canonical.Veto) decimal.Decimal {
	if uncertain(vetoes) {
		return confUncertain
	}
	return confRelatedKnown
}

func relatedExplanation(a, b canonical.Instrument, vetoes []canonical.Veto) string {
	parts := make([]string, 0, len(vetoes))
	for _, v := range vetoes {
		parts = append(parts, fmt.Sprintf("%s (%s)", v.Field, v.Detail))
	}
	s := fmt.Sprintf("related: same subject %s with deadlines %s apart (within %s), but vetoed on %s.",
		a.Terms.Subject, deadlineGap(a, b).Round(time.Minute), BlockWindow, strings.Join(parts, ", "))
	if uncertain(vetoes) {
		s += " Uncertain: at least one vetoed term is unknown, so the pair cannot be equivalent until a human or the LLM verifier resolves it."
	}
	return s
}

func equivalentConfidence(a, b canonical.Instrument) decimal.Decimal {
	conf := confEquivalentBase
	hours := int64(deadlineGap(a, b) / time.Hour)
	conf = conf.Sub(confPerHourGap.Mul(decimal.NewFromInt(hours)))
	for _, in := range []canonical.Instrument{a, b} {
		if in.Terms.Unit == canonical.Unknown {
			conf = conf.Sub(confUnknownMinor)
		}
		if in.Terms.EdgeRules == canonical.Unknown {
			conf = conf.Sub(confUnknownMinor)
		}
		if in.Quality.Has(canonical.QualityDeadlineDefaulted) {
			conf = conf.Sub(confDefaultedDeadl)
		}
	}
	return clampConfidence(conf)
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
	var caveats []string
	if t.Unit == canonical.Unknown || b.Terms.Unit == canonical.Unknown {
		caveats = append(caveats, "unit unknown")
	}
	if t.EdgeRules == canonical.Unknown || b.Terms.EdgeRules == canonical.Unknown {
		caveats = append(caveats, "edge rules not compared")
	}
	if a.Quality.Has(canonical.QualityDeadlineDefaulted) || b.Quality.Has(canonical.QualityDeadlineDefaulted) {
		caveats = append(caveats, "a deadline was taken from the close time")
	}
	if len(caveats) > 0 {
		s += " Caveats: " + strings.Join(caveats, "; ") + "."
	}
	return s
}
