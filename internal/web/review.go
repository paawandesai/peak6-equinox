package web

import (
	"time"

	"github.com/shopspring/decimal"
)

// isQuoteFresh returns true if the quote's age is within the staleAfter threshold.
// Production code: handlers_matches.go lines 304-305 (UI-18 staleness filtering).
func isQuoteFresh(fetchedAt, now time.Time, staleAfter time.Duration) bool {
	age := now.Sub(fetchedAt)
	return age <= staleAfter
}

// shouldFlagForReview returns true when a pair should be flagged for review:
// net gap absolute value >= threshold AND confidence < 0.90.
// Production code: handlers_matches.go line 483 (UI-18 flag predicate).
func shouldFlagForReview(netGap NetGap, confidence, threshold decimal.Decimal) bool {
	if !netGap.HasValue {
		return false
	}
	return netGap.Value.Abs().GreaterThanOrEqual(threshold) &&
		confidence.LessThan(decimal.RequireFromString("0.90"))
}
