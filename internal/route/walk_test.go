package route

import (
	"slices"
	"sort"
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func ptr(s string) *decimal.Decimal {
	v := d(s)
	return &v
}

func lv(price, size string) canonical.Level { return canonical.Level{Price: d(price), Size: d(size)} }

func sameLevels(a, b []canonical.Level) bool {
	return slices.EqualFunc(a, b, func(x, y canonical.Level) bool {
		return x.Price.Equal(y.Price) && x.Size.Equal(y.Size)
	})
}

// TestBookSide: a YES buy hits the asks, a YES sell the bids, and NO orders
// hit the complementary YES side re-priced as 1 − p, still best first.
func TestBookSide(t *testing.T) {
	q := canonical.Quote{
		YesBids: []canonical.Level{lv("0.48", "10"), lv("0.45", "20")},
		YesAsks: []canonical.Level{lv("0.52", "30"), lv("0.55", "40")},
	}
	cases := []struct {
		side    canonical.Side
		outcome canonical.Outcome
		want    []canonical.Level
	}{
		{canonical.SideBuy, canonical.OutcomeYes, q.YesAsks},
		{canonical.SideSell, canonical.OutcomeYes, q.YesBids},
		{canonical.SideBuy, canonical.OutcomeNo, []canonical.Level{lv("0.52", "10"), lv("0.55", "20")}},
		{canonical.SideSell, canonical.OutcomeNo, []canonical.Level{lv("0.48", "30"), lv("0.45", "40")}},
	}
	for _, tc := range cases {
		if got := bookSide(q, tc.side, tc.outcome); !sameLevels(got, tc.want) {
			t.Errorf("bookSide(%s %s) = %v, want %v", tc.side, tc.outcome, got, tc.want)
		}
	}
}

func TestBookSideSkipsInvalidLevels(t *testing.T) {
	q := canonical.Quote{YesAsks: []canonical.Level{lv("1.5", "10"), lv("0.52", "0"), lv("0.55", "40")}}
	if got := bookSide(q, canonical.SideBuy, canonical.OutcomeYes); !sameLevels(got, []canonical.Level{lv("0.55", "40")}) {
		t.Errorf("bookSide kept invalid levels: %v", got)
	}
}

func TestWalkFillsBestFirst(t *testing.T) {
	levels := []canonical.Level{lv("0.50", "50"), lv("0.53", "100"), lv("0.60", "10")}
	f := walk(levels, canonical.SideBuy, d("100"), nil)
	if !f.Quantity.Equal(d("100")) || len(f.Consumed) != 2 {
		t.Fatalf("fill = %+v", f)
	}
	if !f.VWAP().Equal(d("0.515")) {
		t.Errorf("VWAP = %s, want 0.515", f.VWAP())
	}
	if best, _ := f.Best(); !best.Equal(d("0.50")) {
		t.Errorf("best = %s", best)
	}
	if worst, _ := f.Worst(); !worst.Equal(d("0.53")) {
		t.Errorf("worst = %s", worst)
	}
	if !f.Depth.Equal(d("160")) || !f.DepthWithinLimit.Equal(d("160")) {
		t.Errorf("depth = %s within %s", f.Depth, f.DepthWithinLimit)
	}
}

func TestWalkRespectsLimit(t *testing.T) {
	levels := []canonical.Level{lv("0.50", "50"), lv("0.53", "100")}
	f := walk(levels, canonical.SideBuy, d("100"), ptr("0.51"))
	if !f.Quantity.Equal(d("50")) || !f.DepthWithinLimit.Equal(d("50")) || !f.Depth.Equal(d("150")) {
		t.Errorf("buy limit fill = %+v", f)
	}
	bids := []canonical.Level{lv("0.48", "10"), lv("0.45", "20")}
	f = walk(bids, canonical.SideSell, d("100"), ptr("0.46"))
	if !f.Quantity.Equal(d("10")) {
		t.Errorf("sell limit fill = %+v", f)
	}
	f = walk(bids, canonical.SideSell, d("100"), ptr("0.49"))
	if !f.Quantity.IsZero() || len(f.Consumed) != 0 {
		t.Errorf("unreachable sell limit fill = %+v", f)
	}
}

func TestWalkEmptyAndZero(t *testing.T) {
	f := walk(nil, canonical.SideBuy, d("10"), nil)
	if !f.Quantity.IsZero() || !f.VWAP().IsZero() {
		t.Errorf("empty walk = %+v", f)
	}
	if _, ok := f.Best(); ok {
		t.Error("Best reported a price on an empty fill")
	}
	if _, ok := f.Worst(); ok {
		t.Error("Worst reported a price on an empty fill")
	}
	f = walk([]canonical.Level{lv("0.5", "10"), lv("0.6", "0")}, canonical.SideBuy, d("0"), nil)
	if !f.Quantity.IsZero() || !f.Depth.Equal(d("10")) {
		t.Errorf("zero-quantity walk = %+v", f)
	}
}

func TestTouch(t *testing.T) {
	q := canonical.Quote{YesBids: []canonical.Level{lv("0.48", "10")}}
	if p, ok := touch(q, canonical.SideBuy, canonical.OutcomeNo); !ok || !p.Equal(d("0.52")) {
		t.Errorf("NO touch = %s %v", p, ok)
	}
	if _, ok := touch(q, canonical.SideBuy, canonical.OutcomeYes); ok {
		t.Error("touch found asks on a bid-only book")
	}
}

// genAsks draws a valid ask side: distinct prices in (0,1) sorted lowest
// first, with positive sizes of up to two decimal places.
func genAsks() *rapid.Generator[[]canonical.Level] {
	return rapid.Custom(func(t *rapid.T) []canonical.Level {
		n := rapid.IntRange(1, 8).Draw(t, "levels")
		prices := rapid.SliceOfNDistinct(rapid.Int64Range(1, 9999), n, n, rapid.ID[int64]).Draw(t, "prices")
		sort.Slice(prices, func(i, j int) bool { return prices[i] < prices[j] })
		out := make([]canonical.Level, n)
		for i, p := range prices {
			out[i] = canonical.Level{Price: decimal.New(p, -4), Size: decimal.New(rapid.Int64Range(1, 100000).Draw(t, "size"), -2)}
		}
		return out
	})
}

func genQty() *rapid.Generator[decimal.Decimal] {
	return rapid.Custom(func(t *rapid.T) decimal.Decimal {
		return decimal.New(rapid.Int64Range(1, 200000).Draw(t, "qty"), -2)
	})
}

func sumSize(levels []canonical.Level) decimal.Decimal {
	total := decimal.Zero
	for _, l := range levels {
		total = total.Add(l.Size)
	}
	return total
}

// TestWalkProperties: for any book and size, the VWAP lies between the best
// and worst consumed price, the filled quantity never exceeds the book depth
// or the order, and consumed prices respect the limit.
func TestWalkProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		asks := genAsks().Draw(t, "asks")
		qty := genQty().Draw(t, "qty")
		var limit *decimal.Decimal
		if rapid.Bool().Draw(t, "limited") {
			l := decimal.New(rapid.Int64Range(0, 10000).Draw(t, "limit"), -4)
			limit = &l
		}
		f := walk(asks, canonical.SideBuy, qty, limit)
		if f.Quantity.GreaterThan(qty) {
			t.Fatalf("filled %s > order %s", f.Quantity, qty)
		}
		if f.Quantity.GreaterThan(f.Depth) || !f.Depth.Equal(sumSize(asks)) {
			t.Fatalf("filled %s with depth %s (book %s)", f.Quantity, f.Depth, sumSize(asks))
		}
		if f.Quantity.GreaterThan(f.DepthWithinLimit) {
			t.Fatalf("filled %s > depth within limit %s", f.Quantity, f.DepthWithinLimit)
		}
		if !f.Quantity.Equal(sumSize(f.Consumed)) {
			t.Fatalf("quantity %s != consumed %s", f.Quantity, sumSize(f.Consumed))
		}
		if len(f.Consumed) == 0 {
			if limit == nil || !asks[0].Price.GreaterThan(*limit) {
				t.Fatalf("nothing consumed from a reachable book: %+v", f)
			}
			return
		}
		best, _ := f.Best()
		worst, _ := f.Worst()
		if v := f.VWAP(); v.LessThan(best) || v.GreaterThan(worst) {
			t.Fatalf("VWAP %s outside [%s, %s]", v, best, worst)
		}
		for _, l := range f.Consumed {
			if limit != nil && l.Price.GreaterThan(*limit) {
				t.Fatalf("consumed %s above limit %s", l.Price, *limit)
			}
		}
		// Expected fill: everything within the limit, capped at qty.
		want := decimal.Min(qty, f.DepthWithinLimit)
		if !f.Quantity.Equal(want) {
			t.Fatalf("filled %s, want %s", f.Quantity, want)
		}
	})
}

// TestWalkCostMonotonic: the cost of a fill (and its VWAP, for a buy) never
// decreases as the order gets bigger.
func TestWalkCostMonotonic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		asks := genAsks().Draw(t, "asks")
		small := genQty().Draw(t, "small")
		big := small.Add(genQty().Draw(t, "extra"))
		fs := walk(asks, canonical.SideBuy, small, nil)
		fb := walk(asks, canonical.SideBuy, big, nil)
		if fb.Notional.LessThan(fs.Notional) {
			t.Fatalf("cost(%s) = %s > cost(%s) = %s", small, fs.Notional, big, fb.Notional)
		}
		if fb.Quantity.LessThan(fs.Quantity) {
			t.Fatalf("fill(%s) = %s > fill(%s) = %s", small, fs.Quantity, big, fb.Quantity)
		}
		if fb.VWAP().LessThan(fs.VWAP()) {
			t.Fatalf("VWAP(%s) = %s > VWAP(%s) = %s", small, fs.VWAP(), big, fb.VWAP())
		}
	})
}
