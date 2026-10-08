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
		{canonical.RelComplement, "chip-ok"},
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
