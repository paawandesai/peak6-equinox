package web

import (
	"fmt"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// ConfidenceWord maps a confidence score to its bucket word per design.md § 4.
// "high" ≥ 0.90 (the MIN_ROUTE_CONFIDENCE floor), "medium" 0.70 to 0.89, "low" below.
func ConfidenceWord(confidence decimal.Decimal) string {
	if confidence.GreaterThanOrEqual(decimal.RequireFromString("0.90")) {
		return "high"
	}
	if confidence.GreaterThanOrEqual(decimal.RequireFromString("0.70")) {
		return "medium"
	}
	return "low"
}

// BasisRiskWord maps a basis-risk score to its bucket word per design.md § 4.
// "low" < 0.20, "moderate" 0.20 to 0.49, "high" ≥ 0.50. Returns "not scored" when nil.
func BasisRiskWord(br *canonical.BasisRisk) string {
	if br == nil {
		return "not scored"
	}
	if br.Score.LessThan(decimal.RequireFromString("0.20")) {
		return "low"
	}
	if br.Score.LessThan(decimal.RequireFromString("0.50")) {
		return "moderate"
	}
	return "high"
}

// TypeChipClass maps a relationship type to its CSS chip class per design.md § 4.
func TypeChipClass(t canonical.RelationshipType) string {
	switch t {
	case canonical.RelEquivalent, canonical.RelEquivalentGivenFacts, canonical.RelComplement:
		return "chip-ok"
	case canonical.RelImplies:
		return "chip-info"
	case canonical.RelRelated:
		return "chip-warn"
	case canonical.RelUnrelated:
		return "chip-muted"
	default:
		return "chip-muted"
	}
}

// OutcomePhrase returns the human-readable outcome phrase and chip class for a routing decision (design.md § 4).
// It maps the decision state into a phrase and a CSS chip class for display.
//
// Parameters:
//   - chosen: true when the decision has at least one chosen leg (order was routed, fully or partially)
//   - partial: true when the fill is incomplete (IOC filled less than requested)
//   - isComplement: true when any chosen leg is a complement route (bought NO on the complement instrument)
//   - venueCount: the number of distinct venues in chosen legs (1 for single-venue, >1 for sweep)
//
// Returns a phrase ("Routed", "Partially routed", etc.) and a chip class ("chip-ok", "chip-warn", "chip-danger").
// The phrase is used in the routing trace header strip; the chip class determines the visual treatment.
func OutcomePhrase(chosen bool, partial bool, isComplement bool, venueCount int) (phrase, chipClass string) {
	if !chosen {
		return "Not routed", "chip-danger"
	}
	if partial {
		return "Partially routed", "chip-warn"
	}
	if isComplement {
		return "Routed as complement", "chip-ok"
	}
	if venueCount > 1 {
		return fmt.Sprintf("Routed across %d venues", venueCount), "chip-ok"
	}
	return "Routed", "chip-ok"
}
