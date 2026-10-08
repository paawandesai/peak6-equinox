package web

import (
	"testing"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestConfidenceWord(t *testing.T) {
	tests := []struct {
		confidence string
		want       string
	}{
		{"0.00", "low"},
		{"0.69", "low"},
		{"0.70", "medium"},
		{"0.89", "medium"},
		{"0.90", "high"},
		{"1.00", "high"},
	}
	for _, tt := range tests {
		t.Run(tt.confidence, func(t *testing.T) {
			conf, _ := decimal.NewFromString(tt.confidence)
			got := ConfidenceWord(conf)
			if got != tt.want {
				t.Errorf("ConfidenceWord(%s) = %q; want %q", tt.confidence, got, tt.want)
			}
		})
	}
}

func TestBasisRiskWord(t *testing.T) {
	tests := []struct {
		name string
		br   *canonical.BasisRisk
		want string
	}{
		{"nil", nil, "not scored"},
		{"0.00", &canonical.BasisRisk{Score: decimal.NewFromFloat(0.00)}, "low"},
		{"0.19", &canonical.BasisRisk{Score: decimal.NewFromFloat(0.19)}, "low"},
		{"0.20", &canonical.BasisRisk{Score: decimal.NewFromFloat(0.20)}, "moderate"},
		{"0.49", &canonical.BasisRisk{Score: decimal.NewFromFloat(0.49)}, "moderate"},
		{"0.50", &canonical.BasisRisk{Score: decimal.NewFromFloat(0.50)}, "high"},
		{"1.00", &canonical.BasisRisk{Score: decimal.NewFromFloat(1.00)}, "high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BasisRiskWord(tt.br)
			if got != tt.want {
				t.Errorf("BasisRiskWord(%v) = %q; want %q", tt.br, got, tt.want)
			}
		})
	}
}

func TestTypeChipClass(t *testing.T) {
	tests := []struct {
		relType canonical.RelationshipType
		want    string
	}{
		{canonical.RelEquivalent, "chip-ok"},
		{canonical.RelEquivalentGivenFacts, "chip-ok"},
		{canonical.RelComplement, "chip-info"}, // D-098 call 1: complement is blue
		{canonical.RelImplies, "chip-info"},
		{canonical.RelRelated, "chip-warn"},
		{canonical.RelUnrelated, "chip-muted"},
		{canonical.RelationshipType("unknown"), "chip-muted"},
	}
	for _, tt := range tests {
		t.Run(string(tt.relType), func(t *testing.T) {
			got := TypeChipClass(tt.relType)
			if got != tt.want {
				t.Errorf("TypeChipClass(%s) = %q; want %q", tt.relType, got, tt.want)
			}
		})
	}
}

func TestOutcomePhrase(t *testing.T) {
	tests := []struct {
		name       string
		chosen     bool
		partial    bool
		complement bool
		venueCount int
		wantPhrase string
		wantChip   string
	}{
		{"not routed", false, false, false, 0, "Not routed", "chip-danger"},
		{"partial", true, true, false, 1, "Partially routed", "chip-warn"},
		{"complement", true, false, true, 1, "Routed as complement", "chip-ok"},
		{"multi-venue", true, false, false, 2, "Routed across 2 venues", "chip-ok"},
		{"single venue", true, false, false, 1, "Routed", "chip-ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phrase, chip := OutcomePhrase(tt.chosen, tt.partial, tt.complement, tt.venueCount)
			if phrase != tt.wantPhrase || chip != tt.wantChip {
				t.Errorf("OutcomePhrase(%v, %v, %v, %d) = (%q, %q); want (%q, %q)",
					tt.chosen, tt.partial, tt.complement, tt.venueCount,
					phrase, chip, tt.wantPhrase, tt.wantChip)
			}
		})
	}
}

// UI-10: Badge function tests (HANDOFF.md shared system)

func TestTypeBadge(t *testing.T) {
	tests := []struct {
		relType   canonical.RelationshipType
		wantClass string
		wantGlyph string
	}{
		{canonical.RelEquivalent, "b-equivalent", "≡"},
		{canonical.RelEquivalentGivenFacts, "b-equivalent_given_facts", "≡*"},
		{canonical.RelComplement, "b-complement", "¬"}, // D-098 call 1: blue class
		{canonical.RelImplies, "b-implies", "⇒"},
		{canonical.RelRelated, "b-related", "~"},
		{canonical.RelUnrelated, "b-unrelated", "∅"},
	}
	for _, tt := range tests {
		t.Run(string(tt.relType), func(t *testing.T) {
			got := TypeBadge(tt.relType)
			if got.Class != tt.wantClass || got.Glyph != tt.wantGlyph {
				t.Errorf("TypeBadge(%s) = {Class: %q, Glyph: %q}; want {Class: %q, Glyph: %q}",
					tt.relType, got.Class, got.Glyph, tt.wantClass, tt.wantGlyph)
			}
			if got.Text != string(tt.relType) {
				t.Errorf("TypeBadge(%s).Text = %q; want %q", tt.relType, got.Text, string(tt.relType))
			}
		})
	}
}

func TestFreshBadge(t *testing.T) {
	tests := []struct {
		state     FreshState
		wantClass string
		wantGlyph string
		wantText  string
	}{
		{FreshStateFresh, "f-fresh", "✓", "Fresh"},
		{FreshStateWarn, "f-warn", "◐", "Warn"},
		{FreshStateStale, "f-stale", "✕", "Stale"},
		{FreshStatePinned, "f-pinned", "◆", "Pinned"},
		{FreshStateUnknown, "f-unknown", "?", "Unknown"},
		{FreshState("invalid"), "f-unknown", "?", "Unknown"}, // fallback
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := FreshBadge(tt.state)
			if got.Class != tt.wantClass || got.Glyph != tt.wantGlyph || got.Text != tt.wantText {
				t.Errorf("FreshBadge(%s) = {%q, %q, %q}; want {%q, %q, %q}",
					tt.state, got.Class, got.Glyph, got.Text, tt.wantClass, tt.wantGlyph, tt.wantText)
			}
		})
	}
}

func TestHealthBadge(t *testing.T) {
	tests := []struct {
		state     HealthState
		wantClass string
		wantGlyph string
		wantText  string
	}{
		{HealthHealthy, "h-healthy", "✓", "Healthy"},
		{HealthWarning, "h-warning", "!", "Warning"},
		{HealthDegraded, "h-degraded", "✕", "Degraded"},
		{HealthUnknown, "h-unknown", "?", "Unknown"},
		{HealthState("invalid"), "h-unknown", "?", "Unknown"}, // fallback
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := HealthBadge(tt.state)
			if got.Class != tt.wantClass || got.Glyph != tt.wantGlyph || got.Text != tt.wantText {
				t.Errorf("HealthBadge(%s) = {%q, %q, %q}; want {%q, %q, %q}",
					tt.state, got.Class, got.Glyph, got.Text, tt.wantClass, tt.wantGlyph, tt.wantText)
			}
		})
	}
}

func TestVerdictBadge(t *testing.T) {
	tests := []struct {
		verdict   string
		wantClass string
		wantGlyph string
		wantText  string
	}{
		{"match", "v-match", "✓", "match"},
		{"differs", "v-differs", "✕", "differs"},
		{"closed", "v-closed", "◇", "closed by fact"},
		{"unknown", "v-unknown", "?", "unknown"},
		{"invalid", "v-unknown", "?", "unknown"}, // fallback
	}
	for _, tt := range tests {
		t.Run(tt.verdict, func(t *testing.T) {
			got := VerdictBadge(tt.verdict)
			if got.Class != tt.wantClass || got.Glyph != tt.wantGlyph || got.Text != tt.wantText {
				t.Errorf("VerdictBadge(%s) = {%q, %q, %q}; want {%q, %q, %q}",
					tt.verdict, got.Class, got.Glyph, got.Text, tt.wantClass, tt.wantGlyph, tt.wantText)
			}
		})
	}
}

func TestOutcomeBadge(t *testing.T) {
	tests := []struct {
		outcome   string
		wantClass string
		wantGlyph string
		wantText  string
	}{
		{"routed", "o-routed", "✓", "Routed"},
		{"partial", "o-partial", "◐", "Partially routed"},
		{"notrouted", "o-notrouted", "✕", "Not routed"},
		{"invalid", "o-notrouted", "✕", "Not routed"}, // fallback
	}
	for _, tt := range tests {
		t.Run(tt.outcome, func(t *testing.T) {
			got := OutcomeBadge(tt.outcome)
			if got.Class != tt.wantClass || got.Glyph != tt.wantGlyph || got.Text != tt.wantText {
				t.Errorf("OutcomeBadge(%s) = {%q, %q, %q}; want {%q, %q, %q}",
					tt.outcome, got.Class, got.Glyph, got.Text, tt.wantClass, tt.wantGlyph, tt.wantText)
			}
		})
	}
}

func TestConfidenceValue(t *testing.T) {
	tests := []struct {
		confidence string
		want       float64
	}{
		{"0.69", 0.69},
		{"0.70", 0.70},
		{"0.89", 0.89},
		{"0.90", 0.90},
	}
	for _, tt := range tests {
		t.Run(tt.confidence, func(t *testing.T) {
			conf, _ := decimal.NewFromString(tt.confidence)
			got := ConfidenceValue(conf)
			if got != tt.want {
				t.Errorf("ConfidenceValue(%s) = %f; want %f", tt.confidence, got, tt.want)
			}
		})
	}
}
