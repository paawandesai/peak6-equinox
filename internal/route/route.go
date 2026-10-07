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
	// Eligibility holds the staleness, skew, and depth thresholds.
	Eligibility EligibilityConfig
}

// Leg routes.
const (
	RouteDirect = "direct"
)

// Exclusion reasons. Each Exclusion also carries a human-readable Detail.
const (
	ReasonNoRelationship        = "no_relationship"
	ReasonRelationshipMismatch  = "relationship_mismatch"
	ReasonNotRoutable           = "not_routable"
	ReasonComplementUnsupported = "complement_unsupported"
	ReasonLowConfidence         = "low_confidence"
	ReasonQuoteMismatch         = "quote_mismatch"
	ReasonNotOpen               = "not_open"
	ReasonNoBook                = "no_book"
	ReasonNoQuantity            = "no_quantity"
	ReasonInsufficientDepth     = "insufficient_depth"
	ReasonLimitNotReachable     = "limit_not_reachable"
	ReasonFeeUnknown            = "fee_unknown"
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
	leg      canonical.Leg
	fill     fill
	full     bool // the whole order fills
	verified bool // the fee schedule is verified against official docs
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
	}

	cands := []candidate{}
	targetEligible := false
	targetExcludedBySkew := false

	// Check target eligibility (staleness, missing book, depth).
	if targetOK, targetExs := IsEligible(target.Quote, now, cfg.Eligibility); !targetOK {
		for _, ex := range targetExs {
			d.Exclusions = append(d.Exclusions, canonical.Exclusion{
				InstrumentID: target.Instrument.ID,
				Venue:        target.Instrument.Venue,
				Reason:       ex.Reason,
				Detail:       ex.Detail,
			})
		}
	} else {
		targetEligible = true
		cands = append(cands, candidate{Candidate: target, route: RouteDirect})
	}

	// Check each partner's relationship, eligibility, and skew against target.
	for _, p := range partners {
		if ex, ok := partnerExclusion(target, p, cfg); !ok {
			d.Exclusions = append(d.Exclusions, ex)
			continue
		}

		// Eligibility: staleness, missing book, depth.
		if partnerOK, partnerExs := IsEligible(p.Quote, now, cfg.Eligibility); !partnerOK {
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

		// Skew: check time difference against target (only if target was eligible).
		if targetEligible {
			if excludeOlder, warnings := Skew(target.Quote, p.Quote, cfg.Eligibility); excludeOlder {
				older := OlderQuote(target.Quote, p.Quote)
				if older == p.Instrument.ID {
					skewSec := int(target.Quote.FetchedAt.Sub(p.Quote.FetchedAt).Seconds())
					if skewSec < 0 {
						skewSec = -skewSec
					}
					d.Exclusions = append(d.Exclusions, canonical.Exclusion{
						InstrumentID: p.Instrument.ID,
						Venue:        p.Instrument.Venue,
						Reason:       ReasonSkew,
						Detail:       SkewDetail(skewSec),
					})
					continue
				}
				// If target is older, exclude it from cands and record ONCE.
				if older == target.Instrument.ID && !targetExcludedBySkew {
					skewSec := int(p.Quote.FetchedAt.Sub(target.Quote.FetchedAt).Seconds())
					if skewSec < 0 {
						skewSec = -skewSec
					}
					d.Exclusions = append(d.Exclusions, canonical.Exclusion{
						InstrumentID: target.Instrument.ID,
						Venue:        target.Instrument.Venue,
						Reason:       ReasonSkew,
						Detail:       SkewDetail(skewSec),
					})
					targetExcludedBySkew = true
					// Remove target from cands.
					newCands := []candidate{}
					for _, c := range cands {
						if c.Instrument.ID != target.Instrument.ID {
							newCands = append(newCands, c)
						}
					}
					cands = newCands
				}
				// Don't continue - partner is still eligible and should be added to cands.
			} else if len(warnings) > 0 {
				d.Warnings = append(d.Warnings, warnings...)
			}
		}

		cands = append(cands, candidate{Candidate: p, route: RouteDirect})
	}

	sz := size(o, cands)
	var opts []option
	for _, c := range cands {
		opt, ex, ok := evaluate(c, o, sz)
		if !ok {
			d.Exclusions = append(d.Exclusions, ex)
			continue
		}
		opts = append(opts, opt)
	}
	sort.SliceStable(opts, func(i, j int) bool { return better(o.Side, opts[i], opts[j]) })

	for i, opt := range opts {
		alt := canonical.Alternative{Rank: i + 1, Legs: []canonical.Leg{opt.leg}, AllInPerContract: opt.leg.AllInPerContract}
		switch {
		case i == 0:
			alt.Note = NoteChosen
		case !opt.full:
			alt.Note = fmt.Sprintf("%s: %s of %s contracts", NotePartial, opt.leg.Quantity, sz.qty)
		}
		d.Alternatives = append(d.Alternatives, alt)
	}
	if len(opts) > 0 {
		d.Chosen = []canonical.Leg{opts[0].leg}
	}
	d.Warnings = append(d.Warnings, warnings(o, opts, sz)...)
	d.Explanation = explain(d, opts, sz, cfg)
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
	case r.Type == canonical.RelComplement:
		ex.Reason, ex.Detail = ReasonComplementUnsupported, "complement routes (buying the other side) arrive in M3"
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
func evaluate(c candidate, o canonical.Order, sz sizing) (option, canonical.Exclusion, bool) {
	ex := canonical.Exclusion{InstrumentID: c.Instrument.ID, Venue: c.Instrument.Venue}
	fail := func(reason, detail string) (option, canonical.Exclusion, bool) {
		ex.Reason, ex.Detail = reason, detail
		return option{}, ex, false
	}
	if c.Instrument.Status == canonical.StatusClosed || c.Instrument.Status == canonical.StatusSettled {
		return fail(ReasonNotOpen, fmt.Sprintf("instrument is %s", c.Instrument.Status))
	}
	levels := bookSide(c.Quote, o.Side, o.Outcome)
	if c.Quote.Quality.Has(canonical.QualityNoBook) || len(levels) == 0 {
		return fail(ReasonNoBook, fmt.Sprintf("no %s on the %s side", sideName(o.Side), o.Outcome))
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
	case f.Quantity.LessThan(sz.qty) && o.TIF == canonical.TIFFOK:
		if f.Depth.GreaterThanOrEqual(sz.qty) {
			return fail(ReasonLimitNotReachable, fmt.Sprintf("only %s of %s contracts within the %s limit; FOK needs all", f.Quantity, sz.qty, o.LimitPrice))
		}
		return fail(ReasonInsufficientDepth, fmt.Sprintf("only %s of %s contracts available; FOK needs all", f.Quantity, sz.qty))
	}

	fee, err := totalFee(c.Instrument.FeeSchedule, o.Side, f)
	if err != nil {
		return fail(ReasonFeeUnknown, err.Error())
	}
	leg := canonical.Leg{
		InstrumentID:            c.Instrument.ID,
		Venue:                   c.Instrument.Venue,
		Side:                    o.Side,
		Outcome:                 o.Outcome,
		Quantity:                f.Quantity,
		VWAP:                    f.VWAP(),
		FeePerContract:          fee.Div(f.Quantity),
		BasisPenaltyPerContract: decimal.Zero, // priced in M3
		Route:                   c.route,
	}
	leg.AllInPerContract = allIn(leg)
	return option{leg: leg, fill: f, full: f.Quantity.Equal(sz.qty), verified: c.Instrument.FeeSchedule.Verified}, canonical.Exclusion{}, true
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

// better orders options: full fills before partial ones, then the lowest
// all-in cost (buy) or the highest net proceeds (sell), then the larger fill,
// then the deeper book (domain.md rule 6), then the instrument ID so the
// order is deterministic.
func better(side canonical.Side, a, b option) bool {
	if a.full != b.full {
		return a.full
	}
	if !a.leg.AllInPerContract.Equal(b.leg.AllInPerContract) {
		if side == canonical.SideBuy {
			return a.leg.AllInPerContract.LessThan(b.leg.AllInPerContract)
		}
		return a.leg.AllInPerContract.GreaterThan(b.leg.AllInPerContract)
	}
	if !a.leg.Quantity.Equal(b.leg.Quantity) {
		return a.leg.Quantity.GreaterThan(b.leg.Quantity)
	}
	if !a.fill.DepthWithinLimit.Equal(b.fill.DepthWithinLimit) {
		return a.fill.DepthWithinLimit.GreaterThan(b.fill.DepthWithinLimit)
	}
	return a.leg.InstrumentID < b.leg.InstrumentID
}

// warnings lists what a reader should know beyond the ranking.
func warnings(o canonical.Order, opts []option, sz sizing) []string {
	w := []string{}
	for _, opt := range opts {
		if !opt.verified {
			w = append(w, fmt.Sprintf("fees: approximate for %s (schedule not verified against official docs)", opt.leg.Venue))
		}
	}
	if len(opts) > 0 && !opts[0].full {
		w = append(w, fmt.Sprintf("%s: chosen route fills %s of %s contracts (%s)", NotePartial, opts[0].leg.Quantity, sz.qty, o.TIF))
	}
	if sz.notional != nil && sz.ok {
		w = append(w, fmt.Sprintf("quantity %s derived from notional %s at the %s touch", sz.qty, sz.notional, sz.touch))
	}
	w = append(w, "basis-risk penalty is not applied in v0")
	return w
}

// sideName is the book side an order hits.
func sideName(s canonical.Side) string {
	if s == canonical.SideBuy {
		return "asks"
	}
	return "bids"
}
