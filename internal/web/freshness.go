package web

import (
	"math"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// quoteFreshState determines the freshness state for a quote based on its age (UI-19; venue reserved for callers).
// Pinned overrides the age-based state while a round is pinned.
// Between the warn and stale windows (inclusive of warn), returns FreshStateWarn.
func quoteFreshState(_ canonical.Venue, fetchedAt, now time.Time, isPinned bool, warnAfter, staleAfter time.Duration) FreshState {
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

// quoteAgeAt is the one quote-freshness rule (M6 P3) that the matches list,
// the pair page's current touch and the routing trace all apply, so the three
// never disagree. Age is measured against the routing reference (quoteSnapshot
// for a live page, decisionSnapshot for a stored decision), never the wall
// clock, and a quote is stale exactly when the router would exclude it for
// staleness: age > staleAfter (route.IsEligible). A zero reference or fetch
// time is unknown; a fetch after the reference counts as age zero.
func quoteAgeAt(ref, fetchedAt time.Time, staleAfter time.Duration) (time.Duration, FreshState) {
	if ref.IsZero() || fetchedAt.IsZero() {
		return 0, FreshStateUnknown
	}
	age := max(ref.Sub(fetchedAt), 0)
	// No warn window: the trace and the matches list show fresh or stale only.
	return age, quoteFreshState("", fetchedAt, ref, false, noWarnWindow, staleAfter)
}

// noWarnWindow disables quoteFreshState's warn state.
const noWarnWindow = time.Duration(math.MaxInt64)

// worseFresh returns the less fresh of two states: stale, then fresh; unknown
// only when both are unknown (a missing quote is reported elsewhere).
func worseFresh(a, b FreshState) FreshState {
	rank := func(s FreshState) int {
		switch s {
		case FreshStateStale:
			return 2
		case FreshStateFresh:
			return 1
		default:
			return 0
		}
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}
