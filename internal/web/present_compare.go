package web

import (
	"strings"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// The trader layer (ui/plain-language): one comparison row per priced
// candidate, the winner first, in cents per contract and dollars per order,
// and a stacked bar per candidate (price | fee | safety charge).

// compareRows lists the ranked alternatives, then candidates that were priced
// but ruled out as a sure loss (their stored detail carries the all-in).
func compareRows(d canonical.Decision, dc decisionContext, qty decimal.Decimal) []compareRow {
	var rows []compareRow
	best := decimal.Zero
	if len(d.Chosen) > 0 {
		best = averageAllIn(d.Chosen)
	}
	for _, alt := range d.Alternatives {
		if len(alt.Legs) == 0 {
			continue
		}
		price, fee, safety := legComponents(alt.Legs)
		row := compareRow{
			Winner:     alt.Rank == 1 && len(d.Chosen) > 0,
			Where:      altVenues(alt.Legs),
			Price:      centsText(price),
			Fee:        centsText(fee),
			Safety:     safetyText(safety),
			Total:      centsText(alt.AllInPerContract),
			OrderTotal: dollarsText(alt.AllInPerContract.Mul(legsQty(alt.Legs))),
		}
		row.Contract, row.Title, row.Opposite = altContract(alt.Legs, dc)
		if q := legsQty(alt.Legs); q.LessThan(qty) {
			row.OrderTotal += " for " + countText(q)
		}
		switch {
		case row.Winner:
			row.Verdict = "Best"
		case d.Order.Side == canonical.SideSell:
			row.Verdict = centsText(best.Sub(alt.AllInPerContract).Abs()) + " less"
		default:
			row.Verdict = signedCentsText(alt.AllInPerContract.Sub(best)) + " more"
		}
		rows = append(rows, row)
	}
	for _, ex := range d.Exclusions {
		if ex.Reason != exSureLoss {
			continue
		}
		m := sureLossBuyRe.FindStringSubmatch(ex.Detail)
		if m == nil {
			continue
		}
		c, err := decimal.NewFromString(m[1])
		if err != nil {
			continue
		}
		name := dc.nameOf(ex.InstrumentID)
		rows = append(rows, compareRow{
			Excluded:   true,
			Where:      venueName(ex.Venue),
			Contract:   name.Long,
			Title:      dc.titleOf(ex.InstrumentID),
			Opposite:   relTypes(d)[ex.InstrumentID] == canonical.RelComplement,
			Price:      "—",
			Fee:        "—",
			Safety:     "—",
			Total:      centsText(c),
			OrderTotal: dollarsText(c.Mul(qty)),
			Verdict:    "would cost more than it can ever pay out",
		})
	}
	return rows
}

// legComponents is the size-weighted price, fee and safety charge per contract.
func legComponents(legs []canonical.Leg) (price, fee, safety decimal.Decimal) {
	q := legsQty(legs)
	if q.IsZero() {
		return
	}
	for _, l := range legs {
		price = price.Add(l.VWAP.Mul(l.Quantity))
		fee = fee.Add(l.FeePerContract.Mul(l.Quantity))
		safety = safety.Add(l.BasisPenaltyPerContract.Mul(l.Quantity))
	}
	return price.Div(q), fee.Div(q), safety.Div(q)
}

func legsQty(legs []canonical.Leg) decimal.Decimal {
	q := decimal.Zero
	for _, l := range legs {
		q = q.Add(l.Quantity)
	}
	return q
}

// safetyText is the safety charge in cents, or "none" when it is zero.
func safetyText(d decimal.Decimal) string {
	if d.IsZero() {
		return "none"
	}
	return centsText(d)
}

func altVenues(legs []canonical.Leg) string {
	var names []string
	seen := map[canonical.Venue]bool{}
	for _, l := range legs {
		if !seen[l.Venue] {
			seen[l.Venue] = true
			names = append(names, venueName(l.Venue))
		}
	}
	return strings.Join(names, " + ")
}

// altContract names an alternative's contract(s) and whether it goes through
// the opposite contract.
func altContract(legs []canonical.Leg, dc decisionContext) (name, title string, opposite bool) {
	if len(legs) == 1 {
		l := legs[0]
		return dc.nameOf(l.InstrumentID).Long, dc.titleOf(l.InstrumentID), l.Route == "complement"
	}
	names := make([]string, 0, len(legs))
	for _, l := range legs {
		names = append(names, dc.nameOf(l.InstrumentID).Short)
		opposite = opposite || l.Route == "complement"
	}
	return strings.Join(names, " + "), "", opposite
}

// barChart is the stacked-bar SVG: one bar per priced alternative, segments
// in percent of the widest total so the chart scales with its container.
type barChart struct {
	Height int
	Rows   []barRow
}

type barRow struct {
	Label  string // "Polymarket · 0.80¢"
	Winner bool
	TextY  int
	BarY   int
	Segs   []barSeg
}

type barSeg struct {
	Class string // bar-price, bar-fee, bar-safety
	X     string // percent, "0.00"
	W     string // percent
	Name  string // for the <title>: "price 0.40¢"
}

const (
	barRowHeight = 44
	barHeight    = 16
)

// buildCostBars draws one bar per ranked alternative; nil when nothing was priced.
func buildCostBars(d canonical.Decision) *barChart {
	maxTotal := decimal.Zero
	for _, a := range d.Alternatives {
		if a.AllInPerContract.GreaterThan(maxTotal) {
			maxTotal = a.AllInPerContract
		}
	}
	if !maxTotal.IsPositive() {
		return nil
	}
	c := &barChart{}
	for i, a := range d.Alternatives {
		if len(a.Legs) == 0 {
			continue
		}
		price, fee, safety := legComponents(a.Legs)
		row := barRow{
			Label:  altVenues(a.Legs) + " · " + centsText(a.AllInPerContract) + " per contract",
			Winner: a.Rank == 1 && len(d.Chosen) > 0,
			TextY:  i*barRowHeight + 14,
			BarY:   i*barRowHeight + 20,
		}
		x := decimal.Zero
		for _, s := range []struct {
			class, name string
			v           decimal.Decimal
		}{{"bar-price", "price", price}, {"bar-fee", "fee", fee}, {"bar-safety", "safety charge", safety}} {
			if !s.v.IsPositive() {
				continue
			}
			w := s.v.Div(maxTotal).Mul(hundred)
			row.Segs = append(row.Segs, barSeg{Class: s.class, X: x.StringFixed(2), W: w.StringFixed(2), Name: s.name + " " + centsText(s.v)})
			x = x.Add(w)
		}
		c.Rows = append(c.Rows, row)
	}
	c.Height = len(c.Rows) * barRowHeight
	return c
}
