// Package route simulates routing one order across the venues that hold
// equivalent contracts and explains the result (FR-12, FR-27, D-026).
//
// The router is venue-agnostic: it imports only canonical, fees, decimal and
// the standard library (D-028, enforced by imports_test.go). v0 walks each
// candidate's book for the full size, adds fees from the instrument's fee
// schedule, ranks the candidates by all-in cost per contract and lists every
// exclusion with its reason. Splits across venues, complement routes,
// staleness and skew filters, and the basis-risk penalty arrive in M3.
package route

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/fees"
)

// Config holds the router's inputs that are not part of the order. The
// caller fills it from config.Config and the round it is routing against.
type Config struct {
	// RoundID is the fetch round the candidates' quotes come from.
	RoundID int64
	// MinRouteConfidence excludes partners whose relationship confidence is
	// below it (domain.md rule 3; MIN_ROUTE_CONFIDENCE).
	MinRouteConfidence decimal.Decimal
	// BasisPenaltyMultiplier scales the basis-risk score into dollars per
	// contract (M3-02; BASIS_PENALTY_MULTIPLIER, default 0.01 = one cent).
	BasisPenaltyMultiplier decimal.Decimal
	// Eligibility holds the staleness, skew, and depth thresholds.
	Eligibility EligibilityConfig
}

// Leg routes.
const (
	RouteDirect     = "direct"
	RouteComplement = "complement"
)

// Exclusion reasons. Each Exclusion also carries a human-readable Detail.
const (
	ReasonNoRelationship        = "no_relationship"
	ReasonRelationshipMismatch  = "relationship_mismatch"
	ReasonNotRoutable           = "not_routable"
	ReasonComplementUnsupported = "complement_unsupported" // Deprecated (M3-04): complements are now routable; kept for backward compatibility with old decision logs
	ReasonLowConfidence         = "low_confidence"
	ReasonQuoteMismatch         = "quote_mismatch"
	ReasonNotOpen               = "not_open"
	ReasonNoBook                = "no_book"
	ReasonNoQuantity            = "no_quantity"
	ReasonInsufficientDepth     = "insufficient_depth"
	ReasonLimitNotReachable     = "limit_not_reachable"
	ReasonFeeUnknown            = "fee_unknown"
	ReasonBasisRiskUnknown      = "basis_risk_unknown"
	ReasonStale                 = "stale"
	ReasonSkew                  = "skew"
)

// Note values on an Alternative.
const (
	NoteChosen  = "chosen"
	NotePartial = "partial fill"
)

// candidate is an eligible instrument with the route the order would take on it.
type candidate struct {
	canonical.Candidate
	route string
}

// option is a candidate the order can fill on, ranked against the others.
type option struct {
	leg        canonical.Leg
	fill       fill
	full       bool            // the whole order fills
	verified   bool            // the fee schedule is verified against official docs
	basisScore decimal.Decimal // the basis-risk score used for tie-breaking
}

// sizing records how the order's quantity in contracts was determined.
type sizing struct {
	qty      decimal.Decimal
	notional *decimal.Decimal // set when the order was given as a USD notional
	touch    decimal.Decimal  // the touch used to convert the notional
	ok       bool             // false when a notional could not be converted
}

// Route simulates o against target and its partners and returns the
// Decision. It only errors on invalid input (a bad order, config or target);
// every other outcome, including "no venue can fill this", is a Decision
// whose Exclusions and Explanation say why.
func Route(o canonical.Order, target canonical.Candidate, partners []canonical.Candidate, cfg Config, now time.Time) (canonical.Decision, error) {
	if err := o.Validate(); err != nil {
		return canonical.Decision{}, fmt.Errorf("routing: order: %w", err)
	}
	if cfg.RoundID <= 0 {
		return canonical.Decision{}, errors.New("routing: round id is required")
	}
	if err := canonical.ValidateInstrumentID(target.Instrument.ID); err != nil {
		return canonical.Decision{}, fmt.Errorf("routing: target: %w", err)
	}
	if target.Quote.InstrumentID != target.Instrument.ID {
		return canonical.Decision{}, fmt.Errorf("routing: target quote is for %q, not %q", target.Quote.InstrumentID, target.Instrument.ID)
	}

	d := canonical.Decision{
		RoundID:      cfg.RoundID,
		TargetID:     target.Instrument.ID,
		Order:        o,
		Chosen:       []canonical.Leg{},
		Alternatives: []canonical.Alternative{},
		Exclusions:   []canonical.Exclusion{},
		Warnings:     []string{},
		DecidedAt:    now.UTC(),
		Inputs:       []canonical.DecisionInput{},
	}

	// Capture the target's input snapshot (UI-25, D-098 call 3).
	// For the target, relationship type is empty (no relationship to itself) and confidence is 1.0.
	d.Inputs = append(d.Inputs, buildInput(target, o.Outcome, cfg.RoundID, nil))

	// Collect all candidates (target + partners) that pass pre-routing checks and eligibility.
	type pendingCandidate struct {
		cand  canonical.Candidate
		route string
	}
	var pending []pendingCandidate

	// Check target eligibility (staleness, missing book, depth).
	if targetOK, targetExs := IsEligible(target.Quote, o.Side, o.Outcome, cfg.Eligibility); !targetOK {
		for _, ex := range targetExs {
			d.Exclusions = append(d.Exclusions, canonical.Exclusion{
				InstrumentID: target.Instrument.ID,
				Venue:        target.Instrument.Venue,
				Reason:       ex.Reason,
				Detail:       ex.Detail,
			})
		}
	} else {
		pending = append(pending, pendingCandidate{cand: target, route: RouteDirect})
	}

	// Check each partner's relationship and eligibility.
	for _, p := range partners {
		// For complement relationships, the outcome checked is opposite.
		// For other relationships (equivalent, equivalent_given_facts), use the same outcome.
		isComplement := p.Relationship != nil && p.Relationship.Type == canonical.RelComplement
		checkSide := o.Side
		checkOutcome := o.Outcome
		routeType := RouteDirect

		if isComplement {
			// Complement: buying YES on A ≡ buying NO on B (domain.md rule 5)
			checkOutcome = complementOutcome(o.Outcome)
			routeType = RouteComplement
		}

		// Capture partner input snapshot (UI-25, D-098 call 3), including excluded partners.
		d.Inputs = append(d.Inputs, buildInput(p, checkOutcome, cfg.RoundID, p.Relationship))

		if ex, ok := partnerExclusion(target, p, cfg); !ok {
			d.Exclusions = append(d.Exclusions, ex)
			continue
		}

		if partnerOK, partnerExs := IsEligible(p.Quote, checkSide, checkOutcome, cfg.Eligibility); !partnerOK {
			for _, ex := range partnerExs {
				d.Exclusions = append(d.Exclusions, canonical.Exclusion{
					InstrumentID: p.Instrument.ID,
					Venue:        p.Instrument.Venue,
					Reason:       ex.Reason,
					Detail:       ex.Detail,
				})
			}
			continue
		}

		pending = append(pending, pendingCandidate{cand: p, route: routeType})
	}

	// Skew check: use Skew() to check each candidate against the freshest quote.
	// This ensures partner-vs-partner skew is enforced per M3-01 AC.
	var cands []candidate
	if len(pending) > 1 {
		// Find the freshest quote as reference.
		var freshest canonical.Quote
		for _, pc := range pending {
			if pc.cand.Quote.FetchedAt.After(freshest.FetchedAt) {
				freshest = pc.cand.Quote
			}
		}
		// Check each candidate against the reference.
		for _, pc := range pending {
			excludeOlder, warnings := Skew(freshest, pc.cand.Quote, cfg.Eligibility)
			if excludeOlder {
				gapSec := int(freshest.FetchedAt.Sub(pc.cand.Quote.FetchedAt).Seconds())
				d.Exclusions = append(d.Exclusions, canonical.Exclusion{
					InstrumentID: pc.cand.Instrument.ID,
					Venue:        pc.cand.Instrument.Venue,
					Reason:       ReasonSkew,
					Detail:       SkewDetail(gapSec),
				})
			} else {
				d.Warnings = append(d.Warnings, warnings...)
				cands = append(cands, candidate{Candidate: pc.cand, route: pc.route})
			}
		}
	} else if len(pending) == 1 {
		cands = append(cands, candidate{Candidate: pending[0].cand, route: pending[0].route})
	}

	sz := size(o, cands)

	// Evaluate each candidate to know which are viable (for Alternatives listing)
	// Even if sz.ok is false (no touch to convert notional), we still evaluate to
	// collect exclusion reasons (e.g., ReasonNoQuantity for zero-touch candidates)
	var opts []option
	var viableCands []canonical.Candidate
	for _, c := range cands {
		opt, ex, ok := evaluate(c, o, sz, cfg)
		if !ok {
			d.Exclusions = append(d.Exclusions, ex)
			continue
		}
		opts = append(opts, opt)
		viableCands = append(viableCands, c.Candidate)
	}
	sort.SliceStable(opts, func(i, j int) bool { return better(o.Side, opts[i], opts[j]) })

	// Use Sweep to fill across venues (M3-03 greedy sweep)
	// Sweep returns legs in cost order, filling from cheapest venue first
	var legs []canonical.Leg
	if sz.ok && len(viableCands) > 0 {
		// Create a resolved order with quantity (Sweep needs Quantity, not Notional)
		resolvedOrder := o
		resolvedOrder.Quantity = &sz.qty
		resolvedOrder.Notional = nil
		legs = Sweep(resolvedOrder, viableCands, cfg)

		// FOK: add exclusions for any candidate that couldn't fill the full order
		// (Finding #1: FOK needs exclusions even when another venue fills completely)
		if o.TIF == canonical.TIFFOK {
			totalFilled := sumQuantity(legs)
			if totalFilled.LessThan(sz.qty) {
				// Sweep returned nil or partial under FOK, so no fill occurred
				// Add exclusions for each viable candidate showing why it couldn't fill
				for _, opt := range opts {
					reason := ReasonInsufficientDepth
					detail := fmt.Sprintf("FOK requires %s contracts; %s can fill %s", sz.qty, opt.leg.InstrumentID, opt.leg.Quantity)

					// For limit orders, use more specific messaging
					if o.Type == canonical.OrderLimit && opt.leg.Quantity.IsZero() {
						reason = ReasonLimitNotReachable
						detail = fmt.Sprintf("best %s is outside the %s limit", sideName(o.Side), money(*o.LimitPrice))
					} else if o.Type == canonical.OrderLimit && opt.leg.Quantity.LessThan(sz.qty) {
						reason = ReasonLimitNotReachable
						detail = fmt.Sprintf("%s of %s contracts at or better than limit %s", opt.leg.Quantity, sz.qty, money(*o.LimitPrice))
					}

					d.Exclusions = append(d.Exclusions, canonical.Exclusion{
						InstrumentID: opt.leg.InstrumentID,
						Venue:        opt.leg.Venue,
						Reason:       reason,
						Detail:       detail,
					})
				}
				legs = nil // Clear legs since FOK rejected the partial
			} else {
				// FOK filled completely from one or more venues
				// Add exclusions for candidates that couldn't fill the full order alone
				// (Round 3 #1: skip candidates whose InstrumentID is in chosen legs)
				chosenIDs := make(map[string]bool)
				for _, leg := range legs {
					chosenIDs[leg.InstrumentID] = true
				}
				for _, opt := range opts {
					if chosenIDs[opt.leg.InstrumentID] {
						continue // Skip chosen candidates
					}
					if opt.leg.Quantity.LessThan(sz.qty) {
						reason := ReasonInsufficientDepth
						detail := fmt.Sprintf("FOK requires %s contracts; %s can fill %s", sz.qty, opt.leg.InstrumentID, opt.leg.Quantity)
						if o.Type == canonical.OrderLimit {
							reason = ReasonLimitNotReachable
							detail = fmt.Sprintf("%s of %s contracts at or better than limit %s", opt.leg.Quantity, sz.qty, money(*o.LimitPrice))
						}
						d.Exclusions = append(d.Exclusions, canonical.Exclusion{
							InstrumentID: opt.leg.InstrumentID,
							Venue:        opt.leg.Venue,
							Reason:       reason,
							Detail:       detail,
						})
					}
				}
			}
		}
	}

	// Build Alternatives: show single-venue options that were considered
	// FOK excludes partials from Alternatives (M3-03 #3: FOK partials out of Alternatives)
	for i, opt := range opts {
		alt := canonical.Alternative{Rank: i + 1, Legs: []canonical.Leg{opt.leg}, AllInPerContract: opt.leg.AllInPerContract}
		if !opt.full {
			if o.TIF == canonical.TIFFOK {
				continue // FOK: skip partial alternatives
			}
			alt.Note = fmt.Sprintf("%s: %s of %s contracts", NotePartial, opt.leg.Quantity, sz.qty)
		}
		d.Alternatives = append(d.Alternatives, alt)
	}

	// Chosen is the result of the greedy sweep (may be multi-venue)
	d.Chosen = legs

	// Mark the chosen alternative if it matches a single-venue option
	if len(legs) == 1 && len(d.Alternatives) > 0 {
		for i := range d.Alternatives {
			if d.Alternatives[i].Legs[0].InstrumentID == legs[0].InstrumentID &&
				d.Alternatives[i].Legs[0].Quantity.Equal(legs[0].Quantity) {
				// Finding #3: keep NotePartial prefix for IOC partials (e.g., "partial fill: N of M (chosen)")
				if strings.HasPrefix(d.Alternatives[i].Note, NotePartial) {
					d.Alternatives[i].Note = fmt.Sprintf("%s (%s)", d.Alternatives[i].Note, NoteChosen)
				} else {
					d.Alternatives[i].Note = NoteChosen
				}
				break
			}
		}
	} else if len(legs) > 1 {
		// Multi-venue fill: add it as rank 1 Alternative with NoteChosen (+ NotePartial if partial)
		totalQty := decimal.Zero
		totalNotional := decimal.Zero
		for _, leg := range legs {
			totalQty = totalQty.Add(leg.Quantity)
			totalNotional = totalNotional.Add(leg.Quantity.Mul(leg.AllInPerContract))
		}
		avgAllIn := decimal.Zero
		if totalQty.IsPositive() {
			avgAllIn = totalNotional.Div(totalQty)
		}
		note := NoteChosen
		if sz.ok && totalQty.LessThan(sz.qty) {
			// Round 3 #2: match single-leg format "partial fill: N of M (chosen)"
			note = fmt.Sprintf("%s: %s of %s contracts (%s)", NotePartial, totalQty, sz.qty, NoteChosen)
		}
		sweepAlt := canonical.Alternative{
			Rank:             1,
			Legs:             legs,
			AllInPerContract: avgAllIn,
			Note:             note,
		}
		// Prepend the multi-venue alternative and re-rank the rest
		d.Alternatives = append([]canonical.Alternative{sweepAlt}, d.Alternatives...)
		for i := 1; i < len(d.Alternatives); i++ {
			d.Alternatives[i].Rank = i + 1
			if d.Alternatives[i].Note == NoteChosen {
				d.Alternatives[i].Note = "" // remove NoteChosen from old rank 1
			}
		}
	}

	d.Warnings = append(d.Warnings, warnings(o, opts, legs, sz)...)
	if !sz.ok && len(legs) == 0 && len(opts) == 0 {
		// No candidate could convert the notional (no touch anywhere)
		d.Explanation = orderSentence(o, d.TargetID, sz) + " No venue can fill this order: every candidate was excluded."
		if sz.notional != nil {
			d.Explanation = orderSentence(o, d.TargetID, sz) + " Notional order could not be converted: no candidate has a touch."
		}
	} else {
		d.Explanation = explainSweep(d, legs, opts, sz, cfg)
	}
	return d, nil
}

// partnerExclusion checks a partner's relationship to the target and its
// quote before any book is walked (domain.md rule 3).
func partnerExclusion(target, p canonical.Candidate, cfg Config) (canonical.Exclusion, bool) {
	ex := canonical.Exclusion{InstrumentID: p.Instrument.ID, Venue: p.Instrument.Venue}
	r := p.Relationship
	switch {
	case r == nil:
		ex.Reason, ex.Detail = ReasonNoRelationship, "no relationship to the target was supplied"
	case !linksTo(*r, target.Instrument.ID, p.Instrument.ID):
		ex.Reason, ex.Detail = ReasonRelationshipMismatch, fmt.Sprintf("relationship links %s and %s, not the target", r.AID, r.BID)
	case !r.Type.Routable():
		ex.Reason, ex.Detail = ReasonNotRoutable, fmt.Sprintf("relationship type %s is never routed", r.Type)
	case r.Confidence.LessThan(cfg.MinRouteConfidence):
		ex.Reason, ex.Detail = ReasonLowConfidence, fmt.Sprintf("confidence %s is below the %s floor", r.Confidence, cfg.MinRouteConfidence)
	case p.Quote.InstrumentID != p.Instrument.ID:
		ex.Reason, ex.Detail = ReasonQuoteMismatch, fmt.Sprintf("quote is for %s", p.Quote.InstrumentID)
	default:
		return canonical.Exclusion{}, true
	}
	return ex, false
}

// linksTo reports whether r relates exactly the two instruments a and b.
func linksTo(r canonical.Relationship, a, b string) bool {
	return (r.AID == a && r.BID == b) || (r.AID == b && r.BID == a)
}

// complementOutcome returns the opposite outcome (YES ↔ NO).
func complementOutcome(o canonical.Outcome) canonical.Outcome {
	if o == canonical.OutcomeYes {
		return canonical.OutcomeNo
	}
	return canonical.OutcomeYes
}

// size resolves the order's quantity in contracts. A USD notional is
// converted at the touch of the first candidate (the target) that has one,
// rounded down to whole contracts.
func size(o canonical.Order, cands []candidate) sizing {
	if o.Quantity != nil {
		return sizing{qty: *o.Quantity, ok: true}
	}
	s := sizing{notional: o.Notional}
	for _, c := range cands {
		t, ok := touch(c.Quote, o.Side, o.Outcome)
		if !ok || !t.IsPositive() {
			continue
		}
		s.touch, s.ok = t, true
		s.qty = o.Notional.Div(t).Floor()
		return s
	}
	return s
}

// evaluate walks one candidate's book for the order and turns the result
// into a ranked option or an exclusion.
func evaluate(c candidate, o canonical.Order, sz sizing, cfg Config) (option, canonical.Exclusion, bool) {
	ex := canonical.Exclusion{InstrumentID: c.Instrument.ID, Venue: c.Instrument.Venue}
	fail := func(reason, detail string) (option, canonical.Exclusion, bool) {
		ex.Reason, ex.Detail = reason, detail
		return option{}, ex, false
	}
	if c.Instrument.Status == canonical.StatusClosed || c.Instrument.Status == canonical.StatusSettled {
		return fail(ReasonNotOpen, fmt.Sprintf("instrument is %s", c.Instrument.Status))
	}

	// For complement routes, use the opposite outcome (buying YES on A ≡ buying NO on B).
	// The side remains the same (we're still buying or selling).
	evalOutcome := o.Outcome
	if c.route == RouteComplement {
		evalOutcome = complementOutcome(o.Outcome)
	}

	levels := bookSide(c.Quote, o.Side, evalOutcome)
	if c.Quote.Quality.Has(canonical.QualityNoBook) || len(levels) == 0 {
		return fail(ReasonNoBook, fmt.Sprintf("no %s on the %s side", sideName(o.Side), evalOutcome))
	}
	if !sz.ok {
		return fail(ReasonNoQuantity, "notional could not be converted: no candidate has a touch")
	}
	if !sz.qty.IsPositive() {
		return fail(ReasonNoQuantity, fmt.Sprintf("notional %s buys less than one contract at the %s touch", sz.notional, sz.touch))
	}

	f := walk(levels, o.Side, sz.qty, o.LimitPrice)
	switch {
	case f.Quantity.IsZero():
		return fail(ReasonLimitNotReachable, fmt.Sprintf("best %s is %s, outside the %s limit", sideName(o.Side), levels[0].Price, o.LimitPrice))
		// FOK partial-fill check removed: Sweep handles FOK by checking combined fill (M3-03 #2)
	}

	fee, err := totalFee(c.Instrument.FeeSchedule, o.Side, f)
	if err != nil {
		return fail(ReasonFeeUnknown, err.Error())
	}

	// Calculate basis penalty (M3-02): Score × BASIS_PENALTY_MULTIPLIER.
	// Target has no relationship (nil), so no basis penalty.
	// Partners must have BasisRisk; nil means unknown risk (never guess, domain.md).
	basisScore := decimal.Zero
	basisPenalty := decimal.Zero
	if c.Relationship != nil {
		if c.Relationship.BasisRisk == nil {
			return fail(ReasonBasisRiskUnknown, "partner relationship has no basis-risk score")
		}
		basisScore = c.Relationship.BasisRisk.Score
		basisPenalty = basisScore.Mul(cfg.BasisPenaltyMultiplier)
	}

	// For complement routes, the leg shows the actual trade (e.g., NO on the complement instrument).
	legOutcome := o.Outcome
	if c.route == RouteComplement {
		legOutcome = complementOutcome(o.Outcome)
	}

	leg := canonical.Leg{
		InstrumentID:            c.Instrument.ID,
		Venue:                   c.Instrument.Venue,
		Side:                    o.Side,
		Outcome:                 legOutcome,
		Quantity:                f.Quantity,
		VWAP:                    f.VWAP(),
		FeePerContract:          fee.Div(f.Quantity),
		BasisPenaltyPerContract: basisPenalty,
		Route:                   c.route,
	}
	leg.AllInPerContract = allIn(leg)
	return option{leg: leg, fill: f, full: f.Quantity.Equal(sz.qty), verified: c.Instrument.FeeSchedule.Verified, basisScore: basisScore}, canonical.Exclusion{}, true
}

// totalFee sums the taker fee over the consumed levels, each charged at its
// own price the way the venue would charge each matched trade.
func totalFee(s canonical.FeeSchedule, side canonical.Side, f fill) (decimal.Decimal, error) {
	total := decimal.Zero
	for _, l := range f.Consumed {
		fee, err := fees.Compute(s, side, l.Price, l.Size)
		if err != nil {
			return decimal.Zero, fmt.Errorf("fee schedule %q: %w", s.ID, err)
		}
		total = total.Add(fee)
	}
	return total, nil
}

// allIn is the per-contract figure the ranking uses (domain.md rule 6): for a
// buy the all-in cost per $1 payout, VWAP + fee + basis penalty; for a sell
// the net proceeds per contract, VWAP − fee − basis penalty.
func allIn(l canonical.Leg) decimal.Decimal {
	if l.Side == canonical.SideBuy {
		return l.VWAP.Add(l.FeePerContract).Add(l.BasisPenaltyPerContract)
	}
	return l.VWAP.Sub(l.FeePerContract).Sub(l.BasisPenaltyPerContract)
}

// quantizeCost rounds all-in cost to 4 decimal places for transitive comparison
// (M3-02: 0.0001 tolerance = $0.0001 per contract).
func quantizeCost(c decimal.Decimal) decimal.Decimal {
	return c.Round(4)
}

// better orders options: full fills before partial ones, then the lowest
// all-in cost (buy) or the highest net proceeds (sell) quantized to 4 dp
// (M3-02), then the larger fill, then the deeper book, then the lower basis
// score, then the lexicographically first venue name (domain.md rule 6).
func better(side canonical.Side, a, b option) bool {
	if a.full != b.full {
		return a.full
	}
	// Quantize to 4 dp for transitive comparison (M3-02: 0.0001 = $0.0001/contract).
	aQuant := quantizeCost(a.leg.AllInPerContract)
	bQuant := quantizeCost(b.leg.AllInPerContract)
	if !aQuant.Equal(bQuant) {
		if side == canonical.SideBuy {
			return aQuant.LessThan(bQuant)
		}
		return aQuant.GreaterThan(bQuant)
	}
	if !a.leg.Quantity.Equal(b.leg.Quantity) {
		return a.leg.Quantity.GreaterThan(b.leg.Quantity)
	}
	if !a.fill.DepthWithinLimit.Equal(b.fill.DepthWithinLimit) {
		return a.fill.DepthWithinLimit.GreaterThan(b.fill.DepthWithinLimit)
	}
	// Within quantized cost equality, prefer lower basis score (M3-02 tie-break).
	if !a.basisScore.Equal(b.basisScore) {
		return a.basisScore.LessThan(b.basisScore)
	}
	// Final tie-break: venue name lexicographically (deterministic).
	if a.leg.Venue != b.leg.Venue {
		return a.leg.Venue < b.leg.Venue
	}
	return a.leg.InstrumentID < b.leg.InstrumentID
}

// warnings lists what a reader should know beyond the ranking.
func warnings(o canonical.Order, opts []option, legs []canonical.Leg, sz sizing) []string {
	w := []string{}
	for _, opt := range opts {
		if !opt.verified {
			w = append(w, fmt.Sprintf("fees: approximate for %s (schedule not verified against official docs)", opt.leg.Venue))
		}
	}
	// Derive partial warning from swept legs, not opts[0] (M3-03 #3: multi-venue partials)
	if len(legs) > 0 && sz.ok {
		totalFilled := decimal.Zero
		for _, leg := range legs {
			totalFilled = totalFilled.Add(leg.Quantity)
		}
		if totalFilled.LessThan(sz.qty) {
			w = append(w, fmt.Sprintf("%s: chosen route fills %s of %s contracts (%s)", NotePartial, totalFilled, sz.qty, o.TIF))
		}
	}
	if sz.notional != nil && sz.ok {
		w = append(w, fmt.Sprintf("quantity %s derived from notional %s at the %s touch", sz.qty, sz.notional, sz.touch))
	}
	return w
}

// sideName is the book side an order hits.
func sideName(s canonical.Side) string {
	if s == canonical.SideBuy {
		return "asks"
	}
	return "bids"
}

// explainSweep generates explanation for a sweep result (M3-03).
// It handles both single-venue and multi-venue fills.
func explainSweep(d canonical.Decision, legs []canonical.Leg, opts []option, sz sizing, cfg Config) string {
	var b strings.Builder
	b.WriteString(orderSentence(d.Order, d.TargetID, sz))

	if len(legs) == 0 {
		b.WriteString(" No venue can fill this order: every candidate was excluded.")
	} else if len(legs) == 1 {
		// Single-venue fill: include total filled, total cost, average all-in, shortfall (Finding #4)
		filled := legs[0].Quantity
		totalCost := filled.Mul(legs[0].AllInPerContract)
		fillDesc := fmt.Sprintf("%s of %s", filled, sz.qty)
		if filled.Equal(sz.qty) {
			fillDesc = fmt.Sprintf("all %s", filled)
		}

		// For complement routes, explain the opposite-side trade and compare to best direct (M3-04 AC)
		routeDesc := ""
		if legs[0].Route == RouteComplement {
			// Find the best direct (non-complement) option to compare against
			var bestDirectCost decimal.Decimal
			var bestDirectVenue string
			var bestDirectID string
			foundDirect := false
			for _, opt := range opts {
				if opt.leg.Route == RouteDirect {
					if !foundDirect || opt.leg.AllInPerContract.LessThan(bestDirectCost) {
						bestDirectCost = opt.leg.AllInPerContract
						bestDirectVenue = string(opt.leg.Venue)
						bestDirectID = opt.leg.InstrumentID
						foundDirect = true
					}
				}
			}

			routeDesc = fmt.Sprintf(" Chose %s on %s (complement of %s) at %s because the all-in cost (VWAP + fee + basis) was %s per contract",
				strings.ToUpper(string(legs[0].Outcome)), legs[0].InstrumentID, d.TargetID, legs[0].Venue, money(legs[0].AllInPerContract))
			if foundDirect {
				routeDesc += fmt.Sprintf(", vs %s for %s on %s at %s",
					money(bestDirectCost), strings.ToUpper(string(d.Order.Outcome)), bestDirectID, bestDirectVenue)
			}
		} else {
			routeDesc = fmt.Sprintf(" Best route: %s (%s)", legs[0].Venue, legs[0].InstrumentID)
		}

		fmt.Fprintf(&b, "%s fills %s contracts at a VWAP of %s %s Total cost: %s.",
			routeDesc, fillDesc, money(legs[0].VWAP), costClause(legs[0]), money(totalCost))
		if filled.LessThan(sz.qty) {
			shortfall := sz.qty.Sub(filled)
			fmt.Fprintf(&b, " Shortfall: %s contracts (%s).", shortfall, d.Order.TIF)
		}
	} else {
		// Multi-venue sweep fill: include total cost and explicit "filled N of M" (M3-03 #6)
		totalQty := decimal.Zero
		totalCost := decimal.Zero
		for _, leg := range legs {
			totalQty = totalQty.Add(leg.Quantity)
			legCost := leg.Quantity.Mul(leg.AllInPerContract)
			totalCost = totalCost.Add(legCost)
		}
		avgAllIn := decimal.Zero
		if totalQty.IsPositive() {
			avgAllIn = totalCost.Div(totalQty)
		}

		fillDesc := fmt.Sprintf("%s of %s", totalQty, sz.qty)
		if totalQty.Equal(sz.qty) {
			fillDesc = "all"
		}

		fmt.Fprintf(&b, " Best route: %s fill across %d venues (%s contracts at average all-in %s, total cost %s).",
			fillDesc, len(legs), totalQty, money(avgAllIn), money(totalCost))

		// List each leg in cost order
		for i, leg := range legs {
			// For complement routes, indicate the opposite-side trade (M3-04)
			legDesc := fmt.Sprintf("%s (%s)", leg.Venue, leg.InstrumentID)
			if leg.Route == RouteComplement {
				legDesc = fmt.Sprintf("%s (%s %s, complement)", leg.Venue, strings.ToUpper(string(leg.Outcome)), leg.InstrumentID)
			}
			fmt.Fprintf(&b, " [%d] %s: %s contracts at VWAP %s %s",
				i+1, legDesc, leg.Quantity, money(leg.VWAP), costClause(leg))
		}

		if totalQty.LessThan(sz.qty) {
			shortfall := sz.qty.Sub(totalQty)
			fmt.Fprintf(&b, " Shortfall: %s contracts (%s).", shortfall, d.Order.TIF)
		}
	}

	// List single-venue alternatives that were considered
	// Build a map from instrument ID to option so we can get the basis score (Finding #2)
	optMap := make(map[string]option)
	for _, opt := range opts {
		optMap[opt.leg.InstrumentID] = opt
	}
	if len(d.Alternatives) > 1 {
		for _, alt := range d.Alternatives {
			if alt.Note == NoteChosen || len(alt.Legs) > 1 {
				continue // Skip the chosen multi-venue alternative
			}
			leg := alt.Legs[0]
			gap := leg.AllInPerContract.Sub(d.Alternatives[0].AllInPerContract)
			if leg.Side == canonical.SideSell {
				gap = gap.Neg()
			}
			var breakdown string
			if leg.BasisPenaltyPerContract.IsZero() {
				breakdown = fmt.Sprintf("VWAP %s, fee %s", money(leg.VWAP), money(leg.FeePerContract))
			} else {
				// Finding #2: restore basis score (e.g., "basis 0.0300; score 1.0000")
				score := decimal.Zero
				if opt, ok := optMap[leg.InstrumentID]; ok {
					score = opt.basisScore
				}
				breakdown = fmt.Sprintf("VWAP %s, fee %s, basis %s; score %s", money(leg.VWAP), money(leg.FeePerContract), money(leg.BasisPenaltyPerContract), money(score))
			}
			fmt.Fprintf(&b, " %s (%s) ranks #%d at %s per contract (%s; %s worse).",
				leg.Venue, leg.InstrumentID, alt.Rank, money(leg.AllInPerContract), breakdown, money(gap))
		}
	}

	if len(d.Exclusions) > 0 {
		parts := make([]string, 0, len(d.Exclusions))
		for _, ex := range d.Exclusions {
			parts = append(parts, fmt.Sprintf("%s (%s: %s)", ex.InstrumentID, ex.Reason, ex.Detail))
		}
		b.WriteString(" Excluded: " + strings.Join(parts, "; ") + ".")
	}

	// Fee verification status from all opts (chosen + alternatives), not just chosen legs
	// (M3-03 #5: use FeeSchedule.Verified, not venue name)
	var verified, approx []string
	seenVenues := make(map[canonical.Venue]bool)
	for _, opt := range opts {
		if seenVenues[opt.leg.Venue] {
			continue // already added
		}
		seenVenues[opt.leg.Venue] = true
		v := string(opt.leg.Venue)
		if opt.verified {
			verified = append(verified, v)
		} else {
			approx = append(approx, v)
		}
	}

	switch {
	case len(legs) == 0:
		b.WriteString(" Fees: none computed.")
	case len(approx) == 0 && len(verified) > 0:
		fmt.Fprintf(&b, " Fees are verified against official documentation for %s.", strings.Join(uniqueStrings(verified), ", "))
	case len(verified) == 0 && len(approx) > 0:
		fmt.Fprintf(&b, " Fees: approximate for %s (schedule not yet verified against official documentation).", strings.Join(uniqueStrings(approx), ", "))
	case len(verified) > 0 && len(approx) > 0:
		fmt.Fprintf(&b, " Fees are verified for %s but approximate for %s (schedule not yet verified against official documentation).",
			strings.Join(uniqueStrings(verified), ", "), strings.Join(uniqueStrings(approx), ", "))
	}

	fmt.Fprintf(&b, " Basis-risk penalty is applied as score × %s per contract, and partners below a relationship confidence of %s are excluded.",
		cfg.BasisPenaltyMultiplier, cfg.MinRouteConfidence)

	return b.String()
}

// uniqueStrings returns unique strings from a slice, preserving order.
func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// sumQuantity returns the total quantity across all legs.
func sumQuantity(legs []canonical.Leg) decimal.Decimal {
	total := decimal.Zero
	for _, leg := range legs {
		total = total.Add(leg.Quantity)
	}
	return total
}

// buildInput captures the router's input snapshot for one candidate (UI-25, D-098 call 3).
// It records the quote the router read at decision time, so the trace shows the router's
// actual view instead of a later re-query that might disagree.
func buildInput(c canonical.Candidate, outcomeSide canonical.Outcome, roundID int64, rel *canonical.Relationship) canonical.DecisionInput {
	// Determine relationship type and confidence.
	// For the target, rel is nil: type is empty ("self") and confidence is 1.0.
	relType := canonical.RelationshipType("")
	conf := decimal.NewFromInt(1)
	if rel != nil {
		relType = rel.Type
		conf = rel.Confidence
	}

	// Extract the best ask and ask depth from the quote.
	// The side consumed depends on the outcome: YES asks for YES, YES bids for NO.
	// Cap depth at 10 levels (UI-25 AC: "top N levels, capped").
	const maxDepth = 10
	var asks []canonical.Level
	if outcomeSide == canonical.OutcomeYes {
		asks = c.Quote.YesAsks
	} else {
		asks = c.Quote.YesBids
	}
	if len(asks) > maxDepth {
		asks = asks[:maxDepth]
	}

	bestAsk := decimal.Zero
	if len(asks) > 0 {
		bestAsk = asks[0].Price
	}

	return canonical.DecisionInput{
		InstrumentID:     c.Instrument.ID,
		Venue:            c.Instrument.Venue,
		RelationshipType: relType,
		Confidence:       conf,
		OutcomeSide:      outcomeSide,
		BestAsk:          bestAsk,
		AskDepth:         asks,
		FetchedAt:        c.Quote.FetchedAt,
		VenueTS:          c.Quote.VenueTS,
		RoundID:          roundID,
	}
}
