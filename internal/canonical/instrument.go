package canonical

import (
	"errors"
	"fmt"
	"time"
)

// InstrumentStatus is the trading status of an instrument.
type InstrumentStatus string

// Instrument statuses.
const (
	StatusOpen    InstrumentStatus = "open"
	StatusClosed  InstrumentStatus = "closed"
	StatusSettled InstrumentStatus = "settled"
	StatusUnknown InstrumentStatus = Unknown
)

// Valid reports whether s is a known status or StatusUnknown.
func (s InstrumentStatus) Valid() bool {
	return s == StatusOpen || s == StatusClosed || s == StatusSettled || s == StatusUnknown
}

// Instrument is one binary contract with its static terms: it pays $1 if its
// proposition resolves YES (D-007). Adapters never return a partial Instrument
// silently; missing fields are listed in Quality.
type Instrument struct {
	// ID is "<venue>:<key>": "kalshi:<ticker>" or "polymarket:<yes token id>".
	ID            string   `json:"id"`
	Venue         Venue    `json:"venue"`
	VenueMarketID string   `json:"venue_market_id"` // Kalshi ticker, Polymarket condition ID
	VenueEventID  string   `json:"venue_event_id"`  // Kalshi event ticker, Polymarket event slug / negRisk group
	Category      Category `json:"category"`
	Title         string   `json:"title"`
	RulesText     string   `json:"rules_text"`
	Terms         Terms    `json:"terms"`
	// CloseTime is when trading stops (UTC). Zero means the venue did not say; flag it in Quality.
	CloseTime time.Time `json:"close_time"`
	// ExpirationTime is when the venue expects to settle (UTC); zero if not published.
	ExpirationTime time.Time        `json:"expiration_time"`
	Status         InstrumentStatus `json:"status"`
	FeeSchedule    FeeSchedule      `json:"fee_schedule"`
	Quality        Quality          `json:"quality"`
}

// Validate checks the ID format and its venue prefix, the enums, that the
// title is present, that times are UTC, and the nested terms and fee schedule.
func (i Instrument) Validate() error {
	var errs []error
	if err := ValidateInstrumentID(i.ID); err != nil {
		errs = append(errs, err)
	} else if VenueOf(i.ID) != i.Venue {
		errs = append(errs, fmt.Errorf("instrument id %q does not match venue %q", i.ID, i.Venue))
	}
	if !i.Venue.Valid() {
		errs = append(errs, fmt.Errorf("venue %q is not supported", i.Venue))
	}
	if !i.Category.Valid() {
		errs = append(errs, fmt.Errorf("category %q is not supported", i.Category))
	}
	if !i.Status.Valid() {
		errs = append(errs, fmt.Errorf("status %q is not valid", i.Status))
	}
	if i.VenueMarketID == "" {
		errs = append(errs, errors.New("venue_market_id is required"))
	}
	if i.Title == "" {
		errs = append(errs, errors.New("title is required"))
	}
	if err := checkUTC("close_time", i.CloseTime, true); err != nil {
		errs = append(errs, err)
	}
	if err := checkUTC("expiration_time", i.ExpirationTime, true); err != nil {
		errs = append(errs, err)
	}
	if err := i.Terms.Validate(); err != nil {
		errs = append(errs, err)
	}
	if err := i.FeeSchedule.Validate(); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("instrument %q: %w", truncate(i.ID, 64), err)
	}
	return nil
}

// Outcome is a side of a binary contract.
type Outcome string

// Outcomes.
const (
	OutcomeYes Outcome = "yes"
	OutcomeNo  Outcome = "no"
)

// Valid reports whether o is yes or no.
func (o Outcome) Valid() bool { return o == OutcomeYes || o == OutcomeNo }

// Settled is a resolved instrument with its outcome, for the settlement back-test.
type Settled struct {
	Instrument Instrument `json:"instrument"`
	Outcome    Outcome    `json:"outcome"`
	// SettledAt is when the venue settled the market (UTC); zero if not published.
	SettledAt time.Time `json:"settled_at"`
}

// Validate checks the instrument, the outcome and the settlement time.
func (s Settled) Validate() error {
	var errs []error
	if err := s.Instrument.Validate(); err != nil {
		errs = append(errs, err)
	}
	if !s.Outcome.Valid() {
		errs = append(errs, fmt.Errorf("settled outcome %q must be yes or no", s.Outcome))
	}
	if err := checkUTC("settled_at", s.SettledAt, true); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}
