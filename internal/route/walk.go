package route

import (
	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

var one = decimal.NewFromInt(1)

// fill is the result of walking one side of a book for an order.
type fill struct {
	// Consumed lists the levels taken, best first, with the size taken at each.
	Consumed []canonical.Level
	// Quantity is the total size taken and Notional the sum of price × size,
	// so Notional / Quantity is the VWAP.
	Quantity decimal.Decimal
	Notional decimal.Decimal
	// Depth is the total size resting on the side and DepthWithinLimit the
	// part at prices the order's limit allows (all of it for a market order).
	Depth            decimal.Decimal
	DepthWithinLimit decimal.Decimal
}

// VWAP is the size-weighted average price of the consumed levels; zero when
// nothing was consumed.
func (f fill) VWAP() decimal.Decimal {
	if !f.Quantity.IsPositive() {
		return decimal.Zero
	}
	return f.Notional.Div(f.Quantity)
}

// Best returns the first (best) consumed price.
func (f fill) Best() (decimal.Decimal, bool) {
	if len(f.Consumed) == 0 {
		return decimal.Zero, false
	}
	return f.Consumed[0].Price, true
}

// Worst returns the last (worst) consumed price.
func (f fill) Worst() (decimal.Decimal, bool) {
	if len(f.Consumed) == 0 {
		return decimal.Zero, false
	}
	return f.Consumed[len(f.Consumed)-1].Price, true
}

// bookSide returns the levels an order consumes, best first, priced in the
// order's outcome: YES asks for a YES buy, YES bids for a YES sell. For NO
// the complementary YES side is re-priced as 1 − p (domain.md rule 4): the NO
// ask is 1 − the best YES bid and the NO bid is 1 − the best YES ask, so the
// best-first order of the YES side is preserved.
func bookSide(q canonical.Quote, side canonical.Side, outcome canonical.Outcome) []canonical.Level {
	var src []canonical.Level
	flip := outcome == canonical.OutcomeNo
	switch {
	case (side == canonical.SideBuy) != flip:
		src = q.YesAsks
	default:
		src = q.YesBids
	}
	out := make([]canonical.Level, 0, len(src))
	for _, l := range src {
		if l.Validate() != nil {
			continue // never trust external data; an invalid level is simply not liquidity
		}
		p := l.Price
		if flip {
			p = one.Sub(p)
		}
		out = append(out, canonical.Level{Price: p, Size: l.Size})
	}
	return out
}

// withinLimit reports whether a level at price is acceptable to the order:
// no worse than the limit, or anything for a market order (limit nil).
func withinLimit(side canonical.Side, price decimal.Decimal, limit *decimal.Decimal) bool {
	if limit == nil {
		return true
	}
	if side == canonical.SideBuy {
		return price.LessThanOrEqual(*limit)
	}
	return price.GreaterThanOrEqual(*limit)
}

// walk consumes levels (best first) until qty is filled, skipping levels the
// limit excludes. It never consumes more than qty or more than the side holds.
func walk(levels []canonical.Level, side canonical.Side, qty decimal.Decimal, limit *decimal.Decimal) fill {
	var f fill
	remaining := qty
	for _, l := range levels {
		if !l.Size.IsPositive() {
			continue
		}
		f.Depth = f.Depth.Add(l.Size)
		if !withinLimit(side, l.Price, limit) {
			continue
		}
		f.DepthWithinLimit = f.DepthWithinLimit.Add(l.Size)
		if !remaining.IsPositive() {
			continue
		}
		take := decimal.Min(remaining, l.Size)
		f.Consumed = append(f.Consumed, canonical.Level{Price: l.Price, Size: take})
		f.Quantity = f.Quantity.Add(take)
		f.Notional = f.Notional.Add(l.Price.Mul(take))
		remaining = remaining.Sub(take)
	}
	return f
}

// touch returns the best price on the side an order would hit, if any.
func touch(q canonical.Quote, side canonical.Side, outcome canonical.Outcome) (decimal.Decimal, bool) {
	levels := bookSide(q, side, outcome)
	if len(levels) == 0 {
		return decimal.Zero, false
	}
	return levels[0].Price, true
}
