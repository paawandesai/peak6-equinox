package web

import (
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// quoteFreshState determines the freshness state for a quote based on its age and venue (UI-19).
// Pinned overrides the age-based state while a round is pinned.
// Between the warn and stale windows (inclusive of warn), returns FreshStateWarn.
func quoteFreshState(venue canonical.Venue, fetchedAt, now time.Time, isPinned bool, warnAfter, staleAfter time.Duration) FreshState {
	if isPinned {
		return FreshStatePinned
	}
	age := now.Sub(fetchedAt)
	if age > staleAfter {
		return FreshStateStale
	}
	if age >= warnAfter {
		return FreshStateWarn
	}
	return FreshStateFresh
}

// warnThresholdForVenue returns the warn threshold for a venue's quotes (UI-19).
func (s *Server) warnThresholdForVenue(venue canonical.Venue) time.Duration {
	switch venue {
	case canonical.VenueKalshi:
		return s.cfg.KalshiWarnAfter
	case canonical.VenuePolymarket:
		return s.cfg.PolymarketWarnAfter
	default:
		return s.cfg.StaleAfter // fallback: no warn window for unknown venues
	}
}
