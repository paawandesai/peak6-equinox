package canonical

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/shopspring/decimal"
)

// Fee schedule IDs known at M1 (domain.md § Fees; formulas live in internal/fees).
const (
	FeeScheduleKalshiTakerV1 = "kalshi_taker_v1"
	FeeSchedulePolymarketV1  = "polymarket_v1"
)

var feeScheduleIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// FeeSchedule is a fee formula expressed as data: the formula is chosen by ID
// and parameterised by Params (e.g. {"multiplier": 0.07}). internal/fees
// evaluates it.
type FeeSchedule struct {
	ID     string                     `json:"id"`
	Params map[string]decimal.Decimal `json:"params,omitempty"`
	// Verified is true once the schedule has been checked against the venue's
	// official fee documentation; until then explanations say "fees: approximate".
	Verified bool `json:"verified"`
	// Source is the URL of the fee documentation the schedule was checked against.
	Source string `json:"source,omitempty"`
}

// Validate checks the ID is a lower_snake identifier and no parameter is negative.
func (f FeeSchedule) Validate() error {
	var errs []error
	if !feeScheduleIDPattern.MatchString(f.ID) {
		errs = append(errs, fmt.Errorf("fee schedule id %q is not a lower_snake identifier", truncate(f.ID, 64)))
	}
	for k, v := range f.Params {
		if v.IsNegative() {
			errs = append(errs, fmt.Errorf("fee schedule param %q is negative", k))
		}
	}
	if f.Verified && f.Source == "" {
		errs = append(errs, errors.New("a verified fee schedule needs a source"))
	}
	return errors.Join(errs...)
}
