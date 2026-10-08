package match

import (
	"fmt"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
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
// in that order, or nil for the reason. When the Fed exception allows a pair
// through (same meeting via fed_meeting_dates), returns facts used per FR-10/D-021.
func block(a, b canonical.Instrument, f facts.Set) (*blockReason, []canonical.FactUse) {
	if a.Category != b.Category {
		return &blockReason{"category", fmt.Sprintf("%s vs %s", a.Category, b.Category)}, nil
	}
	if a.Terms.Subject == canonical.Unknown || b.Terms.Subject == canonical.Unknown {
		return &blockReason{"subject", fmt.Sprintf("%s vs %s (unknown cannot overlap)", a.Terms.Subject, b.Terms.Subject)}, nil
	}
	if a.Terms.Subject != b.Terms.Subject {
		return &blockReason{"subject", fmt.Sprintf("%s vs %s", a.Terms.Subject, b.Terms.Subject)}, nil
	}
	if a.Terms.Deadline == nil || b.Terms.Deadline == nil {
		return &blockReason{"deadline", "deadline unknown on at least one side"}, nil
	}
	if gap := deadlineGap(a, b); gap > BlockWindow {
		// Special case for Fed: if both deadlines resolve on the same Fed meeting
		// (read from facts.Set § fed_meeting_dates, nearest-meeting assignment),
		// allow the pair through even if the gap exceeds BlockWindow. This handles
		// Polymarket December markets that have a Dec 31 fallback but actually
		// resolve on the Dec 8-9 meeting (D-083, KXFED-26DEC). The matcher reads
		// fed_meeting_dates; Fed block exception (proposed D-095).
		if meetingsFact, ok := f.Get("fed_meeting_dates"); ok && onSameFedMeetingViaFacts(a, b, f) {
			// Record fed_meeting_dates use per FR-10/D-021
			return nil, []canonical.FactUse{meetingsFact.Use()}
		}
		return &blockReason{"deadline", fmt.Sprintf("deadlines %s apart, more than %s", gap, BlockWindow)}, nil
	}
	return nil, nil
}

// deadlineGap is the absolute distance between two known deadlines.
func deadlineGap(a, b canonical.Instrument) time.Duration {
	gap := a.Terms.Deadline.Sub(*b.Terms.Deadline)
	if gap < 0 {
		return -gap
	}
	return gap
}
