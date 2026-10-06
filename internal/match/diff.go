package match

import (
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TermFields lists every term field the contract diff reports, in order.
var TermFields = []string{
	"subject", "metric", "comparator", "threshold", "threshold_high", "unit",
	"deadline", "deadline_tz", "resolution_source", "edge_rules", "polarity",
}

// Diff compares every term field of a and b (FR-09). A field is unknown when
// either side is unknown, match when both are known and equal, else differ.
// Deadlines match only when they are the same instant; the 24 h tolerance is
// applied by the vetoes, not here, so a tolerated gap still shows as differ.
func Diff(a, b canonical.Terms) []canonical.FieldDiff {
	values := func(t canonical.Terms) map[string]string {
		return map[string]string{
			"subject":           t.Subject,
			"metric":            t.Metric,
			"comparator":        string(t.Comparator),
			"threshold":         termValue(t.Threshold),
			"threshold_high":    thresholdHigh(t),
			"unit":              t.Unit,
			"deadline":          deadlineValue(t.Deadline),
			"deadline_tz":       t.DeadlineTZ,
			"resolution_source": t.ResolutionSource,
			"edge_rules":        t.EdgeRules,
			"polarity":          string(t.Polarity),
		}
	}
	va, vb := values(a), values(b)
	out := make([]canonical.FieldDiff, 0, len(TermFields))
	for _, f := range TermFields {
		out = append(out, canonical.FieldDiff{Field: f, A: va[f], B: vb[f], Status: status(va[f], vb[f])})
	}
	return out
}

// thresholdHigh renders the upper bound: "n/a" for non-range comparators so
// two threshold markets match on it rather than both being unknown.
func thresholdHigh(t canonical.Terms) string {
	if t.Comparator != canonical.ComparatorBetween && t.Comparator != canonical.ComparatorUnknown && t.ThresholdHigh == nil {
		return "n/a"
	}
	return termValue(t.ThresholdHigh)
}

func deadlineValue(d *time.Time) string {
	if d == nil {
		return canonical.Unknown
	}
	return d.UTC().Format(time.RFC3339)
}

func status(a, b string) canonical.DiffStatus {
	switch {
	case a == canonical.Unknown || b == canonical.Unknown:
		return canonical.DiffUnknown
	case a == b:
		return canonical.DiffMatch
	}
	return canonical.DiffDiffer
}
