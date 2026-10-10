package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// Wiring for the plain-language presenter: the audit config shown under
// Technical details, the raw JSON of a stored decision, and page titles.

// auditConfig is the router configuration the audit layer lists. These are the
// server's values now; the router does not store them per decision, so the
// stored explanation remains the record of what applied.
type auditConfig struct {
	StaleAfter    string // "2 min"
	SkewMax       string // "30 s"
	MinConfidence string // "0.90"
	BasisMult     string // "0.0100"
}

func (s *Server) auditConfig() auditConfig {
	return auditConfig{
		StaleAfter:    AgeShort(s.cfg.StaleAfter),
		SkewMax:       AgeShort(s.cfg.SkewMax),
		MinConfidence: s.cfg.MinRouteConfidence.StringFixed(2),
		BasisMult:     s.cfg.BasisPenaltyMult.StringFixed(4),
	}
}

// attachPlain adds the presenter's layers and the audit extras to a view.
func (s *Server) attachPlain(v *decisionView, d canonical.Decision, dc decisionContext, id int64) {
	v.Plain = presentDecision(d, dc)
	v.Audit = s.auditConfig()
	if id > 0 {
		v.RawJSONHref = "/decisions/" + strconv.FormatInt(id, 10) + "/json"
	}
	v.Names = make(map[string]string, len(dc.Instruments))
	for _, id := range collectIDs(d) {
		v.Names[id] = venueName(canonical.VenueOf(id)) + " · " + dc.nameOf(id).Long
	}
}

func collectIDs(d canonical.Decision) []string {
	set := collectInstrumentIDs(d)
	for _, in := range d.Inputs {
		set[in.InstrumentID] = true
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	return out
}

// describeDecision sets a decision page's link-preview text to its plain answer.
func describeDecision(p *page, v *decisionView) {
	if v != nil && v.Plain != nil {
		p.Description = v.Plain.Answer
	}
}

// decisionTitle is a decision page's <title>: "Decision #96 · Routed to Polymarket".
func decisionTitle(id int64, v *decisionView) string {
	t := "Decision #" + strconv.FormatInt(id, 10)
	if v != nil && v.Plain != nil {
		t += " · " + v.Plain.Badge
	}
	return t
}

// loadDecisionInstruments loads every instrument a decision mentions in one
// query; missing ones are left out and fall back to their IDs.
func (s *Server) loadDecisionInstruments(ctx context.Context, st *store.Store, d canonical.Decision) map[string]canonical.Instrument {
	ids := collectIDs(d)
	recs, err := st.GetInstruments(ctx, ids)
	if err != nil {
		s.log.Warn("loading instruments for decision", "count", len(ids), "err", err.Error())
		return nil
	}
	out := make(map[string]canonical.Instrument, len(recs))
	for id, rec := range recs {
		out[id] = rec.Instrument
	}
	return out
}

// titlesOf maps instrument IDs to their venue titles.
func titlesOf(ins map[string]canonical.Instrument) map[string]string {
	out := make(map[string]string, len(ins))
	for id, in := range ins {
		out[id] = in.Title
	}
	return out
}

// handleDecisionJSON answers GET /decisions/{id}/json: the stored Decision
// exactly as the router wrote it, for the audit layer's "raw JSON" link.
func (s *Server) handleDecisionJSON(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		http.Error(w, "no such routing decision", http.StatusNotFound)
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
		http.Error(w, "no such routing decision", http.StatusNotFound)
		return
	case err != nil:
		s.log.Error("loading routing decision for json", "decision_id", id, "err", err)
		http.Error(w, "the database is unavailable; try again shortly", http.StatusServiceUnavailable)
		return
	}
	body, err := json.MarshalIndent(rd.Decision, "", "  ")
	if err != nil {
		s.log.Error("encoding routing decision", "decision_id", id, "err", err)
		http.Error(w, "the decision could not be encoded", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write(append(body, '\n')); err != nil {
		s.log.Error("writing routing decision json", "err", err)
	}
}

// liveQuoteAge is a pair page's newest quote for one contract and its age.
type liveQuoteAge struct {
	quote canonical.Quote
	age   string
}

// attachPlainPair adds the pair presenter's view and the plain page title.
// The instruments are loaded with their terms; a lookup failure only drops
// the plain names back to the venue titles.
func (s *Server) attachPlainPair(ctx context.Context, st *store.Store, data *pairPage, rel canonical.Relationship, quotes map[string]liveQuoteAge) {
	recs, err := st.GetInstruments(ctx, []string{rel.AID, rel.BID})
	if err != nil {
		s.log.Warn("loading instruments for pair", "relationship_id", rel.ID, "err", err.Error())
	}
	in := pairInput{Rel: rel, MinConfidence: s.cfg.MinRouteConfidence,
		A: instrumentOr(recs, rel.AID, data.A.Title), B: instrumentOr(recs, rel.BID, data.B.Title)}
	if q, ok := quotes[rel.AID]; ok {
		in.QuoteA, in.AgeA = &q.quote, q.age
	}
	if q, ok := quotes[rel.BID]; ok {
		in.QuoteB, in.AgeB = &q.quote, q.age
	}
	data.Plain = presentPair(in)
	data.Title = data.Plain.Title
	data.Description = data.Plain.Meaning
	data.TargetLabels = map[string]string{
		rel.AID: "A · " + data.Plain.A.Venue + " · " + data.Plain.A.Name,
		rel.BID: "B · " + data.Plain.B.Venue + " · " + data.Plain.B.Name,
	}
}

// instrumentOr returns the stored instrument, or a stub carrying its ID,
// venue and title so names fall back cleanly.
func instrumentOr(recs map[string]store.InstrumentRecord, id, title string) canonical.Instrument {
	if r, ok := recs[id]; ok {
		return r.Instrument
	}
	return canonical.Instrument{ID: id, Venue: canonical.VenueOf(id), Title: title}
}
