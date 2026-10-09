package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

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
	FlaggedCount  int         // UI-18: Count of pairs flagged for review
	Truncated     bool
	// UI-11: filter and sort state
	Sort        string // "priority", "confidence", "id"
	MinConf     string // raw min param value for hidden field
	MinConfText string // "≥ 0.70" etc., empty when no filter
	MinConfHref string // URL to remove the min filter
	Q           string // raw q param value for hidden field
	QText       string // "Title contains "fed"", empty when no filter
	QHref       string // URL to remove the q filter
	HasFilters  bool   // true if any filter is active
	// UI-18: fee estimates for checks key
	FeeEstimates []FeeEstimate
}

// pairGroup is every pair of one relationship type, strongest type first.
type pairGroup struct {
	Type  string
	Label string
	Pairs []pairRow
}

// checkItem is one term check in the Checks column (UI-11).
type checkItem struct {
	Glyph string // "✓", "✕", "◇", "?"
	Class string // CSS class for color
	Word  string // "Date", "Strike", "Direction", "Source"
	SR    string // screen-reader text: "matches", "differs", etc.
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
	// UI-11: new fields for Matches v2
	BasisRisk      string      // "0.38" formatted basis risk score
	BasisWord      string      // "low", "moderate", "high" from BasisRiskWord
	Checks         []checkItem // date, strike, direction, source check glyphs
	MaxQuoteAge    int         // seconds since oldest quote in this pair
	ReviewPriority float64     // sort key: for routable, (conf - 0.90) asc, then veto count desc
	TypeBadge      Badge       // UI-11 Cursor round 1: use {{template "badge"}} not span.chip
	FreshBadge     Badge       // UI-11 Cursor round 1: freshness badge from MaxQuoteAge
	ConfValue      struct {
		Value float64
		Word  string
		Class string
	} // UI-11 Cursor round 1: {{template "conf"}} expects Value/Word/Class
	// UI-18: net gap and review flag
	NetGap        NetGap // fee-adjusted gap at reference size
	NetGapText    string // "0.02" or "(0.01)" per sign rule, empty when !NetGap.HasValue
	IsFlagged     bool   // true when net gap >= REVIEW_EDGE_MIN and confidence < 0.90
	FlaggedReason string // "⚠ review: large edge, low confidence" when IsFlagged
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

// termCheckFields maps contract term fields to their check labels (UI-11).
var termCheckFields = map[string]string{
	"deadline":   "Date",
	"threshold":  "Strike",
	"comparator": "Direction",
	"source":     "Source",
}

// buildChecks derives the 4 term checks (date, strike, direction, source) from the diff rows (UI-11).
// Status "match" → ✓, "differ" → ✕, "differ" + egf → ◇, "unknown" → ?.
func buildChecks(diff []canonical.FieldDiff, isEGF bool) []checkItem {
	// Map field → status from the diff.
	statuses := make(map[string]canonical.DiffStatus)
	for _, d := range diff {
		if _, ok := termCheckFields[d.Field]; ok {
			statuses[d.Field] = d.Status
		}
	}
	// Build checks in order: date, strike, direction, source.
	order := []string{"deadline", "threshold", "comparator", "source"}
	checks := make([]checkItem, 0, 4)
	for _, field := range order {
		status, found := statuses[field]
		if !found {
			status = canonical.DiffUnknown
		}
		checks = append(checks, checkItemForStatus(status, termCheckFields[field], isEGF))
	}
	return checks
}

// checkItemForStatus maps a diff status to a checkItem (UI-11).
func checkItemForStatus(status canonical.DiffStatus, word string, isEGF bool) checkItem {
	switch status {
	case canonical.DiffMatch:
		return checkItem{Glyph: "✓", Class: "check-match", Word: word, SR: "matches"}
	case canonical.DiffDiffer:
		if isEGF {
			return checkItem{Glyph: "◇", Class: "check-egf", Word: word, SR: "differs, closed by fact"}
		}
		return checkItem{Glyph: "✕", Class: "check-differ", Word: word, SR: "differs, veto stands"}
	default: // DiffUnknown
		return checkItem{Glyph: "?", Class: "check-unknown", Word: word, SR: "unknown, not compared"}
	}
}

// removeQueryParam builds a URL without the named query param (UI-11 filter chips).
func removeQueryParam(u *url.URL, key string) string {
	q := u.Query()
	q.Del(key)
	newURL := *u
	newURL.RawQuery = q.Encode()
	return newURL.String()
}

// filterRelationships applies the min confidence and q (title search) filters (UI-11 Cursor round 1).
func filterRelationships(rels []canonical.Relationship, views map[string]instrumentView, minConf decimal.Decimal, hasMin bool, q string, hasQ bool) []canonical.Relationship {
	if !hasMin && !hasQ {
		return rels
	}
	filtered := make([]canonical.Relationship, 0, len(rels))
	qLower := strings.ToLower(q)
	for _, r := range rels {
		if hasMin && r.Confidence.LessThan(minConf) {
			continue
		}
		if hasQ {
			aTitle := strings.ToLower(views[r.AID].Title)
			bTitle := strings.ToLower(views[r.BID].Title)
			if !strings.Contains(aTitle, qLower) && !strings.Contains(bTitle, qLower) {
				continue
			}
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// sortPairRows sorts pairs by the chosen order: "priority", "confidence", or "id" (UI-11).
func sortPairRows(pairs []pairRow, order string) {
	switch order {
	case "confidence":
		sort.SliceStable(pairs, func(i, j int) bool {
			if pairs[i].Confidence != pairs[j].Confidence {
				return pairs[i].Confidence > pairs[j].Confidence // desc
			}
			return pairs[i].ID < pairs[j].ID
		})
	case "id":
		sort.SliceStable(pairs, func(i, j int) bool {
			return pairs[i].ID < pairs[j].ID
		})
	default: // "priority"
		sort.SliceStable(pairs, func(i, j int) bool {
			// UI-18: flagged pairs sort first.
			if pairs[i].IsFlagged != pairs[j].IsFlagged {
				return pairs[i].IsFlagged // flagged = true sorts before false
			}
			// UI-11 Cursor round 1: ReviewPriority first, then veto count desc, then ID.
			if pairs[i].ReviewPriority != pairs[j].ReviewPriority {
				return pairs[i].ReviewPriority < pairs[j].ReviewPriority
			}
			if pairs[i].Vetoes != pairs[j].Vetoes {
				return pairs[i].Vetoes > pairs[j].Vetoes // more vetoes = higher priority
			}
			return pairs[i].ID < pairs[j].ID
		})
	}
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
		page: s.pageBase(round, "Matches", "Which Kalshi and Polymarket contracts are the same bet, and how sure is Equinox?", tz, "matches"),
	}
	if round.ID == 0 {
		data.NoRound = true
		s.render(w, http.StatusOK, "matches", data)
		return
	}

	// UI-11: parse and validate query params (AB-08: bad values fall back to defaults).
	sortParam := r.URL.Query().Get("sort")
	minParam := r.URL.Query().Get("min")
	qParam := r.URL.Query().Get("q")

	// Validate sort: "priority" (default), "confidence", "id".
	sortOrder := "priority"
	if sortParam == "confidence" || sortParam == "id" {
		sortOrder = sortParam
	}
	data.Sort = sortOrder

	// Validate min: must parse as a decimal in [0, 1], else ignored.
	var minConf decimal.Decimal
	hasMinFilter := false
	if minParam != "" {
		if parsed, err := decimal.NewFromString(minParam); err == nil && parsed.GreaterThanOrEqual(decimal.Zero) && parsed.LessThanOrEqual(decimal.NewFromInt(1)) {
			minConf = parsed
			hasMinFilter = true
			data.MinConf = minParam
			data.MinConfText = "≥ " + minParam
			data.MinConfHref = removeQueryParam(r.URL, "min")
		}
	}

	// q filter: title contains (case-insensitive substring).
	hasQFilter := qParam != ""
	if hasQFilter {
		data.Q = qParam
		data.QText = `Title contains "` + qParam + `"`
		data.QHref = removeQueryParam(r.URL, "q")
	}
	data.HasFilters = hasMinFilter || hasQFilter

	rels, err := st.ListRelationships(ctx, store.RelationshipFilter{RoundID: round.ID, Limit: maxMatchRows + 1})
	if err != nil {
		s.dbError(w, "listing relationships", err)
		return
	}
	if len(rels) > maxMatchRows {
		rels, data.Truncated = rels[:maxMatchRows], true
	}

	// UI-11: load views before filtering so q filter can check titles.
	views, err := loadInstrumentViews(ctx, st, relationshipInstrumentIDs(rels))
	if err != nil {
		s.dbError(w, "loading instruments for matches", err)
		return
	}

	// UI-11 Cursor round 1: apply filters (including q on titles) before setting Total.
	rels = filterRelationships(rels, views, minConf, hasMinFilter, qParam, hasQFilter)
	data.Total = len(rels)

	// UI-11: load quotes for MaxQuoteAge calculation (freshness badge).
	// UI-18: also used for net gap computation.
	quotes, err := st.ListQuotes(ctx, round.ID)
	if err != nil {
		s.dbError(w, "loading quotes for matches", err)
		return
	}
	quoteTimes := make(map[string]time.Time, len(quotes))
	quoteMap := make(map[string]*canonical.Quote, len(quotes))
	for i := range quotes {
		quoteTimes[quotes[i].InstrumentID] = quotes[i].FetchedAt
		quoteMap[quotes[i].InstrumentID] = &quotes[i].Quote
	}

	// UI-18: load instruments for fee schedules (net gap computation).
	instrumentRecs, err := st.GetInstruments(ctx, relationshipInstrumentIDs(rels))
	if err != nil {
		s.dbError(w, "loading instruments for matches", err)
		return
	}
	instMap := make(map[string]canonical.Instrument, len(instrumentRecs))
	var instruments []canonical.Instrument
	for id, rec := range instrumentRecs {
		instMap[id] = rec.Instrument
		instruments = append(instruments, rec.Instrument)
	}

	// UI-18: build fee estimates for the checks key.
	data.FeeEstimates = BuildFeeEstimates(instruments)

	// groupPairs needs the raw time for pairRow.AsOf (M3-08 Cursor round 2: UI-02 contract keeps Round.AsOf formatted)
	asOf := round.FinishedAt
	if asOf.IsZero() {
		asOf = round.StartedAt
	}
	// UI-11: groupPairs now accepts sortOrder, qParam, and quoteTimes.
	// UI-18: also accepts quoteMap, instMap, and reviewEdgeMin for net gap computation.
	data.Groups = groupPairs(rels, views, asOf, sortOrder, qParam, quoteTimes, round.ID, quoteMap, instMap, s.cfg.ReviewEdgeMin)

	// UI-03: split groups into Routable (combined, sorted by the chosen order), Implies, Related.
	// Routable pairs are shown as one combined group, not separated by type.
	var routablePairs []pairRow
	for i := range data.Groups {
		g := &data.Groups[i]
		typ := canonical.RelationshipType(g.Type)
		if typ.Routable() {
			routablePairs = append(routablePairs, g.Pairs...)
			data.RoutableCount += len(g.Pairs)
			// UI-18: count flagged pairs.
			for _, p := range g.Pairs {
				if p.IsFlagged {
					data.FlaggedCount++
				}
			}
		} else if typ == canonical.RelImplies {
			data.Implies = g
			data.ImpliesCount = len(g.Pairs)
		} else if typ == canonical.RelRelated {
			data.Related = g
			data.RelatedCount = len(g.Pairs)
		}
	}
	// UI-11: sort by the chosen order.
	sortPairRows(routablePairs, sortOrder)
	if len(routablePairs) > 0 {
		data.Routable = []pairGroup{{Type: "routable", Label: "Routable pairs", Pairs: routablePairs}}
	}
	// Sort implies and related by the chosen order too.
	if data.Implies != nil {
		sortPairRows(data.Implies.Pairs, sortOrder)
	}
	if data.Related != nil {
		sortPairRows(data.Related.Pairs, sortOrder)
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

// groupPairs buckets rels by type, strongest type first, populates UI-11 and UI-18 fields.
// Sorting by sortOrder happens in handleMatches; filtering is done before calling this.
func groupPairs(rels []canonical.Relationship, views map[string]instrumentView, asOf time.Time, _ string, _ string, quoteTimes map[string]time.Time, _ int64, quoteMap map[string]*canonical.Quote, instMap map[string]canonical.Instrument, reviewEdgeMin decimal.Decimal) []pairGroup {
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

		// UI-11: build basis risk fields.
		basisRisk := ""
		basisWord := BasisRiskWord(r.BasisRisk)
		if r.BasisRisk != nil {
			basisRisk = r.BasisRisk.Score.StringFixed(2)
		}

		// UI-11: build checks from diff rows.
		isEGF := r.Type == canonical.RelEquivalentGivenFacts
		checks := buildChecks(r.Diff, isEGF)

		// UI-11 Cursor round 1: ReviewPriority is just (confidence - 0.90) for routable.
		// Veto count is a secondary sort key in sortPairRows, not packed here.
		reviewPriority := 0.0
		confFloat, _ := r.Confidence.Float64()
		if r.Type.Routable() {
			reviewPriority = confFloat - 0.90
		} else {
			reviewPriority = -confFloat // negate so higher confidence = lower priority value
		}

		// UI-11: MaxQuoteAge from stored quote FetchedAt (for freshness badge).
		maxQuoteAge := 0
		now := time.Now()
		if tA, ok := quoteTimes[r.AID]; ok {
			ageA := int(now.Sub(tA).Seconds())
			if ageA > maxQuoteAge {
				maxQuoteAge = ageA
			}
		}
		if tB, ok := quoteTimes[r.BID]; ok {
			ageB := int(now.Sub(tB).Seconds())
			if ageB > maxQuoteAge {
				maxQuoteAge = ageB
			}
		}

		// UI-11 Cursor round 1: calculate FreshState from MaxQuoteAge.
		var freshState FreshState
		if maxQuoteAge == 0 {
			freshState = FreshStateUnknown
		} else if maxQuoteAge < 120 {
			freshState = FreshStateFresh
		} else {
			freshState = FreshStateStale
		}

		// UI-11 Cursor round 1: build ConfValue for {{template "conf"}}.
		confWord := ConfidenceWord(r.Confidence)
		confClass := "w-" + confWord // w-high, w-medium, w-low

		// UI-18: compute net gap (fee-adjusted price difference at reference size).
		aInst := instMap[r.AID]
		bInst := instMap[r.BID]
		aQuote := quoteMap[r.AID]
		bQuote := quoteMap[r.BID]
		netGap := ComputeNetGap(aInst, bInst, aQuote, bQuote)

		// UI-18: format net gap with sign rule (D-098 call 2).
		netGapText := ""
		if netGap.HasValue {
			if netGap.Value.IsNegative() {
				// Unfavourable: show in parentheses.
				netGapText = "(" + netGap.Value.Abs().StringFixed(2) + ")"
			} else {
				// Favourable or zero: show positive.
				netGapText = netGap.Value.StringFixed(2)
			}
		}

		// UI-18: flag for review when net gap >= REVIEW_EDGE_MIN and confidence < 0.90.
		isFlagged := false
		flaggedReason := ""
		if netGap.HasValue && netGap.Value.Abs().GreaterThanOrEqual(reviewEdgeMin) && r.Confidence.LessThan(decimal.RequireFromString("0.90")) {
			isFlagged = true
			flaggedReason = "Likely a wrong pairing, not an arbitrage."
		}

		row := pairRow{
			ID:             r.ID,
			A:              views[r.AID],
			B:              views[r.BID],
			Type:           string(r.Type),
			Confidence:     r.Confidence.StringFixed(2),
			ConfidenceWord: confWord,
			ChipClass:      TypeChipClass(r.Type),
			Method:         string(r.Method),
			Vetoes:         len(r.Vetoes),
			VetoPhrase:     vetoPhrase,
			AsOf:           asOf,
			BasisRisk:      basisRisk,
			BasisWord:      basisWord,
			Checks:         checks,
			MaxQuoteAge:    maxQuoteAge,
			ReviewPriority: reviewPriority,
			TypeBadge:      TypeBadge(r.Type),
			FreshBadge:     FreshBadge(freshState),
			NetGap:         netGap,
			NetGapText:     netGapText,
			IsFlagged:      isFlagged,
			FlaggedReason:  flaggedReason,
		}
		row.ConfValue.Value = ConfidenceValue(r.Confidence)
		row.ConfValue.Word = confWord
		row.ConfValue.Class = confClass
		byType[r.Type] = append(byType[r.Type], row)
	}
	var groups []pairGroup
	for _, t := range canonical.RelationshipTypes() {
		rows := byType[t]
		if len(rows) == 0 {
			continue
		}
		// Initial sort by confidence desc, then ID (will be re-sorted by sortOrder in handleMatches).
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
