package match

import (
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// prop builds a proposition from a comparator phrase.
func prop(t *testing.T, cmp canonical.Comparator, lo string, hi string, negated bool) proposition {
	t.Helper()
	terms := canonical.Terms{Comparator: cmp, Threshold: dec(lo), Polarity: canonical.PolarityPositive}
	if hi != "" {
		terms.ThresholdHigh = dec(hi)
	}
	if negated {
		terms.Polarity = canonical.PolarityNegative
	}
	p, ok := propositionOf(terms)
	if !ok {
		t.Fatalf("proposition %s %s %s", cmp, lo, hi)
	}
	return p
}

func TestPropositionAlgebra(t *testing.T) {
	gt := canonical.ComparatorGT
	gte := canonical.ComparatorGTE
	lt := canonical.ComparatorLT
	lte := canonical.ComparatorLTE
	eq := canonical.ComparatorEQ
	between := canonical.ComparatorBetween
	cases := []struct {
		name          string
		a, b          proposition
		complement    bool
		aForcesB      bool
		bForcesA      bool
		equal         bool
		labelContains string
	}{
		{"partition gt/lte", prop(t, gt, "4", "", false), prop(t, lte, "4", "", false), true, false, false, false, "gt 4"},
		{"partition gte/lt", prop(t, gte, "4", "", false), prop(t, lt, "4", "", false), true, false, false, false, "gte 4"},
		{"gap gt/lt is not a partition", prop(t, gt, "4", "", false), prop(t, lt, "4", "", false), false, false, false, false, ""},
		{"overlap gte/lte is not a partition", prop(t, gte, "4", "", false), prop(t, lte, "4", "", false), false, false, false, false, ""},
		{"polarity flip same interval", prop(t, gt, "4", "", false), prop(t, gt, "4", "", true), true, false, false, false, "not (gt 4)"},
		{"double flip is equal, not complement", prop(t, gt, "4", "", true), prop(t, lte, "4", "", false), false, false, false, true, ""},
		{"gt x forces gte x", prop(t, gt, "4", "", false), prop(t, gte, "4", "", false), false, true, false, false, ""},
		{"higher strike forces lower", prop(t, gt, "5", "", false), prop(t, gt, "4", "", false), false, true, false, false, ""},
		{"lower cut forces higher", prop(t, lt, "3", "", false), prop(t, lte, "4", "", false), false, true, false, false, ""},
		{"eq inside gt", prop(t, eq, "4.25", "", false), prop(t, gt, "4", "", false), false, true, false, false, "eq 4.25"},
		{"eq on open end is not inside", prop(t, eq, "4", "", false), prop(t, gt, "4", "", false), false, false, false, false, ""},
		{"eq on closed end is inside", prop(t, eq, "4", "", false), prop(t, gte, "4", "", false), false, true, false, false, ""},
		{"between inside gt", prop(t, between, "4.25", "4.5", false), prop(t, gt, "4", "", false), false, true, false, false, "between 4.25 and 4.5"},
		{"between crossing gt is neither", prop(t, between, "3.75", "4.5", false), prop(t, gt, "4", "", false), false, false, false, false, ""},
		{"disjoint eq below gt", prop(t, eq, "3.75", "", false), prop(t, gt, "4", "", false), false, false, false, false, ""},
		{"negated both: containment reverses", prop(t, gt, "5", "", true), prop(t, gt, "4", "", true), false, false, true, false, ""},
		{"positive inside negated when disjoint", prop(t, lt, "3", "", false), prop(t, gt, "4", "", true), false, true, false, false, ""},
		{"negated inside positive when they cover", prop(t, lt, "3", "", true), prop(t, gt, "2", "", false), false, true, false, false, ""},
		{"negated not inside positive when a gap remains (but the positive is inside the negated)", prop(t, lt, "3", "", true), prop(t, gt, "4", "", false), false, false, true, false, ""},
		{"same sets", prop(t, gt, "4", "", false), prop(t, gt, "4", "", false), false, false, false, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.complementOf(tc.b); got != tc.complement {
				t.Errorf("complementOf = %v", got)
			}
			if got := tc.b.complementOf(tc.a); got != tc.complement {
				t.Errorf("complementOf (reversed) = %v", got)
			}
			if got := tc.a.subsetOf(tc.b); got != tc.aForcesB {
				t.Errorf("a ⊆ b = %v", got)
			}
			if got := tc.b.subsetOf(tc.a); got != tc.bForcesA {
				t.Errorf("b ⊆ a = %v", got)
			}
			if got := tc.a.equal(tc.b); got != tc.equal {
				t.Errorf("equal = %v", got)
			}
			if tc.labelContains != "" && !strings.Contains(tc.a.String()+" "+tc.b.String(), tc.labelContains) {
				t.Errorf("labels %q %q lack %q", tc.a, tc.b, tc.labelContains)
			}
		})
	}
}

func TestPropositionOfRejectsUnreadable(t *testing.T) {
	for _, terms := range []canonical.Terms{
		{Comparator: canonical.ComparatorUnknown, Threshold: dec("1"), Polarity: canonical.PolarityPositive},
		{Comparator: canonical.ComparatorGT, Polarity: canonical.PolarityPositive},
		{Comparator: canonical.ComparatorGT, Threshold: dec("1"), Polarity: canonical.PolarityUnknown},
		{Comparator: canonical.ComparatorBetween, Threshold: dec("1"), Polarity: canonical.PolarityPositive},
	} {
		if _, ok := propositionOf(terms); ok {
			t.Errorf("read %+v", terms)
		}
	}
}

func TestOnGrid(t *testing.T) {
	step := decimal.RequireFromString("0.25")
	for _, tc := range []struct{ in, want string }{
		{"gt 4", "gte 4.25"}, {"gte 4", "gte 4"}, {"lt 4", "lte 3.75"}, {"gt 4.1", "gt 4.1"}, {"eq 4", "eq 4"},
	} {
		cmp, val, _ := strings.Cut(tc.in, " ")
		p := prop(t, canonical.Comparator(cmp), val, "", false)
		if got := p.onGrid(step).String(); got != tc.want {
			t.Errorf("%s on grid = %s, want %s", tc.in, got, tc.want)
		}
	}
	if !gridPoint(decimal.RequireFromString("-0.5"), step) || gridPoint(decimal.RequireFromString("0.3"), step) {
		t.Error("gridPoint")
	}
}

// genProp draws a proposition over a small grid of thresholds so the
// relations are exercised in every combination.
func genProp(t *rapid.T, label string) proposition {
	cmps := []canonical.Comparator{canonical.ComparatorGT, canonical.ComparatorGTE, canonical.ComparatorLT, canonical.ComparatorLTE, canonical.ComparatorEQ, canonical.ComparatorBetween}
	cmp := rapid.SampledFrom(cmps).Draw(t, label+"cmp")
	lo := decimal.NewFromInt(rapid.Int64Range(-3, 3).Draw(t, label+"lo"))
	terms := canonical.Terms{Comparator: cmp, Threshold: &lo, Polarity: canonical.PolarityPositive}
	if cmp == canonical.ComparatorBetween {
		hi := lo.Add(decimal.NewFromInt(rapid.Int64Range(0, 3).Draw(t, label+"hi")))
		terms.ThresholdHigh = &hi
	}
	if rapid.Bool().Draw(t, label+"neg") {
		terms.Polarity = canonical.PolarityNegative
	}
	p, ok := propositionOf(terms)
	if !ok {
		t.Fatalf("unreadable %+v", terms)
	}
	return p
}

// member reports whether x is in the YES set of p, by brute force.
func member(p proposition, x decimal.Decimal) bool {
	in := true
	if p.iv.lo != nil {
		c := x.Cmp(*p.iv.lo)
		in = in && (c > 0 || (c == 0 && !p.iv.loOpen))
	}
	if p.iv.hi != nil {
		c := x.Cmp(*p.iv.hi)
		in = in && (c < 0 || (c == 0 && !p.iv.hiOpen))
	}
	return in != p.negated
}

// TestPropositionAlgebraAgreesWithMembership: the interval algebra agrees
// with point-by-point membership on a grid finer than the thresholds, and
// the relations have the shapes a reader expects (symmetry of complement
// and equality, antisymmetry of strict containment).
//
// The grid must extend beyond every bound genProp can draw (lo in [-3, 3],
// hi up to lo+3 = 6), with margin on both sides, so that the unbounded tail
// of a negated interval is sampled too. A grid that stopped at 4 made the
// oracle call "not (between 1 and 4)" a subset of "lt 2" (CI, M2-08).
func TestPropositionAlgebraAgreesWithMembership(t *testing.T) {
	var points []decimal.Decimal
	for i := -100; i <= 100; i++ {
		points = append(points, decimal.NewFromInt(int64(i)).Div(decimal.NewFromInt(10)))
	}
	rapid.Check(t, func(t *rapid.T) {
		a, b := genProp(t, "a"), genProp(t, "b")
		subset, complement, equal := true, true, true
		for _, x := range points {
			ma, mb := member(a, x), member(b, x)
			subset = subset && (!ma || mb)
			complement = complement && (ma != mb)
			equal = equal && (ma == mb)
		}
		if got := a.subsetOf(b); got != (subset && !equal) {
			t.Fatalf("%s ⊆ %s = %v, membership says subset=%v equal=%v", a, b, got, subset, equal)
		}
		if got := a.complementOf(b); got != complement {
			t.Fatalf("%s complement of %s = %v, membership says %v", a, b, got, complement)
		}
		if got := a.equal(b); got != equal {
			t.Fatalf("%s equal %s = %v, membership says %v", a, b, got, equal)
		}
		if a.complementOf(b) != b.complementOf(a) || a.equal(b) != b.equal(a) {
			t.Fatalf("asymmetric: %s vs %s", a, b)
		}
		if a.subsetOf(b) && b.subsetOf(a) {
			t.Fatalf("mutual strict containment: %s vs %s", a, b)
		}
	})
}

// TestDerivedTypesNeverRoutableWithSharedVeto: whatever the propositions,
// a veto on subject, deadline or source, or any unknown, leaves the pair
// related (business rules 1 and 2) and never consults a fact.
func TestDerivedTypesNeverRoutableWithSharedVeto(t *testing.T) {
	fs := fedFacts(t)
	rapid.Check(t, func(t *rapid.T) {
		a, b := fedLevel("kalshi:L", "4.00"), fedChange("polymarket:C", canonical.ComparatorGTE, "0.5")
		switch rapid.IntRange(0, 4).Draw(t, "veto") {
		case 0:
			b.Terms.Subject = "fed_funds_rate_x"
		case 1:
			b.Terms.ResolutionSource = "cme_fedwatch"
		case 2:
			b.Terms.ResolutionSource = canonical.Unknown
		case 3:
			d := b.Terms.Deadline.Add(30 * 24 * 60 * 60 * 1e9 / 30) // 24 h later: a deadline gap
			b.Terms.Deadline = &d
		default:
			b.Terms.Polarity = canonical.PolarityUnknown
		}
		rel := Compare(a, b, fs)
		if rel.Type != canonical.RelRelated && rel.Type != canonical.RelUnrelated {
			t.Fatalf("got %s: %s", rel.Type, rel.Explanation)
		}
		if len(rel.FactsUsed) != 0 || rel.BasisRisk != nil {
			t.Fatalf("facts %v / basis %v consulted despite a shared-field veto", rel.FactsUsed, rel.BasisRisk)
		}
	})
}

var _ = facts.Set{}
