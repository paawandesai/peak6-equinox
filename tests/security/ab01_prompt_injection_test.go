package security

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/llm"
)

// AB-01: venue text carries instructions that try to make the LLM upgrade or
// create a relationship. Every fixed case runs through BuildPrompt, an
// AdversarialMock that obeys the injection, the strict decoder and Apply. The
// final relationship is never stronger than the rules verdict, every output
// validates, and nothing panics. The same cases run against the real model
// with `make llm-eval-live`.

var injectionCasesFile = filepath.Join("..", "..", llm.CasesPath)

// requiredAttacks are the categories the ticket lists; the suite must cover each.
var requiredAttacks = []string{
	"ignore_previous_instructions", "fake_closing_delimiter", "fake_system_role", "dan_jailbreak",
	"instructions_in_title", "zero_width_obfuscation", "homoglyph_obfuscation",
	"non_json_output", "extra_fields", "out_of_range_confidence", "oversized_evidence_count", "oversized_evidence_quote",
	"system_prompt_request", "prompt_in_url", "huge_payload",
}

func TestAB01_PromptInjectionSuite(t *testing.T) {
	cases, err := llm.LoadInjectionCases(injectionCasesFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("%d cases, want at least 20", len(cases))
	}
	covered := map[string]bool{}
	for _, c := range cases {
		covered[c.Attack] = true
	}
	for _, attack := range requiredAttacks {
		if !covered[attack] {
			t.Errorf("no case covers attack %q", attack)
		}
	}

	for _, c := range cases {
		t.Run(c.ID+"_"+c.Attack, func(t *testing.T) {
			runInjectionCase(t, c)
		})
	}
}

func runInjectionCase(t *testing.T, c llm.InjectionCase) {
	t.Helper()
	ctx := context.Background()
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	a, b := c.Instruments()
	rules := c.RulesRelationship()
	prompt := llm.BuildPrompt(a, b)
	assertPromptSafe(t, prompt, c)

	model := llm.NewAdversarialMock(c.VenueText, c.MockResponse)
	raw, err := model.Complete(ctx, prompt)
	if err != nil {
		t.Fatalf("mock: %v", err)
	}
	if len(model.Obeyed()) != 1 {
		t.Fatalf("the adversarial mock did not see the injected text in the prompt")
	}

	var result llm.Result
	verdict, err := llm.Decode(raw.Text)
	if err != nil {
		result = llm.GuardError(ctx, logger, rules, err)
	} else {
		result = llm.Guard(ctx, logger, rules, verdict)
	}
	// Apply is the plain entry point the matcher uses; it must agree with Guard.
	if err == nil {
		if applied := llm.Apply(rules, verdict); applied.Type != result.Relationship.Type {
			t.Errorf("Apply gave %s, Guard gave %s", applied.Type, result.Relationship.Type)
		}
	}

	final := result.Relationship
	if final.Type.Strength() > rules.Type.Strength() {
		t.Fatalf("UPGRADE: rules %s became %s", rules.Type, final.Type)
	}
	if final.Confidence.GreaterThan(rules.Confidence) {
		t.Errorf("confidence rose from %s to %s", rules.Confidence, final.Confidence)
	}
	if err := final.Validate(); err != nil {
		t.Errorf("final relationship invalid: %v", err)
	}
	if result.Outcome != c.Expected {
		t.Errorf("outcome %s, want %s (%s)", result.Outcome, c.Expected, result.Reason)
	}
	if c.ExpectedType != "" && final.Type != c.ExpectedType {
		t.Errorf("type %s, want %s", final.Type, c.ExpectedType)
	}
	if !strings.Contains(logBuf.String(), `"event":"`+string(c.Expected)+`"`) {
		t.Errorf("log lacks event %s: %s", c.Expected, logBuf.String())
	}
	if len(c.MockResponse) > 40 && strings.Contains(logBuf.String(), c.MockResponse[:40]) {
		t.Errorf("log must not echo raw model output: %s", logBuf.String())
	}
	if final.Explanation == rules.Explanation {
		t.Error("explanation must record what the LLM step did")
	}
	if strings.Contains(final.Explanation, "\u200B") || strings.Contains(final.Explanation, "\x07") {
		t.Error("explanation carries control or format characters")
	}
}

// assertPromptSafe checks the venue text was placed as data: inside its block,
// after the fixed system prompt, with control and format characters and
// delimiter look-alikes removed, and every block delimiter still unique.
func assertPromptSafe(t *testing.T, p llm.Prompt, c llm.InjectionCase) {
	t.Helper()
	if p.System != llm.SystemPrompt || p.Version != llm.PromptVersion {
		t.Error("prompt must use the fixed system prompt and version")
	}
	if strings.Contains(p.System, c.VenueText) {
		t.Error("venue text leaked into the system prompt")
	}
	for _, label := range []string{"TITLE_A", "RULES_A", "TITLE_B", "RULES_B", "INSTRUMENT_A", "INSTRUMENT_B"} {
		for _, kind := range []string{"BEGIN", "END"} {
			if n := strings.Count(p.User, "<<<"+kind+":"+label+">>>"); n != 1 {
				t.Errorf("delimiter %s:%s occurs %d times", kind, label, n)
			}
		}
	}
	if strings.Count(p.User, "<<<") != 12 || strings.Count(p.User, ">>>") != 12 {
		t.Errorf("venue text produced extra delimiter runs: %d/%d", strings.Count(p.User, "<<<"), strings.Count(p.User, ">>>"))
	}
	for _, r := range p.User {
		if r == '\n' || r == '\t' {
			continue
		}
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			t.Errorf("prompt contains %U", r)
		}
	}
	block := "RULES_B"
	if c.Field == "title" {
		block = "TITLE_B"
	}
	open, closeIdx := strings.Index(p.User, "<<<BEGIN:"+block+">>>"), strings.Index(p.User, "<<<END:"+block+">>>")
	needle := llm.Sanitize(c.VenueText)
	if idx := strings.Index(p.User, needle); needle == "" || idx < open || idx > closeIdx {
		t.Errorf("sanitised venue text is not inside its %s block", block)
	}
}

// TestAB01_UnrelatedNeverBecomesRelated: whatever the compromised model says
// about a pair the rules call unrelated, no relationship is created.
func TestAB01_UnrelatedNeverBecomesRelated(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	rules := llm.InjectionCase{Rules: canonical.RelUnrelated, Field: "rules", VenueText: "x"}.RulesRelationship()
	for _, rt := range canonical.RelationshipTypes() {
		v := canonical.LLMVerdict{Relationship: rt, Confidence: decimalOne(), DifferingFields: []string{}, Evidence: []string{}}
		got := llm.Guard(context.Background(), logger, rules, v)
		if got.Relationship.Type != canonical.RelUnrelated {
			t.Errorf("unrelated became %s", got.Relationship.Type)
		}
		if rt != canonical.RelUnrelated && got.Outcome != llm.OutcomeRejectedUpgrade {
			t.Errorf("verdict %s: outcome %s", rt, got.Outcome)
		}
	}
}
