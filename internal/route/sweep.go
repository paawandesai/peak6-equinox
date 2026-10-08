package route

import (
	"fmt"
	"sort"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Sweep implements the greedy multi-venue sweep algorithm (M3-03, FR-13).
// It ranks candidates by all-in cost per contract (ascending), walks each
// book from best to worst price level, filling as much as possible per venue,
// then combines legs in cost order. Returns the legs that filled, or an empty
// slice if no venue could fill any of the order.
//
// For IOC orders, partial fills are permitted. For FOK orders, partial fills
// are rejected; if the total available liquidity is insufficient, no fill
// occurs.
func Sweep(order canonical.Order, candidates []canonical.Candidate, cfg Config) []canonical.Leg {
	if len(candidates) == 0 {
		return nil
	}

	// Sweep requires a resolved quantity; if only notional is given,
	// the caller must have resolved it via size() first
	if order.Quantity == nil {
		return nil
	}

	// Build a candidate ranking by computing each candidate's option (similar to
	// evaluate() but for ranking purposes). We need full option details for
	// proper tie-breaking (depth, basis, full vs partial).
	type rankedCandidate struct {
		cand canonical.Candidate
		opt  option
	}
	var ranked []rankedCandidate
	for _, cand := range candidates {
		// Check basic viability (not closed, has a book)
		if cand.Instrument.Status == canonical.StatusClosed || cand.Instrument.Status == canonical.StatusSettled {
			continue
		}
		levels := bookSide(cand.Quote, order.Side, order.Outcome)
		if len(levels) == 0 || cand.Quote.Quality.Has(canonical.QualityNoBook) {
			continue
		}

		// Walk this candidate's book for the full order to get its option details
		f := walk(levels, order.Side, *order.Quantity, order.LimitPrice)
		if !f.Quantity.IsPositive() {
			continue // Can't fill anything (limit excluded or no depth)
		}

		// Calculate fee
		fee, err := totalFee(cand.Instrument.FeeSchedule, order.Side, f)
		if err != nil {
			continue // Fee calculation failed
		}

		// Basis penalty
		basisScore := decimal.Zero
		basisPenalty := decimal.Zero
		if cand.Relationship != nil {
			if cand.Relationship.BasisRisk == nil {
				continue // Skip candidates with unknown basis risk
			}
			basisScore = cand.Relationship.BasisRisk.Score
			basisPenalty = basisScore.Mul(cfg.BasisPenaltyMultiplier)
		}

		leg := canonical.Leg{
			InstrumentID:            cand.Instrument.ID,
			Venue:                   cand.Instrument.Venue,
			Side:                    order.Side,
			Outcome:                 order.Outcome,
			Quantity:                f.Quantity,
			VWAP:                    f.VWAP(),
			FeePerContract:          fee.Div(f.Quantity),
			BasisPenaltyPerContract: basisPenalty,
			Route:                   RouteDirect,
		}
		leg.AllInPerContract = allIn(leg)

		opt := option{
			leg:        leg,
			fill:       f,
			full:       f.Quantity.Equal(*order.Quantity),
			verified:   cand.Instrument.FeeSchedule.Verified,
			basisScore: basisScore,
		}

		ranked = append(ranked, rankedCandidate{cand: cand, opt: opt})
	}

	if len(ranked) == 0 {
		return nil
	}

	// Sort using the same better() logic as route.go (full fills first, then cost, depth, basis, venue)
	sort.SliceStable(ranked, func(i, j int) bool {
		return better(order.Side, ranked[i].opt, ranked[j].opt)
	})

	// Greedy sweep: consume from cheapest to most expensive
	// ranked is sorted by better() which prefers full fills, but for greedy sweep
	// we want to fill from cheapest first regardless of whether a more expensive
	// venue could fill completely. The only optimization is: if the absolute
	// cheapest venue can fill completely, use it alone (it's optimal).
	//
	// To determine "cheapest", we need to find the candidate with lowest quantized cost.
	cheapestIdx := 0
	if len(ranked) > 1 {
		cheapestCost := quantizeCost(ranked[0].opt.leg.AllInPerContract)
		for i := 1; i < len(ranked); i++ {
			cost := quantizeCost(ranked[i].opt.leg.AllInPerContract)
			if cost.LessThan(cheapestCost) {
				cheapestCost = cost
				cheapestIdx = i
			}
		}
	}
	// If the cheapest venue can fill completely, it's the optimal single-venue solution
	if ranked[cheapestIdx].opt.full {
		return []canonical.Leg{ranked[cheapestIdx].opt.leg}
	}

	// Multi-venue sweep needed: re-rank by cost only (not full-fill preference)
	sort.SliceStable(ranked, func(i, j int) bool {
		// Rank by quantized all-in cost, then depth, basis, venue
		iCost := quantizeCost(ranked[i].opt.leg.AllInPerContract)
		jCost := quantizeCost(ranked[j].opt.leg.AllInPerContract)
		if !iCost.Equal(jCost) {
			if order.Side == canonical.SideBuy {
				return iCost.LessThan(jCost)
			}
			return iCost.GreaterThan(jCost)
		}
		if !ranked[i].opt.fill.DepthWithinLimit.Equal(ranked[j].opt.fill.DepthWithinLimit) {
			return ranked[i].opt.fill.DepthWithinLimit.GreaterThan(ranked[j].opt.fill.DepthWithinLimit)
		}
		if !ranked[i].opt.basisScore.Equal(ranked[j].opt.basisScore) {
			return ranked[i].opt.basisScore.LessThan(ranked[j].opt.basisScore)
		}
		if ranked[i].opt.leg.Venue != ranked[j].opt.leg.Venue {
			return ranked[i].opt.leg.Venue < ranked[j].opt.leg.Venue
		}
		return ranked[i].opt.leg.InstrumentID < ranked[j].opt.leg.InstrumentID
	})

	// Walk each venue's book for remaining quantity
	remaining := *order.Quantity
	var legs []canonical.Leg
	for _, rc := range ranked {
		if !remaining.IsPositive() {
			break
		}

		// Walk this candidate's book for the remaining quantity (may be less than full order)
		levels := bookSide(rc.cand.Quote, order.Side, order.Outcome)
		f := walk(levels, order.Side, remaining, order.LimitPrice)

		if !f.Quantity.IsPositive() {
			continue
		}

		// Calculate fee for this leg
		fee, err := totalFee(rc.cand.Instrument.FeeSchedule, order.Side, f)
		if err != nil {
			continue
		}

		// Calculate basis penalty
		basisPenalty := decimal.Zero
		if rc.cand.Relationship != nil && rc.cand.Relationship.BasisRisk != nil {
			basisPenalty = rc.cand.Relationship.BasisRisk.Score.Mul(cfg.BasisPenaltyMultiplier)
		}

		leg := canonical.Leg{
			InstrumentID:            rc.cand.Instrument.ID,
			Venue:                   rc.cand.Instrument.Venue,
			Side:                    order.Side,
			Outcome:                 order.Outcome,
			Quantity:                f.Quantity,
			VWAP:                    f.VWAP(),
			FeePerContract:          fee.Div(f.Quantity),
			BasisPenaltyPerContract: basisPenalty,
			Route:                   RouteDirect,
		}
		leg.AllInPerContract = allIn(leg)
		legs = append(legs, leg)
		remaining = remaining.Sub(f.Quantity)
	}

	// Check FOK constraint: if we couldn't fill the full order, reject the partial
	if order.TIF == canonical.TIFFOK && remaining.IsPositive() {
		return nil
	}

	return legs
}

// SweepExplanation generates a human-readable explanation for a multi-venue
// sweep result. It lists legs in cost order, shows the total filled, total
// cost, average all-in price, and any shortfall.
func SweepExplanation(order canonical.Order, legs []canonical.Leg, _ Config) string {
	if len(legs) == 0 {
		return "No venue could fill any of the order."
	}

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

	out := fmt.Sprintf("Filled %s of %s contracts across %d venue(s) at average all-in %s per contract.",
		totalQty, *order.Quantity, len(legs), avgAllIn)

	if totalQty.LessThan(*order.Quantity) {
		shortfall := (*order.Quantity).Sub(totalQty)
		out += fmt.Sprintf(" Shortfall: %s contracts (%s).", shortfall, order.TIF)
	}

	out += " Legs in cost order:"
	for i, leg := range legs {
		out += fmt.Sprintf(" [%d] %s %s %s contracts at VWAP %s + fee %s + basis %s = %s",
			i+1, leg.Venue, leg.InstrumentID, leg.Quantity,
			leg.VWAP, leg.FeePerContract, leg.BasisPenaltyPerContract, leg.AllInPerContract)
	}

	return out
}
