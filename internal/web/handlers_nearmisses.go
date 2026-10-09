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
}

// nearMissRow is one stored `related` pair with the veto(s) that blocked it.
type nearMissRow struct {
	ID          int64
	A, B        instrumentView
	Confidence  string
	VetoBadge   Badge  // badge for the veto that fired
	VetoField   string // field name (e.g., "subject")
	VetoDetail  string // detail text (e.g., "BTC is not ETH")
	TermGap     string // how close (numeric difference or time distance, or "—")
	Facts       []canonical.FactUse
	Explanation string
	LogLink     string // link to the pair's verdict entry
}

// handleNearMisses answers GET /near-misses: every `related` pair stored for
// the current round. `equinox match` keeps a `related` result only when it
// is a near miss (D-061, D-068): the same subject with one term off, or a
// pair a fact was consulted for but did not close. Rows are grouped by the
// veto that fired (UI-14).
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
	rows := nearMissRows(rels, views)
	data.Total = len(rows)
	data.Groups = groupNearMisses(rows)
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
		// The first veto is shown
		if len(r.Vetoes) > 0 {
			v := r.Vetoes[0]
			row.VetoBadge = Badge{Class: "b-danger mono", Glyph: "✕", Text: v.Code}
			row.VetoField = v.Field
			row.VetoDetail = v.Detail
		}
		rows = append(rows, row)
	}
	return rows
}

// groupNearMisses groups rows by veto code. Rows with the same first veto
// code are grouped together, and rows without a veto go under "fact did not
// close". Groups are sorted by code.
func groupNearMisses(rows []nearMissRow) []nearMissGroup {
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
		groups = append(groups, nearMissGroup{
			Code:        code,
			Count:       len(gRows),
			Description: vetoDescription(code),
			Rows:        gRows,
			Anchor:      "group-" + strings.ReplaceAll(code, "_", "-"),
		})
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
				return formatDurationVerbose(dur)
			}
		}
	}
	return "—"
}

// formatDuration formats a duration as a readable distance.
func formatDurationVerbose(d time.Duration) string {
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

// vetoDescription returns a one-line description for each veto code.
func vetoDescription(code string) string {
	switch code {
	case "subject_mismatch":
		return "Different underlying subject"
	case "threshold_mismatch":
		return "Strike or threshold differs"
	case "deadline_mismatch":
		return "Resolution deadline differs"
	case "direction_mismatch":
		return "Comparison direction differs (over vs under)"
	case "source_mismatch":
		return "Data source differs"
	case "fact_did_not_close":
		return "A fact was consulted but did not close the difference"
	default:
		return strings.ReplaceAll(code, "_", " ")
	}
}
