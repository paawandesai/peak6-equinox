package web

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/eval"
)

func TestLoadBacktestResults_Missing(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	vm, err := LoadBacktestResults(filepath.Join(t.TempDir(), "none.json"), logger)
	if err != nil {
		t.Errorf("expected nil error for missing file, got %v", err)
	}
	if vm != nil {
		t.Errorf("expected nil vm for missing file, got %+v", vm)
	}
}

func TestLoadBacktestResults_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backtest.json")

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	summary := eval.BacktestSummary{
		GeneratedAt: now,
		Command:     "equinox backtest --fixtures",
		AsOf:        now.Add(-24 * time.Hour),
		Source:      "fixtures",
		Pairs:       100,
		Checked:     50,
		Agreed:      48,
		Disagreed:   2,
		Mismatches: []eval.BacktestMismatch{
			{
				AID:         "kalshi:KXTEST-001",
				BID:         "polymarket:123",
				ATitle:      "Test market A",
				BTitle:      "Test market B",
				AOutcome:    "yes",
				BOutcome:    "no",
				Type:        canonical.RelEquivalent,
				Confidence:  "0.95",
				Expectation: "same outcome",
			},
		},
		Exceptions: []eval.BacktestException{
			{
				BacktestMismatch: eval.BacktestMismatch{
					AID:         "kalshi:KXTEST-002",
					BID:         "polymarket:456",
					ATitle:      "Test market C",
					BTitle:      "Test market D",
					AOutcome:    "yes",
					BOutcome:    "no",
					Type:        canonical.RelEquivalentGivenFacts,
					Confidence:  "0.90",
					Expectation: "same outcome",
				},
				Reason:   "clarification after market opened",
				Decision: "D-072",
			},
		},
		FactIDs: []string{"fed_rate_2026_03"},
		Passed:  false,
	}

	if err := eval.WriteBacktestSummary(path, summary); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	vm, err := LoadBacktestResults(path, logger)
	if err != nil {
		t.Fatalf("LoadBacktestResults failed: %v", err)
	}
	if vm == nil {
		t.Fatal("expected non-nil vm")
	}

	if vm.Source != "fixtures" {
		t.Errorf("Source = %q, want 'fixtures'", vm.Source)
	}
	if vm.Pairs != 100 {
		t.Errorf("Pairs = %d, want 100", vm.Pairs)
	}
	if vm.Checked != 50 {
		t.Errorf("Checked = %d, want 50", vm.Checked)
	}
	if vm.Agreed != 48 {
		t.Errorf("Agreed = %d, want 48", vm.Agreed)
	}
	if vm.Disagreed != 2 {
		t.Errorf("Disagreed = %d, want 2", vm.Disagreed)
	}
	if vm.Passed {
		t.Error("Passed = true, want false")
	}

	if len(vm.Mismatches) != 1 {
		t.Fatalf("len(Mismatches) = %d, want 1", len(vm.Mismatches))
	}
	m := vm.Mismatches[0]
	if m.AID != "kalshi:KXTEST-001" {
		t.Errorf("Mismatch AID = %q", m.AID)
	}
	if m.Type != canonical.RelEquivalent {
		t.Errorf("Mismatch Type = %q", m.Type)
	}
	if m.Expectation != "same outcome" {
		t.Errorf("Mismatch Expectation = %q", m.Expectation)
	}

	if len(vm.Exceptions) != 1 {
		t.Fatalf("len(Exceptions) = %d, want 1", len(vm.Exceptions))
	}
	e := vm.Exceptions[0]
	if e.AID != "kalshi:KXTEST-002" {
		t.Errorf("Exception AID = %q", e.AID)
	}
	if e.Reason != "clarification after market opened" {
		t.Errorf("Exception Reason = %q", e.Reason)
	}
	if e.Decision != "D-072" {
		t.Errorf("Exception Decision = %q", e.Decision)
	}

	if len(vm.FactIDs) != 1 || vm.FactIDs[0] != "fed_rate_2026_03" {
		t.Errorf("FactIDs = %v", vm.FactIDs)
	}
}

func TestLoadBacktestResults_Malformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	if err := os.WriteFile(path, []byte(`{"bad json`), 0o600); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	vm, err := LoadBacktestResults(path, logger)
	if err == nil {
		t.Error("expected error for malformed file, got nil")
	}
	if vm != nil {
		t.Errorf("expected nil vm for error case, got %+v", vm)
	}
}
