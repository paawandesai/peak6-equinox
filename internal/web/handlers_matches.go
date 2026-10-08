package web

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// maxMatchRows caps how many relationships the matches page lists. The
// page fetches one more to know whether it was cut (NFR-01).
const maxMatchRows = 1000

// matchesPage is the data for matches.html.
type matchesPage struct {
	page
	NoRound       bool
	Groups        []pairGroup // All groups (for legacy routes); UI-03 uses Routable, Implies, Related separately
	Routable      []pairGroup // Routable types: equivalent, equivalent_given_facts, complement
	Implies       *pairGroup  // Implies group (nil if empty)
	Related       *pairGroup  // Related group (nil if empty)
	Total         int         // Total pairs kept
	RoutableCount int         // Count of routable pairs
	ImpliesCount  int         // Count of implies pairs
	RelatedCount  int         // Count of related pairs
	Truncated     bool
}

// pairGroup is every pair of one relationship type, strongest type first.
type pairGroup struct {
	Type  string
	Label string
	Pairs []pairRow
}

// pairRow is one relationship as the matches table shows it.
type pairRow struct {
	ID             int64
	A, B           instrumentView
	Type           string
	Confidence     string
	ConfidenceWord string // "high", "medium", "low" from design.md § 4
	ChipClass      string // "chip-ok", "chip-warn", etc. from TypeChipClass
	Method         string
	Vetoes         int
	VetoPhrase     string    // "3 vetoes" or "closed by facts" per design.md § 6
	AsOf           time.Time // Round timestamp, formatted with formatTime in templates (M3-08)
}

// typeLabels say what each relationship type means (domain.md), in one line.
var typeLabels = map[canonical.RelationshipType]string{
	canonical.RelEquivalent:           "Same bet: both contracts pay on the same proposition.",
	canonical.RelEquivalentGivenFacts: "Same bet once a documented external fact is applied.",
	canonical.RelComplement:           "Opposite sides: YES on one is NO on the other.",
	canonical.RelImplies:              "One resolving YES forces the other YES.",
	canonical.RelRelated:              "Near miss: same subject, but a term differs or a veto applies.",
	canonical.RelUnrelated:            "Compared and found unrelated.",
}

// handleMatches answers GET /: every pair in the current round (pinned,
// else the latest ok or partial round), grouped by relationship type (US-01).
func (s *Server) handleMatches(w http.ResponseWriter, r *http.Request) {
	st, ok := s.storeOrFail(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	round := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	data := matchesPage{
		page: s.pageBase(round, "Matches", "Which Kalshi and Polymarket contracts are the same bet, and how sure is Equinox?", tz),
	}
	if round.ID == 0 {
		data.NoRound = true
		s.render(w, http.StatusOK, "matches", data)
		return
	}

	rels, err := st.ListRelationships(ctx, store.RelationshipFilter{RoundID: round.ID, Limit: maxMatchRows + 1})
	if err != nil {
		s.dbError(w, "listing relationships", err)
		return
	}
	if len(rels) > maxMatchRows {
		rels, data.Truncated = rels[:maxMatchRows], true
	}
	data.Total = len(rels)

	views, err := loadInstrumentViews(ctx, st, relationshipInstrumentIDs(rels))
	if err != nil {
		s.dbError(w, "loading instruments for matches", err)
		return
	}
	// groupPairs needs the raw time for pairRow.AsOf (M3-08 Cursor round 2: UI-02 contract keeps Round.AsOf formatted)
	asOf := round.FinishedAt
	if asOf.IsZero() {
		asOf = round.StartedAt
	}
	data.Groups = groupPairs(rels, views, asOf)

	// UI-03: split groups into Routable (combined, sorted by confidence), Implies, Related.
	// Routable pairs are shown as one combined group, not separated by type.
	var routablePairs []pairRow
	for i := range data.Groups {
		g := &data.Groups[i]
		typ := canonical.RelationshipType(g.Type)
		if typ.Routable() {
			routablePairs = append(routablePairs, g.Pairs...)
			data.RoutableCount += len(g.Pairs)
		} else if typ == canonical.RelImplies {
			data.Implies = g
			data.ImpliesCount = len(g.Pairs)
		} else if typ == canonical.RelRelated {
			data.Related = g
			data.RelatedCount = len(g.Pairs)
		}
	}
	// Sort all routable pairs by confidence desc, then ID (across all routable types).
	sort.SliceStable(routablePairs, func(i, j int) bool {
		if routablePairs[i].Confidence != routablePairs[j].Confidence {
			return routablePairs[i].Confidence > routablePairs[j].Confidence
		}
		return routablePairs[i].ID < routablePairs[j].ID
	})
	if len(routablePairs) > 0 {
		data.Routable = []pairGroup{{Type: "routable", Label: "Routable pairs", Pairs: routablePairs}}
	}

	s.render(w, http.StatusOK, "matches", data)
}

// relationshipInstrumentIDs lists every distinct instrument ID in rels.
func relationshipInstrumentIDs(rels []canonical.Relationship) []string {
	seen := make(map[string]bool, 2*len(rels))
	ids := make([]string, 0, 2*len(rels))
	for _, r := range rels {
		for _, id := range []string{r.AID, r.BID} {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// groupPairs buckets rels by type, strongest type first, and orders each
// bucket by confidence (highest first) then ID so the page is deterministic.
func groupPairs(rels []canonical.Relationship, views map[string]instrumentView, asOf time.Time) []pairGroup {
	byType := make(map[canonical.RelationshipType][]pairRow)
	for _, r := range rels {
		// UI-03: compute veto phrase per design.md § 6.
		vetoPhrase := ""
		if len(r.Vetoes) > 0 {
			if r.Type == canonical.RelEquivalentGivenFacts {
				vetoPhrase = "closed by facts"
			} else {
				vetoPhrase = fmt.Sprintf("%d veto", len(r.Vetoes))
				if len(r.Vetoes) > 1 {
					vetoPhrase += "es"
				}
			}
		}

		byType[r.Type] = append(byType[r.Type], pairRow{
			ID:             r.ID,
			A:              views[r.AID],
			B:              views[r.BID],
			Type:           string(r.Type),
			Confidence:     r.Confidence.StringFixed(2),
			ConfidenceWord: ConfidenceWord(r.Confidence),
			ChipClass:      TypeChipClass(r.Type),
			Method:         string(r.Method),
			Vetoes:         len(r.Vetoes),
			VetoPhrase:     vetoPhrase,
			AsOf:           asOf,
		})
	}
	var groups []pairGroup
	for _, t := range canonical.RelationshipTypes() {
		rows := byType[t]
		if len(rows) == 0 {
			continue
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Confidence != rows[j].Confidence {
				return rows[i].Confidence > rows[j].Confidence
			}
			return rows[i].ID < rows[j].ID
		})
		groups = append(groups, pairGroup{Type: string(t), Label: typeLabels[t], Pairs: rows})
	}
	return groups
}
