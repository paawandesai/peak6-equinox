package web

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Badge represents a badge component with glyph, text and CSS class (UI-10, HANDOFF.md).
type Badge struct {
	Class string // CSS class (b-equivalent, f-fresh, h-healthy, v-match, o-routed, etc.)
	Glyph string // Unicode glyph (≡, ✓, ✕, ◐, ?, etc.)
	Text  string // Human-readable text
}

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

// ConfidenceValue returns the float64 value of a decimal confidence for use in meters and display.
func ConfidenceValue(confidence decimal.Decimal) float64 {
	f, _ := confidence.Float64()
	return f
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
// D-098 call 1: complement uses chip-info (blue), same as implies.
func TypeChipClass(t canonical.RelationshipType) string {
	switch t {
	case canonical.RelEquivalent, canonical.RelEquivalentGivenFacts:
		return "chip-ok"
	case canonical.RelImplies, canonical.RelComplement:
		return "chip-info"
	case canonical.RelRelated:
		return "chip-warn"
	case canonical.RelUnrelated:
		return "chip-muted"
	default:
		return "chip-muted"
	}
}

// TypeBadge returns a Badge for a relationship type (UI-10, HANDOFF.md).
// D-098 call 1: complement is blue (b-complement uses --eq-info).
func TypeBadge(t canonical.RelationshipType) Badge {
	switch t {
	case canonical.RelEquivalent:
		return Badge{Class: "b-equivalent", Glyph: "≡", Text: string(t)}
	case canonical.RelEquivalentGivenFacts:
		return Badge{Class: "b-equivalent_given_facts", Glyph: "≡*", Text: string(t)}
	case canonical.RelComplement:
		return Badge{Class: "b-complement", Glyph: "¬", Text: string(t)}
	case canonical.RelImplies:
		return Badge{Class: "b-implies", Glyph: "⇒", Text: string(t)}
	case canonical.RelRelated:
		return Badge{Class: "b-related", Glyph: "~", Text: string(t)}
	case canonical.RelUnrelated:
		return Badge{Class: "b-unrelated", Glyph: "∅", Text: string(t)}
	case "self":
		// UI-13, UI-25: target instrument in input snapshot (not a relationship)
		return Badge{Class: "b-ok", Glyph: "○", Text: "target"}
	default:
		return Badge{Class: "b-unrelated", Glyph: "∅", Text: string(t)}
	}
}

// FreshState represents the freshness state of data (UI-10, HANDOFF.md).
type FreshState string

// Freshness states (UI-10, HANDOFF.md). Warn is part of the set but only renders when UI-19's config exists.
const (
	FreshStateFresh   FreshState = "fresh"
	FreshStateWarn    FreshState = "warn" // UI-19 only (not in UI-10)
	FreshStateStale   FreshState = "stale"
	FreshStateUnknown FreshState = "unknown"
	FreshStatePinned  FreshState = "pinned"
)

// FreshBadge returns a Badge for a freshness state (UI-10, HANDOFF.md).
// Warn is part of the set but only renders when UI-19's config exists.
func FreshBadge(state FreshState) Badge {
	switch state {
	case FreshStateFresh:
		return Badge{Class: "f-fresh", Glyph: "✓", Text: "Fresh"}
	case FreshStateWarn:
		return Badge{Class: "f-warn", Glyph: "◐", Text: "Warn"}
	case FreshStateStale:
		return Badge{Class: "f-stale", Glyph: "✕", Text: "Stale"}
	case FreshStatePinned:
		return Badge{Class: "f-pinned", Glyph: "◆", Text: "Pinned"}
	default:
		return Badge{Class: "f-unknown", Glyph: "?", Text: "Unknown"}
	}
}

// HealthState represents admin health states (UI-10, HANDOFF.md).
type HealthState string

// Health states for admin page components (UI-10, HANDOFF.md).
const (
	HealthHealthy  HealthState = "healthy"
	HealthWarning  HealthState = "warning"
	HealthDegraded HealthState = "degraded"
	HealthUnknown  HealthState = "unknown"
)

// HealthBadge returns a Badge for a health state (UI-10, HANDOFF.md).
func HealthBadge(state HealthState) Badge {
	switch state {
	case HealthHealthy:
		return Badge{Class: "h-healthy", Glyph: "✓", Text: "Healthy"}
	case HealthWarning:
		return Badge{Class: "h-warning", Glyph: "!", Text: "Warning"}
	case HealthDegraded:
		return Badge{Class: "h-degraded", Glyph: "✕", Text: "Degraded"}
	default:
		return Badge{Class: "h-unknown", Glyph: "?", Text: "Unknown"}
	}
}

// VerdictBadge returns a Badge for a contract diff verdict (UI-10, HANDOFF.md).
func VerdictBadge(verdict string) Badge {
	switch verdict {
	case "match":
		return Badge{Class: "v-match", Glyph: "✓", Text: "match"}
	case "differs":
		return Badge{Class: "v-differs", Glyph: "✕", Text: "differs"}
	case "closed":
		return Badge{Class: "v-closed", Glyph: "◇", Text: "closed by fact"}
	default:
		return Badge{Class: "v-unknown", Glyph: "?", Text: "unknown"}
	}
}

// OutcomeBadge returns a Badge for a routing outcome (UI-10, HANDOFF.md).
func OutcomeBadge(outcome string) Badge {
	switch outcome {
	case "routed":
		return Badge{Class: "o-routed", Glyph: "✓", Text: "Routed"}
	case "partial":
		return Badge{Class: "o-partial", Glyph: "◐", Text: "Partially routed"}
	case "notrouted":
		return Badge{Class: "o-notrouted", Glyph: "✕", Text: "Not routed"}
	default:
		return Badge{Class: "o-notrouted", Glyph: "✕", Text: "Not routed"}
	}
}

// RoundStatusBadge returns a Badge for a round status (UI-17, HANDOFF.md).
func RoundStatusBadge(status string) Badge {
	switch status {
	case "ok":
		return Badge{Class: "b-ok", Glyph: "✓", Text: "ok"}
	case "partial":
		return Badge{Class: "b-partial", Glyph: "◐", Text: "partial"}
	case "failed":
		return Badge{Class: "b-failed", Glyph: "✕", Text: "failed"}
	default:
		return Badge{Class: "b-running", Glyph: "…", Text: "running"}
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

// Age computes a human-readable age string from a duration (UI-10, stamp define).
func Age(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%d s ago", s)
	}
	m := s / 60
	if m < 60 {
		return fmt.Sprintf("%d min ago", m)
	}
	h := m / 60
	if h < 24 {
		return fmt.Sprintf("%d h ago", h)
	}
	days := h / 24
	return fmt.Sprintf("%d days ago", days)
}
