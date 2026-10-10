package web

import (
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

var one = decimal.NewFromInt(1)

// The price-history chart (FR-25, US-07) is server-rendered SVG built from
// stored quotes across fetch rounds, so it needs no script and no new CDN
// asset: the CSP stays as M1-09 left it. Coordinates are pixels, computed
// in float64 for layout only; prices stay decimal until they are placed.
const (
	chartWidth   = 720
	chartHeight  = 260
	chartLeft    = 44 // room for the y-axis labels
	chartRight   = 12
	chartTop     = 12
	chartBottom  = 32 // room for the x-axis labels
	maxHistoryPt = 600
)

// historyChart is the data for the "history" template.
type historyChart struct {
	Width, Height int
	// Plot area, for the frame and grid.
	X0, Y0, X1, Y1 string
	PlotW, PlotH   string
	Series         []historySeries
	Spread         historySpread // M4-75: cross-venue spread series
	YTicks         []chartTick
	XTicks         []chartTick
	Points         int
	Rounds         int
	From, To       time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
	Empty          bool
	Rows           []historyRow
	Tz             string // Timezone for formatTime in templates (M3-08 Cursor round 2)
}

// historySeries is one instrument's line. Mid is the polyline of mids;
// Bid and Ask bound it. A missing side leaves a gap in that polyline.
type historySeries struct {
	Key   string // "a" or "b", for the CSS class
	ID    string
	Venue string
	Mid   string
	Bid   string
	Ask   string
	Dots  []chartPt // one marker per mid, so a single round is still visible
	N     int
	Last  string // last mid, for the legend
}

// historySpread is the cross-venue spread series (M4-75: mid A - mid B,
// polarity-aware for complements). Missing price on either venue produces a gap.
type historySpread struct {
	Line string // polyline of spread values
	Dots []chartPt
	N    int
	Last string // last spread value, for the legend
}

type chartPt struct{ X, Y string }

type chartTick struct {
	X, Y   string // position in pixels
	Label  string
	Anchor string // SVG text-anchor
}

// historyRow is one point of the data table under the chart.
type historyRow struct {
	At      time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
	RoundID int64
	Venue   string
	Bid     string
	Ask     string
	Mid     string
}

// buildHistoryChart lays out points for the pair (aID first, bID second).
// Prices are on a fixed [0,1] axis, the domain every price lives in, so two
// venues are always on the same scale. The spread (M4-75) is mid A − mid B for
// most types, or mid A − (1 − mid B) for complements, on a [−1,1] axis with 0
// at the centre. Missing price on either venue leaves a gap. Time runs from
// the first to the last stored fetch; a single round is centred.
func buildHistoryChart(aID, bID string, relType canonical.RelationshipType, pts []store.QuotePoint, tz string) historyChart {
	c := historyChart{Width: chartWidth, Height: chartHeight,
		X0: px(chartLeft), Y0: px(chartTop), X1: px(chartWidth - chartRight), Y1: px(chartHeight - chartBottom),
		PlotW: px(chartWidth - chartLeft - chartRight), PlotH: px(chartHeight - chartTop - chartBottom), Tz: tz}
	if len(pts) == 0 {
		c.Empty = true
		return c
	}
	first, last := pts[0].FetchedAt, pts[0].FetchedAt
	rounds := map[int64]bool{}
	for _, p := range pts {
		if p.FetchedAt.Before(first) {
			first = p.FetchedAt
		}
		if p.FetchedAt.After(last) {
			last = p.FetchedAt
		}
		rounds[p.RoundID] = true
	}
	c.Points, c.Rounds, c.From, c.To = len(pts), len(rounds), first, last
	span := last.Sub(first)
	xOf := func(t time.Time) float64 {
		w := float64(chartWidth - chartLeft - chartRight)
		if span <= 0 {
			return chartLeft + w/2
		}
		return chartLeft + w*float64(t.Sub(first))/float64(span)
	}
	for i, id := range []string{aID, bID} {
		sr := historySeries{Key: string(rune('a' + i)), ID: id, Venue: venueOf(id), Last: "n/a"}
		var mid, bid, ask []byte
		var lastMid *chartPt
		for _, p := range pts {
			if p.InstrumentID != id {
				continue
			}
			sr.N++
			x := xOf(p.FetchedAt)
			mid = appendPoint(mid, x, p.Mid)
			bid = appendPoint(bid, x, p.BestBid)
			ask = appendPoint(ask, x, p.BestAsk)
			if p.Mid.Valid {
				sr.Last = p.Mid.Decimal.StringFixed(4)
				// M5-02: only mark the last point per series
				pt := chartPt{X: fpx(x), Y: fpx(yOf(p.Mid.Decimal))}
				lastMid = &pt
			}
			c.Rows = append(c.Rows, historyRow{At: p.FetchedAt, RoundID: p.RoundID, Venue: sr.Venue,
				Bid: nullPrice(p.BestBid), Ask: nullPrice(p.BestAsk), Mid: nullPrice(p.Mid)})
		}
		if lastMid != nil {
			sr.Dots = []chartPt{*lastMid}
		}
		sr.Mid, sr.Bid, sr.Ask = string(mid), string(bid), string(ask)
		c.Series = append(c.Series, sr)
	}
	// M4-75: spread series (mid A − mid B, or mid A − (1 − mid B) for complements)
	c.Spread = buildSpreadSeries(aID, bID, relType, pts, xOf)
	for _, v := range []string{"0", "0.25", "0.5", "0.75", "1"} {
		d := decimal.RequireFromString(v)
		c.YTicks = append(c.YTicks, chartTick{X: px(chartLeft - 6), Y: fpx(yOf(d)), Label: d.StringFixed(2), Anchor: "end"})
	}
	const stamp = "01-02 15:04 UTC"
	if span > 0 {
		c.XTicks = []chartTick{
			{X: fpx(xOf(first)), Y: px(chartHeight - 10), Label: first.UTC().Format(stamp), Anchor: "start"},
			{X: fpx(xOf(last)), Y: px(chartHeight - 10), Label: last.UTC().Format(stamp), Anchor: "end"},
		}
	} else {
		c.XTicks = []chartTick{{X: fpx(xOf(first)), Y: px(chartHeight - 10), Label: first.UTC().Format(stamp), Anchor: "middle"}}
	}
	return c
}

// buildSpreadSeries calculates the cross-venue spread for each point where both
// mids are present. For complements, spread = mid A − (1 − mid B); otherwise
// spread = mid A − mid B. Missing mid on either venue produces a gap (M4-75).
// M5-02: timestamps are sorted before building points so Last is the latest value.
func buildSpreadSeries(aID, bID string, relType canonical.RelationshipType, pts []store.QuotePoint, xOf func(time.Time) float64) historySpread {
	sp := historySpread{Last: "n/a"}
	// Group points by fetch time and instrument
	type key struct {
		t  time.Time
		id string
	}
	mids := make(map[key]decimal.Decimal)
	timesMap := make(map[time.Time]bool)
	for _, p := range pts {
		if !p.Mid.Valid {
			continue
		}
		k := key{t: p.FetchedAt, id: p.InstrumentID}
		mids[k] = p.Mid.Decimal
		timesMap[p.FetchedAt] = true
	}
	// Sort timestamps so Last is the latest value (M5-02: fixes F4)
	var times []time.Time
	for t := range timesMap {
		times = append(times, t)
	}
	sort.Slice(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})
	// Calculate spread for each time where both mids exist
	var line []byte
	var lastDot *chartPt
	isComplement := relType == canonical.RelComplement
	for _, t := range times {
		midA, hasA := mids[key{t: t, id: aID}]
		midB, hasB := mids[key{t: t, id: bID}]
		if !hasA || !hasB {
			continue
		}
		var spread decimal.Decimal
		if isComplement {
			spread = midA.Add(midB).Sub(one)
		} else {
			spread = midA.Sub(midB)
		}
		sp.N++
		x := xOf(t)
		if len(line) > 0 {
			line = append(line, ' ')
		}
		line = strconv.AppendFloat(line, x, 'f', 1, 64)
		line = append(line, ',')
		line = strconv.AppendFloat(line, yOfSpread(spread), 'f', 1, 64)
		// M5-02: only mark the last point
		pt := chartPt{X: fpx(x), Y: fpx(yOfSpread(spread))}
		lastDot = &pt
		sp.Last = spread.StringFixed(4)
	}
	if lastDot != nil {
		sp.Dots = []chartPt{*lastDot}
	}
	sp.Line = string(line)
	return sp
}

// yOf maps a price in [0,1] to a pixel row (1 at the top). Out-of-range
// values, which the store rejects anyway, are clamped.
func yOf(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	h := float64(chartHeight - chartTop - chartBottom)
	return chartTop + h*(1-f)
}

// yOfSpread maps a spread value in [−1,1] to a pixel row (1 at the top, −1 at
// the bottom, 0 at the centre). Out-of-range values are clamped.
func yOfSpread(d decimal.Decimal) float64 {
	f, _ := d.Float64()
	if f < -1 {
		f = -1
	}
	if f > 1 {
		f = 1
	}
	h := float64(chartHeight - chartTop - chartBottom)
	return chartTop + h*0.5*(1-f)
}

// appendPoint adds "x,y " to an SVG points list when the price is present.
func appendPoint(b []byte, x float64, d decimal.NullDecimal) []byte {
	if !d.Valid {
		return b
	}
	if len(b) > 0 {
		b = append(b, ' ')
	}
	b = strconv.AppendFloat(b, x, 'f', 1, 64)
	b = append(b, ',')
	return strconv.AppendFloat(b, yOf(d.Decimal), 'f', 1, 64)
}

func px(n int) string          { return strconv.Itoa(n) }
func fpx(f float64) string     { return strconv.FormatFloat(f, 'f', 1, 64) }
func venueOf(id string) string { return string(canonical.VenueOf(id)) }
