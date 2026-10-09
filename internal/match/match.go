// Package match classifies pairs of instruments into the relationship types
// of docs/domain.md with deterministic rules (D-003, D-018):
//
//	block → hard vetoes → classify (+ facts) → contract diff → basis risk → explanation
//
// Precision first (D-020): equivalent is returned only when every vetoed term
// is known on both sides and equal, and the deadlines are within 24 h (D-019;
// identical for crypto, D-060). Pairs that fail blocking are unrelated.
//
// When vetoes fire only on the proposition itself (metric, comparator,
// threshold, polarity) and every other vetoed field is known and equal, the
// pair may still be one of the derived types (derive.go, facts.go):
//
//	equivalent_given_facts  the same payout once a fact from config/facts.yaml
//	                        is applied (the Fed level-vs-change rule)
//	complement              YES on A pays exactly when NO on B pays
//	implies                 YES on one side forces YES on the other
//
// Any unknown on a vetoed field, or a veto on subject, deadline or resolution
// source, caps the pair at related (business rule 2): facts never override a
// veto. Every fact consulted is recorded in Relationship.FactsUsed, and every
// routable verdict carries a basis-risk score (basis.go).
//
// This package imports only canonical, facts, decimal and the standard
// library; it knows nothing about venues (imports_test.go).
package match

import (
	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// Compare classifies the pair (a, b) using the facts in f (architecture.md).
// The zero facts.Set means no facts are available: the matcher then never
// returns equivalent_given_facts and says which fact it would have needed.
//
// The result's RoundID is zero: the caller that persists it sets the round it
// belongs to. Method is always rules; the LLM verifier may later downgrade the
// result and set rules+llm. Confidence is the matcher's confidence in the
// returned type, in [0,1]; the rules are in classify.go.
func Compare(a, b canonical.Instrument, f facts.Set) canonical.Relationship {
	blocked, blockFacts := block(a, b, f)
	vetoes := vetoes(a, b)
	rel := canonical.Relationship{
		AID:       a.ID,
		BID:       b.ID,
		Method:    canonical.MethodRules,
		Diff:      Diff(a.Terms, b.Terms),
		Vetoes:    vetoes,
		FactsUsed: []canonical.FactUse{},
	}
	v := classify(a, b, blocked, vetoes, f)
	rel.Type, rel.Confidence, rel.Explanation = v.typ, v.confidence, v.explanation
	// Merge facts from block (Fed exception) and classify (derivation)
	allFacts := append([]canonical.FactUse{}, blockFacts...)
	allFacts = append(allFacts, v.facts...)
	if len(allFacts) > 0 {
		rel.FactsUsed = allFacts
	}
	if rel.Type.Routable() || rel.Type == canonical.RelImplies {
		br := basisRisk(a, b, v)
		rel.BasisRisk = &br
	}
	// Build and attach the matcher evidence (UI-20, D-110)
	evidence := buildEvidence(a, b, v, vetoes)
	rel.Evidence = &evidence
	return rel
}

// termValue renders a term field for diffs and explanations.
func termValue(d *decimal.Decimal) string {
	if d == nil {
		return canonical.Unknown
	}
	return d.String()
}
