package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// pairPage is the data for pair.html: the contract diff, vetoes, explanation,
// the touch on each venue and the route form (US-02).
type pairPage struct {
	page
	ID          int64
	Type        string
	TypeLabel   string
	Confidence  string
	Method      string
	Explanation string
	A, B        instrumentView
	QuoteA      touchView
	QuoteB      touchView
	Diff        []canonical.FieldDiff
	Vetoes      []canonical.Veto
	Facts       []canonical.FactUse
	BasisRisk   *canonical.BasisRisk
	NearMiss    bool // a stored related pair: shown with the veto that fired
	Types       []typeView
	History     historyChart
	LogEntries  []logEntryView
	Form        routeFormView
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
	data := pairPage{
		page: s.pageBase(round, "Pair "+r.PathValue("id"), "Why does Equinox think these two contracts are, or are not, the same bet, and where would an order fill best?", tz),
		ID:   rel.ID, Type: string(rel.Type), TypeLabel: typeLabels[rel.Type], Confidence: rel.Confidence.StringFixed(2),
		Method: string(rel.Method), Explanation: rel.Explanation, A: viewA, B: viewB,
		Diff: rel.Diff, Vetoes: rel.Vetoes, Facts: rel.FactsUsed, BasisRisk: rel.BasisRisk,
		NearMiss: rel.Type == canonical.RelRelated, Types: allTypeViews(),
		Form: defaultRouteForm(rel),
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
