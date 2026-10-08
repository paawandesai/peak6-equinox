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
	Type           string
	TypeLabel      string
	TypeChipClass  string // UI-04: chip class for the type
	Confidence     string
	ConfidenceWord string // UI-04: bucket word
	Method         string
	BasisRiskScore string // UI-04: score as string or "not scored"
	BasisRiskWord  string // UI-04: bucket word
	Routable       bool   // UI-04: yes/no in the strip
	VetoHeading    string // UI-04: heading variant
	Explanation    string
	A, B           instrumentView
	QuoteA         touchView
	QuoteB         touchView
	DiffRows       []diffRowView // UI-04: diff with annotations
	Vetoes         []canonical.Veto
	Facts          []canonical.FactUse
	BasisRisk      *canonical.BasisRisk
	NearMiss       bool // a stored related pair: shown with the veto that fired
	Types          []typeView
	History        historyChart
	LogEntries     []logEntryView
	Form           routeFormView
}

// diffRowView is one diff row with annotations for vetoes and facts (UI-04).
type diffRowView struct {
	Field      string
	A          string
	B          string
	Status     string // "match", "differ", "unknown"
	StatusText string // status word with color token
	Annotation string // veto or fact annotation, or empty
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

// touchView is one venue's current touch in the round, for display only.
type touchView struct {
	Venue      string
	Present    bool
	BestBid    string
	BestAsk    string
	Mid        string
	BidLevels  int
	AskLevels  int
	FetchedAt  time.Time  // Raw UTC time, formatted with formatTime in templates (M3-08)
	VenueTS    *time.Time // Optional venue timestamp (M3-08)
	VenueTSStr string     // "not sent" when VenueTS is nil
	Quality    []canonical.QualityIssue
	Tz         string // Timezone for formatTime in sub-templates (M3-08)
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

	// UI-04: build diff rows with veto and fact annotations
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

	// UI-04: basis risk score and word
	basisRiskScore := ""
	basisRiskWord := BasisRiskWord(rel.BasisRisk)
	if rel.BasisRisk != nil {
		basisRiskScore = rel.BasisRisk.Score.StringFixed(2) + " " + basisRiskWord
	} else {
		basisRiskScore = basisRiskWord
	}

	data := pairPage{
		page:           s.pageBase(round, "Pair "+r.PathValue("id"), "Why does Equinox think these two contracts are, or are not, the same bet, and where would an order fill best?", tz, "pair"),
		ID:             rel.ID,
		Type:           string(rel.Type),
		TypeLabel:      typeLabels[rel.Type],
		TypeChipClass:  TypeChipClass(rel.Type), // UI-04
		Confidence:     rel.Confidence.StringFixed(2),
		ConfidenceWord: ConfidenceWord(rel.Confidence), // UI-04
		Method:         string(rel.Method),
		BasisRiskScore: basisRiskScore,      // UI-04
		BasisRiskWord:  basisRiskWord,       // UI-04
		Routable:       rel.Type.Routable(), // UI-04
		VetoHeading:    vetoHeading,         // UI-04
		Explanation:    rel.Explanation,
		A:              viewA,
		B:              viewB,
		DiffRows:       diffRows, // UI-04
		Vetoes:         rel.Vetoes,
		Facts:          rel.FactsUsed,
		BasisRisk:      rel.BasisRisk,
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
		}
		q.dest.Venue = string(canonical.VenueOf(q.id))
		q.dest.Tz = tz
	}
	s.render(w, http.StatusOK, "pair", data)
}

// buildDiffRows maps vetoes and facts onto diff rows by term name (UI-04).
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

	for _, d := range rel.Diff {
		row := diffRowView{
			Field:      d.Field,
			A:          d.A,
			B:          d.B,
			Status:     string(d.Status),
			StatusText: string(d.Status),
		}

		// Add veto annotation if present
		if veto, ok := vetoByField[d.Field]; ok {
			if factsList != "" {
				// For pairs with facts, show all facts
				row.Annotation = fmt.Sprintf("veto %s, closed by facts: %s", veto.Code, factsList)
			} else {
				// For other types or pairs without facts, just show the veto
				row.Annotation = fmt.Sprintf("veto %s", veto.Code)
			}
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
