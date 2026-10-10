package canonical

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// RelationshipType classifies how two instruments' payouts relate (D-017).
type RelationshipType string

// Relationship types, strongest first (domain.md).
const (
	RelEquivalent           RelationshipType = "equivalent"
	RelEquivalentGivenFacts RelationshipType = "equivalent_given_facts"
	RelComplement           RelationshipType = "complement"
	RelImplies              RelationshipType = "implies"
	RelRelated              RelationshipType = "related"
	RelUnrelated            RelationshipType = "unrelated"
)

// RelationshipTypes lists every type, strongest first.
func RelationshipTypes() []RelationshipType {
	return []RelationshipType{RelEquivalent, RelEquivalentGivenFacts, RelComplement, RelImplies, RelRelated, RelUnrelated}
}

// Strength orders types for the downgrade-only guard (architecture.md):
// equivalent (5) > equivalent_given_facts (4) > complement = implies (3) >
// related (2) > unrelated (1). An invalid type has strength 0.
func (t RelationshipType) Strength() int {
	switch t {
	case RelEquivalent:
		return 5
	case RelEquivalentGivenFacts:
		return 4
	case RelComplement, RelImplies:
		return 3
	case RelRelated:
		return 2
	case RelUnrelated:
		return 1
	}
	return 0
}

// Valid reports whether t is a known relationship type.
func (t RelationshipType) Valid() bool { return t.Strength() > 0 }

// CanBecome reports whether a relationship of type t may be changed to next
// without upgrading it: next is t itself or strictly weaker. Because complement
// and implies share a strength, either may only move to related or unrelated.
func (t RelationshipType) CanBecome(next RelationshipType) bool {
	if !t.Valid() || !next.Valid() {
		return false
	}
	return next == t || next.Strength() < t.Strength()
}

// Routable reports whether pairs of this type are routing candidates
// (domain.md business rule 3).
func (t RelationshipType) Routable() bool {
	return t == RelEquivalent || t == RelEquivalentGivenFacts || t == RelComplement
}

// Method records how a relationship was decided.
type Method string

// Methods.
const (
	MethodRules    Method = "rules"
	MethodRulesLLM Method = "rules+llm"
)

// Valid reports whether m is a known method.
func (m Method) Valid() bool { return m == MethodRules || m == MethodRulesLLM }

// DiffStatus is the comparison result for one term field.
type DiffStatus string

// Diff statuses.
const (
	DiffMatch   DiffStatus = "match"
	DiffDiffer  DiffStatus = "differ"
	DiffUnknown DiffStatus = Unknown
)

// Valid reports whether s is a known diff status.
func (s DiffStatus) Valid() bool { return s == DiffMatch || s == DiffDiffer || s == DiffUnknown }

// FieldDiff is one row of the contract diff: a term field on A and on B.
type FieldDiff struct {
	Field  string     `json:"field"`
	A      string     `json:"a"`
	B      string     `json:"b"`
	Status DiffStatus `json:"status"`
}

// Veto is a hard veto that blocks equivalence (D-019), e.g. a subject or
// threshold mismatch, with the field it concerns and a readable detail.
type Veto struct {
	Code   string `json:"code"`  // e.g. "subject_mismatch", "deadline_gap"
	Field  string `json:"field"` // the term field checked
	Detail string `json:"detail"`
}

// FactUse records an external fact applied to a decision (FR-10).
type FactUse struct {
	Key    string    `json:"key"` // e.g. "fed_upper_bound"
	Value  string    `json:"value"`
	Source string    `json:"source"`
	AsOf   time.Time `json:"as_of"`
}

// BasisComponent is one documented contributor to basis risk (FR-11).
type BasisComponent struct {
	Name   string          `json:"name"` // e.g. "cutoff_gap", "source_difference"
	Score  decimal.Decimal `json:"score"`
	Detail string          `json:"detail"`
}

// BasisRisk is the residual risk that two matched contracts resolve
// differently, as a score in [0,1] with its components.
type BasisRisk struct {
	Score      decimal.Decimal  `json:"score"`
	Components []BasisComponent `json:"components"`
}

// LLM verdict limits (M1-11 schema).
const (
	MaxLLMEvidence      = 3
	MaxLLMEvidenceChars = 200
)

// LLMVerdict is the verifier's structured answer for a pair. internal/llm
// decodes it strictly; Apply may only use it to downgrade (D-022).
type LLMVerdict struct {
	Relationship    RelationshipType `json:"relationship"`
	DifferingFields []string         `json:"differing_fields"`
	Confidence      decimal.Decimal  `json:"confidence"`
	Evidence        []string         `json:"evidence"`
}

// ContributionTerm is one term's signed contribution to the final confidence (UI-20, D-112).
type ContributionTerm struct {
	Term  string          `json:"term"`  // e.g. "prior", "unknown_unit_a", "deadline_gap_hours"
	Value decimal.Decimal `json:"value"` // Signed: positive for prior, negative for deductions
}

// MatcherEvidence records the matcher's confidence calculation components (UI-20, D-112).
// It is optional and may be nil for relationships stored before this feature.
type MatcherEvidence struct {
	Prior         decimal.Decimal    `json:"prior"`                    // Base confidence before deductions
	Contributions []ContributionTerm `json:"contributions"`            // Per-term signed contributions
	Total         decimal.Decimal    `json:"total"`                    // Sum (equals stored confidence)
	PreVetoScore  *decimal.Decimal   `json:"pre_veto_score,omitempty"` // For near misses: score before vetoes
	RequiredScore *decimal.Decimal   `json:"required_score,omitempty"` // For near misses: MIN_ROUTE_CONFIDENCE (0.90)
}

// Validate checks that total equals the sum of prior and contributions, and that
// pre_veto_score and required_score are in [0,1] when present.
func (e MatcherEvidence) Validate() error {
	var errs []error
	// Check prior is in [0,1]
	if err := checkUnit("evidence.prior", e.Prior); err != nil {
		errs = append(errs, err)
	}
	// Check total is in [0,1]
	if err := checkUnit("evidence.total", e.Total); err != nil {
		errs = append(errs, err)
	}
	// Verify sum: sum of all contributions (including prior) == total
	sum := decimal.Zero
	for _, c := range e.Contributions {
		sum = sum.Add(c.Value)
	}
	// Allow small rounding error (1e-10)
	if sum.Sub(e.Total).Abs().GreaterThan(decimal.NewFromFloat(1e-10)) {
		errs = append(errs, fmt.Errorf("evidence.total %s does not equal sum of contributions %s",
			e.Total, sum))
	}
	// Check optional fields
	if e.PreVetoScore != nil {
		if err := checkUnit("evidence.pre_veto_score", *e.PreVetoScore); err != nil {
			errs = append(errs, err)
		}
	}
	if e.RequiredScore != nil {
		if err := checkUnit("evidence.required_score", *e.RequiredScore); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Validate checks the type, confidence range and evidence limits.
func (v LLMVerdict) Validate() error {
	var errs []error
	if !v.Relationship.Valid() {
		errs = append(errs, fmt.Errorf("llm relationship %q is not valid", truncate(string(v.Relationship), 64)))
	}
	if err := checkUnit("llm confidence", v.Confidence); err != nil {
		errs = append(errs, err)
	}
	if len(v.Evidence) > MaxLLMEvidence {
		errs = append(errs, fmt.Errorf("llm evidence has %d quotes; at most %d allowed", len(v.Evidence), MaxLLMEvidence))
	}
	for i, e := range v.Evidence {
		if utf8.RuneCountInString(e) > MaxLLMEvidenceChars {
			errs = append(errs, fmt.Errorf("llm evidence[%d] exceeds %d characters", i, MaxLLMEvidenceChars))
		}
	}
	return errors.Join(errs...)
}

// Relationship is the classified link between instruments A and B in a round.
type Relationship struct {
	ID         int64            `json:"id,omitempty"`
	RoundID    int64            `json:"round_id"`
	AID        string           `json:"a_id"`
	BID        string           `json:"b_id"`
	Type       RelationshipType `json:"type"`
	Confidence decimal.Decimal  `json:"confidence"`
	Method     Method           `json:"method"`
	Diff       []FieldDiff      `json:"diff"`
	Vetoes     []Veto           `json:"vetoes"`
	FactsUsed  []FactUse        `json:"facts_used"`
	BasisRisk  *BasisRisk       `json:"basis_risk,omitempty"`
	// Evidence records the matcher's confidence calculation (UI-20, D-112).
	// It is nil for relationships stored before this feature.
	Evidence *MatcherEvidence `json:"evidence,omitempty"`
	// Explanation is a human-readable sentence naming the deciding fields. Required.
	Explanation string `json:"explanation"`
}

// Validate checks the round, both instrument IDs (distinct), enums, the
// confidence range, diff statuses, basis-risk range and a non-empty explanation.
func (r Relationship) Validate() error {
	var errs []error
	if r.RoundID <= 0 {
		errs = append(errs, errors.New("round_id is required"))
	}
	for _, id := range []string{r.AID, r.BID} {
		if err := ValidateInstrumentID(id); err != nil {
			errs = append(errs, err)
		}
	}
	if r.AID == r.BID {
		errs = append(errs, errors.New("a_id and b_id must differ"))
	}
	if !r.Type.Valid() {
		errs = append(errs, fmt.Errorf("type %q is not valid", truncate(string(r.Type), 64)))
	}
	if !r.Method.Valid() {
		errs = append(errs, fmt.Errorf("method %q is not valid", truncate(string(r.Method), 64)))
	}
	if err := checkUnit("confidence", r.Confidence); err != nil {
		errs = append(errs, err)
	}
	for i, d := range r.Diff {
		if !d.Status.Valid() {
			errs = append(errs, fmt.Errorf("diff[%d] status %q is not valid", i, d.Status))
		}
	}
	if r.BasisRisk != nil {
		if err := checkUnit("basis_risk.score", r.BasisRisk.Score); err != nil {
			errs = append(errs, err)
		}
	}
	if r.Evidence != nil {
		if err := r.Evidence.Validate(); err != nil {
			errs = append(errs, err)
		}
		// Evidence.Total must equal the relationship's Confidence
		if !r.Evidence.Total.Equal(r.Confidence) {
			errs = append(errs, fmt.Errorf("evidence.total %s does not match confidence %s",
				r.Evidence.Total, r.Confidence))
		}
	}
	if strings.TrimSpace(r.Explanation) == "" {
		errs = append(errs, errors.New("explanation is required"))
	}
	return errors.Join(errs...)
}

// checkUnit returns an error unless d is in [0,1].
func checkUnit(field string, d decimal.Decimal) error {
	if d.LessThan(zero) || d.GreaterThan(one) {
		return fmt.Errorf("%s %s is outside [0,1]", field, d)
	}
	return nil
}
