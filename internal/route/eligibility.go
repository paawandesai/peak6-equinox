package route

import (
	"fmt"
	"math"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// EligibilityConfig holds the staleness, skew, and depth thresholds for
// routing eligibility checks (M3-01, FR-15, NFR-09).
type EligibilityConfig struct {
	// StaleAfter is the maximum age of a quote. Quotes older than this are
	// ineligible (now - FetchedAt > StaleAfter).
	StaleAfter time.Duration
	// SkewWarn is the skew threshold for a warning (abs(q1.FetchedAt - q2.FetchedAt)).
	SkewWarn time.Duration
	// SkewMax is the skew threshold for exclusion. The older quote is excluded.
	SkewMax time.Duration
	// MinBookDepth is the minimum total book depth (sum of both sides) for
	// eligibility. A quote with less depth is ineligible.
	MinBookDepth decimal.Decimal
}

// EligibilityIssue describes one reason a quote is ineligible.
type EligibilityIssue struct {
	Reason string
	Detail string
}

// IsEligible checks quote-local eligibility: staleness, missing book, and
// insufficient depth. It returns false and a list of issues if the quote
// is ineligible, or true and an empty list if eligible.
//
// Staleness: now - q.FetchedAt > cfg.StaleAfter excludes with ReasonStale.
// A missing or zero FetchedAt is ineligible.
//
// Missing book: q.Quality flag no_book or both YesAsks and YesBids empty
// excludes with ReasonNoBook.
//
// Insufficient depth: if the total size across both sides is less than
// cfg.MinBookDepth, exclude with ReasonInsufficientDepth.
func IsEligible(q canonical.Quote, now time.Time, cfg EligibilityConfig) (bool, []EligibilityIssue) {
	var exs []EligibilityIssue

	// Staleness check: FetchedAt must be present and recent.
	if q.FetchedAt.IsZero() {
		exs = append(exs, EligibilityIssue{
			Reason: ReasonStale,
			Detail: "fetched_at is missing or zero",
		})
	} else {
		age := now.Sub(q.FetchedAt)
		if age > cfg.StaleAfter {
			exs = append(exs, EligibilityIssue{
				Reason: ReasonStale,
				Detail: fmt.Sprintf("fetched %d s ago", int(age.Seconds())),
			})
		}
	}

	// Missing book check: Quality flag or both sides empty.
	hasNoBook := q.Quality.Has(canonical.QualityNoBook) || (len(q.YesAsks) == 0 && len(q.YesBids) == 0)
	if hasNoBook {
		exs = append(exs, EligibilityIssue{
			Reason: ReasonNoBook,
			Detail: "no book available",
		})
		// If there's no book at all, don't bother checking depth.
		return len(exs) == 0, exs
	}

	// Insufficient depth check: sum both sides and compare to MinBookDepth.
	totalDepth := decimal.Zero
	for _, l := range q.YesBids {
		if l.Size.IsPositive() {
			totalDepth = totalDepth.Add(l.Size)
		}
	}
	for _, l := range q.YesAsks {
		if l.Size.IsPositive() {
			totalDepth = totalDepth.Add(l.Size)
		}
	}
	if totalDepth.LessThan(cfg.MinBookDepth) {
		exs = append(exs, EligibilityIssue{
			Reason: ReasonInsufficientDepth,
			Detail: fmt.Sprintf("%s contracts available", totalDepth),
		})
	}

	return len(exs) == 0, exs
}

// Skew checks the time difference between two quotes' FetchedAt timestamps.
// If abs(q1.FetchedAt - q2.FetchedAt) > cfg.SkewMax, it returns excludeOlder=true
// to exclude the older quote with ReasonSkew. If the skew is between cfg.SkewWarn
// and cfg.SkewMax, it returns excludeOlder=false and a warning.
//
// If either FetchedAt is zero, no exclusion or warning is returned (staleness
// will handle missing timestamps separately).
func Skew(q1, q2 canonical.Quote, cfg EligibilityConfig) (excludeOlder bool, warnings []string) {
	if q1.FetchedAt.IsZero() || q2.FetchedAt.IsZero() {
		return false, nil
	}

	diff := q1.FetchedAt.Sub(q2.FetchedAt)
	absDiff := time.Duration(math.Abs(float64(diff)))

	if absDiff > cfg.SkewMax {
		return true, nil
	}
	if absDiff > cfg.SkewWarn {
		return false, []string{fmt.Sprintf("skew_warning: %d s behind", int(absDiff.Seconds()))}
	}
	return false, nil
}

// SkewDetail returns the detail string for a skew exclusion, given the time
// difference in seconds.
func SkewDetail(skewSeconds int) string {
	return fmt.Sprintf("%d s behind", skewSeconds)
}

// OlderQuote returns the instrument ID of the older quote (by FetchedAt).
// If times are equal or either is zero, returns empty string.
func OlderQuote(q1, q2 canonical.Quote) string {
	if q1.FetchedAt.IsZero() || q2.FetchedAt.IsZero() {
		return ""
	}
	if q1.FetchedAt.Before(q2.FetchedAt) {
		return q1.InstrumentID
	}
	if q2.FetchedAt.Before(q1.FetchedAt) {
		return q2.InstrumentID
	}
	return ""
}
