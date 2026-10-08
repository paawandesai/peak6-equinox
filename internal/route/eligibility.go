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
	// Snapshot is the round snapshot time against which quote age is measured
	// (NFR-09, D-077). Typically round.FinishedAt, or round.StartedAt if zero.
	Snapshot time.Time
	// StaleAfter is the maximum age of a quote. Quotes older than this are
	// ineligible (Snapshot - FetchedAt > StaleAfter).
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
// insufficient depth on the side the order consumes. Returns false and issues if ineligible.
//
// Staleness: cfg.Snapshot - q.FetchedAt > cfg.StaleAfter excludes with ReasonStale.
// Missing book: q.Quality flag no_book or both sides empty excludes with ReasonNoBook.
// Insufficient depth: if depth on the consumed side < cfg.MinBookDepth, exclude with
// ReasonInsufficientDepth. Buy YES consumes YesAsks; sell YES consumes YesBids.
func IsEligible(q canonical.Quote, side canonical.Side, outcome canonical.Outcome, cfg EligibilityConfig) (bool, []EligibilityIssue) {
	var exs []EligibilityIssue

	// Staleness check: FetchedAt must be present and recent (relative to snapshot).
	// Fail closed if snapshot is unknown.
	if cfg.Snapshot.IsZero() {
		exs = append(exs, EligibilityIssue{
			Reason: ReasonStale,
			Detail: "round snapshot time unknown",
		})
	} else if q.FetchedAt.IsZero() {
		exs = append(exs, EligibilityIssue{
			Reason: ReasonStale,
			Detail: "fetched_at is missing or zero",
		})
	} else {
		age := cfg.Snapshot.Sub(q.FetchedAt)
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

	// Insufficient depth check: check depth on the side the order consumes.
	// Buy YES consumes YesAsks (sellers); sell YES consumes YesBids (buyers).
	// This is stricter than total-depth (Cursor reviewer fix).
	var consumedSide []canonical.Level
	if side == canonical.SideBuy && outcome == canonical.OutcomeYes {
		consumedSide = q.YesAsks
	} else if side == canonical.SideSell && outcome == canonical.OutcomeYes {
		consumedSide = q.YesBids
	} else if side == canonical.SideBuy && outcome == canonical.OutcomeNo {
		consumedSide = q.YesBids // buy NO = sell YES
	} else { // sell NO
		consumedSide = q.YesAsks // sell NO = buy YES
	}

	sideDepth := decimal.Zero
	for _, l := range consumedSide {
		if l.Size.IsPositive() {
			sideDepth = sideDepth.Add(l.Size)
		}
	}
	if sideDepth.LessThan(cfg.MinBookDepth) {
		exs = append(exs, EligibilityIssue{
			Reason: ReasonInsufficientDepth,
			Detail: fmt.Sprintf("%s contracts available on consumed side", sideDepth),
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
