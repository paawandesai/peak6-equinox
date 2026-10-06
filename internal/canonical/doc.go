// Package canonical holds the venue-independent types every other package
// speaks, and their validation.
//
// Contracts (docs/architecture.md § Interfaces and contracts):
//   - Prices are decimal.Decimal in [0,1] per $1 payout; sizes are contracts.
//   - Times are time.Time in UTC (Location() == time.UTC).
//   - Unknown is a value: a term that cannot be parsed is recorded as Unknown,
//     never guessed.
//   - Every Relationship and Decision carries a non-empty Explanation and the
//     round ID.
//
// This package imports only the standard library and shopspring/decimal.
package canonical
