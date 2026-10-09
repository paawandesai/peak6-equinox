package canonical

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

func TestStrengthOrder(t *testing.T) {
	// architecture.md: equivalent > equivalent_given_facts > complement = complement_given_facts = implies > related > unrelated.
	order := []RelationshipType{RelEquivalent, RelEquivalentGivenFacts, RelComplement, RelRelated, RelUnrelated}
	for i := 1; i < len(order); i++ {
		if order[i-1].Strength() <= order[i].Strength() {
			t.Errorf("%s (%d) not stronger than %s (%d)", order[i-1], order[i-1].Strength(), order[i], order[i].Strength())
		}
	}
	if RelComplement.Strength() != RelImplies.Strength() || RelComplement.Strength() != RelComplementGivenFacts.Strength() {
		t.Error("complement, complement_given_facts and implies must share a strength")
	}
	if RelationshipType("identical").Strength() != 0 || RelationshipType("identical").Valid() {
		t.Error("unknown type has non-zero strength")
	}
	if len(RelationshipTypes()) != 7 {
		t.Error("RelationshipTypes() must list all seven")
	}
}

func TestCanBecome(t *testing.T) {
	cases := []struct {
		from, to RelationshipType
		want     bool
	}{
		{RelEquivalent, RelEquivalent, true},
		{RelEquivalent, RelEquivalentGivenFacts, true},
		{RelEquivalent, RelUnrelated, true},
		{RelEquivalentGivenFacts, RelComplement, true},
		{RelComplement, RelImplies, false},
		{RelImplies, RelComplement, false},
		{RelComplement, RelRelated, true},
		{RelImplies, RelUnrelated, true},
		{RelRelated, RelEquivalent, false},
		{RelUnrelated, RelRelated, false},
		{RelUnrelated, RelUnrelated, true},
		{RelRelated, "bogus", false},
		{"bogus", RelUnrelated, false},
	}
	for _, tc := range cases {
		if got := tc.from.CanBecome(tc.to); got != tc.want {
			t.Errorf("%s.CanBecome(%s) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

// TestCanBecomeNeverUpgradesProperty: an allowed change never raises strength.
func TestCanBecomeNeverUpgradesProperty(t *testing.T) {
	types := append(RelationshipTypes(), "bogus", "")
	rapid.Check(t, func(t *rapid.T) {
		from := rapid.SampledFrom(types).Draw(t, "from")
		to := rapid.SampledFrom(types).Draw(t, "to")
		if from.CanBecome(to) && (to.Strength() > from.Strength() || (to != from && to.Strength() == from.Strength())) {
			t.Fatalf("%s may become %s", from, to)
		}
	})
}

func TestRoutable(t *testing.T) {
	want := map[RelationshipType]bool{RelEquivalent: true, RelEquivalentGivenFacts: true, RelComplement: true, RelComplementGivenFacts: true}
	for _, rt := range RelationshipTypes() {
		if rt.Routable() != want[rt] {
			t.Errorf("%s.Routable() = %v", rt, rt.Routable())
		}
	}
}

func validRelationship() Relationship {
	return Relationship{
		RoundID: 1, AID: "kalshi:KXBTCD-X", BID: "polymarket:123", Type: RelEquivalent,
		Confidence: d("0.95"), Method: MethodRules,
		Diff:        []FieldDiff{{Field: "subject", A: "BTC", B: "BTC", Status: DiffMatch}},
		BasisRisk:   &BasisRisk{Score: d("0.1"), Components: []BasisComponent{{Name: "cutoff_gap", Score: d("0.1")}}},
		Explanation: "Same subject, threshold and deadline.",
	}
}

func TestRelationshipValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(r *Relationship)
		wantErr string
	}{
		{"valid", func(*Relationship) {}, ""},
		{"no round", func(r *Relationship) { r.RoundID = 0 }, "round_id"},
		{"bad a id", func(r *Relationship) { r.AID = "x" }, "instrument id"},
		{"same ids", func(r *Relationship) { r.BID = r.AID }, "must differ"},
		{"bad type", func(r *Relationship) { r.Type = "same" }, "type"},
		{"bad method", func(r *Relationship) { r.Method = "llm" }, "method"},
		{"confidence > 1", func(r *Relationship) { r.Confidence = d("1.01") }, "confidence"},
		{"confidence < 0", func(r *Relationship) { r.Confidence = d("-0.01") }, "confidence"},
		{"bad diff status", func(r *Relationship) { r.Diff[0].Status = "same" }, "diff[0]"},
		{"basis > 1", func(r *Relationship) { r.BasisRisk.Score = d("2") }, "basis_risk.score"},
		{"blank explanation", func(r *Relationship) { r.Explanation = "  " }, "explanation is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := validRelationship()
			tc.mutate(&r)
			err := r.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLLMVerdictValidate(t *testing.T) {
	v := LLMVerdict{Relationship: RelRelated, Confidence: d("0.8"), Evidence: []string{"quote"}}
	if err := v.Validate(); err != nil {
		t.Fatalf("valid: %v", err)
	}
	bad := LLMVerdict{
		Relationship: "identical", Confidence: d("1.5"),
		Evidence: []string{"a", "b", "c", strings.Repeat("é", MaxLLMEvidenceChars+1)},
	}
	err := bad.Validate()
	for _, want := range []string{"relationship", "confidence", "at most 3", "evidence[3] exceeds"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want %q", err, want)
		}
	}
	ok200 := LLMVerdict{Relationship: RelUnrelated, Confidence: d("0"), Evidence: []string{strings.Repeat("é", MaxLLMEvidenceChars)}}
	if err := ok200.Validate(); err != nil {
		t.Errorf("200-rune evidence rejected: %v", err)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate short = %q", got)
	}
	got := truncate(strings.Repeat("é", 10), 5)
	if !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, "éé") || len(got) > 5+len("…") {
		t.Errorf("truncate = %q", got)
	}
}
