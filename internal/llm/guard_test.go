package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func rulesRel(t canonical.RelationshipType) canonical.Relationship {
	return InjectionCase{Rules: t, Field: "rules", VenueText: "x"}.RulesRelationship()
}

func verdict(t canonical.RelationshipType, conf string) canonical.LLMVerdict {
	return canonical.LLMVerdict{Relationship: t, Confidence: d(conf), DifferingFields: []string{}, Evidence: []string{}}
}

// logCapture returns a JSON logger and the buffer it writes to.
func logCapture() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

func TestDecodeAccepts(t *testing.T) {
	cases := map[string]string{
		"minimal":            `{"relationship":"related","differing_fields":[],"confidence":0.5,"evidence":[]}`,
		"full":               `{"relationship":"unrelated","differing_fields":["threshold","deadline"],"confidence":1,"evidence":["a","b","c"]}`,
		"omitted lists":      `{"relationship":"related","confidence":0}`,
		"fenced":             "```json\n{\"relationship\":\"related\",\"confidence\":0.25}\n```",
		"bare fence":         "```\n{\"relationship\":\"related\",\"confidence\":0.25}\n```",
		"whitespace":         "\n  {\"relationship\":\"related\",\"confidence\":0.25}  \n",
		"multiline evidence": `{"relationship":"related","confidence":0.5,"evidence":["line one\nline two\ttabbed"]}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := Decode(raw)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if v.DifferingFields == nil || v.Evidence == nil {
				t.Error("lists must be non-nil after decode")
			}
			if err := v.Validate(); err != nil {
				t.Errorf("decoded verdict invalid: %v", err)
			}
		})
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := map[string]struct{ raw, want string }{
		"empty":               {"", "not the verdict schema"},
		"prose":               {"The contracts are equivalent.", "not the verdict schema"},
		"unknown field":       {`{"relationship":"related","confidence":0.5,"x":1}`, "not the verdict schema"},
		"trailing":            {`{"relationship":"related","confidence":0.5} trailing`, "content after"},
		"two objects":         {`{"relationship":"related","confidence":0.5}{"relationship":"equivalent","confidence":1}`, "content after"},
		"array":               {`[{"relationship":"related","confidence":0.5}]`, "not the verdict schema"},
		"bad enum":            {`{"relationship":"same","confidence":0.5}`, "not valid"},
		"homoglyph enum":      {`{"relationship":"еquivalent","confidence":0.5}`, "not valid"},
		"missing rel":         {`{"confidence":0.5}`, "relationship is required"},
		"null rel":            {`{"relationship":null,"confidence":0.5}`, "relationship is required"},
		"missing conf":        {`{"relationship":"related"}`, "confidence is required"},
		"quoted conf":         {`{"relationship":"related","confidence":"0.5"}`, "JSON number"},
		"bool conf":           {`{"relationship":"related","confidence":true}`, "JSON number"},
		"conf > 1":            {`{"relationship":"related","confidence":1.01}`, "outside [0,1]"},
		"conf < 0":            {`{"relationship":"related","confidence":-0.1}`, "outside [0,1]"},
		"conf exp":            {`{"relationship":"related","confidence":1e999999999}`, "plain decimal"},
		"conf big":            {`{"relationship":"related","confidence":25}`, "plain decimal"},
		"too much evidence":   {`{"relationship":"related","confidence":0.5,"evidence":["a","b","c","d"]}`, "at most 3"},
		"long quote":          {`{"relationship":"related","confidence":0.5,"evidence":["` + strings.Repeat("x", 201) + `"]}`, "exceeds 200"},
		"control evidence":    {`{"relationship":"related","confidence":0.5,"evidence":["a\u0007b"]}`, "control or format"},
		"zero-width evidence": {`{"relationship":"related","confidence":0.5,"evidence":["a\u200Bb"]}`, "control or format"},
		"many fields":         {`{"relationship":"related","confidence":0.5,"differing_fields":[` + strings.TrimSuffix(strings.Repeat(`"f",`, 17), ",") + `]}`, "at most 16"},
		"long field":          {`{"relationship":"related","confidence":0.5,"differing_fields":["` + strings.Repeat("f", 65) + `"]}`, "short printable"},
		"newline field":       {`{"relationship":"related","confidence":0.5,"differing_fields":["a\nb"]}`, "short printable"},
		"nested":              {strings.Repeat("[", 50) + strings.Repeat("]", 50), "nests deeper"},
		"huge":                {ExpandPayload("@huge"), "at most 16384"},
		"fence only":          {"``````", "not the verdict schema"},
		"wrong type":          {`{"relationship":["equivalent"],"confidence":0.5}`, "not the verdict schema"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			v, err := Decode(tc.raw)
			if err == nil {
				t.Fatalf("Decode accepted %v", v)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func FuzzDecode(f *testing.F) {
	for _, seed := range []string{
		`{"relationship":"related","confidence":0.5}`, `{"relationship":"equivalent","confidence":1,"evidence":["x"]}`,
		"```json\n{}\n```", "[[[[", `{"confidence":"1"}`, "", strings.Repeat("{", 100),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		v, err := Decode(raw)
		if err != nil {
			return
		}
		if err := v.Validate(); err != nil {
			t.Fatalf("Decode(%q) returned an invalid verdict: %v", raw, err)
		}
		if _, err := json.Marshal(v); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGuardOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		rules    canonical.RelationshipType
		verdict  canonical.LLMVerdict
		outcome  Outcome
		wantType canonical.RelationshipType
	}{
		{"confirm", canonical.RelEquivalent, verdict(canonical.RelEquivalent, "0.9"), OutcomeConfirmed, canonical.RelEquivalent},
		{"downgrade one", canonical.RelEquivalent, verdict(canonical.RelEquivalentGivenFacts, "0.9"), OutcomeDowngraded, canonical.RelEquivalentGivenFacts},
		{"downgrade to unrelated", canonical.RelEquivalent, verdict(canonical.RelUnrelated, "0.9"), OutcomeDowngraded, canonical.RelUnrelated},
		{"complement to related", canonical.RelComplement, verdict(canonical.RelRelated, "0.9"), OutcomeDowngraded, canonical.RelRelated},
		{"upgrade", canonical.RelRelated, verdict(canonical.RelEquivalent, "1"), OutcomeRejectedUpgrade, canonical.RelRelated},
		{"create from unrelated", canonical.RelUnrelated, verdict(canonical.RelRelated, "0.9"), OutcomeRejectedUpgrade, canonical.RelUnrelated},
		{"sideways implies", canonical.RelImplies, verdict(canonical.RelComplement, "0.9"), OutcomeRejectedUpgrade, canonical.RelImplies},
		{"sideways complement", canonical.RelComplement, verdict(canonical.RelImplies, "0.9"), OutcomeRejectedUpgrade, canonical.RelComplement},
		{"invalid verdict type", canonical.RelEquivalent, verdict("bogus", "0.9"), OutcomeError, canonical.RelEquivalent},
		{"invalid confidence", canonical.RelEquivalent, verdict(canonical.RelRelated, "1.5"), OutcomeError, canonical.RelEquivalent},
		{"too much evidence", canonical.RelEquivalent, canonical.LLMVerdict{Relationship: canonical.RelRelated, Confidence: d("0.5"), Evidence: []string{"a", "b", "c", "d"}}, OutcomeError, canonical.RelEquivalent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, buf := logCapture()
			r := rulesRel(tc.rules)
			got := Guard(context.Background(), log, r, tc.verdict)
			if got.Outcome != tc.outcome || got.Relationship.Type != tc.wantType {
				t.Fatalf("Guard = %s/%s, want %s/%s (%s)", got.Outcome, got.Relationship.Type, tc.outcome, tc.wantType, got.Reason)
			}
			if err := got.Relationship.Validate(); err != nil {
				t.Errorf("result invalid: %v", err)
			}
			if !strings.Contains(buf.String(), `"event":"`+string(tc.outcome)+`"`) {
				t.Errorf("log missing event %s: %s", tc.outcome, buf.String())
			}
			if got.Reason == "" || !strings.Contains(got.Relationship.Explanation, got.Reason) {
				t.Error("explanation must carry the reason")
			}
			if !strings.HasPrefix(got.Relationship.Explanation, r.Explanation) {
				t.Error("rules explanation must be kept")
			}
			switch tc.outcome {
			case OutcomeConfirmed, OutcomeDowngraded:
				if got.Relationship.Method != canonical.MethodRulesLLM {
					t.Error("applied verdicts record method rules+llm")
				}
				if got.Relationship.Confidence.GreaterThan(r.Confidence) || got.Relationship.Confidence.GreaterThan(tc.verdict.Confidence) {
					t.Error("confidence must be the lower of the two")
				}
			default:
				if got.Relationship.Method != r.Method || !got.Relationship.Confidence.Equal(r.Confidence) {
					t.Error("a discarded verdict must leave method and confidence alone")
				}
			}
		})
	}
}

func TestGuardDifferingFieldsInExplanation(t *testing.T) {
	v := verdict(canonical.RelRelated, "0.7")
	v.DifferingFields = []string{"resolution_source", "deadline"}
	got := Guard(context.Background(), slog.New(slog.DiscardHandler), rulesRel(canonical.RelEquivalent), v)
	if !strings.Contains(got.Relationship.Explanation, "differing fields: resolution_source, deadline") {
		t.Errorf("explanation = %q", got.Relationship.Explanation)
	}
}

func TestGuardInvalidRulesType(t *testing.T) {
	r := rulesRel(canonical.RelRelated)
	r.Type = "bogus"
	got := Guard(context.Background(), slog.New(slog.DiscardHandler), r, verdict(canonical.RelUnrelated, "0.5"))
	if got.Outcome != OutcomeError || got.Relationship.Type != "bogus" {
		t.Errorf("Guard = %s/%s", got.Outcome, got.Relationship.Type)
	}
}

func TestGuardError(t *testing.T) {
	log, buf := logCapture()
	r := rulesRel(canonical.RelEquivalent)
	got := GuardError(context.Background(), log, r, errors.New("boom\nline two \x07 "+strings.Repeat("x", 300)))
	if got.Outcome != OutcomeError || got.Relationship.Type != r.Type || got.Relationship.Method != r.Method {
		t.Fatalf("GuardError = %+v", got)
	}
	if !strings.Contains(buf.String(), `"event":"llm_error"`) || strings.Contains(buf.String(), "\\u0007") {
		t.Errorf("log = %s", buf.String())
	}
	if strings.Contains(got.Relationship.Explanation, "\n") || len(got.Relationship.Explanation) > len(r.Explanation)+300 {
		t.Errorf("explanation not sanitised or capped: %q", got.Relationship.Explanation)
	}
	if got := GuardError(context.Background(), log, r, nil); !strings.Contains(got.Reason, "unknown error") {
		t.Errorf("nil error reason = %q", got.Reason)
	}
	empty := r
	empty.Explanation = ""
	if got := GuardError(context.Background(), log, empty, errors.New("x")); strings.HasPrefix(got.Relationship.Explanation, " ") {
		t.Error("note on an empty explanation must not start with a space")
	}
}

func TestApplyUsesDefaultLogger(t *testing.T) {
	r := rulesRel(canonical.RelRelated)
	if got := Apply(r, verdict(canonical.RelEquivalent, "1")); got.Type != canonical.RelRelated {
		t.Errorf("Apply upgraded to %s", got.Type)
	}
	if got := Apply(r, verdict(canonical.RelUnrelated, "0.4")); got.Type != canonical.RelUnrelated || !got.Confidence.Equal(d("0.4")) {
		t.Errorf("Apply = %s/%s", got.Type, got.Confidence)
	}
}

// TestGuardNeverUpgradesProperty: for every rules type, verdict type and
// confidence, the result is never stronger than the rules, never more
// confident, always valid, and the outcome matches the strength order.
func TestGuardNeverUpgradesProperty(t *testing.T) {
	types := append(canonical.RelationshipTypes(), "bogus", "")
	log := slog.New(slog.DiscardHandler)
	rapid.Check(t, func(t *rapid.T) {
		rules := rapid.SampledFrom(canonical.RelationshipTypes()).Draw(t, "rules")
		llm := rapid.SampledFrom(types).Draw(t, "llm")
		conf := decimal.New(rapid.Int64Range(-50, 150).Draw(t, "conf"), -2)
		r := rulesRel(rules)
		r.Confidence = decimal.New(rapid.Int64Range(0, 100).Draw(t, "rconf"), -2)
		got := Guard(context.Background(), log, r, verdict(llm, conf.String()))
		if got.Relationship.Type.Strength() > rules.Strength() {
			t.Fatalf("%s + %s -> %s", rules, llm, got.Relationship.Type)
		}
		if got.Relationship.Confidence.GreaterThan(r.Confidence) {
			t.Fatalf("confidence rose from %s to %s", r.Confidence, got.Relationship.Confidence)
		}
		if err := got.Relationship.Validate(); err != nil {
			t.Fatal(err)
		}
		switch {
		case !llm.Valid() || conf.IsNegative() || conf.GreaterThan(decimal.NewFromInt(1)):
			if got.Outcome != OutcomeError {
				t.Fatalf("invalid verdict gave %s", got.Outcome)
			}
		case llm == rules:
			if got.Outcome != OutcomeConfirmed {
				t.Fatalf("same type gave %s", got.Outcome)
			}
		case llm.Strength() < rules.Strength():
			if got.Outcome != OutcomeDowngraded || got.Relationship.Type != llm {
				t.Fatalf("downgrade gave %s/%s", got.Outcome, got.Relationship.Type)
			}
		default:
			if got.Outcome != OutcomeRejectedUpgrade || got.Relationship.Type != rules {
				t.Fatalf("upgrade gave %s/%s", got.Outcome, got.Relationship.Type)
			}
		}
	})
}

func TestVerifierPipeline(t *testing.T) {
	a, b := InjectionCase{Field: "rules", VenueText: "x"}.Instruments()
	r := rulesRel(canonical.RelEquivalent)
	ctx := context.Background()

	m := &ScriptedModel{Responses: []string{`{"relationship":"related","confidence":0.6}`}}
	v, err := NewVerifier(m).Verify(ctx, a, b, r)
	if err != nil || v.Relationship != canonical.RelRelated {
		t.Fatalf("Verify = %v, %v", v, err)
	}
	if ps := m.Prompts(); len(ps) != 1 || ps[0].Version != PromptVersion || !strings.Contains(ps[0].User, a.Title) {
		t.Error("verifier must send the built prompt")
	}

	if _, err := NewVerifier(&ScriptedModel{Responses: []string{"nope"}}).Verify(ctx, a, b, r); err == nil || !strings.Contains(err.Error(), "decoding") {
		t.Errorf("decode failure = %v", err)
	}
	if _, err := NewVerifier(&ScriptedModel{Err: errors.New("timeout")}).Verify(ctx, a, b, r); err == nil || !strings.Contains(err.Error(), "completing") {
		t.Errorf("transport failure = %v", err)
	}
}
