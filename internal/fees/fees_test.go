package fees

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func kalshi(multiplier string, extra ...string) canonical.FeeSchedule {
	s := canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{ParamMultiplier: d(multiplier)}}
	for i := 0; i+1 < len(extra); i += 2 {
		s.Params[extra[i]] = d(extra[i+1])
	}
	return s
}

func polymarket(rate string, extra ...string) canonical.FeeSchedule {
	s := canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1, Params: map[string]decimal.Decimal{ParamRate: d(rate)}}
	for i := 0; i+1 < len(extra); i += 2 {
		s.Params[extra[i]] = d(extra[i+1])
	}
	return s
}

func mustCompute(t *testing.T, s canonical.FeeSchedule, price, qty string) decimal.Decimal {
	t.Helper()
	fee, err := Compute(s, canonical.SideBuy, d(price), d(qty))
	if err != nil {
		t.Fatalf("Compute(%s, %s, %s): %v", s.ID, price, qty, err)
	}
	return fee
}

// TestKalshiWorkedExamples: round up to the cent of 0.07 × C × P × (1 − P)
// (domain.md § Fees). 100 contracts at 50¢ is exactly $1.75; one contract at
// 50¢ is 1.75¢, which rounds up to 2¢.
func TestKalshiWorkedExamples(t *testing.T) {
	cases := []struct{ price, qty, want string }{
		{"0.50", "100", "1.75"},
		{"0.50", "1", "0.02"},
		{"0.10", "100", "0.63"},
		{"0.90", "100", "0.63"},
		{"0.33", "7", "0.11"},   // 0.108339 → 0.11
		{"0.99", "100", "0.07"}, // 0.0693 → 0.07
		{"0", "100", "0"},
		{"1", "100", "0"},
		{"0.50", "0", "0"},
	}
	for _, tc := range cases {
		if got := mustCompute(t, kalshi("0.07"), tc.price, tc.qty); !got.Equal(d(tc.want)) {
			t.Errorf("kalshi fee(%s × %s) = %s, want %s", tc.qty, tc.price, got, tc.want)
		}
	}
}

// TestKalshiParams: the series fee_multiplier scales the coefficient (0 means
// fee-free), and round_to changes the rounding increment.
func TestKalshiParams(t *testing.T) {
	if got := mustCompute(t, kalshi("0"), "0.50", "100"); !got.IsZero() {
		t.Errorf("multiplier 0: fee = %s, want 0", got)
	}
	// 0.035 × 100 × 0.25 = 0.875 → ceil to the cent = 0.88
	if got := mustCompute(t, kalshi("0.035"), "0.50", "100"); !got.Equal(d("0.88")) {
		t.Errorf("multiplier 0.035: fee = %s, want 0.88", got)
	}
	if got := mustCompute(t, kalshi("0.07", ParamRoundTo, "0.0001"), "0.50", "1"); !got.Equal(d("0.0175")) {
		t.Errorf("round_to 0.0001: fee = %s, want 0.0175", got)
	}
}

// TestPolymarketWorkedExamples checks the Crypto column of the official fee
// table (100 shares, rate 0.07; docs.polymarket.com/trading/fees, read
// 2026-10-06). The table shows cents; the exact values keep 5 places.
func TestPolymarketWorkedExamples(t *testing.T) {
	cases := []struct{ price, want, shown string }{
		{"0.01", "0.0693", "0.07"},
		{"0.05", "0.3325", "0.33"},
		{"0.10", "0.63", "0.63"},
		{"0.45", "1.7325", "1.73"},
		{"0.50", "1.75", "1.75"},
		{"0.55", "1.7325", "1.73"},
		{"0.95", "0.3325", "0.33"},
		{"0.99", "0.0693", "0.07"},
	}
	for _, tc := range cases {
		got := mustCompute(t, polymarket("0.07"), tc.price, "100")
		if !got.Equal(d(tc.want)) {
			t.Errorf("polymarket fee(100 × %s) = %s, want %s", tc.price, got, tc.want)
		}
		if shown := got.StringFixed(2); shown != tc.shown {
			t.Errorf("polymarket fee(100 × %s) shows as %s, table says %s", tc.price, shown, tc.shown)
		}
	}
}

// TestPolymarketPrecision: fees keep 5 decimal places and anything smaller
// than 0.00001 rounds to zero.
func TestPolymarketPrecision(t *testing.T) {
	if got := mustCompute(t, polymarket("0.07"), "0.001", "1"); !got.Equal(d("0.00007")) {
		t.Errorf("fee = %s, want 0.00007", got) // 0.00006993
	}
	if got := mustCompute(t, polymarket("0.07"), "0.001", "0.01"); !got.IsZero() {
		t.Errorf("fee = %s, want 0", got) // 0.0000006993
	}
	if got := mustCompute(t, polymarket("0"), "0.50", "100"); !got.IsZero() {
		t.Errorf("rate 0: fee = %s, want 0", got)
	}
}

// TestPolymarketExponent: the exponent applies to the p × (1 − p) term.
func TestPolymarketExponent(t *testing.T) {
	if got := mustCompute(t, polymarket("0.07", ParamExponent, "1"), "0.50", "100"); !got.Equal(d("1.75")) {
		t.Errorf("exponent 1: fee = %s, want 1.75", got)
	}
	if got := mustCompute(t, polymarket("0.25", ParamExponent, "2"), "0.50", "100"); !got.Equal(d("1.5625")) {
		t.Errorf("exponent 2: fee = %s, want 1.5625", got) // 100 × 0.25 × 0.0625
	}
	if got := mustCompute(t, polymarket("0.07", ParamExponent, "0"), "0.50", "100"); !got.Equal(d("7")) {
		t.Errorf("exponent 0: fee = %s, want 7", got)
	}
}

// TestKalshiEdgeCases: M3-05 worked examples for edge cases
// (0-fee markets, rounding up, symmetric prices), verified against
// "Fee Schedule for July 2026, 7.7.26 Update" (Q-8).
func TestKalshiEdgeCases(t *testing.T) {
	// 0-fee market: multiplier 0 means fee-free series
	if got := mustCompute(t, kalshi("0"), "0.50", "100"); !got.IsZero() {
		t.Errorf("0-fee market: fee = %s, want 0", got)
	}
	// Rounding up: 0.07 × 1 × 0.50 × 0.50 = 0.0175 (1.75¢) → rounds to next cent = 0.02
	if got := mustCompute(t, kalshi("0.07"), "0.50", "1"); !got.Equal(d("0.02")) {
		t.Errorf("round up to cent: fee(1 × 0.50) = %s, want 0.02", got)
	}
	// Rounding up from exactly $0.011: 0.07 × 1 × 0.20 × 0.80 = 0.0112 → rounds to 0.02
	if got := mustCompute(t, kalshi("0.07"), "0.20", "1"); !got.Equal(d("0.02")) {
		t.Errorf("round up 0.0112: fee(1 × 0.20) = %s, want 0.02", got)
	}
	// Symmetric prices: fee at P and (1 − P) should match
	feeAt10 := mustCompute(t, kalshi("0.07"), "0.10", "100")
	feeAt90 := mustCompute(t, kalshi("0.07"), "0.90", "100")
	if !feeAt10.Equal(feeAt90) {
		t.Errorf("symmetric: fee(100 × 0.10) = %s != fee(100 × 0.90) = %s", feeAt10, feeAt90)
	}
	if !feeAt10.Equal(d("0.63")) {
		t.Errorf("symmetric prices: fee = %s, want 0.63", feeAt10)
	}
}

// TestKalshiOfficialTableExamples: Q-8 worked examples from the official
// "General Trading Fees Table" in the July 2026 fee schedule.
func TestKalshiOfficialTableExamples(t *testing.T) {
	cases := []struct{ price, qty, want string }{
		{"0.05", "100", "0.34"}, // Table: 100 @ $0.05 → $0.34
		{"0.50", "100", "1.75"}, // Table: 100 @ $0.50 → $1.75
		{"0.65", "100", "1.60"}, // Table: 100 @ $0.65 → $1.60
	}
	for _, tc := range cases {
		if got := mustCompute(t, kalshi("0.07"), tc.price, tc.qty); !got.Equal(d(tc.want)) {
			t.Errorf("official table: fee(%s × %s) = %s, want %s", tc.qty, tc.price, got, tc.want)
		}
	}
}

// TestPolymarketAdditionalWorkedExamples: M3-05 additional worked examples
// beyond the official 100 shares at 50¢ = $1.75.
func TestPolymarketAdditionalWorkedExamples(t *testing.T) {
	// Exponent ≠ 1: quadratic fee curve
	// 100 × 0.25 × (0.50 × 0.50)^2 = 100 × 0.25 × 0.0625 = 1.5625
	if got := mustCompute(t, polymarket("0.25", ParamExponent, "2"), "0.50", "100"); !got.Equal(d("1.5625")) {
		t.Errorf("exponent 2: fee = %s, want 1.5625", got)
	}
	// Fee-free market: rate 0
	if got := mustCompute(t, polymarket("0"), "0.50", "100"); !got.IsZero() {
		t.Errorf("fee-free market: fee = %s, want 0", got)
	}
	// Smallest non-zero fee: 0.00001 USDC
	// 1 contract × 0.07 × 0.001 × 0.999 ≈ 0.00006993 → rounds to 0.00007
	if got := mustCompute(t, polymarket("0.07"), "0.001", "1"); !got.Equal(d("0.00007")) {
		t.Errorf("smallest non-zero fee: fee = %s, want 0.00007", got)
	}
}

func TestComputeSellMatchesBuy(t *testing.T) {
	for _, s := range []canonical.FeeSchedule{kalshi("0.07"), polymarket("0.07")} {
		buy, err := Compute(s, canonical.SideBuy, d("0.37"), d("250"))
		if err != nil {
			t.Fatal(err)
		}
		sell, err := Compute(s, canonical.SideSell, d("0.37"), d("250"))
		if err != nil {
			t.Fatal(err)
		}
		if !buy.Equal(sell) {
			t.Errorf("%s: buy fee %s != sell fee %s", s.ID, buy, sell)
		}
	}
}

func TestComputeErrors(t *testing.T) {
	cases := []struct {
		name  string
		s     canonical.FeeSchedule
		side  canonical.Side
		price string
		qty   string
		want  error
	}{
		{"unknown schedule", canonical.FeeSchedule{ID: "nope_v9"}, canonical.SideBuy, "0.5", "1", ErrUnknownSchedule},
		{"bad id", canonical.FeeSchedule{ID: "Bad ID"}, canonical.SideBuy, "0.5", "1", ErrInvalidParam},
		{"kalshi missing multiplier", canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1}, canonical.SideBuy, "0.5", "1", ErrMissingParam},
		{"kalshi negative multiplier", kalshi("-0.07"), canonical.SideBuy, "0.5", "1", ErrInvalidParam},
		{"kalshi zero round_to", kalshi("0.07", ParamRoundTo, "0"), canonical.SideBuy, "0.5", "1", ErrInvalidParam},
		{"polymarket missing rate", canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1}, canonical.SideBuy, "0.5", "1", ErrMissingParam},
		{"polymarket fractional exponent", polymarket("0.07", ParamExponent, "1.5"), canonical.SideBuy, "0.5", "1", ErrInvalidParam},
		{"polymarket negative exponent", polymarket("0.07", ParamExponent, "-1"), canonical.SideBuy, "0.5", "1", ErrInvalidParam},
		{"price above 1", kalshi("0.07"), canonical.SideBuy, "1.01", "1", ErrInvalidInput},
		{"negative price", kalshi("0.07"), canonical.SideBuy, "-0.5", "1", ErrInvalidInput},
		{"negative qty", kalshi("0.07"), canonical.SideBuy, "0.5", "-1", ErrInvalidInput},
		{"bad side", kalshi("0.07"), canonical.Side("hold"), "0.5", "1", ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compute(tc.s, tc.side, d(tc.price), d(tc.qty))
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestKnown(t *testing.T) {
	if !Known(canonical.FeeScheduleKalshiTakerV1) || !Known(canonical.FeeSchedulePolymarketV1) || Known("other") {
		t.Error("Known reports the wrong schedules")
	}
}

// genPrice draws a price in [0,1] with up to 4 decimal places (Kalshi's
// centi-cent grid is the finest either venue uses).
func genPrice() *rapid.Generator[decimal.Decimal] {
	return rapid.Custom(func(t *rapid.T) decimal.Decimal {
		return decimal.New(rapid.Int64Range(0, 10000).Draw(t, "price_bp"), -4)
	})
}

// genQty draws a non-negative size with up to 2 decimal places.
func genQty() *rapid.Generator[decimal.Decimal] {
	return rapid.Custom(func(t *rapid.T) decimal.Decimal {
		return decimal.New(rapid.Int64Range(0, 1_000_000_00).Draw(t, "qty_hundredths"), -2)
	})
}

func genSchedule() *rapid.Generator[canonical.FeeSchedule] {
	return rapid.Custom(func(t *rapid.T) canonical.FeeSchedule {
		coeff := decimal.New(rapid.Int64Range(0, 2000).Draw(t, "coeff_bp"), -4) // 0 … 0.2
		if rapid.Bool().Draw(t, "kalshi") {
			return canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{ParamMultiplier: coeff}}
		}
		return canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1, Params: map[string]decimal.Decimal{ParamRate: coeff}}
	})
}

// TestFeeProperties: for both schedules and any valid inputs, the fee is
// never negative, never more than the fee on a larger order (monotonic
// non-decreasing in size), symmetric in P ↔ 1 − P, and bounded above by the
// unrounded formula plus one rounding step.
func TestFeeProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := genSchedule().Draw(t, "schedule")
		p := genPrice().Draw(t, "price")
		q := genQty().Draw(t, "qty")
		fee, err := Compute(s, canonical.SideBuy, p, q)
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		if fee.IsNegative() {
			t.Fatalf("fee %s < 0", fee)
		}
		more := q.Add(decimal.New(rapid.Int64Range(0, 1_000_00).Draw(t, "extra_hundredths"), -2))
		feeMore, err := Compute(s, canonical.SideBuy, p, more)
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		if feeMore.LessThan(fee) {
			t.Fatalf("fee(%s) = %s > fee(%s) = %s: not monotonic", q, fee, more, feeMore)
		}
		mirror, err := Compute(s, canonical.SideBuy, one.Sub(p), q)
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		if !mirror.Equal(fee) {
			t.Fatalf("fee(%s) = %s != fee(%s) = %s: not symmetric", p, fee, one.Sub(p), mirror)
		}
		var coeff, step decimal.Decimal
		if s.ID == canonical.FeeScheduleKalshiTakerV1 {
			coeff, step = s.Params[ParamMultiplier], defaultKalshiRoundTo
		} else {
			coeff, step = s.Params[ParamRate], decimal.New(1, -polymarketPrecision)
		}
		raw := coeff.Mul(q).Mul(p).Mul(one.Sub(p))
		if fee.GreaterThan(raw.Add(step)) {
			t.Fatalf("fee %s exceeds raw %s + step %s", fee, raw, step)
		}
		if fee.Sub(raw).Abs().GreaterThan(step) {
			t.Fatalf("fee %s is more than one step %s from raw %s", fee, step, raw)
		}
	})
}
