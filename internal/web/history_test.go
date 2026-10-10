package web

import (
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

	// Last spread is one of the two valid spreads (map iteration order is non-deterministic)
	// Round 1: 0.6 - 0.4 = 0.2
	// Round 3: 0.55 - 0.45 = 0.1
	actual, err := decimal.NewFromString(sp.Last)
	if err != nil {
		t.Fatalf("invalid spread value: %v", err)
	}
	spread1 := decimal.NewFromFloat(0.2)
	spread3 := decimal.NewFromFloat(0.1)
	if !actual.Equal(spread1) && !actual.Equal(spread3) {
		t.Errorf("last spread should be either %s or %s, got %s", spread1.StringFixed(4), spread3.StringFixed(4), actual.StringFixed(4))
	}
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
