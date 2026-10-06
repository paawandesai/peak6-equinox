package match

import (
	"fmt"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// DeadlineTolerance is the generic deadline tolerance (D-019: deadlines more
// than 24 h apart are a hard veto). "Within 24 h" is read strictly, so two
// daily markets exactly one day apart are never equivalent (D-058).
const DeadlineTolerance = 24 * time.Hour

// exactDeadlineCategories lists categories whose deadline is the measurement
// instant itself (a price read at 5 PM is a different bet from one read at
// noon), so equivalence needs identical deadlines rather than the generic
// tolerance (D-058). This is stricter than D-019, never looser.
var exactDeadlineCategories = map[canonical.Category]bool{canonical.CategoryCrypto: true}

// Veto codes. A *_mismatch code means both sides are known and differ; a
// *_unknown code means at least one side could not be parsed (business rule 2:
// an unknown on a vetoed field cannot be equivalent).
const (
	VetoSubjectMismatch    = "subject_mismatch"
	VetoSubjectUnknown     = "subject_unknown"
	VetoMetricMismatch     = "metric_mismatch"
	VetoMetricUnknown      = "metric_unknown"
	VetoComparatorMismatch = "comparator_mismatch"
	VetoComparatorUnknown  = "comparator_unknown"
	VetoThresholdMismatch  = "threshold_mismatch"
	VetoThresholdUnknown   = "threshold_unknown"
	VetoDeadlineGap        = "deadline_gap"      // further apart than DeadlineTolerance
	VetoDeadlineMismatch   = "deadline_mismatch" // not the same instant, in an exact-deadline category
	VetoDeadlineUnknown    = "deadline_unknown"
	VetoSourceMismatch     = "source_mismatch"
	VetoSourceUnknown      = "source_unknown"
	VetoPolarityMismatch   = "polarity_mismatch"
	VetoPolarityUnresolved = "polarity_unresolved"
)

// vetoes applies the hard vetoes (FR-08, D-019) to every vetoed field and
// returns one Veto per failure, in field order. The vetoed fields are the
// proposition (subject, metric, comparator, threshold), the resolution window
// (deadline, within DeadlineTolerance), the resolution source and the polarity:
// together they are what D-017 requires to be identical for equivalence.
func vetoes(a, b canonical.Instrument) []canonical.Veto {
	var out []canonical.Veto
	add := func(code, field, detail string) {
		out = append(out, canonical.Veto{Code: code, Field: field, Detail: detail})
	}
	stringField := func(field, mismatch, unknown, va, vb string) {
		switch {
		case va == canonical.Unknown || vb == canonical.Unknown:
			add(unknown, field, fmt.Sprintf("%s vs %s", va, vb))
		case va != vb:
			add(mismatch, field, fmt.Sprintf("%s vs %s", va, vb))
		}
	}
	ta, tb := a.Terms, b.Terms
	stringField("subject", VetoSubjectMismatch, VetoSubjectUnknown, ta.Subject, tb.Subject)
	stringField("metric", VetoMetricMismatch, VetoMetricUnknown, ta.Metric, tb.Metric)
	stringField("comparator", VetoComparatorMismatch, VetoComparatorUnknown, string(ta.Comparator), string(tb.Comparator))

	switch {
	case ta.Threshold == nil || tb.Threshold == nil:
		add(VetoThresholdUnknown, "threshold", fmt.Sprintf("%s vs %s", termValue(ta.Threshold), termValue(tb.Threshold)))
	case !ta.Threshold.Equal(*tb.Threshold):
		add(VetoThresholdMismatch, "threshold", fmt.Sprintf("%s vs %s", ta.Threshold, tb.Threshold))
	case ta.Comparator == canonical.ComparatorBetween && tb.Comparator == canonical.ComparatorBetween:
		// The upper bound only exists for ranges; a comparator mismatch already covers range vs threshold.
		switch {
		case ta.ThresholdHigh == nil || tb.ThresholdHigh == nil:
			add(VetoThresholdUnknown, "threshold_high", fmt.Sprintf("%s vs %s", termValue(ta.ThresholdHigh), termValue(tb.ThresholdHigh)))
		case !ta.ThresholdHigh.Equal(*tb.ThresholdHigh):
			add(VetoThresholdMismatch, "threshold_high", fmt.Sprintf("%s vs %s", ta.ThresholdHigh, tb.ThresholdHigh))
		}
	}

	switch {
	case ta.Deadline == nil || tb.Deadline == nil:
		add(VetoDeadlineUnknown, "deadline", "deadline unknown on at least one side")
	default:
		gap := deadlineGap(a, b)
		stated := fmt.Sprintf("%s vs %s", ta.Deadline.UTC().Format(time.RFC3339), tb.Deadline.UTC().Format(time.RFC3339))
		switch {
		case gap >= DeadlineTolerance:
			add(VetoDeadlineGap, "deadline", fmt.Sprintf("%s: %s apart, not within %s", stated, gap, DeadlineTolerance))
		case gap > 0 && exactDeadlineCategories[a.Category] && exactDeadlineCategories[b.Category]:
			add(VetoDeadlineMismatch, "deadline", fmt.Sprintf("%s: %s apart; %s deadlines are the measurement instant and must be identical",
				stated, gap, a.Category))
		}
	}

	stringField("resolution_source", VetoSourceMismatch, VetoSourceUnknown, ta.ResolutionSource, tb.ResolutionSource)
	stringField("polarity", VetoPolarityMismatch, VetoPolarityUnresolved, string(ta.Polarity), string(tb.Polarity))
	return out
}

// uncertain reports whether any veto is due to an unknown term rather than a
// known difference. Such pairs are marked uncertain for LLM adjudication.
func uncertain(vetoes []canonical.Veto) bool {
	for _, v := range vetoes {
		switch v.Code {
		case VetoSubjectUnknown, VetoMetricUnknown, VetoComparatorUnknown, VetoThresholdUnknown,
			VetoDeadlineUnknown, VetoSourceUnknown, VetoPolarityUnresolved:
			return true
		}
	}
	return false
}
