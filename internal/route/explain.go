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

// explain writes the one-paragraph summary every Decision must carry: the
// order, the chosen route with each cost term, the ranked alternatives, every
// exclusion with its reason, and the fee and basis caveats.
func explain(d canonical.Decision, opts []option, sz sizing, cfg Config) string {
	var b strings.Builder
	b.WriteString(orderSentence(d.Order, d.TargetID, sz))
	if len(opts) == 0 {
		b.WriteString(" No venue can fill this order: every candidate was excluded.")
	} else {
		b.WriteString(" " + chosenSentence(opts[0], sz))
		for i, opt := range opts[1:] {
			b.WriteString(" " + alternativeSentence(i+2, opt, opts[0], sz))
		}
	}
	if len(d.Exclusions) > 0 {
		parts := make([]string, 0, len(d.Exclusions))
		for _, ex := range d.Exclusions {
			parts = append(parts, fmt.Sprintf("%s (%s: %s)", ex.InstrumentID, ex.Reason, ex.Detail))
		}
		b.WriteString(" Excluded: " + strings.Join(parts, "; ") + ".")
	}
	b.WriteString(" " + feeSentence(opts))
	fmt.Fprintf(&b, " No basis-risk penalty is applied in v0, and partners below a relationship confidence of %s are excluded.", cfg.MinRouteConfidence)
	return b.String()
}

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

func chosenSentence(opt option, sz sizing) string {
	l := opt.leg
	filled := "all " + l.Quantity.String()
	if !opt.full {
		filled = fmt.Sprintf("only %s of %s", l.Quantity, sz.qty)
	}
	levels := "1 level"
	if n := len(opt.fill.Consumed); n != 1 {
		levels = fmt.Sprintf("%d levels", n)
	}
	return fmt.Sprintf("Best route: %s (%s) fills %s contracts at a VWAP of %s across %s %s.",
		l.Venue, l.InstrumentID, filled, money(l.VWAP), levels, costClause(l))
}

// costClause spells out the per-contract terms that make up the all-in figure.
func costClause(l canonical.Leg) string {
	if l.Side == canonical.SideBuy {
		return fmt.Sprintf("plus a fee of %s per contract, an all-in cost of %s per $1 payout", money(l.FeePerContract), money(l.AllInPerContract))
	}
	return fmt.Sprintf("less a fee of %s per contract, net proceeds of %s per contract", money(l.FeePerContract), money(l.AllInPerContract))
}

func alternativeSentence(rank int, opt, best option, sz sizing) string {
	l := opt.leg
	gap := l.AllInPerContract.Sub(best.leg.AllInPerContract)
	if l.Side == canonical.SideSell {
		gap = gap.Neg()
	}
	s := fmt.Sprintf("%s (%s) ranks #%d at %s per contract (VWAP %s, fee %s; %s worse)",
		l.Venue, l.InstrumentID, rank, money(l.AllInPerContract), money(l.VWAP), money(l.FeePerContract), money(gap))
	if !opt.full {
		s += fmt.Sprintf(", filling only %s of %s contracts", l.Quantity, sz.qty)
	}
	return s + "."
}

// feeSentence says which venues' fees are verified and which are approximate.
func feeSentence(opts []option) string {
	var verified, approx []string
	for _, opt := range opts {
		v := string(opt.leg.Venue)
		if opt.verified {
			verified = append(verified, v)
		} else {
			approx = append(approx, v)
		}
	}
	switch {
	case len(opts) == 0:
		return "Fees: none computed."
	case len(approx) == 0:
		return fmt.Sprintf("Fees are verified against official documentation for %s.", strings.Join(verified, ", "))
	case len(verified) == 0:
		return fmt.Sprintf("Fees: approximate for %s (schedule not yet verified against official documentation).", strings.Join(approx, ", "))
	}
	return fmt.Sprintf("Fees are verified for %s but approximate for %s (schedule not yet verified against official documentation).",
		strings.Join(verified, ", "), strings.Join(approx, ", "))
}
