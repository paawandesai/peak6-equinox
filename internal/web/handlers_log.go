package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// maxLogRows caps the decision-log page (NFR-01).
const maxLogRows = 200

// logPage is the data for log.html (US-08, FR-21).
type logPage struct {
	page
	Kind    string // active filter, or "" for every kind
	Kinds   []logKindOption
	Entries []logEntryView
	Limit   int
}

type logKindOption struct {
	Value, Label string
	Selected     bool
}

// logEntryView is one decision_log row with the page it points at. Link is
// built by this package from a validated numeric ref, never from free text.
type logEntryView struct {
	ID        int64
	At        string
	Kind      string
	Ref       string
	Link      string
	LinkLabel string
	Summary   string
}

// logKinds are the filter options, in display order.
var logKinds = []struct {
	kind  store.LogKind
	label string
}{
	{store.LogRelationship, "relationship verdicts"},
	{store.LogRouting, "routing decisions"},
	{store.LogLLM, "LLM verifier"},
	{store.LogIngest, "fetch rounds"},
}

// handleLog answers GET /log?kind=: the newest decision-log entries across
// relationship verdicts, routing decisions, LLM verdicts and fetch rounds,
// each linked to the pair or stored decision it is about.
func (s *Server) handleLog(w http.ResponseWriter, r *http.Request) {
	st, ok := s.storeOrFail(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	kind := store.LogKind(r.URL.Query().Get("kind"))
	if kind != "" && !kind.Valid() {
		s.renderError(w, http.StatusNotFound, "No such decision-log kind.")
		return
	}
	data := logPage{
		page: page{
			Title:    "Decision log",
			Question: "What did Equinox decide recently, and why?",
			Assets:   pinnedAssets,
		},
		Kind: string(kind), Limit: maxLogRows,
	}
	for _, k := range logKinds {
		data.Kinds = append(data.Kinds, logKindOption{Value: string(k.kind), Label: k.label, Selected: k.kind == kind})
	}
	entries, err := st.ListLog(ctx, store.LogFilter{Kind: kind, Limit: maxLogRows})
	if err != nil {
		s.dbError(w, "listing the decision log", err)
		return
	}
	data.Entries = make([]logEntryView, 0, len(entries))
	for _, e := range entries {
		data.Entries = append(data.Entries, newLogEntryView(e))
	}
	s.render(w, http.StatusOK, "log", data)
}

func newLogEntryView(e store.LogEntry) logEntryView {
	v := logEntryView{ID: e.ID, At: formatUTC(e.CreatedAt), Kind: string(e.Kind), Ref: e.Ref, Summary: e.Summary}
	v.Link, v.LinkLabel = logEntryLink(e.Kind, e.Ref)
	return v
}

// logEntryLink maps an entry to the page about it. The ref must be a valid
// path ID (AB-08); anything else gets no link.
func logEntryLink(kind store.LogKind, ref string) (href, label string) {
	id, ok := parseID(ref)
	if !ok {
		return "", ""
	}
	switch kind {
	case store.LogRelationship, store.LogLLM:
		return "/pairs/" + strconv.FormatInt(id, 10), "pair #" + ref
	case store.LogRouting:
		return "/decisions/" + strconv.FormatInt(id, 10), "decision #" + ref
	case store.LogIngest:
		return "", "round " + ref
	}
	return "", ""
}

// decisionPage is the data for decision.html: a stored routing decision
// rendered with the same trace as POST /route.
type decisionPage struct {
	routePage
	CreatedAt string
}

// handleDecision answers GET /decisions/{id}: a stored routing decision
// (FR-17), linked from the decision log.
func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		s.renderError(w, http.StatusNotFound, "No such routing decision.")
		return
	}
	st, ok := s.storeOrFail(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	rd, err := st.GetRoutingDecision(ctx, id)
	switch {
	case isNotFound(err):
		s.renderError(w, http.StatusNotFound, "No such routing decision.")
		return
	case err != nil:
		s.dbError(w, "loading routing decision", err)
		return
	}
	round, err := st.GetRound(ctx, rd.RoundID)
	if err != nil && !isNotFound(err) {
		s.dbError(w, "loading the decision's round", err)
		return
	}
	snapshot := time.Time{}
	if err == nil {
		snapshot = round.SnapshotAt()
	}
	data := decisionPage{
		routePage: routePage{
			page: page{
				Title:    "Decision " + r.PathValue("id"),
				Question: "What did this simulated order do, and why did the router choose that venue?",
				Assets:   pinnedAssets,
			},
			DecisionID: rd.ID,
			Decision:   newDecisionView(rd.Decision, countPartners(rd.Decision), snapshot),
		},
		CreatedAt: formatUTC(rd.CreatedAt),
	}
	if err == nil {
		data.Round = newRoundInfo(round)
	}
	s.render(w, http.StatusOK, "decision", data)
}

// countPartners recovers how many partner instruments the router considered
// from a stored decision: every partner ends up as an alternative or an
// exclusion, and the target itself is neither a partner nor counted.
func countPartners(d canonical.Decision) int {
	seen := map[string]bool{}
	for _, alt := range d.Alternatives {
		for _, leg := range alt.Legs {
			seen[leg.InstrumentID] = true
		}
	}
	for _, ex := range d.Exclusions {
		seen[ex.InstrumentID] = true
	}
	delete(seen, d.TargetID)
	return len(seen)
}
