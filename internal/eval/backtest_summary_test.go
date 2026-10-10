package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestBuildBacktestSummary(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)

	r := BacktestReport{
		Instruments: map[canonical.Venue]int{
			canonical.VenueKalshi:     10,
			canonical.VenuePolymarket: 12,
		},
		Pairs: 50,
		ByType: map[canonical.RelationshipType]int{
			canonical.RelEquivalent:           2,
			canonical.RelEquivalentGivenFacts: 3,
			canonical.RelComplement:           1,
		},
		Checked: []SettledPair{
			{
				A: canonical.Settled{
					Instrument: canonical.Instrument{
						ID:    "kalshi:KXTEST-001",
						Venue: canonical.VenueKalshi,
						Title: "Test market A",
					},
					Outcome: canonical.OutcomeYes,
				},
				B: canonical.Settled{
					Instrument: canonical.Instrument{
						ID:    "polymarket:123",
						Venue: canonical.VenuePolymarket,
						Title: "Test market B",
					},
					Outcome: canonical.OutcomeYes,
				},
				Rel: canonical.Relationship{
					Type:       canonical.RelEquivalent,
					Confidence: decimal.NewFromFloat(0.95),
				},
				Expect: ExpectSame,
				Agree:  true,
			},
			{
				A: canonical.Settled{
					Instrument: canonical.Instrument{
						ID:    "kalshi:KXTEST-002",
						Venue: canonical.VenueKalshi,
						Title: "Test market C",
					},
					Outcome: canonical.OutcomeYes,
				},
				B: canonical.Settled{
					Instrument: canonical.Instrument{
						ID:    "polymarket:456",
						Venue: canonical.VenuePolymarket,
						Title: "Test market D",
					},
					Outcome: canonical.OutcomeNo,
				},
				Rel: canonical.Relationship{
					Type:       canonical.RelEquivalent,
					Confidence: decimal.NewFromFloat(0.92),
				},
				Expect: ExpectSame,
				Agree:  false,
			},
			{
				A: canonical.Settled{
					Instrument: canonical.Instrument{
						ID:    "kalshi:KXTEST-003",
						Venue: canonical.VenueKalshi,
						Title: "Test market E",
					},
					Outcome: canonical.OutcomeYes,
				},
				B: canonical.Settled{
					Instrument: canonical.Instrument{
						ID:    "polymarket:789",
						Venue: canonical.VenuePolymarket,
						Title: "Test market F",
					},
					Outcome: canonical.OutcomeNo,
				},
				Rel: canonical.Relationship{
					Type:       canonical.RelEquivalentGivenFacts,
					Confidence: decimal.NewFromFloat(0.90),
				},
				Expect: ExpectSame,
				Agree:  false,
				Exception: &Exception{
					AID:      "kalshi:KXTEST-003",
					BID:      "polymarket:789",
					Reason:   "clarification after market opened changed interpretation",
					Decision: "D-072",
				},
			},
		},
		FactIDs: []string{"fed_rate_2026_03"},
	}

	s := BuildBacktestSummary(now, "equinox backtest --fixtures", asOf, "fixtures", r)

	if s.GeneratedAt != now.Truncate(time.Second) {
		t.Errorf("GeneratedAt = %v, want %v", s.GeneratedAt, now.Truncate(time.Second))
	}
	if s.Command != "equinox backtest --fixtures" {
		t.Errorf("Command = %q, want %q", s.Command, "equinox backtest --fixtures")
	}
	if s.AsOf != asOf.Truncate(time.Second) {
		t.Errorf("AsOf = %v, want %v", s.AsOf, asOf.Truncate(time.Second))
	}
	if s.Source != "fixtures" {
		t.Errorf("Source = %q, want %q", s.Source, "fixtures")
	}
	if s.Pairs != 50 {
		t.Errorf("Pairs = %d, want 50", s.Pairs)
	}
	if s.Checked != 3 {
		t.Errorf("Checked = %d, want 3", s.Checked)
	}
	if s.Agreed != 1 {
		t.Errorf("Agreed = %d, want 1", s.Agreed)
	}
	if s.Disagreed != 2 {
		t.Errorf("Disagreed = %d, want 2", s.Disagreed)
	}
	if len(s.Mismatches) != 1 {
		t.Fatalf("len(Mismatches) = %d, want 1", len(s.Mismatches))
	}
	if len(s.Exceptions) != 1 {
		t.Fatalf("len(Exceptions) = %d, want 1", len(s.Exceptions))
	}
	if s.Passed {
		t.Error("Passed = true, want false (one equivalent mismatch is not allowlisted)")
	}

	m := s.Mismatches[0]
	if m.AID != "kalshi:KXTEST-002" {
		t.Errorf("Mismatch AID = %q, want %q", m.AID, "kalshi:KXTEST-002")
	}
	if m.BID != "polymarket:456" {
		t.Errorf("Mismatch BID = %q, want %q", m.BID, "polymarket:456")
	}
	if m.AOutcome != "yes" {
		t.Errorf("Mismatch AOutcome = %q, want 'yes'", m.AOutcome)
	}
	if m.BOutcome != "no" {
		t.Errorf("Mismatch BOutcome = %q, want 'no'", m.BOutcome)
	}
	if m.Type != canonical.RelEquivalent {
		t.Errorf("Mismatch Type = %q, want %q", m.Type, canonical.RelEquivalent)
	}
	if m.Expectation != string(ExpectSame) {
		t.Errorf("Mismatch Expectation = %q, want %q", m.Expectation, ExpectSame)
	}

	e := s.Exceptions[0]
	if e.AID != "kalshi:KXTEST-003" {
		t.Errorf("Exception AID = %q, want %q", e.AID, "kalshi:KXTEST-003")
	}
	if e.Reason != "clarification after market opened changed interpretation" {
		t.Errorf("Exception Reason = %q", e.Reason)
	}
	if e.Decision != "D-072" {
		t.Errorf("Exception Decision = %q, want 'D-072'", e.Decision)
	}

	if len(s.FactIDs) != 1 || s.FactIDs[0] != "fed_rate_2026_03" {
		t.Errorf("FactIDs = %v, want [fed_rate_2026_03]", s.FactIDs)
	}
}

func TestBacktestSummaryValidate(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

	valid := BacktestSummary{
		GeneratedAt: now,
		Command:     "equinox backtest --fixtures",
		AsOf:        now,
		Source:      "fixtures",
		Pairs:       10,
		Checked:     5,
		Agreed:      4,
		Disagreed:   1,
		Passed:      true,
	}

	if err := valid.Validate(); err != nil {
		t.Errorf("valid summary failed validation: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func(*BacktestSummary)
		wantErr string
	}{
		{
			name:    "missing generated_at",
			mutate:  func(s *BacktestSummary) { s.GeneratedAt = time.Time{} },
			wantErr: "generated_at is required",
		},
		{
			name:    "missing as_of",
			mutate:  func(s *BacktestSummary) { s.AsOf = time.Time{} },
			wantErr: "as_of is required",
		},
		{
			name:    "invalid source",
			mutate:  func(s *BacktestSummary) { s.Source = "invalid" },
			wantErr: "source must be 'fixtures' or 'live'",
		},
		{
			name:    "negative pairs",
			mutate:  func(s *BacktestSummary) { s.Pairs = -1 },
			wantErr: "counts must not be negative",
		},
		{
			name: "invalid mismatch a_id",
			mutate: func(s *BacktestSummary) {
				s.Mismatches = []BacktestMismatch{{AID: "invalid", BID: "polymarket:123", Type: canonical.RelEquivalent, AOutcome: "yes", BOutcome: "no"}}
			},
			wantErr: "invalid a_id",
		},
		{
			name: "invalid mismatch type",
			mutate: func(s *BacktestSummary) {
				s.Mismatches = []BacktestMismatch{{AID: "kalshi:KX-01", BID: "polymarket:123", Type: "invalid", AOutcome: "yes", BOutcome: "no"}}
			},
			wantErr: "unknown relationship type",
		},
		{
			name: "invalid outcome",
			mutate: func(s *BacktestSummary) {
				s.Mismatches = []BacktestMismatch{{AID: "kalshi:KX-01", BID: "polymarket:123", Type: canonical.RelEquivalent, AOutcome: "invalid", BOutcome: "no"}}
			},
			wantErr: "invalid a_outcome",
		},
		{
			name: "exception missing reason",
			mutate: func(s *BacktestSummary) {
				s.Exceptions = []BacktestException{{BacktestMismatch: BacktestMismatch{AID: "kalshi:KX-01", BID: "polymarket:123", Type: canonical.RelEquivalent, AOutcome: "yes", BOutcome: "no"}, Decision: "D-072"}}
			},
			wantErr: "reason is required",
		},
		{
			name: "exception missing decision",
			mutate: func(s *BacktestSummary) {
				s.Exceptions = []BacktestException{{BacktestMismatch: BacktestMismatch{AID: "kalshi:KX-01", BID: "polymarket:123", Type: canonical.RelEquivalent, AOutcome: "yes", BOutcome: "no"}, Reason: "test"}}
			},
			wantErr: "decision is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := valid
			tt.mutate(&s)
			err := s.Validate()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestWriteAndLoadBacktestSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backtest_summary.json")

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	s := BacktestSummary{
		GeneratedAt: now,
		Command:     "equinox backtest --fixtures",
		AsOf:        now.Add(-24 * time.Hour),
		Source:      "fixtures",
		Pairs:       100,
		Checked:     50,
		Agreed:      48,
		Disagreed:   2,
		Mismatches: []BacktestMismatch{
			{
				AID:         "kalshi:KXTEST-001",
				BID:         "polymarket:123",
				ATitle:      "Test A",
				BTitle:      "Test B",
				AOutcome:    "yes",
				BOutcome:    "no",
				Type:        canonical.RelEquivalent,
				Confidence:  "0.95",
				Expectation: string(ExpectSame),
			},
		},
		Exceptions: []BacktestException{
			{
				BacktestMismatch: BacktestMismatch{
					AID:         "kalshi:KXTEST-002",
					BID:         "polymarket:456",
					ATitle:      "Test C",
					BTitle:      "Test D",
					AOutcome:    "yes",
					BOutcome:    "no",
					Type:        canonical.RelEquivalentGivenFacts,
					Confidence:  "0.90",
					Expectation: string(ExpectSame),
				},
				Reason:   "clarification changed interpretation",
				Decision: "D-072",
			},
		},
		FactIDs: []string{"fed_rate_2026_03", "btc_price_2026_10"},
		Passed:  true,
	}

	if err := WriteBacktestSummary(path, s); err != nil {
		t.Fatalf("WriteBacktestSummary failed: %v", err)
	}

	loaded, err := LoadBacktestSummary(path)
	if err != nil {
		t.Fatalf("LoadBacktestSummary failed: %v", err)
	}

	if loaded.GeneratedAt != s.GeneratedAt {
		t.Errorf("GeneratedAt = %v, want %v", loaded.GeneratedAt, s.GeneratedAt)
	}
	if loaded.Command != s.Command {
		t.Errorf("Command = %q, want %q", loaded.Command, s.Command)
	}
	if loaded.Source != s.Source {
		t.Errorf("Source = %q, want %q", loaded.Source, s.Source)
	}
	if loaded.Pairs != s.Pairs {
		t.Errorf("Pairs = %d, want %d", loaded.Pairs, s.Pairs)
	}
	if len(loaded.Mismatches) != len(s.Mismatches) {
		t.Errorf("len(Mismatches) = %d, want %d", len(loaded.Mismatches), len(s.Mismatches))
	}
	if len(loaded.Exceptions) != len(s.Exceptions) {
		t.Errorf("len(Exceptions) = %d, want %d", len(loaded.Exceptions), len(s.Exceptions))
	}
	if loaded.Passed != s.Passed {
		t.Errorf("Passed = %v, want %v", loaded.Passed, s.Passed)
	}
}

func TestLoadBacktestSummary_UnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "summary.json")

	// Write JSON with an unknown field
	data := `{
		"generated_at": "2026-10-09T12:00:00Z",
		"command": "test",
		"as_of": "2026-10-09T00:00:00Z",
		"source": "fixtures",
		"pairs": 10,
		"checked": 5,
		"agreed": 5,
		"disagreed": 0,
		"mismatches": [],
		"exceptions": [],
		"fact_ids": [],
		"passed": true,
		"unknown_field": "should cause error"
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadBacktestSummary(path)
	if err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error = %q, want substring 'unknown field'", err.Error())
	}
}

func TestLoadBacktestSummary_SizeCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "too_large.json")

	// Create a file larger than MaxBacktestSummaryBytes
	large := make([]byte, MaxBacktestSummaryBytes+1)
	if err := os.WriteFile(path, large, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadBacktestSummary(path)
	if err == nil {
		t.Fatal("expected error for oversized file, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %q, want substring 'exceeds'", err.Error())
	}
}

func TestClipTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"short title", "short title"},
		{strings.Repeat("a", maxSummaryTitle), strings.Repeat("a", maxSummaryTitle)},
		{strings.Repeat("a", maxSummaryTitle+1), strings.Repeat("a", maxSummaryTitle-1) + "…"},
		{strings.Repeat("a", maxSummaryTitle+50), strings.Repeat("a", maxSummaryTitle-1) + "…"},
	}

	for _, tt := range tests {
		got := clipTitle(tt.input)
		if got != tt.want {
			t.Errorf("clipTitle(%q) = %q, want %q", tt.input[:20]+"...", got[:20], tt.want[:20])
		}
		if len(got) > maxSummaryTitle {
			t.Errorf("clipTitle returned string longer than %d: %d", maxSummaryTitle, len(got))
		}
	}
}
