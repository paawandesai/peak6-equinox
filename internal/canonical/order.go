package canonical

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Side is buy or sell.
type Side string

// Sides.
const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// Valid reports whether s is buy or sell.
func (s Side) Valid() bool { return s == SideBuy || s == SideSell }

// OrderType is market or limit.
type OrderType string

// Order types.
const (
	OrderMarket OrderType = "market"
	OrderLimit  OrderType = "limit"
)

// Valid reports whether t is market or limit.
func (t OrderType) Valid() bool { return t == OrderMarket || t == OrderLimit }

// TimeInForce is the fill rule for a simulated order.
type TimeInForce string

// Times in force: IOC fills what it can now; FOK fills completely or not at all.
const (
	TIFIOC TimeInForce = "IOC"
	TIFFOK TimeInForce = "FOK"
)

// Valid reports whether t is IOC or FOK.
func (t TimeInForce) Valid() bool { return t == TIFIOC || t == TIFFOK }

// Order is a hypothetical order to route. It is only ever simulated.
// Exactly one of Quantity (contracts) or Notional (USD, converted at the
// touch by the router) is set.
type Order struct {
	Side     Side             `json:"side"`
	Outcome  Outcome          `json:"outcome"`
	Quantity *decimal.Decimal `json:"quantity,omitempty"`
	Notional *decimal.Decimal `json:"notional,omitempty"`
	Type     OrderType        `json:"type"`
	// LimitPrice is required for limit orders and must be in [0,1]; nil for market orders.
	LimitPrice *decimal.Decimal `json:"limit_price,omitempty"`
	TIF        TimeInForce      `json:"tif"`
}

// Validate checks the enums, that exactly one positive size is given, and
// that a limit price is present (and in [0,1]) only for limit orders.
func (o Order) Validate() error {
	var errs []error
	if !o.Side.Valid() {
		errs = append(errs, fmt.Errorf("side %q is not valid", truncate(string(o.Side), 32)))
	}
	if !o.Outcome.Valid() {
		errs = append(errs, fmt.Errorf("outcome %q is not valid", truncate(string(o.Outcome), 32)))
	}
	if !o.Type.Valid() {
		errs = append(errs, fmt.Errorf("type %q is not valid", truncate(string(o.Type), 32)))
	}
	if !o.TIF.Valid() {
		errs = append(errs, fmt.Errorf("tif %q is not valid", truncate(string(o.TIF), 32)))
	}
	switch {
	case (o.Quantity == nil) == (o.Notional == nil):
		errs = append(errs, errors.New("exactly one of quantity or notional is required"))
	case o.Quantity != nil && !o.Quantity.IsPositive():
		errs = append(errs, fmt.Errorf("quantity %s must be positive", o.Quantity))
	case o.Notional != nil && !o.Notional.IsPositive():
		errs = append(errs, fmt.Errorf("notional %s must be positive", o.Notional))
	}
	switch {
	case o.Type == OrderLimit && o.LimitPrice == nil:
		errs = append(errs, errors.New("limit orders need a limit price"))
	case o.Type == OrderMarket && o.LimitPrice != nil:
		errs = append(errs, errors.New("market orders take no limit price"))
	case o.LimitPrice != nil:
		if err := ValidatePrice(*o.LimitPrice); err != nil {
			errs = append(errs, fmt.Errorf("limit price: %w", err))
		}
	}
	return errors.Join(errs...)
}

// Candidate is one instrument the router may fill on, with its current quote.
// For partners, Relationship links it to the target; it is nil for the target.
type Candidate struct {
	Instrument   Instrument    `json:"instrument"`
	Quote        Quote         `json:"quote"`
	Relationship *Relationship `json:"relationship,omitempty"`
}

// Leg is a simulated fill on one instrument, with its per-contract cost terms.
// AllInPerContract = VWAP + FeePerContract + BasisPenaltyPerContract (domain.md rule 6).
type Leg struct {
	InstrumentID            string          `json:"instrument_id"`
	Venue                   Venue           `json:"venue"`
	Side                    Side            `json:"side"`
	Outcome                 Outcome         `json:"outcome"`
	Quantity                decimal.Decimal `json:"quantity"`
	VWAP                    decimal.Decimal `json:"vwap"`
	FeePerContract          decimal.Decimal `json:"fee_per_contract"`
	BasisPenaltyPerContract decimal.Decimal `json:"basis_penalty_per_contract"`
	AllInPerContract        decimal.Decimal `json:"all_in_per_contract"`
	// Route is "direct", or "complement" when buying NO on a complement partner.
	Route string `json:"route"`
}

// Alternative is a ranked routing option that was considered.
type Alternative struct {
	Rank int   `json:"rank"`
	Legs []Leg `json:"legs"`
	// AllInPerContract is the size-weighted all-in cost across Legs.
	AllInPerContract decimal.Decimal `json:"all_in_per_contract"`
	Note             string          `json:"note,omitempty"`
}

// Exclusion is a candidate the router did not use, with the reason.
type Exclusion struct {
	InstrumentID string `json:"instrument_id"`
	Venue        Venue  `json:"venue"`
	Reason       string `json:"reason"` // e.g. "stale", "insufficient_depth", "limit_not_reachable"
	Detail       string `json:"detail"`
}

// DecisionInput records one candidate considered by the router at decision time.
// It captures the router's actual input snapshot, so the trace shows what the
// router saw (not a later re-query that might disagree). UI-25, D-098 call 3.
type DecisionInput struct {
	InstrumentID string `json:"instrument_id"`
	Venue        Venue  `json:"venue"`
	// RelationshipType is the partner's relationship type to the target ("self" for the target itself).
	RelationshipType RelationshipType `json:"relationship_type"`
	Confidence       decimal.Decimal  `json:"confidence"` // 1.0 for the target
	// OutcomeSide is the outcome the router checked on this candidate.
	OutcomeSide Outcome `json:"outcome_side"`
	// BestAsk and AskDepth record the book at decision time (top N levels, capped).
	BestAsk  decimal.Decimal `json:"best_ask"`
	AskDepth []Level         `json:"ask_depth"`
	// Quote times from the stored quote.
	FetchedAt time.Time  `json:"fetched_at"`
	VenueTS   *time.Time `json:"venue_ts,omitempty"`
	RoundID   int64      `json:"round_id"`
}

// Decision is the router's output for one Order (FR-12/FR-13).
type Decision struct {
	RoundID      int64            `json:"round_id"`
	TargetID     string           `json:"target_id"`
	TargetType   RelationshipType `json:"target_type"` // relationship type of the pair being routed (UI-25, D-098 call 3)
	Order        Order            `json:"order"`
	Chosen       []Leg            `json:"chosen"`
	Alternatives []Alternative    `json:"alternatives"`
	Exclusions   []Exclusion      `json:"exclusions"`
	Warnings     []string         `json:"warnings"`
	// Explanation is the one-paragraph plain-English summary. Required.
	Explanation string    `json:"explanation"`
	DecidedAt   time.Time `json:"decided_at"`
	// Inputs records the quotes the router read, one per candidate considered (eligible or not).
	// Empty for decisions stored before UI-25. UI-25, D-098 call 3.
	Inputs []DecisionInput `json:"inputs,omitempty"`
}

// Validate checks the round, target, order, decision time and that the
// explanation is non-empty.
func (d Decision) Validate() error {
	var errs []error
	if d.RoundID <= 0 {
		errs = append(errs, errors.New("round_id is required"))
	}
	if err := ValidateInstrumentID(d.TargetID); err != nil {
		errs = append(errs, err)
	}
	if err := d.Order.Validate(); err != nil {
		errs = append(errs, err)
	}
	if err := checkUTC("decided_at", d.DecidedAt, false); err != nil {
		errs = append(errs, err)
	}
	if strings.TrimSpace(d.Explanation) == "" {
		errs = append(errs, errors.New("explanation is required"))
	}
	return errors.Join(errs...)
}
