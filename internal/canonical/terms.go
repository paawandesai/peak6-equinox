package canonical

import (
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// Comparator is how a proposition compares its metric with the threshold.
type Comparator string

// Comparators. ComparatorUnknown is recorded when the text cannot be parsed.
const (
	ComparatorGT      Comparator = "gt"
	ComparatorGTE     Comparator = "gte"
	ComparatorLT      Comparator = "lt"
	ComparatorLTE     Comparator = "lte"
	ComparatorEQ      Comparator = "eq"
	ComparatorBetween Comparator = "between"
	ComparatorUnknown Comparator = Unknown
)

// Valid reports whether c is a known comparator or ComparatorUnknown.
func (c Comparator) Valid() bool {
	switch c {
	case ComparatorGT, ComparatorGTE, ComparatorLT, ComparatorLTE, ComparatorEQ, ComparatorBetween, ComparatorUnknown:
		return true
	}
	return false
}

// Polarity says whether an instrument's YES side pays when its stated
// proposition is true (positive) or when it is false (negative, e.g. "Will BTC
// NOT close above X"). Unresolvable polarity is a hard veto (D-019).
type Polarity string

// Polarities.
const (
	PolarityPositive Polarity = "positive"
	PolarityNegative Polarity = "negative"
	PolarityUnknown  Polarity = Unknown
)

// Valid reports whether p is a known polarity or PolarityUnknown.
func (p Polarity) Valid() bool {
	return p == PolarityPositive || p == PolarityNegative || p == PolarityUnknown
}

// Terms are the structured fields extracted from an instrument's title and
// rules (domain.md). String fields hold Unknown when unparsed; Threshold and
// Deadline are nil when unparsed.
type Terms struct {
	Subject    string     `json:"subject"`    // e.g. "BTC", "fed_upper_bound"
	Metric     string     `json:"metric"`     // e.g. "price", "rate"
	Comparator Comparator `json:"comparator"` // how Metric compares with Threshold
	// Threshold is the comparison value; for ComparatorBetween it is the lower bound.
	Threshold *decimal.Decimal `json:"threshold,omitempty"`
	// ThresholdHigh is the upper bound for ComparatorBetween, else nil.
	ThresholdHigh *decimal.Decimal `json:"threshold_high,omitempty"`
	Unit          string           `json:"unit"` // e.g. "USD", "percent", "bps"
	// Deadline is the UTC instant the proposition is evaluated at.
	Deadline *time.Time `json:"deadline,omitempty"`
	// DeadlineTZ is the timezone the venue states the deadline in (IANA name).
	DeadlineTZ       string   `json:"deadline_tz"`
	ResolutionSource string   `json:"resolution_source"`
	EdgeRules        string   `json:"edge_rules"` // early close, ties, cancellations
	Polarity         Polarity `json:"polarity"`
}

// UnknownTerms returns Terms with every field unknown.
func UnknownTerms() Terms {
	return Terms{
		Subject:          Unknown,
		Metric:           Unknown,
		Comparator:       ComparatorUnknown,
		Unit:             Unknown,
		DeadlineTZ:       Unknown,
		ResolutionSource: Unknown,
		EdgeRules:        Unknown,
		Polarity:         PolarityUnknown,
	}
}

// Validate checks enums are valid, no string term is empty (use Unknown),
// the deadline is UTC, and between-thresholds are ordered.
func (t Terms) Validate() error {
	var errs []error
	for _, f := range [...]struct{ name, value string }{
		{"subject", t.Subject}, {"metric", t.Metric}, {"unit", t.Unit}, {"deadline_tz", t.DeadlineTZ},
		{"resolution_source", t.ResolutionSource}, {"edge_rules", t.EdgeRules},
	} {
		if f.value == "" {
			errs = append(errs, fmt.Errorf("terms.%s is empty; record %q instead", f.name, Unknown))
		}
	}
	if !t.Comparator.Valid() {
		errs = append(errs, fmt.Errorf("terms.comparator %q is not valid", t.Comparator))
	}
	if !t.Polarity.Valid() {
		errs = append(errs, fmt.Errorf("terms.polarity %q is not valid", t.Polarity))
	}
	if t.Deadline != nil {
		if err := checkUTC("terms.deadline", *t.Deadline, false); err != nil {
			errs = append(errs, err)
		}
	}
	if t.Comparator == ComparatorBetween && t.Threshold != nil && t.ThresholdHigh != nil &&
		t.ThresholdHigh.LessThan(*t.Threshold) {
		errs = append(errs, errors.New("terms.threshold_high is below terms.threshold"))
	}
	if t.Comparator != ComparatorBetween && t.ThresholdHigh != nil {
		errs = append(errs, errors.New("terms.threshold_high is only allowed with comparator between"))
	}
	return errors.Join(errs...)
}
