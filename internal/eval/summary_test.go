package eval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

var summaryNow = time.Date(2026, 10, 7, 12, 0, 0, 999, time.UTC)

// contractKeys are the required keys the web page accepts (internal/web/handlers_eval.go).
// D-070 defined these; UI-23 (D-104) adds optional fields.
var (
	contractTopKeys      = []string{"command", "generated_at", "labels", "labels_file", "method", "notes", "round_id", "types"}
	contractOptionalKeys = []string{"confusion", "labels_sha256", "previous", "thresholds"} // UI-23
	contractTypeKeys     = []string{"fn", "fp", "precision", "recall", "support", "tp", "type"}
)

func TestSummaryRoundTripsAgainstTheContract(t *testing.T) {
	labels, err := LoadLabels(fixtureLabels)
	if err != nil {
		t.Fatal(err)
	}
	r := Evaluate(labels, fixturePredictor(t), canonical.MethodRules)
	s := r.Summary(summaryNow, "equinox eval --labels x.yaml", 7, fixtureLabels, len(labels.Labels), "", "")
	if s.Labels != 7 || s.RoundID != 7 || s.Method != "rules" || !s.GeneratedAt.Equal(summaryNow.Truncate(time.Second)) || len(s.Types) != 6 {
		t.Fatalf("summary = %+v", s)
	}
	eq := s.Types[0]
	if eq.Type != "equivalent" || eq.Support != 2 || eq.TP != 1 || eq.FP != 1 || eq.FN != 1 || eq.Precision == nil || !eq.Precision.Equal(decimal.RequireFromString("0.5")) {
		t.Errorf("equivalent = %+v", eq)
	}
	if s.Types[1].Precision != nil || s.Types[1].Recall != nil {
		t.Errorf("undefined ratios must be null: %+v", s.Types[1])
	}
	joined := strings.Join(s.Notes, "\n")
	for _, want := range []string{"BELOW the 0.95 target", "rules+llm: not evaluated (M3)", "not evaluated: kalshi:FX-A7 / polymarket:1007"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes lack %q: %v", want, s.Notes)
		}
	}

	path := filepath.Join(t.TempDir(), "sub", "eval_summary.json")
	if err := WriteSummary(path, s); err != nil {
		t.Fatal(err)
	}
	back, err := LoadSummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.Command != s.Command || back.Labels != s.Labels || len(back.Types) != len(s.Types) || !back.Types[0].Precision.Equal(*s.Types[0].Precision) || back.Types[1].Recall != nil {
		t.Errorf("round trip = %+v", back)
	}
	// Exactly the contract's keys, and precision/recall as strings or null.
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	// Check all required keys are present, and allow UI-23 optional keys
	got := sortedKeys(generic)
	for _, required := range contractTopKeys {
		found := false
		for _, k := range got {
			if k == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing required key %q in %v", required, got)
		}
	}
	// Check no unexpected keys
	allowed := make(map[string]bool)
	for _, k := range contractTopKeys {
		allowed[k] = true
	}
	for _, k := range contractOptionalKeys {
		allowed[k] = true
	}
	for _, k := range got {
		if !allowed[k] {
			t.Errorf("unexpected key %q in %v", k, got)
		}
	}
	var types []map[string]json.RawMessage
	if err := json.Unmarshal(generic["types"], &types); err != nil {
		t.Fatal(err)
	}
	for i, m := range types {
		if got := sortedKeys(m); strings.Join(got, ",") != strings.Join(contractTypeKeys, ",") {
			t.Errorf("types[%d] keys = %v", i, got)
		}
		for _, k := range []string{"precision", "recall"} {
			v := string(m[k])
			if v != "null" && !strings.HasPrefix(v, `"`) {
				t.Errorf("types[%d].%s = %s, want decimal string or null", i, k, v)
			}
		}
	}
	if !strings.Contains(string(raw), `"generated_at": "2026-10-07T12:00:00Z"`) {
		t.Errorf("generated_at not RFC 3339 UTC: %s", raw)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("temp files left: %v", entries)
	}
}

func TestSummaryEmptyLabelsAndTargetMet(t *testing.T) {
	s := Evaluate(Labels{Version: 1}, nil, canonical.MethodRules).Summary(summaryNow, "equinox eval", 0, "labels.yaml", 0, "", "")
	if s.Labels != 0 || len(s.Types) != 6 || s.Types[0].Precision != nil {
		t.Errorf("empty = %+v", s)
	}
	if !strings.Contains(strings.Join(s.Notes, "\n"), "no confirmed labels yet") || !strings.Contains(s.Notes[1], "not measurable") {
		t.Errorf("notes = %v", s.Notes)
	}
	if err := s.Validate(); err != nil {
		t.Error(err)
	}
	l := Labels{Version: 1, Labels: []Label{{AID: "kalshi:A", BID: "polymarket:1", Label: canonical.RelEquivalent, ConfirmedBy: "h", ConfirmedOn: "2026-10-07"}}}
	met := Evaluate(l, func(string, string) (canonical.RelationshipType, decimal.Decimal, bool) {
		return canonical.RelEquivalent, decimal.NewFromInt(1), true
	}, canonical.MethodRules).
		Summary(summaryNow, "equinox eval", 3, "labels.yaml", 1, "", "")
	if !strings.Contains(met.Notes[0], "meets the 0.95 target") || met.Types[0].Recall == nil || !met.Types[0].Recall.Equal(decimal.NewFromInt(1)) {
		t.Errorf("met = %+v", met)
	}
}

func TestSummaryValidateAndLoadReject(t *testing.T) {
	good := Summary{GeneratedAt: summaryNow, Method: "rules", Types: []TypeSummary{{Type: "equivalent"}}, Notes: []string{}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	one := decimal.NewFromInt(2)
	neg := decimal.NewFromInt(-1)
	bad := map[string]Summary{
		"zero time":    {Method: "rules"},
		"neg labels":   {GeneratedAt: summaryNow, Method: "rules", Labels: -1},
		"bad method":   {GeneratedAt: summaryNow, Method: "guess"},
		"bad type":     {GeneratedAt: summaryNow, Method: "rules", Types: []TypeSummary{{Type: "same"}}},
		"neg count":    {GeneratedAt: summaryNow, Method: "rules", Types: []TypeSummary{{Type: "related", TP: -1}}},
		"precision >1": {GeneratedAt: summaryNow, Method: "rules", Types: []TypeSummary{{Type: "related", Precision: &one}}},
		"recall <0":    {GeneratedAt: summaryNow, Method: "rules", Types: []TypeSummary{{Type: "related", Recall: &neg}}},
	}
	for name, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if err := WriteSummary(filepath.Join(t.TempDir(), "s.json"), s); err == nil {
			t.Errorf("%s: written", name)
		}
	}
	dir := t.TempDir()
	files := map[string]string{
		"unknown field": `{"generated_at":"2026-10-07T12:00:00Z","command":"","round_id":0,"labels_file":"","labels":0,"method":"rules","types":[],"notes":[],"extra":1}`,
		"not json":      `{`,
		"invalid":       `{"generated_at":"0001-01-01T00:00:00Z","method":"rules","types":[],"notes":[]}`,
		"oversized":     `{"notes":["` + strings.Repeat("x", MaxSummaryBytes) + `"]}`,
	}
	for name, body := range files {
		p := filepath.Join(dir, "f.json")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadSummary(p); err == nil {
			t.Errorf("%s: loaded", name)
		}
	}
	if _, err := LoadSummary(filepath.Join(dir, "none.json")); err == nil {
		t.Error("missing file loaded")
	}
	if err := WriteSummary(filepath.Join(dir, "f.json", "x.json"), good); err == nil {
		t.Error("write under a file succeeded")
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// UI-23 tests

func TestBuildConfusionMatrix(t *testing.T) {
	outcomes := []Outcome{
		{Label: Label{Label: canonical.RelEquivalent, AID: "a1", BID: "b1"}, Predicted: canonical.RelEquivalent, Confidence: decimal.NewFromInt(1)},
		{Label: Label{Label: canonical.RelEquivalent, AID: "a2", BID: "b2"}, Predicted: canonical.RelRelated, Confidence: decimal.NewFromInt(1)},
		{Label: Label{Label: canonical.RelRelated, AID: "a3", BID: "b3"}, Predicted: canonical.RelRelated, Confidence: decimal.NewFromInt(1)},
	}
	cm := buildConfusionMatrix(outcomes)
	if cm == nil || len(cm.Cells) == 0 {
		t.Fatal("expected non-nil confusion matrix")
	}
	key := "equivalent:equivalent"
	if cell, ok := cm.Cells[key]; !ok || cell.Count != 1 || len(cell.PairIDs) != 1 {
		t.Errorf("cell %s = %+v, want count=1 with 1 pair", key, cell)
	}
	key = "equivalent:related"
	if cell, ok := cm.Cells[key]; !ok || cell.Count != 1 {
		t.Errorf("cell %s = %+v, want count=1", key, cell)
	}
}

func TestBuildThresholds(t *testing.T) {
	outcomes := []Outcome{
		{Label: Label{Label: canonical.RelEquivalent}, Predicted: canonical.RelEquivalent, Confidence: decimal.RequireFromString("0.95")},
		{Label: Label{Label: canonical.RelEquivalent}, Predicted: canonical.RelEquivalent, Confidence: decimal.RequireFromString("0.85")},
		{Label: Label{Label: canonical.RelEquivalent}, Predicted: canonical.RelRelated, Confidence: decimal.RequireFromString("0.75")},
	}
	thresholds := buildThresholds(outcomes)
	if len(thresholds) != 7 {
		t.Fatalf("got %d thresholds, want 7", len(thresholds))
	}
	for _, th := range thresholds {
		if th.Threshold.Equal(decimal.RequireFromString("0.90")) {
			if th.TP != 1 || th.FP != 0 {
				t.Errorf("at 0.90: TP=%d FP=%d, want TP=1 FP=0", th.TP, th.FP)
			}
		}
	}
}

func TestHashLabelsFile(t *testing.T) {
	hash1, err := HashLabelsFile(fixtureLabels)
	if err != nil {
		t.Fatal(err)
	}
	if len(hash1) != 64 {
		t.Errorf("hash length = %d, want 64", len(hash1))
	}
	hash2, err := HashLabelsFile(fixtureLabels)
	if err != nil {
		t.Fatal(err)
	}
	if hash1 != hash2 {
		t.Errorf("hash not stable: %s vs %s", hash1, hash2)
	}
}

func TestRotatePreviousSummary(t *testing.T) {
	dir := t.TempDir()
	summaryPath := filepath.Join(dir, "eval_summary.json")
	s := Summary{GeneratedAt: time.Now().UTC(), Method: "rules", Labels: 1}
	if err := WriteSummary(summaryPath, s); err != nil {
		t.Fatal(err)
	}
	prevPath := RotatePreviousSummary(summaryPath)
	if prevPath == "" {
		t.Fatal("rotation failed")
	}
	if _, err := os.Stat(prevPath); err != nil {
		t.Errorf("prev file not created: %v", err)
	}
}

// TestHashLabelsFileErrors tests error paths for coverage.
func TestHashLabelsFileErrors(t *testing.T) {
	_, err := HashLabelsFile("/nonexistent/file.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

// TestRotatePreviousSummaryEdgeCases tests edge cases for coverage.
func TestRotatePreviousSummaryEdgeCases(t *testing.T) {
	// Empty path
	result := RotatePreviousSummary("")
	if result != "" {
		t.Errorf("RotatePreviousSummary(\"\") = %q, want \"\"", result)
	}

	// Nonexistent source file
	result = RotatePreviousSummary("/nonexistent/summary.json")
	if result != "" {
		t.Errorf("RotatePreviousSummary(nonexistent) = %q, want \"\"", result)
	}
}

// TestWriteSummaryErrors tests error paths for coverage.
func TestWriteSummaryErrors(t *testing.T) {
	s := Summary{GeneratedAt: time.Now().UTC(), Method: "rules", Labels: 1}

	// Invalid path (directory doesn't exist)
	err := WriteSummary("/nonexistent/dir/summary.json", s)
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}

	// Read-only directory (if we can create one)
	dir := t.TempDir()
	readOnlyPath := filepath.Join(dir, "readonly", "summary.json")
	if err := os.Mkdir(filepath.Join(dir, "readonly"), 0o400); err == nil {
		err = WriteSummary(readOnlyPath, s)
		if err == nil {
			t.Error("expected error for read-only directory")
		}
	}
}
