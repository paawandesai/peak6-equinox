package web

import (
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// pageBase builds the common page fields from the current round (M3-07: centralizes round loading and banner logic).
func (s *Server) pageBase(r store.Round, title, question string) page {
	var round *roundInfo
	var bnr banner

	if r.ID == 0 {
		// No round available: show the no-round fallback banner
		bnr = banner{Show: true, Pinned: false}
	} else {
		round = newRoundInfo(r)
		if r.Pinned {
			// Pinned round: show the demo mode banner
			asOf := r.FinishedAt
			if asOf.IsZero() {
				asOf = r.StartedAt
			}
			// Format in ET (America/New_York) as the default timezone (M3-08)
			etLoc, _ := time.LoadLocation("America/New_York")
			asOfET := asOf.In(etLoc).Format("2006-01-02 15:04:05 MST")
			bnr = banner{Show: true, Pinned: true, AsOf: asOfET, RoundID: r.ID}
		}
		// else: unpinned round, no banner (bnr is zero value)
	}

	return page{
		Title:    title,
		Question: question,
		Assets:   pinnedAssets,
		Round:    round,
		Banner:   bnr,
	}
}
