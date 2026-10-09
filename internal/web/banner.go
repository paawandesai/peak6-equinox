package web

import (
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// pageBase builds the common page fields from the current round (M3-07: centralizes round loading and banner logic).
// The timezone parameter (ET/CT/UTC) controls how times are formatted in templates (M3-08).
// The pageName parameter sets .Page for aria-current navigation highlighting (UI-10).
func (s *Server) pageBase(r store.Round, title, question, timezone, pageName string) page {
	var round *roundInfo
	var bnr banner
	var stamp *stampInfo

	if r.ID == 0 {
		// No round available: show the no-round fallback banner
		bnr = banner{Show: true, Pinned: false}
	} else {
		round = newRoundInfo(r, timezone)
		// Compute freshness stamp (UI-10)
		asOf := r.FinishedAt
		if asOf.IsZero() {
			asOf = r.StartedAt
		}
		age := s.now().Sub(asOf)
		var freshState FreshState
		if r.Pinned {
			freshState = FreshStatePinned
			// Pinned round: show the demo mode banner formatted in the user's timezone (M3-08)
			bnr = banner{Show: true, Pinned: true, AsOf: formatTime(asOf, timezone), RoundID: r.ID}
		} else if r.Status == store.RoundOK || r.Status == store.RoundPartial {
			// Determine freshness based on D-077 page-freshness threshold and UI-19 warn window
			ageMinutes := int(age.Minutes())
			if s.cfg.StaleRoundWarnMinutes > 0 && ageMinutes >= s.cfg.StaleRoundWarnMinutes {
				freshState = FreshStateStale
			} else if s.cfg.RoundWarnMinutes > 0 && ageMinutes >= s.cfg.RoundWarnMinutes {
				freshState = FreshStateWarn
			} else {
				freshState = FreshStateFresh
			}
		} else {
			freshState = FreshStateUnknown
		}
		stamp = &stampInfo{
			Badge:    FreshBadge(freshState),
			AsOf:     formatTime(asOf, timezone),
			DateTime: asOf.UTC().Format(time.RFC3339),
			Age:      Age(age),
		}
	}

	return page{
		Title:    title,
		Question: question,
		Assets:   pinnedAssets,
		Round:    round,
		Stamp:    stamp,
		Banner:   bnr,
		Timezone: timezone,
		Page:     pageName,
	}
}
