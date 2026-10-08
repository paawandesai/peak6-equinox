package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/route"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// Order input bounds (ticket M1-09). Quantity is in contracts.
const (
	minQuantity = 1
	maxQuantity = 100_000
	// maxRouteBody bounds the form body; a valid form is well under 1 KB.
	maxRouteBody = 4096
	// maxRoutePartners caps how many relationships one route call loads.
	maxRoutePartners = 1000
	// maxFieldLen bounds any single form value before it is interpreted.
	maxFieldLen = 64
)

// routeFormView is the route form as the pair page renders it: the pair,
// both targets, and the values to pre-fill.
type routeFormView struct {
	PairID   int64
	Targets  []string
	Target   string
	Side     string
	Outcome  string
	Quantity string
	Type     string
	Limit    string
	TIF      string
}

func defaultRouteForm(rel canonical.Relationship) routeFormView {
	return routeFormView{
		PairID: rel.ID, Targets: []string{rel.AID, rel.BID}, Target: rel.AID,
		Side: string(canonical.SideBuy), Outcome: string(canonical.OutcomeYes), Quantity: "100",
		Type: string(canonical.OrderMarket), TIF: string(canonical.TIFIOC),
	}
}

// routeInput is the validated POST /route form.
type routeInput struct {
	PairID int64
	Target string
	Order  canonical.Order
}

// parseRouteForm validates every field and returns the input or the list of
// problems, worded for the person filling in the form. Enums are checked
// against the canonical types; nothing is guessed.
func parseRouteForm(get func(string) string) (routeInput, []string) {
	var (
		in   routeInput
		errs []string
	)
	// raw bounds a value's length; field also trims surrounding whitespace.
	// IDs are taken raw so they must match their format exactly (AB-08).
	raw := func(name string) string {
		v := get(name)
		if len(v) > maxFieldLen {
			errs = append(errs, name+" is too long")
			return ""
		}
		return v
	}
	field := func(name string) string { return strings.TrimSpace(raw(name)) }
	var ok bool
	if in.PairID, ok = parseID(raw("pair")); !ok {
		errs = append(errs, "pair must be a pair ID from this site")
	}
	in.Target = raw("target")
	if err := canonical.ValidateInstrumentID(in.Target); err != nil {
		errs = append(errs, "target must be one of the pair's instrument IDs")
	}
	o := canonical.Order{
		Side:    canonical.Side(strings.ToLower(field("side"))),
		Outcome: canonical.Outcome(strings.ToLower(field("outcome"))),
		Type:    canonical.OrderType(strings.ToLower(field("type"))),
		TIF:     canonical.TimeInForce(strings.ToUpper(field("tif"))),
	}
	if !o.Side.Valid() {
		errs = append(errs, "side must be buy or sell")
	}
	if !o.Outcome.Valid() {
		errs = append(errs, "outcome must be yes or no")
	}
	if !o.Type.Valid() {
		errs = append(errs, "type must be market or limit")
	}
	if !o.TIF.Valid() {
		errs = append(errs, "tif must be IOC or FOK")
	}
	qty, err := decimal.NewFromString(field("quantity"))
	switch {
	case err != nil:
		errs = append(errs, fmt.Sprintf("quantity must be a number of contracts between %d and %d", minQuantity, maxQuantity))
	case qty.LessThan(decimal.NewFromInt(minQuantity)) || qty.GreaterThan(decimal.NewFromInt(maxQuantity)):
		errs = append(errs, fmt.Sprintf("quantity must be between %d and %d contracts", minQuantity, maxQuantity))
	default:
		o.Quantity = &qty
	}
	limit := field("limit")
	switch {
	case o.Type == canonical.OrderLimit && limit == "":
		errs = append(errs, "a limit order needs a limit price between 0 and 1")
	case o.Type == canonical.OrderMarket && limit != "":
		errs = append(errs, "a market order takes no limit price; clear it or choose a limit order")
	case limit != "":
		p, err := decimal.NewFromString(limit)
		if err != nil || canonical.ValidatePrice(p) != nil {
			errs = append(errs, "limit price must be a number between 0 and 1")
		} else {
			o.LimitPrice = &p
		}
	}
	if len(errs) == 0 {
		if err := o.Validate(); err != nil {
			errs = append(errs, "order is not valid: "+err.Error())
		}
	}
	in.Order = o
	return in, errs
}

// routePage is the data for route.html and the route_result fragment.
type routePage struct {
	page
	PairID     int64
	Errors     []string
	DecisionID int64
	Decision   *decisionView
}

// decisionView is a Decision prepared for the template.
type decisionView struct {
	RoundID      int64
	TargetID     string
	DecidedAt    time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
	Snapshot     time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
	OrderLine    string
	Chosen       []canonical.Leg
	Alternatives []canonical.Alternative
	Exclusions   []canonical.Exclusion
	Warnings     []string
	Explanation  string
	Partners     int
}

func newDecisionView(d canonical.Decision, partners int, snapshot time.Time) *decisionView {
	return &decisionView{
		RoundID: d.RoundID, TargetID: d.TargetID, DecidedAt: d.DecidedAt,
		Snapshot: snapshot, OrderLine: orderLine(d.Order),
		Chosen: d.Chosen, Alternatives: d.Alternatives, Exclusions: d.Exclusions, Warnings: d.Warnings,
		Explanation: d.Explanation, Partners: partners,
	}
}

// orderLine is the order in one line, FIX-style (FR-27).
func orderLine(o canonical.Order) string {
	size := "?"
	switch {
	case o.Quantity != nil:
		size = o.Quantity.String() + " contracts"
	case o.Notional != nil:
		size = "$" + o.Notional.String() + " notional"
	}
	line := fmt.Sprintf("%s %s %s, %s %s", o.Side, size, strings.ToUpper(string(o.Outcome)), o.Type, o.TIF)
	if o.LimitPrice != nil {
		line += " at " + o.LimitPrice.StringFixed(4)
	}
	return line
}

// isHTMX reports whether the request came from htmx and wants a fragment.
func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// handleRoute answers POST /route: validate the form, run the router against
// the pair's round, store the decision, and render the trace (US-03, FR-12).
// It never places an order anywhere; the only write is the decision row and
// its decision-log entry (AB-14).
func (s *Server) handleRoute(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRouteBody)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.routeFailure(w, r, http.StatusRequestEntityTooLarge, 0, "the form is too large")
			return
		}
		s.routeFailure(w, r, http.StatusBadRequest, 0, "the form could not be read")
		return
	}
	in, problems := parseRouteForm(r.PostForm.Get)
	if len(problems) > 0 {
		s.routeFailure(w, r, http.StatusBadRequest, in.PairID, problems...)
		return
	}
	if s.deps.Store == nil {
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the database is not configured on this server")
		return
	}
	st := s.deps.Store
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	// Get current round from context (pinned or latest) - NFR-04
	currentRound := roundFromContext(r.Context())

	rel, err := st.GetRelationship(ctx, in.PairID)
	switch {
	case isNotFound(err):
		s.routeFailure(w, r, http.StatusNotFound, in.PairID, "no such pair")
		return
	case err != nil:
		s.log.Error("loading relationship for route", "err", err)
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the database is unavailable; try again shortly")
		return
	}

	// Determine which round to use for routing and banner (M3-07, NFR-04)
	var round store.Round
	if currentRound.Pinned {
		// NFR-04: when a round is pinned, reject pairs from other rounds
		if rel.RoundID != currentRound.ID {
			s.routeFailure(w, r, http.StatusNotFound, in.PairID, "this pair belongs to a different round")
			return
		}
		round = currentRound
	} else {
		// Unpinned or no current round: use the pair's own round (allows routing older pairs from /log)
		round, err = st.GetRound(ctx, rel.RoundID)
		if err != nil {
			s.log.Error("loading round for route", "err", err)
			s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the database is unavailable; try again shortly")
			return
		}
	}

	if in.Target != rel.AID && in.Target != rel.BID {
		s.routeFailure(w, r, http.StatusBadRequest, in.PairID, "target must be one of the pair's instrument IDs")
		return
	}
	target, partners, err := s.loadRouteInputs(ctx, st, round.ID, in.Target)
	switch {
	case isNotFound(err):
		s.routeFailure(w, r, http.StatusNotFound, in.PairID, "the target instrument is no longer in the store")
		return
	case err != nil:
		s.log.Error("loading route inputs", "err", err)
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the database is unavailable; try again shortly")
		return
	}
	cfg := route.Config{
		RoundID:                round.ID,
		MinRouteConfidence:     s.cfg.MinRouteConfidence,
		BasisPenaltyMultiplier: s.cfg.BasisPenaltyMult,
		Eligibility: route.EligibilityConfig{
			Snapshot:     round.SnapshotAt(),
			StaleAfter:   s.cfg.StaleAfter,
			SkewWarn:     s.cfg.SkewWarn,
			SkewMax:      s.cfg.SkewMax,
			MinBookDepth: s.cfg.MinBookDepth,
		},
	}
	dec, err := route.Route(in.Order, target, partners, cfg, s.now())
	if err != nil {
		// Route only errors on invalid input, which the form checks already
		// caught; report it as a form problem rather than a server fault.
		s.routeFailure(w, r, http.StatusBadRequest, in.PairID, "order rejected: "+err.Error())
		return
	}
	id, err := st.InsertRoutingDecision(ctx, dec)
	if err != nil {
		s.log.Error("storing routing decision", "err", err)
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the decision could not be stored; try again shortly")
		return
	}
	if _, err := st.AppendLog(ctx, store.LogRouting, strconv.FormatInt(id, 10), routeLogSummary(dec), dec, dec.DecidedAt); err != nil {
		s.log.Error("logging routing decision", "decision_id", id, "err", err)
		s.routeFailure(w, r, http.StatusServiceUnavailable, in.PairID, "the decision could not be logged; try again shortly")
		return
	}
	s.log.Info("routing decision stored", "decision_id", id, "round_id", dec.RoundID, "target", dec.TargetID,
		"chosen", len(dec.Chosen), "alternatives", len(dec.Alternatives), "exclusions", len(dec.Exclusions))

	tz := timezoneFromRequest(r)
	p := s.routePageHeader(in.PairID, round, tz)
	p.StaleWarning = s.staleRoundWarning(round)
	data := routePage{
		page:       p,
		PairID:     in.PairID,
		DecisionID: id,
		Decision:   newDecisionView(dec, len(partners), round.SnapshotAt()),
	}
	s.renderRoute(w, r, http.StatusOK, data)
}

func (s *Server) routePageHeader(pairID int64, round store.Round, tz string) page {
	return s.pageBase(round, "Routing trace", fmt.Sprintf("Where would this order fill best, and why? (pair %d)", pairID), tz)
}

// routeFailure renders the problems at the given status: as a fragment for
// htmx (which swaps 400 responses in, see the htmx-config meta) or as a
// full page otherwise.
func (s *Server) routeFailure(w http.ResponseWriter, r *http.Request, status int, pairID int64, problems ...string) {
	round := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	s.renderRoute(w, r, status, routePage{page: s.routePageHeader(pairID, round, tz), PairID: pairID, Errors: problems})
}

func (s *Server) renderRoute(w http.ResponseWriter, r *http.Request, status int, data routePage) {
	if isHTMX(r) {
		s.renderTemplate(w, status, "route", "route_result", data)
		return
	}
	s.render(w, status, "route", data)
}

// loadRouteInputs reads the target with its quote and every instrument
// related to it in the round, each with its quote and relationship, the way
// `equinox route` does. A partner whose instrument is missing is logged and
// skipped; a missing quote becomes an empty, flagged book.
func (s *Server) loadRouteInputs(ctx context.Context, st *store.Store, roundID int64, targetID string) (canonical.Candidate, []canonical.Candidate, error) {
	now := s.now()
	target, err := loadCandidate(ctx, st, roundID, targetID, now)
	if err != nil {
		return canonical.Candidate{}, nil, err
	}
	rels, err := st.ListRelationships(ctx, store.RelationshipFilter{RoundID: roundID, InstrumentID: targetID, Limit: maxRoutePartners})
	if err != nil {
		return canonical.Candidate{}, nil, err
	}
	var partners []canonical.Candidate
	for i := range rels {
		rel := rels[i]
		otherID := rel.BID
		if otherID == targetID {
			otherID = rel.AID
		}
		c, err := loadCandidate(ctx, st, roundID, otherID, now)
		if err != nil {
			if !isNotFound(err) {
				return canonical.Candidate{}, nil, err
			}
			s.log.Warn("skipping route partner", "instrument", otherID, "relationship_id", rel.ID, "err", err.Error())
			continue
		}
		c.Relationship = &rel
		partners = append(partners, c)
	}
	return target, partners, nil
}

func loadCandidate(ctx context.Context, st *store.Store, roundID int64, id string, now time.Time) (canonical.Candidate, error) {
	rec, err := st.GetInstrument(ctx, id)
	if err != nil {
		return canonical.Candidate{}, err
	}
	q, err := quoteFor(ctx, st, roundID, id, now)
	if err != nil {
		return canonical.Candidate{}, err
	}
	return canonical.Candidate{Instrument: rec.Instrument, Quote: q}, nil
}

// routeLogSummary is the one-line decision-log entry for a routing decision
// (the same wording as `equinox route`).
func routeLogSummary(dec canonical.Decision) string {
	size := "?"
	switch {
	case dec.Order.Quantity != nil:
		size = dec.Order.Quantity.String() + " contracts"
	case dec.Order.Notional != nil:
		size = "$" + dec.Order.Notional.String()
	}
	if len(dec.Chosen) == 0 {
		return fmt.Sprintf("route %s %s %s on %s: no venue can fill (%d excluded)", dec.Order.Side, size, dec.Order.Outcome, dec.TargetID, len(dec.Exclusions))
	}
	l := dec.Chosen[0]
	return fmt.Sprintf("route %s %s %s on %s: %s %s at %s per contract", dec.Order.Side, size, dec.Order.Outcome, dec.TargetID,
		l.Venue, l.InstrumentID, l.AllInPerContract.StringFixed(4))
}
