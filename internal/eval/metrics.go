package eval

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// EquivalentPrecisionTarget is NFR-06: precision at `equivalent` on the golden
// set must be at least 0.95 (D-020). Recall is reported with no target.
var EquivalentPrecisionTarget = decimal.RequireFromString("0.95")

// metricPlaces is how many decimals a reported ratio keeps.
const metricPlaces = 4

// Predictor returns the matcher's type for a labelled pair, or false when the
// pair cannot be classified (an instrument is not in the store).
type Predictor func(aID, bID string) (canonical.RelationshipType, bool)

// TypeMetrics is the confusion count for one relationship type, treating that
// type as the positive class.
type TypeMetrics struct {
	Type      canonical.RelationshipType
	Labelled  int // pairs the human labelled with this type (support)
	Predicted int // pairs the matcher said are this type
	TP        int // labelled and predicted
	FP        int // predicted, labelled otherwise
	FN        int // labelled, predicted otherwise
}

// Precision is TP / (TP + FP); ok is false when nothing was predicted.
func (m TypeMetrics) Precision() (decimal.Decimal, bool) { return ratio(m.TP, m.TP+m.FP) }

// Recall is TP / (TP + FN); ok is false when nothing was labelled.
func (m TypeMetrics) Recall() (decimal.Decimal, bool) { return ratio(m.TP, m.TP+m.FN) }

// Outcome is one evaluated label with the matcher's answer.
type Outcome struct {
	Label     Label
	Predicted canonical.RelationshipType
	Correct   bool
}

// Report is the result of one evaluation.
type Report struct {
	Method    canonical.Method
	Evaluated int // labels with a prediction
	Correct   int
	// ByType has one entry per relationship type, strongest first.
	ByType []TypeMetrics
	// Outcomes lists the evaluated labels in file order.
	Outcomes []Outcome
	// Missing lists labels no prediction was available for.
	Missing []Label
}

// Evaluate scores labels against predict, rules only (rules+LLM is M3).
// Labels whose pair predict cannot classify are listed in Missing and
// excluded from every count, so a stale label never counts as a miss.
func Evaluate(labels Labels, predict Predictor) Report {
	r := Report{Method: canonical.MethodRules}
	byType := map[canonical.RelationshipType]*TypeMetrics{}
	for _, t := range canonical.RelationshipTypes() {
		byType[t] = &TypeMetrics{Type: t}
	}
	for _, l := range labels.Labels {
		got, ok := predict(l.AID, l.BID)
		if !ok {
			r.Missing = append(r.Missing, l)
			continue
		}
		r.Evaluated++
		correct := got == l.Label
		if correct {
			r.Correct++
		}
		r.Outcomes = append(r.Outcomes, Outcome{Label: l, Predicted: got, Correct: correct})
		want := byType[l.Label]
		want.Labelled++
		pred, known := byType[got]
		if !known {
			// A type this build does not know: counts as a miss for the label.
			want.FN++
			continue
		}
		pred.Predicted++
		if correct {
			want.TP++
		} else {
			want.FN++
			pred.FP++
		}
	}
	for _, t := range canonical.RelationshipTypes() {
		r.ByType = append(r.ByType, *byType[t])
	}
	return r
}

// Metrics returns the counts for one type (the zero value for an unknown type).
func (r Report) Metrics(t canonical.RelationshipType) TypeMetrics {
	for _, m := range r.ByType {
		if m.Type == t {
			return m
		}
	}
	return TypeMetrics{Type: t}
}

// Accuracy is Correct / Evaluated; ok is false when nothing was evaluated.
func (r Report) Accuracy() (decimal.Decimal, bool) { return ratio(r.Correct, r.Evaluated) }

// EquivalentPrecision is the NFR-06 figure; ok is false when the matcher
// predicted no `equivalent` pair among the labelled ones.
func (r Report) EquivalentPrecision() (decimal.Decimal, bool) {
	return r.Metrics(canonical.RelEquivalent).Precision()
}

// MeetsTarget reports whether the NFR-06 target is met. measured is false
// when there is no `equivalent` prediction to measure precision on; met is
// then false too.
func (r Report) MeetsTarget() (met, measured bool) {
	p, ok := r.EquivalentPrecision()
	if !ok {
		return false, false
	}
	return p.GreaterThanOrEqual(EquivalentPrecisionTarget), true
}

// String renders the report for the CLI.
func (r Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "golden set (%s): %d labels evaluated, %d correct (accuracy %s)", r.Method, r.Evaluated, r.Correct, fmtRatio(r.Accuracy()))
	if len(r.Missing) > 0 {
		fmt.Fprintf(&b, ", %d not evaluated", len(r.Missing))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%-24s %8s %9s %4s %4s %4s %9s %9s\n", "type", "labelled", "predicted", "TP", "FP", "FN", "precision", "recall")
	for _, m := range r.ByType {
		fmt.Fprintf(&b, "%-24s %8d %9d %4d %4d %4d %9s %9s\n", m.Type, m.Labelled, m.Predicted, m.TP, m.FP, m.FN, fmtRatio(m.Precision()), fmtRatio(m.Recall()))
	}
	eq := r.Metrics(canonical.RelEquivalent)
	switch met, measured := r.MeetsTarget(); {
	case !measured:
		fmt.Fprintf(&b, "equivalent precision: n/a (no equivalent prediction among the labelled pairs); target >= %s (NFR-06)\n", EquivalentPrecisionTarget)
	case met:
		fmt.Fprintf(&b, "equivalent precision %s on %d predictions: target >= %s met (NFR-06)\n", fmtRatio(eq.Precision()), eq.Predicted, EquivalentPrecisionTarget)
	default:
		fmt.Fprintf(&b, "equivalent precision %s on %d predictions: BELOW target %s (NFR-06)\n", fmtRatio(eq.Precision()), eq.Predicted, EquivalentPrecisionTarget)
	}
	b.WriteString("rules+llm: not evaluated (M3)\n")
	for _, l := range r.Missing {
		fmt.Fprintf(&b, "not evaluated: %s / %s (no prediction: instrument not found)\n", l.AID, l.BID)
	}
	return b.String()
}

// Detail lists every outcome, one line each, for `equinox eval --verbose`.
func (r Report) Detail() string {
	var b strings.Builder
	for _, o := range r.Outcomes {
		mark := "ok  "
		if !o.Correct {
			mark = "MISS"
		}
		fmt.Fprintf(&b, "%s label=%s rules=%s %s / %s\n", mark, o.Label.Label, o.Predicted, o.Label.AID, o.Label.BID)
	}
	return b.String()
}

// ratio is num/den rounded to metricPlaces; ok is false when den is zero.
func ratio(num, den int) (decimal.Decimal, bool) {
	if den == 0 {
		return decimal.Zero, false
	}
	return decimal.NewFromInt(int64(num)).DivRound(decimal.NewFromInt(int64(den)), metricPlaces), true
}

// fmtRatio renders a ratio, or n/a.
func fmtRatio(d decimal.Decimal, ok bool) string {
	if !ok {
		return "n/a"
	}
	return d.StringFixed(metricPlaces)
}
