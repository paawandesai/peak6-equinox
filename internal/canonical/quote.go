package canonical

import (
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

var (
	zero = decimal.Zero
	one  = decimal.NewFromInt(1)
	two  = decimal.NewFromInt(2)
)

// ValidatePrice checks p is a probability in [0,1] per $1 payout (NFR-08).
func ValidatePrice(p decimal.Decimal) error {
	if p.LessThan(zero) || p.GreaterThan(one) {
		return fmt.Errorf("price %s is outside [0,1]", p)
	}
	return nil
}

// Level is one price level of an order book: Size contracts resting at Price.
type Level struct {
	Price decimal.Decimal `json:"price"`
	Size  decimal.Decimal `json:"size"`
}

// Validate checks the price is in [0,1] and the size is positive.
func (l Level) Validate() error {
	if err := ValidatePrice(l.Price); err != nil {
		return err
	}
	if !l.Size.IsPositive() {
		return fmt.Errorf("size %s must be positive", l.Size)
	}
	return nil
}

// Quote is one instrument's order book at a point in time, normalised to the
// YES side: YesBids sorted best (highest) first, YesAsks best (lowest) first.
type Quote struct {
	InstrumentID string  `json:"instrument_id"`
	YesBids      []Level `json:"yes_bids"`
	YesAsks      []Level `json:"yes_asks"`
	// FetchedAt is when Equinox received the book (UTC).
	FetchedAt time.Time `json:"fetched_at"`
	// VenueTS is the venue's own book timestamp (UTC), if it sent one.
	VenueTS *time.Time `json:"venue_ts,omitempty"`
	Quality Quality    `json:"quality"`
}

// Validate checks the instrument ID, every level, best-first ordering on each
// side, and that FetchedAt (required) and VenueTS (optional) are UTC.
func (q Quote) Validate() error {
	var errs []error
	if err := ValidateInstrumentID(q.InstrumentID); err != nil {
		errs = append(errs, err)
	}
	if err := checkUTC("fetched_at", q.FetchedAt, false); err != nil {
		errs = append(errs, err)
	}
	if q.VenueTS != nil {
		if err := checkUTC("venue_ts", *q.VenueTS, false); err != nil {
			errs = append(errs, err)
		}
	}
	errs = append(errs, validateSide("yes_bids", q.YesBids, func(prev, cur decimal.Decimal) bool { return cur.LessThan(prev) })...)
	errs = append(errs, validateSide("yes_asks", q.YesAsks, func(prev, cur decimal.Decimal) bool { return cur.GreaterThan(prev) })...)
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("quote %q: %w", truncate(q.InstrumentID, 64), err)
	}
	return nil
}

// validateSide checks each level and that prices strictly worsen down the book.
func validateSide(name string, levels []Level, worse func(prev, cur decimal.Decimal) bool) []error {
	var errs []error
	for i, l := range levels {
		if err := l.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("%s[%d]: %w", name, i, err))
			continue
		}
		if i > 0 && !worse(levels[i-1].Price, l.Price) {
			errs = append(errs, fmt.Errorf("%s[%d]: price %s is not worse than the level before; sort best first", name, i, l.Price))
		}
	}
	return errs
}

// BestBid returns the highest YES bid, if any.
func (q Quote) BestBid() (decimal.Decimal, bool) {
	if len(q.YesBids) == 0 {
		return decimal.Decimal{}, false
	}
	return q.YesBids[0].Price, true
}

// BestAsk returns the lowest YES ask, if any.
func (q Quote) BestAsk() (decimal.Decimal, bool) {
	if len(q.YesAsks) == 0 {
		return decimal.Decimal{}, false
	}
	return q.YesAsks[0].Price, true
}

// Mid returns (best bid + best ask) / 2 when both sides exist. Display only;
// never used for execution (domain.md).
func (q Quote) Mid() (decimal.Decimal, bool) {
	bid, okBid := q.BestBid()
	ask, okAsk := q.BestAsk()
	if !okBid || !okAsk {
		return decimal.Decimal{}, false
	}
	return bid.Add(ask).Div(two), true
}
