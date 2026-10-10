package web

import (
	"fmt"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/fees"
)

// referenceSize is the contract quantity used to compute net gaps (UI-18).
var referenceSize = decimal.NewFromInt(100)

// NetGap holds the fee-adjusted price gap for one pair at the reference size.
type NetGap struct {
	Value    decimal.Decimal // net gap in dollars per contract (positive = A is cheaper)
	HasValue bool            // false when quotes are missing or stale
	Reason   string          // "no kalshi quote" / "kalshi one-sided: no YES ask" / "" when HasValue
}

// FeeEstimate holds one venue's fee assumption for display (UI-18 ◆ 3).
type FeeEstimate struct {
	Venue       string
	Description string // "7% × C × P × (1-P), rounded up to 1¢" etc.
	Source      string // URL
	Approximate bool   // true when schedule.Verified is false
}

// ComputeNetGap calculates the fee-adjusted best-price gap between two instruments
// at the reference size (UI-18 ◆ 1). Quotes must be from the same round. Returns
// NetGap{HasValue: false} when either quote is missing or the instrument has no
// verified fee schedule.
func ComputeNetGap(aInst, bInst canonical.Instrument, aQuote, bQuote *canonical.Quote) NetGap {
	// Check quotes exist.
	if aQuote == nil {
		return NetGap{HasValue: false, Reason: fmt.Sprintf("no %s quote", aInst.Venue)}
	}
	if bQuote == nil {
		return NetGap{HasValue: false, Reason: fmt.Sprintf("no %s quote", bInst.Venue)}
	}

	// Check quotes have asks. A book with bids only is one-sided, not missing (M6 P3).
	if len(aQuote.YesAsks) == 0 {
		return NetGap{HasValue: false, Reason: fmt.Sprintf("%s %s", aInst.Venue, oneSidedNote(aQuote))}
	}
	if len(bQuote.YesAsks) == 0 {
		return NetGap{HasValue: false, Reason: fmt.Sprintf("%s %s", bInst.Venue, oneSidedNote(bQuote))}
	}

	// Best ask prices.
	aBestAsk := aQuote.YesAsks[0].Price
	bBestAsk := bQuote.YesAsks[0].Price

	// Compute fees at reference size.
	aFee, aErr := fees.Compute(aInst.FeeSchedule, canonical.SideBuy, aBestAsk, referenceSize)
	if aErr != nil {
		return NetGap{HasValue: false, Reason: fmt.Sprintf("fee error: %s", aInst.Venue)}
	}
	bFee, bErr := fees.Compute(bInst.FeeSchedule, canonical.SideBuy, bBestAsk, referenceSize)
	if bErr != nil {
		return NetGap{HasValue: false, Reason: fmt.Sprintf("fee error: %s", bInst.Venue)}
	}

	// All-in per contract: VWAP + fee/qty (basis is not included per D-098 call 2 and HANDOFF.md ◆ 1).
	aAllIn := aBestAsk.Add(aFee.Div(referenceSize))
	bAllIn := bBestAsk.Add(bFee.Div(referenceSize))

	// Net gap: positive when A is cheaper (favourable to route via A instead of B).
	gap := bAllIn.Sub(aAllIn)
	return NetGap{Value: gap, HasValue: true, Reason: ""}
}

// DescribeFeeSchedule returns a human-readable description of a fee schedule
// for the checks key (UI-18 ◆ 3).
func DescribeFeeSchedule(sched canonical.FeeSchedule) string {
	switch sched.ID {
	case canonical.FeeScheduleKalshiTakerV1:
		mult, ok := sched.Params["multiplier"]
		if !ok {
			return "unknown formula"
		}
		roundTo, ok := sched.Params["round_to"]
		if !ok {
			roundTo = decimal.RequireFromString("0.01")
		}
		// Format round_to in cents: 0.01 → 1¢, 0.001 → 0.1¢.
		roundToCents := roundTo.Mul(decimal.NewFromInt(100))
		var roundToStr string
		if roundToCents.LessThan(decimal.NewFromInt(1)) {
			roundToStr = roundToCents.StringFixed(1)
		} else {
			roundToStr = roundToCents.StringFixed(0)
		}
		return fmt.Sprintf("%s × C × P × (1-P), rounded up to %s¢",
			mult.Mul(decimal.NewFromInt(100)).StringFixed(1),
			roundToStr)
	case canonical.FeeSchedulePolymarketV1:
		rate, ok := sched.Params["rate"]
		if !ok {
			return "unknown formula"
		}
		exp, ok := sched.Params["exponent"]
		if !ok {
			exp = decimal.NewFromInt(1)
		}
		if exp.Equal(decimal.NewFromInt(1)) {
			return fmt.Sprintf("C × %s × P × (1-P)",
				rate.StringFixed(2))
		}
		return fmt.Sprintf("C × %s × (P × (1-P))^%s",
			rate.StringFixed(2), exp.StringFixed(0))
	default:
		return "unknown formula"
	}
}

// BuildFeeEstimates returns the fee estimate for each unique venue in the given
// instruments (UI-18 ◆ 3).
func BuildFeeEstimates(instruments []canonical.Instrument) []FeeEstimate {
	seen := make(map[canonical.Venue]bool)
	var estimates []FeeEstimate
	for _, inst := range instruments {
		if seen[inst.Venue] {
			continue
		}
		seen[inst.Venue] = true
		est := FeeEstimate{
			Venue:       string(inst.Venue),
			Description: DescribeFeeSchedule(inst.FeeSchedule),
			Approximate: !inst.FeeSchedule.Verified,
			Source:      inst.FeeSchedule.Source,
		}
		estimates = append(estimates, est)
	}
	return estimates
}
