package web

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// pairPage is the data for pair.html: the contract diff, vetoes, explanation,
// the touch on each venue and the route form (US-02).
type pairPage struct {
	page
	ID             int64
	TypeBadge      Badge  // UI-12: large badge in verdict panel
	Type           string
	TypeLabel      string
	TypeChipClass  string      // UI-04: chip class for the type
	Confidence     confView    // UI-12: structured for {{template "conf"}}
	ConfidenceOld  string      // kept for backwards compat during transition
	ConfidenceWord string      // UI-04: bucket word
	Method         string
	BasisRisk      basisView   // UI-12: structured with value, word, class
	BasisRiskScore string      // UI-04: score as string or "not scored"
	BasisRiskWord  string      // UI-04: bucket word
	DiffRowCount   string      // UI-12: "N diff rows" for the link
	Routable       bool        // UI-04: yes/no in the strip
	VetoHeading    string      // UI-04: heading variant
	Explanation    string
	A, B           instrumentView
	QuoteA         touchView
	QuoteB         touchView
	DiffRows       []diffRowView // UI-04, UI-12: diff with part grouping and word-level tokens
	DiffCounts     string        // UI-12: "7 terms · 2 match · 3 differ · 2 closed · 0 unknown"
	Vetoes         []canonical.Veto
	Facts          []canonical.FactUse
	BasisRiskFull  *canonical.BasisRisk // renamed for clarity
	BasisComponents []basisComponentView // UI-12: with Row field
	NearMiss       bool // a stored related pair: shown with the veto that fired
	Types          []typeView
	History        historyChart
	LogEntries     []logEntryView
	Form           routeFormView
}

// confView is the confidence for the {{template "conf"}} define (UI-12).
type confView struct {
	Value float64 // for meter value
	Word  string  // high/medium/low
	Class string  // w-high/w-medium/w-low
}

// basisView is the basis risk for display (UI-12).
type basisView struct {
	Score string // "0.38" or "not scored"
	Word  string // low/moderate/high or "not scored"
	Class string // w-low/w-moderate/w-high or empty
}

// basisComponentView is one basis risk component with its diff row link (UI-12).
type basisComponentView struct {
	Name   string
	Score  string
	Detail string
	Row    string // diff row field, or empty
}

// diffRowView is one diff row with annotations for vetoes and facts (UI-04, UI-12).
type diffRowView struct {
	Field      string
	A          string
	B          string
	ATokens    []diffToken // UI-12: word-level diff tokens for template wrapping
	BTokens    []diffToken // UI-12: word-level diff tokens for template wrapping
	Status     string      // "match", "differ", "unknown"
	StatusText string      // status word with color token
	Annotation string      // veto or fact annotation, or empty
	Part       string      // UI-12: rule part (resolution criteria, strike, direction, etc.)
	Class      string      // UI-12: CSS class (t-match, t-differ, t-closed, t-unknown)
	Badge      Badge       // UI-12: verdict badge
	BasisLink  string      // UI-12: link to basis component, or empty
}

// diffToken is one word/space token with its operation for word-level diffs (UI-12).
// The template wraps delete/insert tokens in <del>/<ins> tags. HANDOFF.md: no template.HTML (AB-07).
type diffToken struct {
	Op   string // "keep", "delete", "insert"
	Text string // the token text, pre-escaped by the template
}

// typeView is one relationship type for the legend on the pair page.
type typeView struct {
	Type     string
	Label    string
	Routable bool
}

func allTypeViews() []typeView {
	out := make([]typeView, 0, len(canonical.RelationshipTypes()))
	for _, t := range canonical.RelationshipTypes() {
		out = append(out, typeView{Type: string(t), Label: typeLabels[t], Routable: t.Routable()})
	}
	return out
}

// maxPairLogEntries caps the decision-log entries shown on a pair page.
const maxPairLogEntries = 20

// fieldToPart maps a diff field to its rule part for grouping (UI-12, HANDOFF.md).
// Order: Resolution criteria (subject, metric), Strike, Direction, Timeframe, Sources, Qualifying.
func fieldToPart(field string) string {
	switch field {
	case "subject", "metric":
		return "Resolution criteria"
	case "threshold":
		return "Strike / threshold"
	case "comparator":
		return "Direction"
	case "deadline":
		return "Timeframe / cutoff"
	case "source":
		return "Sources"
	case "edge_rules":
		return "Qualifying / edge cases"
	default:
		return "Other terms"
	}
}

// partOrder returns the display order for rule parts (UI-12, HANDOFF.md).
var partOrder = []string{
	"Resolution criteria",
	"Strike / threshold",
	"Direction",
	"Timeframe / cutoff",
	"Sources",
	"Qualifying / edge cases",
	"Other terms",
}

// wordDiff produces simple word-level diff tokens (UI-12).
// This is a minimal implementation: split on whitespace, mark differing tokens.
// HANDOFF.md: the template wraps these in <del>/<ins>, never template.HTML (AB-07).
func wordDiff(a, b string) ([]diffToken, []diffToken) {
	if a == b {
		// Identical: return as-is with keep
		tokA := []diffToken{{Op: "keep", Text: a}}
		tokB := []diffToken{{Op: "keep", Text: b}}
		return tokA, tokB
	}
	// Simple word-level diff: split on spaces and mark changes
	wordsA := strings.Fields(a)
	wordsB := strings.Fields(b)

	var tokA, tokB []diffToken
	if a == "" {
		// A is empty, B is all inserts
		for _, w := range wordsB {
			tokB = append(tokB, diffToken{Op: "insert", Text: w})
		}
	} else if b == "" {
		// B is empty, A is all deletes
		for _, w := range wordsA {
			tokA = append(tokA, diffToken{Op: "delete", Text: w})
		}
	} else {
		// Both present: mark as different (simple approach for now)
		for _, w := range wordsA {
			tokA = append(tokA, diffToken{Op: "delete", Text: w})
		}
		for _, w := range wordsB {
			tokB = append(tokB, diffToken{Op: "insert", Text: w})
		}
	}
	return tokA, tokB
}

// touchView is one venue's current touch in the round, for display only.
type touchView struct {
	Venue       string
	Present     bool
	BestBid     string
	BestAsk     string
	Mid         string
	BidLevels   int
	AskLevels   int
	FetchedAt   time.Time  // Raw UTC time, formatted with formatTime in templates (M3-08)
	VenueTS     *time.Time // Optional venue timestamp (M3-08)
	VenueTSStr  string     // "not sent" when VenueTS is nil
	Quality     []canonical.QualityIssue
	Tz          string    // Timezone for formatTime in sub-templates (M3-08)
	Age         string    // UI-12: age string for stamp (e.g. "14 s ago")
	FreshBadge  Badge     // UI-12: freshness badge
	FreshState  string    // UI-12: fresh/stale/unknown/pinned
	StampAsOf   string    // UI-12: formatted timestamp for stamp
	StampDT     string    // UI-12: datetime attribute
}

func newTouchView(q store.QuoteRecord) touchView {
	venueTSStr := "not sent"
	return touchView{
		Present: true, BestBid: nullPrice(q.BestBid), BestAsk: nullPrice(q.BestAsk), Mid: nullPrice(q.Mid),
		BidLevels: len(q.YesBids), AskLevels: len(q.YesAsks), FetchedAt: q.FetchedAt, VenueTS: q.VenueTS,
		VenueTSStr: venueTSStr, Quality: q.Quality,
	}
}

// handlePair answers GET /pairs/{id}.
func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		s.renderErrorWithContext(w, r, http.StatusNotFound, "No such pair.")
		return
	}
	st, ok := s.storeOrFail(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	// Get current round from context (pinned or latest)
	currentRound := roundFromContext(r.Context())

	rel, err := st.GetRelationship(ctx, id)
	switch {
	case isNotFound(err):
		s.renderErrorWithContext(w, r, http.StatusNotFound, "No such pair.")
		return
	case err != nil:
		s.dbError(w, "loading relationship", err)
		return
	}

	// Determine which round to use for data and banner (M3-07, NFR-04)
	var round store.Round
	if currentRound.Pinned {
		// NFR-04: when a round is pinned, reject pairs from other rounds
		if rel.RoundID != currentRound.ID {
			s.renderErrorWithContext(w, r, http.StatusNotFound, "This pair belongs to a different round.")
			return
		}
		round = currentRound
	} else {
		// Unpinned or no current round: use the pair's own round (allows viewing older pairs from /log)
		round, err = st.GetRound(ctx, rel.RoundID)
		if err != nil {
			s.dbError(w, "loading the pair's round", err)
			return
		}
	}
	views, err := loadInstrumentViews(ctx, st, []string{rel.AID, rel.BID})
	if err != nil {
		s.dbError(w, "loading the pair's instruments", err)
		return
	}
	tz := timezoneFromRequest(r)
	viewA, viewB := views[rel.AID], views[rel.BID]
	viewA.Tz, viewB.Tz = tz, tz

	// UI-04, UI-12: build diff rows with veto and fact annotations, word-level tokens
	diffRows := buildDiffRows(rel)

	// UI-04: veto heading variant
	vetoHeading := ""
	if len(rel.Vetoes) > 0 {
		if rel.Type == canonical.RelEquivalentGivenFacts {
			vetoHeading = fmt.Sprintf("%d term differences, closed by facts", len(rel.Vetoes))
		} else {
			vetoHeading = fmt.Sprintf("Vetoes (%d): each blocks equivalent", len(rel.Vetoes))
		}
	}

	// UI-04, UI-12: basis risk score and word
	basisRiskScore := ""
	basisRiskWord := BasisRiskWord(rel.BasisRisk)
	basisClass := ""
	if rel.BasisRisk != nil {
		basisRiskScore = rel.BasisRisk.Score.StringFixed(2)
		switch basisRiskWord {
		case "low":
			basisClass = "w-low"
		case "moderate":
			basisClass = "w-moderate"
		case "high":
			basisClass = "w-high"
		}
	} else {
		basisRiskScore = "not scored"
		basisRiskWord = "not scored"
	}

	// UI-12: confidence for {{template "conf"}}
	confValue := ConfidenceValue(rel.Confidence)
	confWord := ConfidenceWord(rel.Confidence)
	confClass := ""
	switch confWord {
	case "high":
		confClass = "w-high"
	case "medium":
		confClass = "w-medium"
	case "low":
		confClass = "w-low"
	}

	// UI-12: diff counts
	matchCount, differCount, closedCount, unknownCount := 0, 0, 0, 0
	for _, row := range diffRows {
		switch row.Status {
		case "match":
			matchCount++
		case "differ":
			if row.Class == "t-closed" {
				closedCount++
			} else {
				differCount++
			}
		case "unknown":
			unknownCount++
		}
	}
	diffCounts := fmt.Sprintf("%d terms · %d match · %d differ · %d closed · %d unknown",
		len(diffRows), matchCount, differCount, closedCount, unknownCount)
	diffRowCount := fmt.Sprintf("%d diff rows", len(diffRows))

	// UI-12: basis components with row links
	var basisComponents []basisComponentView
	if rel.BasisRisk != nil {
		for _, c := range rel.BasisRisk.Components {
			bc := basisComponentView{
				Name:   c.Name,
				Score:  c.Score.StringFixed(2),
				Detail: c.Detail,
			}
			// Simple heuristic to link components to diff rows
			if strings.Contains(c.Name, "cutoff") || strings.Contains(c.Name, "deadline") {
				bc.Row = "deadline"
			} else if strings.Contains(c.Name, "source") {
				bc.Row = "source"
			}
			basisComponents = append(basisComponents, bc)
		}
	}

	data := pairPage{
		page:           s.pageBase(round, "Pair "+r.PathValue("id"), "Why does Equinox think these two contracts are, or are not, the same bet, and where would an order fill best?", tz, "pair"),
		ID:             rel.ID,
		TypeBadge:      TypeBadge(rel.Type), // UI-12
		Type:           string(rel.Type),
		TypeLabel:      typeLabels[rel.Type],
		TypeChipClass:  TypeChipClass(rel.Type), // UI-04
		Confidence: confView{ // UI-12
			Value: confValue,
			Word:  confWord,
			Class: confClass,
		},
		ConfidenceOld:  rel.Confidence.StringFixed(2),
		ConfidenceWord: confWord, // UI-04
		Method:         string(rel.Method),
		BasisRisk: basisView{ // UI-12
			Score: basisRiskScore,
			Word:  basisRiskWord,
			Class: basisClass,
		},
		BasisRiskScore: basisRiskScore + " " + basisRiskWord, // UI-04 (kept for compat)
		BasisRiskWord:  basisRiskWord,                        // UI-04
		DiffRowCount:   diffRowCount,                         // UI-12
		Routable:       rel.Type.Routable(),                  // UI-04
		VetoHeading:    vetoHeading,                          // UI-04
		Explanation:    rel.Explanation,
		A:              viewA,
		B:              viewB,
		DiffRows:       diffRows,        // UI-04, UI-12
		DiffCounts:     diffCounts,      // UI-12
		Vetoes:         rel.Vetoes,
		Facts:          rel.FactsUsed,
		BasisRiskFull:  rel.BasisRisk,   // renamed
		BasisComponents: basisComponents, // UI-12
		NearMiss:       rel.Type == canonical.RelRelated,
		Types:          allTypeViews(),
		Form:           defaultRouteForm(rel),
	}
	points, err := st.QuoteHistory(ctx, []string{rel.AID, rel.BID}, maxHistoryPt)
	if err != nil {
		s.dbError(w, "loading the pair's price history", err)
		return
	}
	data.History = buildHistoryChart(rel.AID, rel.BID, points, tz)
	entries, err := st.ListLog(ctx, store.LogFilter{Kind: store.LogRelationship, Ref: strconv.FormatInt(rel.ID, 10), Limit: maxPairLogEntries})
	if err != nil {
		s.dbError(w, "loading the pair's decision-log entries", err)
		return
	}
	for _, e := range entries {
		data.LogEntries = append(data.LogEntries, newLogEntryView(e))
	}
	// UI-12: load quotes with freshness
	now := time.Now().UTC()
	for _, q := range []struct {
		id   string
		dest *touchView
	}{{rel.AID, &data.QuoteA}, {rel.BID, &data.QuoteB}} {
		quote, err := st.GetQuote(ctx, rel.RoundID, q.id)
		switch {
		case isNotFound(err):
			*q.dest = touchView{}
		case err != nil:
			s.dbError(w, "loading the pair's quotes", err)
			return
		default:
			*q.dest = newTouchView(quote)
			// UI-12: compute age and freshness
			age := now.Sub(quote.FetchedAt)
			q.dest.Age = Age(age)
			q.dest.StampAsOf = formatTime(quote.FetchedAt, tz)
			q.dest.StampDT = quote.FetchedAt.Format(time.RFC3339)

			// Determine freshness state (UI-10: only Fresh/Stale/Unknown for now; Warn comes in UI-19)
			// Use D-077 stale threshold
			staleThreshold := s.cfg.StaleAfter
			freshState := FreshStateFresh
			if currentRound.Pinned {
				freshState = FreshStatePinned
			} else if age > staleThreshold {
				freshState = FreshStateStale
			}
			q.dest.FreshState = string(freshState)
			q.dest.FreshBadge = FreshBadge(freshState)
		}
		q.dest.Venue = string(canonical.VenueOf(q.id))
		q.dest.Tz = tz
	}
	s.render(w, http.StatusOK, "pair", data)
}

// buildDiffRows maps vetoes and facts onto diff rows by term name (UI-04, UI-12).
// UI-12 adds: Part grouping, CSS class, badge, word-level tokens, basis link.
func buildDiffRows(rel canonical.Relationship) []diffRowView {
	rows := make([]diffRowView, 0, len(rel.Diff))

	// Build veto map by field
	vetoByField := make(map[string]canonical.Veto)
	for _, v := range rel.Vetoes {
		vetoByField[v.Field] = v
	}

	// Build fact list for equivalent_given_facts annotation
	var factsList string
	if rel.Type == canonical.RelEquivalentGivenFacts && len(rel.FactsUsed) > 0 {
		factPairs := make([]string, 0, len(rel.FactsUsed))
		for _, f := range rel.FactsUsed {
			factPairs = append(factPairs, fmt.Sprintf("%s=%s", f.Key, f.Value))
		}
		factsList = strings.Join(factPairs, ", ")
	}

	// Build basis component map by field (for basis link)
	basisByField := make(map[string]bool)
	if rel.BasisRisk != nil {
		for _, c := range rel.BasisRisk.Components {
			// Extract field from component name (e.g. "cutoff_gap" -> "deadline")
			// This is a simple heuristic; the actual mapping may differ
			if strings.Contains(c.Name, "cutoff") || strings.Contains(c.Name, "deadline") {
				basisByField["deadline"] = true
			}
			if strings.Contains(c.Name, "source") {
				basisByField["source"] = true
			}
		}
	}

	for _, d := range rel.Diff {
		row := diffRowView{
			Field:      d.Field,
			A:          d.A,
			B:          d.B,
			Status:     string(d.Status),
			StatusText: string(d.Status),
			Part:       fieldToPart(d.Field), // UI-12
		}

		// UI-12: word-level diff tokens
		row.ATokens, row.BTokens = wordDiff(d.A, d.B)

		// UI-12: CSS class based on status
		isClosed := false
		if veto, ok := vetoByField[d.Field]; ok && rel.Type == canonical.RelEquivalentGivenFacts {
			isClosed = true
			_ = veto // use veto below in annotation
		}

		switch d.Status {
		case canonical.DiffMatch:
			row.Class = "t-match"
			row.Badge = VerdictBadge("match")
		case canonical.DiffDiffer:
			if isClosed {
				row.Class = "t-closed"
				row.Badge = VerdictBadge("closed")
			} else {
				row.Class = "t-differ"
				row.Badge = VerdictBadge("differs")
			}
		case canonical.DiffUnknown:
			row.Class = "t-unknown"
			row.Badge = VerdictBadge("unknown")
		default:
			row.Class = "t-unknown"
			row.Badge = VerdictBadge("unknown")
		}

		// Add veto annotation if present
		if veto, ok := vetoByField[d.Field]; ok {
			if factsList != "" {
				// For pairs with facts, show all facts
				row.Annotation = fmt.Sprintf("veto %s, closed by facts", veto.Code)
			} else {
				// For other types or pairs without facts, just show the veto
				row.Annotation = fmt.Sprintf("veto %s", veto.Code)
			}
		}

		// UI-12: basis link if this field contributes to basis risk
		if basisByField[d.Field] {
			row.BasisLink = "#basis"
		}

		rows = append(rows, row)
	}

	return rows
}

// quoteFor builds the router's view of an instrument's book in a round. A
// missing quote becomes an empty book flagged no_book so the router can
// explain the exclusion instead of failing (as `equinox route` does).
func quoteFor(ctx context.Context, st *store.Store, roundID int64, id string, now time.Time) (canonical.Quote, error) {
	q, err := st.GetQuote(ctx, roundID, id)
	switch {
	case isNotFound(err):
		empty := canonical.Quote{InstrumentID: id, FetchedAt: now}
		empty.Quality.Add(canonical.QualityNoBook, "", "no quote in this round")
		return empty, nil
	case err != nil:
		return canonical.Quote{}, err
	}
	return q.Quote, nil
}
