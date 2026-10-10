package web

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// nearMissesPage is the data for near_misses.html (US-01, FR-21).
type nearMissesPage struct {
	page
	NoRound         bool
	Groups          []nearMissGroup
	Total           int
	Truncated       int // rows dropped by the cap
	VetoCount       int // rows where at least one hard veto fired
	FactDidNotClose int // rows where no veto fired (Total - VetoCount)
}

// nearMissGroup groups near-miss rows by the veto code that fired.
type nearMissGroup struct {
	Code        string // veto code, or "fact_did_not_close"
	Count       int    // rows in this group
	Description string // one-line description of this failure reason
	Rows        []nearMissRow
	Anchor      string // HTML id for this section
	Truncated   int    // rows not shown when capped at 25 (M5-10)
	ShowAllLink string // link to show all rows for this group (M5-10)
}

// nearMissRow is one stored `related` pair with the veto(s) that blocked it.
type nearMissRow struct {
	ID              int64
	A, B            instrumentView
	Confidence      string
	VetoBadge       Badge  // badge for the veto that fired
	VetoField       string // field name (e.g., "subject")
	VetoDetail      string // detail text (e.g., "BTC is not ETH")
	VetoDetailWords string // comparator rendered as words (M5-10)
	TermGap         string // how close (numeric difference or time distance, or "—")
	Score           string // UI-20: "X.XX of Y.YY required" or "not recorded"
	Facts           []canonical.FactUse
	Explanation     string
	LogLink         string // link to the pair's verdict entry
}

// handleNearMisses answers GET /near-misses: every `related` pair stored for
// the current round. `equinox match` keeps a `related` result only when it
// is a near miss (D-061, D-068): the same subject with one term off, or a
// pair a fact was consulted for but did not close. Rows are grouped by the
// veto that fired (UI-14). M5-10 adds ?veto=<code>&all=1 to show all rows
// for a specific group (default: 25 per group).
func (s *Server) handleNearMisses(w http.ResponseWriter, r *http.Request) {
	st, ok := s.storeOrFail(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	// M5-10: Parse query parameters
	vetoFilter := r.URL.Query().Get("veto")
	showAll := r.URL.Query().Get("all") == "1"

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
	rows := nearMissRows(rels, views)
	data.Total = len(rows)

	// M5-10: If veto filter is set, validate it exists in the data
	if vetoFilter != "" {
		found := false
		for _, row := range rows {
			code := row.VetoBadge.Text
			if code == "" {
				code = "fact_did_not_close"
			}
			if code == vetoFilter {
				found = true
				break
			}
		}
		if !found {
			s.renderErrorWithContext(w, r, http.StatusBadRequest, "Unknown veto code.")
			return
		}
	}

	data.Groups = groupNearMisses(rows, vetoFilter, showAll)
	data.VetoCount = 0
	for _, row := range rows {
		if row.VetoBadge.Text != "" {
			data.VetoCount++
		}
	}
	data.FactDidNotClose = data.Total - data.VetoCount
	s.render(w, http.StatusOK, "near_misses", data)
}

// nearMissRows builds rows from relationships, computing TermGap and badges.
func nearMissRows(rels []canonical.Relationship, views map[string]instrumentView) []nearMissRow {
	rows := make([]nearMissRow, 0, len(rels))
	for _, r := range rels {
		row := nearMissRow{
			ID:          r.ID,
			A:           views[r.AID],
			B:           views[r.BID],
			Confidence:  r.Confidence.StringFixed(2),
			Facts:       r.FactsUsed,
			Explanation: r.Explanation,
			LogLink:     fmt.Sprintf("/pairs/%d", r.ID),
			TermGap:     computeTermGap(r.Diff),
		}
		// UI-20: Pre-veto score
		if r.Evidence != nil && r.Evidence.PreVetoScore != nil && r.Evidence.RequiredScore != nil {
			row.Score = fmt.Sprintf("%s of %s required",
				r.Evidence.PreVetoScore.StringFixed(2),
				r.Evidence.RequiredScore.StringFixed(2))
		} else {
			row.Score = "not recorded"
		}
		// The first veto is shown
		if len(r.Vetoes) > 0 {
			v := r.Vetoes[0]
			row.VetoBadge = Badge{Class: "b-danger mono", Glyph: "✕", Text: v.Code}
			row.VetoField = v.Field
			row.VetoDetail = v.Detail
			// M5-10: Render comparators as words
			row.VetoDetailWords = comparatorToWords(v.Detail)
		}
		rows = append(rows, row)
	}
	return rows
}

// groupNearMisses groups rows by veto code. Rows with the same first veto
// code are grouped together, and rows without a veto go under "fact did not
// close". Groups are sorted by code. M5-10 adds filtering by veto code and
// a 25-row cap per group (unless showAll is true).
func groupNearMisses(rows []nearMissRow, vetoFilter string, showAll bool) []nearMissGroup {
	const maxRowsPerGroup = 25

	codeMap := make(map[string][]nearMissRow)
	for _, row := range rows {
		code := row.VetoBadge.Text
		if code == "" {
			code = "fact_did_not_close"
		}
		codeMap[code] = append(codeMap[code], row)
	}
	var groups []nearMissGroup
	for code, gRows := range codeMap {
		// M5-10: Skip groups not matching the filter
		if vetoFilter != "" && code != vetoFilter {
			continue
		}

		group := nearMissGroup{
			Code:        code,
			Count:       len(gRows),
			Description: vetoDescription(code),
			Anchor:      "group-" + strings.ReplaceAll(code, "_", "-"),
		}

		// M5-10: Cap rows at 25 unless showAll or this is the filtered group with all=1
		if !showAll && len(gRows) > maxRowsPerGroup {
			group.Rows = gRows[:maxRowsPerGroup]
			group.Truncated = len(gRows) - maxRowsPerGroup
			group.ShowAllLink = fmt.Sprintf("/near-misses?veto=%s&all=1#%s", code, group.Anchor)
		} else {
			group.Rows = gRows
		}

		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		// "fact_did_not_close" always last
		if groups[i].Code == "fact_did_not_close" {
			return false
		}
		if groups[j].Code == "fact_did_not_close" {
			return true
		}
		return groups[i].Code < groups[j].Code
	})
	return groups
}

// computeTermGap returns "how close" the near miss was: the numeric difference
// for thresholds, time distance for deadlines, or "—" for non-numeric fields.
func computeTermGap(diff []canonical.FieldDiff) string {
	for _, d := range diff {
		if d.Status != canonical.DiffDiffer {
			continue
		}
		// Try parsing as decimals for threshold
		if a, err1 := decimal.NewFromString(d.A); err1 == nil {
			if b, err2 := decimal.NewFromString(d.B); err2 == nil {
				gap := a.Sub(b).Abs()
				return gap.StringFixed(2)
			}
		}
		// Try parsing as time for deadline
		if ta, err1 := time.Parse(time.RFC3339, d.A); err1 == nil {
			if tb, err2 := time.Parse(time.RFC3339, d.B); err2 == nil {
				dur := ta.Sub(tb)
				if dur < 0 {
					dur = -dur
				}
				return formatDurationDistance(dur)
			}
		}
	}
	return "—"
}

// formatDurationDistance formats a duration as a readable distance.
func formatDurationDistance(d time.Duration) string {
	absDays := math.Abs(d.Hours() / 24)
	if absDays >= 1 {
		return fmt.Sprintf("%.0f day%s", absDays, pluralS(int(absDays)))
	}
	absHours := math.Abs(d.Hours())
	if absHours >= 1 {
		return fmt.Sprintf("%.0f hour%s", absHours, pluralS(int(absHours)))
	}
	absMinutes := math.Abs(d.Minutes())
	return fmt.Sprintf("%.0f minute%s", absMinutes, pluralS(int(absMinutes)))
}

// pluralS returns "s" if n != 1, else "".
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// vetoDescription returns a one-sentence plain description for each veto
// code in internal/match/veto.go, plus "fact_did_not_close" (M5-06). The
// /glossary page lists the same text; TestVetoDescriptionCoverage fails if a
// code in veto.go has no entry here.
func vetoDescription(code string) string {
	switch code {
	case "subject_mismatch":
		return "Different underlying subject: the two contracts are about different things, such as BTC and ETH."
	case "subject_unknown":
		return "The subject could not be read from at least one contract, so the pair cannot be the same bet."
	case "metric_mismatch":
		return "The contracts measure different things, such as a closing price and an intraday high, or a rate level and a rate change."
	case "metric_unknown":
		return "What is being measured could not be read from at least one contract."
	case "comparator_mismatch":
		return "The comparison differs, such as above versus below, or strictly above versus at or above."
	case "comparator_unknown":
		return "The comparison could not be read from at least one contract, or it states two."
	case "threshold_mismatch":
		return "Strike or threshold differs: any difference in the number counts, with no tick-size tolerance."
	case "threshold_unknown":
		return "The strike or threshold could not be read from at least one contract."
	case "deadline_gap":
		return "The resolution deadlines are 24 hours or more apart."
	case "deadline_mismatch":
		return "Resolution deadline differs: in categories with an exact measurement time, such as crypto, any gap at all counts."
	case "deadline_unknown":
		return "The resolution deadline could not be read from at least one contract."
	case "source_mismatch":
		return "Data source differs: the contracts settle on different authorities or indexes."
	case "source_unknown":
		return "The resolution source could not be read from at least one contract."
	case "polarity_mismatch":
		return "One contract asks whether something happens and the other whether it does not."
	case "polarity_unresolved":
		return "Whether the contract asks for the event or its negation could not be read."
	case "fact_did_not_close":
		return "A fact was consulted but did not close the difference, so the pair stays related."
	default:
		return strings.ReplaceAll(code, "_", " ")
	}
}

// comparatorToWords converts comparator codes like "gt vs lt" to readable
// phrases like "above vs below" (M5-10). Keeps the code in <code> elements.
func comparatorToWords(detail string) string {
	// Replace longer codes first to avoid partial replacements
	// (e.g., "gte" before "gt" to avoid "abovee")
	replacements := []struct{ code, word string }{
		{"gte", "at or above"},
		{"lte", "at or below"},
		{"between", "between"},
		{"gt", "above"},
		{"lt", "below"},
		{"eq", "equal to"},
	}

	result := detail
	for _, r := range replacements {
		result = strings.ReplaceAll(result, r.code, r.word)
	}

	return result
}
