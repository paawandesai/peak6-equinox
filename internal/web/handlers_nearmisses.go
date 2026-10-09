package web

import (
	"context"
	"net/http"
	"sort"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// nearMissesPage is the data for near_misses.html (US-01, FR-21).
type nearMissesPage struct {
	page
	NoRound   bool
	Rows      []nearMissRow
	Total     int
	Truncated int // rows dropped by the cap
	VetoCount int // rows where at least one hard veto fired
}

// nearMissRow is one stored `related` pair with the veto(s) that blocked it.
type nearMissRow struct {
	ID          int64
	A, B        instrumentView
	Confidence  string
	Vetoes      []canonical.Veto
	Facts       []canonical.FactUse
	Explanation string
}

// handleNearMisses answers GET /near-misses: every `related` pair stored for
// the current round. `equinox match` keeps a `related` result only when it
// is a near miss (D-061, D-068): the same subject with one term off, or a
// pair a fact was consulted for but did not close. Each row names the hard
// veto that fired, or says that no veto fired and the fact did not apply.
func (s *Server) handleNearMisses(w http.ResponseWriter, r *http.Request) {
	st, ok := s.storeOrFail(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	round := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	data := nearMissesPage{
		page: s.pageBase(round, "Near misses", "Which pairs looked like the same bet but were rejected, and by which rule?", tz, "near_misses"),
	}
	if round.ID == 0 {
		data.NoRound = true
		s.render(w, http.StatusOK, "near_misses", data)
		return
	}

	rels, err := st.ListRelationships(ctx, store.RelationshipFilter{RoundID: round.ID, Type: canonical.RelRelated, Limit: maxMatchRows + 1})
	if err != nil {
		s.dbError(w, "listing near misses", err)
		return
	}
	if len(rels) > maxMatchRows {
		rels, data.Truncated = rels[:maxMatchRows], len(rels)-maxMatchRows
	}
	views, err := loadInstrumentViews(ctx, st, relationshipInstrumentIDs(rels))
	if err != nil {
		s.dbError(w, "loading instruments for near misses", err)
		return
	}
	data.Rows = nearMissRows(rels, views)
	data.Total = len(data.Rows)
	for _, row := range data.Rows {
		if len(row.Vetoes) > 0 {
			data.VetoCount++
		}
	}
	s.render(w, http.StatusOK, "near_misses", data)
}

// nearMissRows orders rows with vetoed pairs first, then by confidence
// (highest first) and ID, so the page is deterministic.
func nearMissRows(rels []canonical.Relationship, views map[string]instrumentView) []nearMissRow {
	rows := make([]nearMissRow, 0, len(rels))
	for _, r := range rels {
		rows = append(rows, nearMissRow{
			ID: r.ID, A: views[r.AID], B: views[r.BID], Confidence: r.Confidence.StringFixed(2),
			Vetoes: r.Vetoes, Facts: r.FactsUsed, Explanation: r.Explanation,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		vi, vj := len(rows[i].Vetoes) > 0, len(rows[j].Vetoes) > 0
		if vi != vj {
			return vi
		}
		if rows[i].Confidence != rows[j].Confidence {
			return rows[i].Confidence > rows[j].Confidence
		}
		return rows[i].ID < rows[j].ID
	})
	return rows
}
