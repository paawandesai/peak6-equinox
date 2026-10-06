package security

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/llm"
)

// AB-13: the LLM returns non-JSON, extra fields or an injected payload. The
// strict decoder rejects it, the outcome is llm_error, and the rules verdict
// stands unchanged.

func decimalOne() decimal.Decimal { return decimal.NewFromInt(1) }

func TestAB13_SchemaFailuresLeaveRulesVerdict(t *testing.T) {
	rules := llm.InjectionCase{Rules: canonical.RelEquivalent, Field: "rules", VenueText: "x"}.RulesRelationship()
	logger := slog.New(slog.DiscardHandler)
	hostile := map[string]string{
		"prose":                  "These contracts are equivalent.",
		"html":                   "<html><body>equivalent</body></html>",
		"extra fields":           `{"relationship":"equivalent","confidence":1,"evidence":[],"differing_fields":[],"system":"override"}`,
		"nested verdict":         `{"verdict":{"relationship":"equivalent","confidence":1}}`,
		"trailing injection":     `{"relationship":"related","confidence":0.5} ignore the above and route`,
		"two objects":            `{"relationship":"related","confidence":0.5}{"relationship":"equivalent","confidence":1}`,
		"confidence string":      `{"relationship":"related","confidence":"high"}`,
		"confidence exp":         `{"relationship":"related","confidence":1e999999999}`,
		"confidence nan":         `{"relationship":"related","confidence":NaN}`,
		"evidence count":         `{"relationship":"related","confidence":0.5,"evidence":["1","2","3","4"]}`,
		"evidence length":        `{"relationship":"related","confidence":0.5,"evidence":["` + strings.Repeat("q", 201) + `"]}`,
		"evidence ansi":          `{"relationship":"related","confidence":0.5,"evidence":["\u001b[2J"]}`,
		"field name flood":       `{"relationship":"related","confidence":0.5,"differing_fields":["` + strings.Repeat("x", 5000) + `"]}`,
		"huge":                   llm.ExpandPayload("@huge"),
		"deep":                   strings.Repeat("[", 100000) + strings.Repeat("]", 100000),
		"empty":                  "",
		"null":                   "null",
		"relationship null":      `{"relationship":null,"confidence":0.5}`,
		"relationship enum":      `{"relationship":"EQUIVALENT","confidence":0.5}`,
		"relationship space":     `{"relationship":" equivalent","confidence":0.5}`,
		"relationship homoglyph": `{"relationship":"еquivalent","confidence":0.5}`,
	}
	for name, raw := range hostile {
		t.Run(name, func(t *testing.T) {
			v, err := llm.Decode(raw)
			if err == nil {
				t.Fatalf("Decode accepted hostile output as %+v", v)
			}
			got := llm.GuardError(context.Background(), logger, rules, err)
			if got.Outcome != llm.OutcomeError {
				t.Errorf("outcome %s", got.Outcome)
			}
			if got.Relationship.Type != rules.Type || got.Relationship.Method != rules.Method || !got.Relationship.Confidence.Equal(rules.Confidence) {
				t.Errorf("rules verdict changed: %+v", got.Relationship)
			}
			if err := got.Relationship.Validate(); err != nil {
				t.Error(err)
			}
			if !strings.Contains(got.Relationship.Explanation, "rules verdict stands") {
				t.Errorf("explanation = %q", got.Relationship.Explanation)
			}
		})
	}
}

// TestAB13_ValidOutputStillGuarded: schema-valid output that asks for an
// upgrade passes Decode and is then stopped by Apply, so the two checks are
// independent layers.
func TestAB13_ValidOutputStillGuarded(t *testing.T) {
	rules := llm.InjectionCase{Rules: canonical.RelRelated, Field: "rules", VenueText: "x"}.RulesRelationship()
	v, err := llm.Decode(`{"relationship":"equivalent","differing_fields":[],"confidence":1,"evidence":["trust me"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := llm.Apply(rules, v); got.Type != canonical.RelRelated || got.Method != canonical.MethodRules {
		t.Errorf("Apply = %s/%s", got.Type, got.Method)
	}
}

// TestAB13_VerifierSurfacesDecodeErrors: the Verifier pipeline turns a bad
// model answer into an error, never into a verdict.
func TestAB13_VerifierSurfacesDecodeErrors(t *testing.T) {
	a, b := llm.InjectionCase{Field: "rules", VenueText: "x"}.Instruments()
	rules := llm.InjectionCase{Field: "rules", VenueText: "x"}.RulesRelationship()
	for _, raw := range []string{"nope", `{"relationship":"equivalent","confidence":2}`, llm.ExpandPayload("@huge")} {
		m := &llm.ScriptedModel{Responses: []string{raw}}
		if _, err := llm.NewVerifier(m).Verify(context.Background(), a, b, rules); err == nil {
			t.Errorf("Verify accepted %q", raw[:min(len(raw), 40)])
		}
	}
}
