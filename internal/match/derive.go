package match

import (
	"fmt"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// Derived types (D-017, domain.md): complement, implies and
// equivalent_given_facts are decided by reasoning about the two propositions
// as sets of outcomes of one shared measurement. That reasoning is only sound
// when the measurement itself is shared, so a derivation is attempted only
// when (derivable):
//
//   - no veto comes from an unknown term (business rule 2),
//   - subject, deadline and resolution source agree (the vetoes on them are
//     the shared-measurement vetoes; facts never override them), and
//   - the unit is known and the same on both sides, and both thresholds are
//     known (interval arithmetic needs numbers in one unit).
//
// A proposition is a set of values of the metric (an interval: "above x" is
// (x, +inf), "between lo and hi" is [lo, hi]) plus a polarity: positive YES
// pays inside the interval, negative YES pays outside it. The rules are then
// set algebra, with no tolerance: a one-cent difference stays a difference.
//
//	complement  YES_A is exactly the complement of YES_B: the same interval
//	            with opposite polarity, or two intervals that partition the
//	            line ((-inf, x] and (x, +inf)) with the same polarity.
//	implies     YES_A ⊆ YES_B (A forces B) or YES_B ⊆ YES_A (B forces A); the
//	            explanation names the direction. Equal sets are never
//	            reported here: with no veto the pair is equivalent, and with a
//	            veto (gt x positive vs lte x negative) business rule 1 keeps
//	            it related.

// derivation is a derived verdict and what it rests on.
type derivation struct {
	typ canonical.RelationshipType
	// forward is true when A forces B (implies only); false when B forces A.
	forward bool
	// facts are the facts the derivation applied, in the order consulted.
	facts []canonical.FactUse
	// grid is true when equality relied on the rate-step grid (facts.go).
	grid bool
	// meeting is true when both deadlines were confirmed against the meeting
	// fact (facts.go), which replaces the deadline confidence deductions.
	meeting bool
	// detail is the human-readable reasoning, appended to the explanation.
	detail string
}

// derivationNote is what a failed derivation still reports: the facts it
// consulted and why they did not close the gap. Both may be empty.
type derivationNote struct {
	facts  []canonical.FactUse
	reason string
}

// derive tries the derived types for a vetoed pair. It returns nil and a
// note when none applies.
func derive(a, b canonical.Instrument, vetoes []canonical.Veto, f facts.Set) (*derivation, derivationNote) {
	if !derivable(a.Terms, b.Terms, vetoes) {
		return nil, derivationNote{}
	}
	if hasVeto(vetoes, VetoMetricMismatch) {
		return deriveWithFacts(a, b, f)
	}
	// Try same-metric Fed pairs (both rate_change) with grid rounding (D-109).
	if d, note := deriveSameMetricFed(a, b, vetoes, f); d != nil || note.reason != "" {
		return d, note
	}
	pa, okA := propositionOf(a.Terms)
	pb, okB := propositionOf(b.Terms)
	if !okA || !okB {
		return nil, derivationNote{}
	}
	return deriveFrom(pa, pb, nil), derivationNote{}
}

// deriveFrom applies the set algebra to two propositions of one measurement.
// facts, when non-nil, are the facts that produced the propositions.
func deriveFrom(pa, pb proposition, facts []canonical.FactUse) *derivation {
	switch {
	case pa.complementOf(pb):
		return &derivation{typ: canonical.RelComplement, facts: facts,
			detail: fmt.Sprintf("YES on %s is exactly NO on %s", pa, pb)}
	case pa.subsetOf(pb):
		return &derivation{typ: canonical.RelImplies, forward: true, facts: facts,
			detail: fmt.Sprintf("%s forces %s", pa, pb)}
	case pb.subsetOf(pa):
		return &derivation{typ: canonical.RelImplies, forward: false, facts: facts,
			detail: fmt.Sprintf("%s forces %s", pb, pa)}
	}
	return nil
}

// derivable reports whether the vetoes leave a shared measurement to reason
// about (see the package comment above).
func derivable(ta, tb canonical.Terms, vetoes []canonical.Veto) bool {
	if uncertain(vetoes) {
		return false
	}
	for _, v := range vetoes {
		switch v.Code {
		case VetoSubjectMismatch, VetoDeadlineGap, VetoDeadlineMismatch, VetoSourceMismatch:
			return false
		}
	}
	return ta.Unit != canonical.Unknown && ta.Unit == tb.Unit && ta.Threshold != nil && tb.Threshold != nil
}

func hasVeto(vetoes []canonical.Veto, code string) bool {
	for _, v := range vetoes {
		if v.Code == code {
			return true
		}
	}
	return false
}

// interval is a set of metric values with optional open or closed ends. A nil
// bound is unbounded.
type interval struct {
	lo, hi         *decimal.Decimal
	loOpen, hiOpen bool
}

// proposition is the YES set of one instrument: an interval, taken as is for
// positive polarity and complemented for negative polarity.
type proposition struct {
	iv      interval
	negated bool
	label   string // "gt 4.00" style, for explanations
}

// propositionOf reads the proposition stated by t. It fails when the
// comparator is unknown, a needed threshold is missing or the polarity is
// unknown (those pairs never reach the derivations, but the check is cheap).
func propositionOf(t canonical.Terms) (proposition, bool) {
	if t.Threshold == nil || t.Polarity == canonical.PolarityUnknown {
		return proposition{}, false
	}
	x := *t.Threshold
	var iv interval
	switch t.Comparator {
	case canonical.ComparatorGT:
		iv = interval{lo: &x, loOpen: true}
	case canonical.ComparatorGTE:
		iv = interval{lo: &x}
	case canonical.ComparatorLT:
		iv = interval{hi: &x, hiOpen: true}
	case canonical.ComparatorLTE:
		iv = interval{hi: &x}
	case canonical.ComparatorEQ:
		iv = interval{lo: &x, hi: &x}
	case canonical.ComparatorBetween:
		if t.ThresholdHigh == nil {
			return proposition{}, false
		}
		hi := *t.ThresholdHigh
		iv = interval{lo: &x, hi: &hi}
	default:
		return proposition{}, false
	}
	return proposition{iv: iv, negated: t.Polarity == canonical.PolarityNegative, label: labelOf(iv)}, true
}

// String renders the proposition for explanations: "gt 4.00" or "not (gt 4.00)".
func (p proposition) String() string {
	if p.negated {
		return "not (" + p.label + ")"
	}
	return p.label
}

// shift moves the interval by d (used to place a change on a level ladder).
func (p proposition) shift(d decimal.Decimal) proposition {
	out := p
	if p.iv.lo != nil {
		lo := p.iv.lo.Add(d)
		out.iv.lo = &lo
	}
	if p.iv.hi != nil {
		hi := p.iv.hi.Add(d)
		out.iv.hi = &hi
	}
	out.label = labelOf(out.iv)
	return out
}

// labelOf renders an interval as a comparator phrase: "gt 4.00", "gte 4.25",
// "eq 0.25", "between 3.25 and 3.50", "lte -0.50".
func labelOf(iv interval) string {
	switch {
	case iv.lo != nil && iv.hi != nil && iv.lo.Equal(*iv.hi):
		return "eq " + iv.lo.String()
	case iv.lo != nil && iv.hi != nil:
		return fmt.Sprintf("between %s and %s", iv.lo, iv.hi)
	case iv.lo != nil && iv.loOpen:
		return "gt " + iv.lo.String()
	case iv.lo != nil:
		return "gte " + iv.lo.String()
	case iv.hi != nil && iv.hiOpen:
		return "lt " + iv.hi.String()
	case iv.hi != nil:
		return "lte " + iv.hi.String()
	}
	return "any value"
}

// complementOf reports whether YES_p is exactly the complement of YES_q.
func (p proposition) complementOf(q proposition) bool {
	if p.negated != q.negated {
		return p.iv.equal(q.iv)
	}
	return p.iv.partitionWith(q.iv)
}

// subsetOf reports whether YES_p ⊆ YES_q, strictly (equal sets are excluded).
func (p proposition) subsetOf(q proposition) bool {
	var subset bool
	switch {
	case !p.negated && !q.negated:
		subset = p.iv.subsetOf(q.iv)
	case p.negated && q.negated:
		subset = q.iv.subsetOf(p.iv) // not P ⊆ not Q  ⟺  Q ⊆ P
	case !p.negated && q.negated:
		subset = p.iv.disjointFrom(q.iv) // P ⊆ not Q  ⟺  P ∩ Q = ∅
	default:
		subset = p.iv.coversWith(q.iv) // not P ⊆ Q  ⟺  P ∪ Q = everything
	}
	return subset && !p.equal(q)
}

// equal reports whether YES_p and YES_q are the same set.
func (p proposition) equal(q proposition) bool {
	if p.negated == q.negated {
		return p.iv.equal(q.iv)
	}
	return p.iv.partitionWith(q.iv)
}

// cmpLower compares two lower bounds: -1 when i's is lower (i extends
// further down), 0 when equal, 1 when i's is higher.
func cmpLower(i, j interval) int {
	switch {
	case i.lo == nil && j.lo == nil:
		return 0
	case i.lo == nil:
		return -1
	case j.lo == nil:
		return 1
	}
	if c := i.lo.Cmp(*j.lo); c != 0 {
		return c
	}
	switch {
	case i.loOpen == j.loOpen:
		return 0
	case i.loOpen: // (x is above [x
		return 1
	}
	return -1
}

// cmpUpper compares two upper bounds: -1 when i's is lower, 0 equal, 1 when
// i's is higher (i extends further up).
func cmpUpper(i, j interval) int {
	switch {
	case i.hi == nil && j.hi == nil:
		return 0
	case i.hi == nil:
		return 1
	case j.hi == nil:
		return -1
	}
	if c := i.hi.Cmp(*j.hi); c != 0 {
		return c
	}
	switch {
	case i.hiOpen == j.hiOpen:
		return 0
	case i.hiOpen: // x) is below x]
		return -1
	}
	return 1
}

func (i interval) equal(j interval) bool { return cmpLower(i, j) == 0 && cmpUpper(i, j) == 0 }

// subsetOf reports whether i ⊆ j.
func (i interval) subsetOf(j interval) bool { return cmpLower(i, j) >= 0 && cmpUpper(i, j) <= 0 }

// disjointFrom reports whether i ∩ j is empty.
func (i interval) disjointFrom(j interval) bool {
	return i.endsBelow(j) || j.endsBelow(i)
}

// endsBelow reports whether every value of i is below every value of j.
func (i interval) endsBelow(j interval) bool {
	if i.hi == nil || j.lo == nil {
		return false
	}
	c := i.hi.Cmp(*j.lo)
	return c < 0 || (c == 0 && (i.hiOpen || j.loOpen))
}

// coversWith reports whether i ∪ j is the whole line.
func (i interval) coversWith(j interval) bool {
	return i.coversUpTo(j) || j.coversUpTo(i)
}

// coversUpTo reports whether i is unbounded below, j unbounded above, and
// they meet or overlap.
func (i interval) coversUpTo(j interval) bool {
	if i.lo != nil || j.hi != nil {
		return false
	}
	if i.hi == nil || j.lo == nil {
		return true
	}
	c := i.hi.Cmp(*j.lo)
	return c > 0 || (c == 0 && (!i.hiOpen || !j.loOpen))
}

// partitionWith reports whether i and j are disjoint and together cover the
// line: one is (-inf, x] or (-inf, x) and the other (x, +inf) or [x, +inf)
// with exactly one of them containing x.
func (i interval) partitionWith(j interval) bool {
	return i.disjointFrom(j) && i.coversWith(j)
}
