package route

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// pricePlaces is how many decimals an explanation shows for prices and costs.
const pricePlaces = 4

func money(d decimal.Decimal) string { return d.StringFixed(pricePlaces) }

// explain and related functions are replaced by explainSweep in route.go (M3-03).
// Kept here for backwards compatibility and potential future use.

func orderSentence(o canonical.Order, targetID string, sz sizing) string {
	kind := "market"
	if o.Type == canonical.OrderLimit {
		kind = "limit"
	}
	s := fmt.Sprintf("Simulated %s of %s %s contracts on %s as a %s %s order", o.Side, sz.qty, strings.ToUpper(string(o.Outcome)), targetID, kind, o.TIF)
	if o.LimitPrice != nil {
		s += " at " + money(*o.LimitPrice)
	}
	switch {
	case sz.notional != nil && sz.ok:
		s += fmt.Sprintf(", sized from a %s notional at the %s touch", money(*sz.notional), money(sz.touch))
	case sz.notional != nil:
		s += fmt.Sprintf(", sized from a %s notional that no candidate could price", money(*sz.notional))
	}
	return s + "."
}

// costClause spells out the per-contract terms that make up the all-in figure.
func costClause(l canonical.Leg) string {
	if l.Side == canonical.SideBuy {
		if l.BasisPenaltyPerContract.IsZero() {
			return fmt.Sprintf("plus a fee of %s per contract, an all-in cost of %s per $1 payout", money(l.FeePerContract), money(l.AllInPerContract))
		}
		return fmt.Sprintf("plus a fee of %s and basis penalty of %s per contract, an all-in cost of %s per $1 payout",
			money(l.FeePerContract), money(l.BasisPenaltyPerContract), money(l.AllInPerContract))
	}
	if l.BasisPenaltyPerContract.IsZero() {
		return fmt.Sprintf("less a fee of %s per contract, net proceeds of %s per contract", money(l.FeePerContract), money(l.AllInPerContract))
	}
	return fmt.Sprintf("less a fee of %s and basis penalty of %s per contract, net proceeds of %s per contract",
		money(l.FeePerContract), money(l.BasisPenaltyPerContract), money(l.AllInPerContract))
}
