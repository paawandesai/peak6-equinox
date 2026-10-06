// Package fees evaluates a canonical.FeeSchedule. The formula is chosen by the
// schedule ID and parameterised by the schedule's Params, so adapters supply
// fees as data and the router never needs venue-specific code (FR-16, D-028).
//
// Formulas and the official sources they were checked against are recorded in
// docs/domain.md § Fees. A schedule whose Verified flag is false is still
// computed; the router then labels the result "fees: approximate".
package fees

import (
	"errors"
	"fmt"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Parameter names, by schedule.
const (
	// ParamMultiplier (kalshi_taker_v1, required) is the coefficient applied to
	// C × P × (1 − P): 0.07 × the series' fee_multiplier.
	ParamMultiplier = "multiplier"
	// ParamRoundTo (kalshi_taker_v1, optional) is the increment the order fee is
	// rounded up to, in dollars. Defaults to one cent.
	ParamRoundTo = "round_to"
	// ParamRate (polymarket_v1, required) is the market's feeSchedule.rate.
	ParamRate = "rate"
	// ParamExponent (polymarket_v1, optional) is the market's
	// feeSchedule.exponent, applied to the p × (1 − p) term. Defaults to 1.
	ParamExponent = "exponent"
)

// Defaults for optional parameters.
var (
	defaultKalshiRoundTo = decimal.RequireFromString("0.01")
	defaultExponent      = decimal.NewFromInt(1)
)

// polymarketPrecision is the number of decimal places Polymarket keeps on a
// fee ("Fees are rounded to 5 decimal places").
const polymarketPrecision = 5

// Errors returned by Compute. Callers wrap them; the router turns them into an
// exclusion rather than guessing a fee.
var (
	ErrUnknownSchedule = errors.New("unknown fee schedule")
	ErrMissingParam    = errors.New("missing fee parameter")
	ErrInvalidParam    = errors.New("invalid fee parameter")
	ErrInvalidInput    = errors.New("invalid fee input")
)

var one = decimal.NewFromInt(1)

// Compute returns the taker fee, in dollars, for filling qty contracts at
// price under schedule s. Both venues charge the same fee for buys and sells
// at a given price, but side is validated so a bad order never reaches here.
// A zero qty costs nothing.
func Compute(s canonical.FeeSchedule, side canonical.Side, price, qty decimal.Decimal) (decimal.Decimal, error) {
	if !side.Valid() {
		return decimal.Zero, fmt.Errorf("%w: side %q", ErrInvalidInput, side)
	}
	if err := canonical.ValidatePrice(price); err != nil {
		return decimal.Zero, fmt.Errorf("%w: %w", ErrInvalidInput, err)
	}
	if qty.IsNegative() {
		return decimal.Zero, fmt.Errorf("%w: quantity %s is negative", ErrInvalidInput, qty)
	}
	if err := s.Validate(); err != nil {
		return decimal.Zero, fmt.Errorf("%w: %w", ErrInvalidParam, err)
	}
	switch s.ID {
	case canonical.FeeScheduleKalshiTakerV1:
		return kalshiTakerV1(s, price, qty)
	case canonical.FeeSchedulePolymarketV1:
		return polymarketV1(s, price, qty)
	}
	return decimal.Zero, fmt.Errorf("%w: %q", ErrUnknownSchedule, s.ID)
}

// Known reports whether Compute implements schedule id.
func Known(id string) bool {
	return id == canonical.FeeScheduleKalshiTakerV1 || id == canonical.FeeSchedulePolymarketV1
}

// kalshiTakerV1 is Kalshi's general taker fee: round up to the next round_to
// of multiplier × C × P × (1 − P), where multiplier is 0.07 × the series'
// fee_multiplier. The rounding applies to the whole order, not per contract.
func kalshiTakerV1(s canonical.FeeSchedule, price, qty decimal.Decimal) (decimal.Decimal, error) {
	m, err := required(s, ParamMultiplier)
	if err != nil {
		return decimal.Zero, err
	}
	roundTo, err := optional(s, ParamRoundTo, defaultKalshiRoundTo)
	if err != nil {
		return decimal.Zero, err
	}
	if !roundTo.IsPositive() {
		return decimal.Zero, fmt.Errorf("%w: %s must be positive", ErrInvalidParam, ParamRoundTo)
	}
	raw := m.Mul(qty).Mul(price).Mul(one.Sub(price))
	return ceilTo(raw, roundTo), nil
}

// polymarketV1 is Polymarket's taker fee: C × rate × (p × (1 − p))^exponent,
// rounded to 5 decimal places. Makers pay nothing; our orders are takers.
func polymarketV1(s canonical.FeeSchedule, price, qty decimal.Decimal) (decimal.Decimal, error) {
	rate, err := required(s, ParamRate)
	if err != nil {
		return decimal.Zero, err
	}
	exp, err := optional(s, ParamExponent, defaultExponent)
	if err != nil {
		return decimal.Zero, err
	}
	if !exp.IsInteger() {
		return decimal.Zero, fmt.Errorf("%w: %s must be a whole number", ErrInvalidParam, ParamExponent)
	}
	curve := price.Mul(one.Sub(price))
	if !exp.Equal(one) {
		curve = curve.Pow(exp)
	}
	return qty.Mul(rate).Mul(curve).Round(polymarketPrecision), nil
}

// ceilTo rounds v up to the next multiple of step (step > 0).
func ceilTo(v, step decimal.Decimal) decimal.Decimal {
	return v.Div(step).Ceil().Mul(step)
}

func required(s canonical.FeeSchedule, name string) (decimal.Decimal, error) {
	v, ok := s.Params[name]
	if !ok {
		return decimal.Zero, fmt.Errorf("%w: %s needs %q", ErrMissingParam, s.ID, name)
	}
	return v, nil
}

func optional(s canonical.FeeSchedule, name string, def decimal.Decimal) (decimal.Decimal, error) {
	v, ok := s.Params[name]
	if !ok {
		return def, nil
	}
	return v, nil
}
