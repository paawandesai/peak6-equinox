package web

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// M4-75: spread calculation tests (property-based with rapid)

func TestSpreadCalculation_Equivalent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate two prices in [0, 1]
		midA := genPrice(t)
		midB := genPrice(t)

		now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
		pts := []store.QuotePoint{
			{InstrumentID: "a", FetchedAt: now, Mid: decimal.NullDecimal{Valid: true, Decimal: midA}},
			{InstrumentID: "b", FetchedAt: now, Mid: decimal.NullDecimal{Valid: true, Decimal: midB}},
		}

		xOf := func(_ time.Time) float64 { return 100 }
		sp := buildSpreadSeries("a", "b", canonical.RelEquivalent, pts, xOf)

		// For equivalent pairs: spread = midA - midB
		expected := midA.Sub(midB)
		if sp.N != 1 {
			t.Fatalf("expected 1 spread point, got %d", sp.N)
		}
		actual, err := decimal.NewFromString(sp.Last)
		if err != nil {
			t.Fatalf("invalid spread value: %v", err)
		}
		if !actual.Equal(expected) {
			t.Fatalf("spread mismatch: expected %s, got %s", expected.StringFixed(4), actual.StringFixed(4))
		}
		// Spread must be in [-1, 1]
		if actual.LessThan(decimal.NewFromInt(-1)) || actual.GreaterThan(decimal.NewFromInt(1)) {
			t.Fatalf("spread out of range [-1, 1]: %s", actual.String())
		}
	})
}

func TestSpreadCalculation_Complement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate two prices in [0, 1]
		midA := genPrice(t)
		midB := genPrice(t)

		now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
		pts := []store.QuotePoint{
			{InstrumentID: "a", FetchedAt: now, Mid: decimal.NullDecimal{Valid: true, Decimal: midA}},
			{InstrumentID: "b", FetchedAt: now, Mid: decimal.NullDecimal{Valid: true, Decimal: midB}},
		}

		xOf := func(_ time.Time) float64 { return 100 }
		sp := buildSpreadSeries("a", "b", canonical.RelComplement, pts, xOf)

		// For complement pairs: spread = midA + midB - 1
		expected := midA.Add(midB).Sub(one)
		if sp.N != 1 {
			t.Fatalf("expected 1 spread point, got %d", sp.N)
		}
		actual, err := decimal.NewFromString(sp.Last)
		if err != nil {
			t.Fatalf("invalid spread value: %v", err)
		}
		if !actual.Equal(expected) {
			t.Fatalf("spread mismatch: expected %s, got %s", expected.StringFixed(4), actual.StringFixed(4))
		}
		// Spread must be in [-1, 1]
		if actual.LessThan(decimal.NewFromInt(-1)) || actual.GreaterThan(decimal.NewFromInt(1)) {
			t.Fatalf("spread out of range [-1, 1]: %s", actual.String())
		}
	})
}

func TestSpreadCalculation_MissingPrice(t *testing.T) {
	// When one side has missing mid, no spread point is generated
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	pts := []store.QuotePoint{
		{InstrumentID: "a", FetchedAt: now, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.5)}},
		{InstrumentID: "b", FetchedAt: now, Mid: decimal.NullDecimal{Valid: false}},
	}

	xOf := func(_ time.Time) float64 { return 100 }
	sp := buildSpreadSeries("a", "b", canonical.RelEquivalent, pts, xOf)

	if sp.N != 0 {
		t.Errorf("expected 0 spread points when one mid is missing, got %d", sp.N)
	}
	if sp.Last != "n/a" {
		t.Errorf("expected Last to be 'n/a' when no spread points, got %s", sp.Last)
	}
}

func TestSpreadCalculation_MultipleRounds(t *testing.T) {
	// Generate multiple rounds with varying presence of mids
	t1 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(5 * time.Minute)
	t3 := t1.Add(10 * time.Minute)

	pts := []store.QuotePoint{
		// Round 1: both present
		{InstrumentID: "a", FetchedAt: t1, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.6)}},
		{InstrumentID: "b", FetchedAt: t1, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.4)}},
		// Round 2: only A present (gap)
		{InstrumentID: "a", FetchedAt: t2, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.7)}},
		{InstrumentID: "b", FetchedAt: t2, Mid: decimal.NullDecimal{Valid: false}},
		// Round 3: both present
		{InstrumentID: "a", FetchedAt: t3, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.55)}},
		{InstrumentID: "b", FetchedAt: t3, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.45)}},
	}

	xOf := func(_ time.Time) float64 { return 100 }
	sp := buildSpreadSeries("a", "b", canonical.RelEquivalent, pts, xOf)

	// Should have 2 spread points (rounds 1 and 3)
	if sp.N != 2 {
		t.Errorf("expected 2 spread points, got %d", sp.N)
	}

	// M5-02: Last spread must be the latest (round 3: 0.55 - 0.45 = 0.1), not round 1
	actual, err := decimal.NewFromString(sp.Last)
	if err != nil {
		t.Fatalf("invalid spread value: %v", err)
	}
	spread3 := decimal.NewFromFloat(0.1)
	if !actual.Equal(spread3) {
		t.Errorf("last spread should be %s (latest), got %s", spread3.StringFixed(4), actual.StringFixed(4))
	}
}

// M5-02: Spread series must sort timestamps so Last is the latest value (F4).
func TestSpreadSeries_ShuffledTimestamps(t *testing.T) {
	// Feed shuffled timestamps; assert points are strictly ascending and Last equals the newest value
	t1 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	t2 := t1.Add(5 * time.Minute)
	t3 := t1.Add(10 * time.Minute)

	// Deliberately shuffle the input order
	pts := []store.QuotePoint{
		// Round 3 first
		{InstrumentID: "a", FetchedAt: t3, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.55)}},
		{InstrumentID: "b", FetchedAt: t3, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.45)}},
		// Round 1 second
		{InstrumentID: "a", FetchedAt: t1, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.6)}},
		{InstrumentID: "b", FetchedAt: t1, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.4)}},
		// Round 2 last
		{InstrumentID: "a", FetchedAt: t2, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.7)}},
		{InstrumentID: "b", FetchedAt: t2, Mid: decimal.NullDecimal{Valid: true, Decimal: decimal.NewFromFloat(0.5)}},
	}

	xOf := func(tm time.Time) float64 {
		// Map t1 → 0, t2 → 100, t3 → 200 (increasing x for increasing time)
		if tm.Equal(t1) {
			return 0
		}
		if tm.Equal(t2) {
			return 100
		}
		return 200
	}
	sp := buildSpreadSeries("a", "b", canonical.RelEquivalent, pts, xOf)

	if sp.N != 3 {
		t.Fatalf("expected 3 spread points, got %d", sp.N)
	}

	// Parse the polyline to check x-coordinates are ascending
	points := strings.Fields(sp.Line)
	if len(points) != 3 {
		t.Fatalf("expected 3 points in polyline, got %d", len(points))
	}

	// Extract x-coordinates
	var xs []float64
	for _, pt := range points {
		parts := strings.Split(pt, ",")
		if len(parts) != 2 {
			t.Fatalf("invalid point format: %s", pt)
		}
		var x float64
		if _, err := fmt.Sscanf(parts[0], "%f", &x); err != nil {
			t.Fatalf("parsing x from %s: %v", pt, err)
		}
		xs = append(xs, x)
	}

	// Assert x-coordinates are strictly ascending (0 < 100 < 200)
	if len(xs) != 3 || xs[0] >= xs[1] || xs[1] >= xs[2] {
		t.Errorf("x-coordinates not strictly ascending: %v", xs)
	}

	// Last spread must be the latest (t3: 0.55 - 0.45 = 0.1)
	expected := decimal.NewFromFloat(0.1)
	actual, err := decimal.NewFromString(sp.Last)
	if err != nil {
		t.Fatalf("invalid spread value: %v", err)
	}
	if !actual.Equal(expected) {
		t.Errorf("last spread should be %s (latest), got %s", expected.StringFixed(4), actual.StringFixed(4))
	}
}

// M5-02: Property test that timestamps are always sorted regardless of input order.
func TestSpreadSeries_TimestampOrdering(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate 2-10 timestamps
		n := rapid.IntRange(2, 10).Draw(t, "n")
		baseTime := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

		var times []time.Time
		for i := 0; i < n; i++ {
			offset := rapid.IntRange(0, 3600).Draw(t, "offset")
			times = append(times, baseTime.Add(time.Duration(offset)*time.Second))
		}

		// Build shuffled points
		var pts []store.QuotePoint
		for _, tm := range times {
			midA := genPrice(t)
			midB := genPrice(t)
			pts = append(pts,
				store.QuotePoint{InstrumentID: "a", FetchedAt: tm, Mid: decimal.NullDecimal{Valid: true, Decimal: midA}},
				store.QuotePoint{InstrumentID: "b", FetchedAt: tm, Mid: decimal.NullDecimal{Valid: true, Decimal: midB}},
			)
		}

		xOf := func(tm time.Time) float64 {
			// Map time to x in ascending order
			return float64(tm.Unix())
		}
		sp := buildSpreadSeries("a", "b", canonical.RelEquivalent, pts, xOf)

		if sp.N == 0 {
			return // No spread points, nothing to check
		}

		// Parse polyline and assert x-coordinates are non-decreasing (strictly ascending if all times are unique)
		points := strings.Fields(sp.Line)
		if len(points) != sp.N {
			t.Fatalf("polyline point count mismatch: expected %d, got %d", sp.N, len(points))
		}

		var xs []float64
		for _, pt := range points {
			parts := strings.Split(pt, ",")
			if len(parts) != 2 {
				t.Fatalf("invalid point format: %s", pt)
			}
			var x float64
			if _, err := fmt.Sscanf(parts[0], "%f", &x); err != nil {
				t.Fatalf("parsing x from %s: %v", pt, err)
			}
			xs = append(xs, x)
		}

		// Assert non-decreasing (ascending if all times are unique)
		for i := 1; i < len(xs); i++ {
			if xs[i] < xs[i-1] {
				t.Fatalf("x-coordinates not ascending: %v", xs)
			}
		}

		// Last spread value must come from the latest timestamp
		// (we can't easily verify without re-computing, but we check it's valid)
		if _, err := decimal.NewFromString(sp.Last); err != nil {
			t.Fatalf("invalid Last spread value: %v", err)
		}
	})
}

func TestYOfSpread_Range(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate spread in [-1, 1]
		f := rapid.Float64Range(-1, 1).Draw(t, "spread")
		spread := decimal.NewFromFloat(f)

		y := yOfSpread(spread)

		// Y should be in chart bounds
		if y < float64(chartTop) || y > float64(chartHeight-chartBottom) {
			t.Fatalf("yOfSpread(%s) = %f out of chart bounds [%d, %d]", spread.String(), y, chartTop, chartHeight-chartBottom)
		}

		// Y at 0 should be at the centre
		if spread.IsZero() {
			expectedY := float64(chartTop) + float64(chartHeight-chartTop-chartBottom)*0.5
			if y != expectedY {
				t.Fatalf("yOfSpread(0) should be at centre: expected %f, got %f", expectedY, y)
			}
		}

		// Positive spread (top half) - use epsilon for floating point comparison
		epsilon := decimal.NewFromFloat(1e-10)
		if spread.GreaterThan(epsilon) {
			midY := float64(chartTop) + float64(chartHeight-chartTop-chartBottom)*0.5
			if y >= midY {
				t.Fatalf("yOfSpread(%s) = %f should be above centre %f", spread.String(), y, midY)
			}
		}

		// Negative spread (bottom half) - use epsilon for floating point comparison
		if spread.LessThan(epsilon.Neg()) {
			midY := float64(chartTop) + float64(chartHeight-chartTop-chartBottom)*0.5
			if y <= midY {
				t.Fatalf("yOfSpread(%s) = %f should be below centre %f", spread.String(), y, midY)
			}
		}
	})
}

func TestYOfSpread_Clamping(t *testing.T) {
	// Out-of-range values should be clamped
	cases := []struct {
		input    decimal.Decimal
		expected float64
	}{
		{decimal.NewFromFloat(-2), float64(chartHeight - chartBottom)}, // clamped to -1
		{decimal.NewFromFloat(2), float64(chartTop)},                   // clamped to 1
		{decimal.NewFromInt(-1), float64(chartHeight - chartBottom)},
		{decimal.NewFromInt(1), float64(chartTop)},
		{decimal.Zero, float64(chartTop) + float64(chartHeight-chartTop-chartBottom)*0.5},
	}

	for _, tc := range cases {
		y := yOfSpread(tc.input)
		if y != tc.expected {
			t.Errorf("yOfSpread(%s): expected %f, got %f", tc.input.String(), tc.expected, y)
		}
	}
}

// genPrice generates a decimal price in [0, 1] with up to 4 decimal places
func genPrice(t *rapid.T) decimal.Decimal {
	// Generate integer in [0, 10000] and divide by 10000 to get 4 decimal places
	n := rapid.Int64Range(0, 10000).Draw(t, "price")
	return decimal.NewFromInt(n).Div(decimal.NewFromInt(10000))
}
