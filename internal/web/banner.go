package web

import (
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// pageBase builds the common page fields from the current round (M3-07: centralizes round loading and banner logic).
// The timezone parameter (ET/CT/UTC/Local) controls how times are formatted in templates (M3-08).
func (s *Server) pageBase(r store.Round, title, question, timezone string) page {
	var round *roundInfo
	var bnr banner

	if r.ID == 0 {
		// No round available: show the no-round fallback banner
		bnr = banner{Show: true, Pinned: false}
	} else {
		round = newRoundInfo(r, timezone)
		if r.Pinned {
			// Pinned round: show the demo mode banner formatted in the user's timezone (M3-08)
			asOf := r.FinishedAt
			if asOf.IsZero() {
				asOf = r.StartedAt
			}
			bnr = banner{Show: true, Pinned: true, AsOf: formatTime(asOf, timezone), RoundID: r.ID}
		}
		// else: unpinned round, no banner (bnr is zero value)
	}

	return page{
		Title:    title,
		Question: question,
		Assets:   pinnedAssets,
		Round:    round,
		Banner:   bnr,
		Timezone: timezone,
	}
}
