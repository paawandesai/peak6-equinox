package match

import (
	"fmt"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// BlockWindow is how far apart two deadlines may be for the pair to be a
// candidate at all (FR-07: ±7 days).
const BlockWindow = 7 * 24 * time.Hour

// blockReason says why a pair is not a candidate; empty means it passed.
type blockReason struct {
	field  string
	detail string
}

// block applies FR-07 candidate blocking: same category, overlapping subject
// and deadlines within BlockWindow. An unknown subject or a missing deadline
// cannot overlap, so the pair is blocked. It returns the first reason found,
// in that order, or nil.
func block(a, b canonical.Instrument) *blockReason {
	if a.Category != b.Category {
		return &blockReason{"category", fmt.Sprintf("%s vs %s", a.Category, b.Category)}
	}
	if a.Terms.Subject == canonical.Unknown || b.Terms.Subject == canonical.Unknown {
		return &blockReason{"subject", fmt.Sprintf("%s vs %s (unknown cannot overlap)", a.Terms.Subject, b.Terms.Subject)}
	}
	if a.Terms.Subject != b.Terms.Subject {
		return &blockReason{"subject", fmt.Sprintf("%s vs %s", a.Terms.Subject, b.Terms.Subject)}
	}
	if a.Terms.Deadline == nil || b.Terms.Deadline == nil {
		return &blockReason{"deadline", "deadline unknown on at least one side"}
	}
	if gap := deadlineGap(a, b); gap > BlockWindow {
		return &blockReason{"deadline", fmt.Sprintf("deadlines %s apart, more than %s", gap, BlockWindow)}
	}
	return nil
}

// deadlineGap is the absolute distance between two known deadlines.
func deadlineGap(a, b canonical.Instrument) time.Duration {
	gap := a.Terms.Deadline.Sub(*b.Terms.Deadline)
	if gap < 0 {
		return -gap
	}
	return gap
}
