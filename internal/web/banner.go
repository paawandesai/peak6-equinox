package web

import (
	"fmt"
	"html/template"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// demoBanner returns the demo mode banner HTML when r is pinned, else "".
// NFR-04: when a round is pinned, all pages show "Demo mode: data as of <timestamp>".
// The timestamp is formatted in ET (default timezone per M3-08).
func demoBanner(r store.Round) template.HTML {
	if !r.Pinned {
		return ""
	}
	asOf := r.FinishedAt
	if asOf.IsZero() {
		asOf = r.StartedAt
	}
	// Format in ET (America/New_York) as the default timezone
	etLoc, _ := time.LoadLocation("America/New_York")
	asOfET := asOf.In(etLoc).Format("2006-01-02 15:04:05 MST")
	// #nosec G203: timestamp is escaped; link is constant
	return template.HTML(fmt.Sprintf(`Demo mode: data as of %s (round %d). <a href="/admin">Unpin</a>`,
		template.HTMLEscapeString(asOfET), r.ID))
}

// noRoundBanner returns the fallback banner when no round exists.
func noRoundBanner() template.HTML {
	return template.HTML(`No data available yet. Run ingest from <a href="/admin">/admin</a>.`)
}

// pageBase builds the common page fields from the current round (M3-07: centralizes round loading and banner logic).
func (s *Server) pageBase(r store.Round, title, question string) page {
	var round *roundInfo
	var banner template.HTML

	if r.ID == 0 {
		// No round available
		banner = noRoundBanner()
	} else {
		round = newRoundInfo(r)
		banner = demoBanner(r)
	}

	return page{
		Title:      title,
		Question:   question,
		Assets:     pinnedAssets,
		Round:      round,
		DemoBanner: banner,
	}
}
