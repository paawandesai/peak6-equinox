// Package match classifies pairs of instruments into the relationship types
// of docs/domain.md with deterministic rules (D-003, D-018):
//
//	block → hard vetoes → classify + contract diff → explanation
//
// Precision first (D-020): equivalent is returned only when every vetoed term
// is known on both sides and equal, and the deadlines are within 24 h (D-019).
// Any veto, and any unknown on a vetoed field, caps the pair at related. Pairs
// that fail blocking are unrelated.
//
// M1 (v0) returns equivalent, related and unrelated. equivalent_given_facts,
// complement and implies, and the soft checks and basis risk, follow in M2.
//
// This package imports only canonical, decimal and the standard library; it
// knows nothing about venues.
package match

import (
	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Facts gives the matcher access to external facts (FR-10, D-021). It is
// implemented by internal/facts in M2; nil means no facts are available. The
// crypto rules use no facts.
type Facts interface {
	// Lookup returns the fact stored under key, if any.
	Lookup(key string) (canonical.FactUse, bool)
}

// Compare classifies the pair (a, b). The result's RoundID is zero: the caller
// that persists it sets the round it belongs to. Method is always rules; the
// LLM verifier (M1-11) may later downgrade the result and set rules+llm.
//
// Confidence is the matcher's confidence in the returned type, in [0,1]; the
// rules are in classify.go.
func Compare(a, b canonical.Instrument, _ Facts) canonical.Relationship {
	blocked := block(a, b)
	vetoes := vetoes(a, b)
	diff := Diff(a.Terms, b.Terms)
	rel := canonical.Relationship{
		AID:       a.ID,
		BID:       b.ID,
		Method:    canonical.MethodRules,
		Diff:      diff,
		Vetoes:    vetoes,
		FactsUsed: []canonical.FactUse{},
	}
	rel.Type, rel.Confidence, rel.Explanation = classify(a, b, blocked, vetoes)
	return rel
}

// termValue renders a term field for diffs and explanations.
func termValue(d *decimal.Decimal) string {
	if d == nil {
		return canonical.Unknown
	}
	return d.String()
}
