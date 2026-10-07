package eval

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

const (
	fixtureLabels     = "../../testdata/golden/testdata/labels_small.yaml"
	fixtureCandidates = "../../testdata/golden/testdata/candidates_small.yaml"
	starterLabels     = "../../testdata/golden/labels.yaml"
	realCandidates    = "../../testdata/golden/candidates.yaml"
)

func TestLoadLabelsFixture(t *testing.T) {
	l, err := LoadLabels(fixtureLabels)
	if err != nil {
		t.Fatal(err)
	}
	if l.Version != Version || l.RoundID != 7 || len(l.Labels) != 7 {
		t.Fatalf("labels = %+v", l)
	}
	if l.Labels[4].AID != "polymarket:1005" || l.Labels[4].Label != canonical.RelImplies {
		t.Errorf("label[4] = %+v", l.Labels[4])
	}
}

// The shipped starter file is valid and holds no labels: nothing invented.
func TestStarterLabelsIsEmptyAndValid(t *testing.T) {
	l, err := LoadLabels(starterLabels)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Labels) != 0 {
		t.Fatalf("starter labels.yaml has %d labels; it must ship empty until a human confirms pairs", len(l.Labels))
	}
	raw, _ := os.ReadFile(starterLabels)
	for _, want := range []string{"confirmed_by", "confirmed_on", "Tier 3", "equivalent_given_facts"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("starter labels.yaml header does not document %q", want)
		}
	}
}

func TestLoadCandidatesFixtureAndReal(t *testing.T) {
	c, err := LoadCandidates(fixtureCandidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Candidates) != 7 || c.Method != canonical.MethodRules {
		t.Fatalf("candidates = %+v", c)
	}
	sug := c.Suggestions()
	if sug[PairKey("polymarket:1005", "kalshi:FX-A5")] != canonical.RelImplies {
		t.Errorf("suggestions ignore venue order: %v", sug)
	}
	if _, err := os.Stat(realCandidates); err == nil {
		shipped, err := LoadCandidates(realCandidates)
		if err != nil {
			t.Fatalf("shipped candidates.yaml: %v", err)
		}
		if len(shipped.Candidates) == 0 || shipped.RoundID <= 0 {
			t.Errorf("shipped candidates.yaml has %d candidates from round %d", len(shipped.Candidates), shipped.RoundID)
		}
	}
}

func TestLoadLabelsRejectsBadFiles(t *testing.T) {
	cases := map[string]string{
		"missing":          "",
		"empty":            "   \n",
		"unknown key":      "version: 1\nlabels:\n  - a_id: kalshi:A\n    b_id: polymarket:1\n    label: equivalent\n    confirmed_by: x\n    confirmed_on: 2026-10-07\n    extra: 1\n",
		"version":          "version: 2\nlabels: []\n",
		"bad type":         "version: 1\nlabels:\n  - a_id: kalshi:A\n    b_id: polymarket:1\n    label: same\n    confirmed_by: x\n    confirmed_on: 2026-10-07\n",
		"no confirmer":     "version: 1\nlabels:\n  - a_id: kalshi:A\n    b_id: polymarket:1\n    label: equivalent\n    confirmed_by: ' '\n    confirmed_on: 2026-10-07\n",
		"bad date":         "version: 1\nlabels:\n  - a_id: kalshi:A\n    b_id: polymarket:1\n    label: equivalent\n    confirmed_by: x\n    confirmed_on: yesterday\n",
		"same ids":         "version: 1\nlabels:\n  - a_id: kalshi:A\n    b_id: kalshi:A\n    label: equivalent\n    confirmed_by: x\n    confirmed_on: 2026-10-07\n",
		"bad id":           "version: 1\nlabels:\n  - a_id: 'kalshi:A B'\n    b_id: polymarket:1\n    label: equivalent\n    confirmed_by: x\n    confirmed_on: 2026-10-07\n",
		"duplicate (swap)": "version: 1\nlabels:\n  - a_id: kalshi:A\n    b_id: polymarket:1\n    label: equivalent\n    confirmed_by: x\n    confirmed_on: 2026-10-07\n  - a_id: polymarket:1\n    b_id: kalshi:A\n    label: related\n    confirmed_by: x\n    confirmed_on: 2026-10-07\n",
		"long note":        "version: 1\nlabels:\n  - a_id: kalshi:A\n    b_id: polymarket:1\n    label: equivalent\n    confirmed_by: x\n    confirmed_on: 2026-10-07\n    note: " + strings.Repeat("n", MaxNoteLen+1) + "\n",
		"not yaml":         "version: [1\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "labels.yaml")
			if name != "missing" {
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := LoadLabels(path); err == nil {
				t.Errorf("%s: accepted", name)
			}
		})
	}
}

func TestLoadLabelsRejectsOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.yaml")
	big := "version: 1\nlabels: []\n# " + strings.Repeat("x", MaxFileBytes) + "\n"
	if err := os.WriteFile(path, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLabels(path); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("err = %v", err)
	}
}

func TestLoadCandidatesRejectsBadFiles(t *testing.T) {
	good := "version: 1\nround_id: 3\ndrafted_at: 2026-10-07T00:00:00Z\nmethod: rules\ncandidates:\n  - a_id: kalshi:A\n    a_title: a\n    b_id: polymarket:1\n    b_title: b\n    category: crypto\n    suggested: related\n    confidence: \"0.5\"\n    why: w\n    vetoes: []\n    explanation: e\n"
	cases := map[string]string{
		"no round":    strings.Replace(good, "round_id: 3", "round_id: 0", 1),
		"bad method":  strings.Replace(good, "method: rules", "method: guess", 1),
		"bad type":    strings.Replace(good, "suggested: related", "suggested: same", 1),
		"bad id":      strings.Replace(good, "a_id: kalshi:A", "a_id: kalshi", 1),
		"same ids":    strings.Replace(good, "b_id: polymarket:1", "b_id: kalshi:A", 1),
		"unknown key": good + "    score: 1\n",
		"duplicate":   good + "  - a_id: polymarket:1\n    a_title: a\n    b_id: kalshi:A\n    b_title: b\n    category: crypto\n    suggested: related\n    confidence: \"0.5\"\n    why: w\n    vetoes: []\n    explanation: e\n",
	}
	path := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(path, []byte(good), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCandidates(path); err != nil {
		t.Fatalf("good file rejected: %v", err)
	}
	for name, body := range cases {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCandidates(path); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := LoadCandidates(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("missing file accepted")
	}
}

func TestWriteCandidatesRoundTripAndRefusesLabels(t *testing.T) {
	dir := t.TempDir()
	c := Candidates{Version: Version, RoundID: 9, DraftedAt: time.Date(2026, 10, 7, 5, 0, 0, 0, time.UTC), Method: canonical.MethodRules,
		Candidates: []Candidate{{AID: "kalshi:A", ATitle: "a", BID: "polymarket:1", BTitle: "b", Category: canonical.CategoryCrypto,
			Suggested: canonical.RelEquivalent, Confidence: "0.98", Why: "w", Vetoes: []string{}, Explanation: "e"}}}
	path := filepath.Join(dir, "sub", "candidates.yaml")
	if err := WriteCandidates(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := LoadCandidates(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.RoundID != 9 || len(got.Candidates) != 1 || got.Candidates[0].Suggested != canonical.RelEquivalent || !got.DraftedAt.Equal(c.DraftedAt) {
		t.Errorf("round trip = %+v", got)
	}
	raw, _ := os.ReadFile(filepath.Clean(path))
	if !strings.HasPrefix(string(raw), "# Golden-set candidates") || !strings.Contains(string(raw), "MACHINE OUTPUT") {
		t.Errorf("header missing: %q", string(raw[:80]))
	}
	for _, p := range []string{filepath.Join(dir, LabelsFileName), LabelsFileName, "testdata/golden/" + LabelsFileName} {
		if err := WriteCandidates(p, c); !errors.Is(err, ErrLabelsPath) {
			t.Errorf("WriteCandidates(%q) = %v, want ErrLabelsPath", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, LabelsFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Error("labels.yaml was created")
	}
	if err := WriteCandidates(filepath.Join(dir, "bad.yaml"), Candidates{}); err == nil {
		t.Error("invalid candidates written")
	}
	if err := WriteCandidates(filepath.Join(path, "x.yaml"), c); err == nil {
		t.Error("write under a file succeeded")
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestPairKeyIsOrderFree(t *testing.T) {
	if PairKey("kalshi:A", "polymarket:1") != PairKey("polymarket:1", "kalshi:A") {
		t.Error("PairKey depends on order")
	}
	if PairKey("kalshi:A", "polymarket:1") == PairKey("kalshi:A", "polymarket:2") {
		t.Error("PairKey collides")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("abcdef", 3); got != "abc…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("ab", 3); got != "ab" {
		t.Errorf("truncate = %q", got)
	}
}
