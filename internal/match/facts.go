package match

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// The Fed level-vs-change rule (domain.md equivalent_given_facts, the M2
// demo): Kalshi ladders state the level of the upper bound after a meeting
// ("above 4.00% following the Oct 28, 2026 meeting") while Polymarket buckets
// state the change decided at that meeting ("increase by 50+ bps after the
// October 2026 meeting"). The two describe one measurement only once the level
// in force before the meeting is known, and that level is an external fact
// (FR-10, D-021), never read from venue text. Three facts take part:
//
//	fed_upper_bound   the upper bound in force before the meeting, in percent;
//	                  a change bucket "+d" is the level "fed_upper_bound + d".
//	fed_next_meeting  the decision date of the FOMC meeting fed_upper_bound is
//	                  the level before. The rule applies only when both
//	                  deadlines sit on that meeting (within MeetingWindow): a
//	                  later meeting's bucket depends on this meeting's decision
//	                  (domain.md: related, conditional on a prior event), so the
//	                  fact must not be applied to it.
//	fed_rate_step     the grid the upper bound moves on (0.25 percentage
//	                  points). A level ladder says "above 4.00", a bucket says
//	                  "at least +25 bps" (at or above 4.25): on the grid those
//	                  are the same set. Without this fact the sets stay
//	                  different and the pair is at best implies.
//
// Every fact consulted is reported in FactsUsed, including on a related
// verdict, so a human can see which fact decided the pair either way.

// Fact ids the matcher consults (config/facts.yaml).
const (
	FactFedUpperBound  = facts.FedUpperBound
	FactFedNextMeeting = "fed_next_meeting"
	FactFedRateStep    = "fed_rate_step"
)

// Fed vocabulary the rule recognises (internal/normalize/fed.go, D-067); the
// matcher must not import normalize, so the strings are repeated here.
const (
	fedSubject          = "fed_funds_rate"
	fedMetricUpperBound = "rate_upper_bound"
	fedMetricChange     = "rate_change"
)

// MeetingWindow is how far a deadline may sit from the meeting date (UTC
// midnight) for the pair to count as resolving on that meeting: Kalshi states
// the statement time on the meeting day and Polymarket's close time is the
// following morning UTC.
const MeetingWindow = 48 * time.Hour

// deriveWithFacts applies the Fed rule to a pair whose only shared-field veto
// is the metric. It returns nil with a note naming the missing or inapplicable
// fact when the rule cannot close the gap.
func deriveWithFacts(a, b canonical.Instrument, f facts.Set) (*derivation, derivationNote) {
	level, change, ok := fedSides(a, b)
	if !ok {
		return nil, derivationNote{}
	}

	// Try the no-negative-range implies logic as a fallback when fed_upper_bound
	// cannot place the change (e.g., different meeting from fed_next_meeting).
	base, baseOK := f.Get(FactFedUpperBound)
	if !baseOK {
		if d, note := deriveNoNegativeRange(a, b, f); d != nil {
			return d, note
		}
		return nil, derivationNote{reason: fmt.Sprintf("the change bucket cannot be placed on the level ladder without fact %s", FactFedUpperBound)}
	}
	used := []canonical.FactUse{base.Use()}
	baseLevel, err := base.Decimal()
	if err != nil {
		return nil, derivationNote{facts: used, reason: fmt.Sprintf("fact %s is not a number (%q)", FactFedUpperBound, base.Value)}
	}
	meeting, meetingOK := f.Get(FactFedNextMeeting)
	if !meetingOK {
		return nil, derivationNote{facts: used, reason: fmt.Sprintf("fact %s=%s cannot be applied without fact %s saying which meeting it precedes",
			FactFedUpperBound, base.Value, FactFedNextMeeting)}
	}
	used = append(used, meeting.Use())
	meetingDay, err := time.ParseInLocation(facts.DateLayout, meeting.Value, time.UTC)
	if err != nil {
		return nil, derivationNote{facts: used, reason: fmt.Sprintf("fact %s is not a date (%q)", FactFedNextMeeting, meeting.Value)}
	}
	for _, in := range []canonical.Instrument{level, change} {
		if !onMeeting(in.Terms.Deadline, meetingDay) {
			// Try no-negative-range as fallback for different meetings
			if d, note := deriveNoNegativeRange(a, b, f); d != nil {
				return d, note
			}
			return nil, derivationNote{facts: used, reason: fmt.Sprintf(
				"fact %s=%s is the level before the %s meeting (%s), but %s resolves on %s: a later meeting's change depends on the prior decision, so the fact does not apply",
				FactFedUpperBound, base.Value, meeting.Value, FactFedNextMeeting, in.ID, in.Terms.Deadline.UTC().Format(facts.DateLayout))}
		}
	}

	pl, okL := propositionOf(level.Terms)
	pc, okC := propositionOf(change.Terms)
	if !okL || !okC {
		return nil, derivationNote{facts: used, reason: "a proposition could not be read"}
	}
	placed := pc.shift(baseLevel)
	given := fmt.Sprintf("given %s=%s (level before the %s meeting, %s), %s %s is the level %s",
		FactFedUpperBound, base.Value, meeting.Value, FactFedNextMeeting, change.ID, pc, placed)
	note := derivationNote{facts: used}
	order := func(lp, cp proposition) (proposition, proposition) {
		if a.ID == level.ID {
			return lp, cp
		}
		return cp, lp
	}

	// Order of claims, strongest first: equal sets (exactly, then on the
	// rate grid), then complement or containment (exactly, then on the grid).
	// Grid equality is checked before exact containment because "gte 4.50
	// forces gt 4.25" is true but weaker than "on the grid they are the same
	// set". A verdict lists only the facts it rests on; the note lists every
	// fact consulted.
	pa, pb := order(pl, placed)
	if pa.equal(pb) {
		return &derivation{typ: canonical.RelEquivalentGivenFacts, facts: used, meeting: true,
			detail: given + "; the two sets coincide"}, note
	}
	step, stepOK := f.Get(FactFedRateStep)
	var (
		stepSize decimal.Decimal
		gridOK   bool
		gridNote string
	)
	if stepOK {
		d, err := step.Decimal()
		switch {
		case err != nil || !d.IsPositive():
			gridNote = fmt.Sprintf("fact %s is not a positive number (%q)", FactFedRateStep, step.Value)
		default:
			stepSize, gridOK = d, true
		}
	} else {
		gridNote = fmt.Sprintf("fact %s (the rate grid) is not available to compare them on the grid", FactFedRateStep)
	}
	withStep := used
	var ga, gb proposition
	var onGrid string
	if gridOK {
		withStep = append(append([]canonical.FactUse{}, used...), step.Use())
		note.facts = withStep
		gl, gp := pl.onGrid(stepSize), placed.onGrid(stepSize)
		onGrid = fmt.Sprintf("on the %s grid (%s) %s is %s and %s is %s", step.Value, FactFedRateStep, pl, gl, placed, gp)
		ga, gb = order(gl, gp)
		if ga.equal(gb) {
			return &derivation{typ: canonical.RelEquivalentGivenFacts, facts: withStep, meeting: true, grid: true,
				detail: given + "; " + onGrid + ": the same set"}, note
		}
	}
	if d := deriveFrom(pa, pb, used); d != nil {
		d.meeting = true
		d.detail = given + "; " + d.detail
		return d, note
	}
	if gridOK {
		if d := deriveFrom(ga, gb, withStep); d != nil {
			d.meeting, d.grid = true, true
			d.detail = given + "; " + onGrid + "; " + d.detail
			return d, note
		}
		note.reason = given + "; " + onGrid + ": the sets neither coincide, complement nor contain each other"
		return nil, note
	}
	note.reason = fmt.Sprintf("%s; %s and %s differ, and %s", given, pl, placed, gridNote)
	return nil, note
}

// fedSides identifies which instrument states the level and which the change.
func fedSides(a, b canonical.Instrument) (level, change canonical.Instrument, ok bool) {
	if a.Category != canonical.CategoryFed || b.Category != canonical.CategoryFed ||
		a.Terms.Subject != fedSubject || b.Terms.Subject != fedSubject {
		return a, b, false
	}
	switch {
	case a.Terms.Metric == fedMetricUpperBound && b.Terms.Metric == fedMetricChange:
		return a, b, true
	case b.Terms.Metric == fedMetricUpperBound && a.Terms.Metric == fedMetricChange:
		return b, a, true
	}
	return a, b, false
}

// onMeeting reports whether a deadline sits within MeetingWindow of the
// meeting day's UTC midnight.
func onMeeting(deadline *time.Time, meetingDay time.Time) bool {
	if deadline == nil {
		return false
	}
	gap := deadline.Sub(meetingDay)
	return gap >= 0 && gap <= MeetingWindow
}

// onGrid closes the open ends of a proposition on a grid of the given step:
// (x, +inf) becomes [x+step, +inf) and (-inf, x) becomes (-inf, x-step] when
// x is a grid point. Ends off the grid are left as they are.
func (p proposition) onGrid(step decimal.Decimal) proposition {
	out := p
	if p.iv.lo != nil && p.iv.loOpen && gridPoint(*p.iv.lo, step) {
		lo := p.iv.lo.Add(step)
		out.iv.lo, out.iv.loOpen = &lo, false
	}
	if p.iv.hi != nil && p.iv.hiOpen && gridPoint(*p.iv.hi, step) {
		hi := p.iv.hi.Sub(step)
		out.iv.hi, out.iv.hiOpen = &hi, false
	}
	out.label = labelOf(out.iv)
	return out
}

// gridPoint reports whether x is a whole number of steps.
func gridPoint(x, step decimal.Decimal) bool {
	q := x.Div(step)
	return q.Equal(q.Truncate(0))
}

// onSameFedMeetingViaFacts reports whether both instruments are Fed markets whose
// deadlines resolve on the same Fed meeting, read from facts.Set § fed_meeting_dates.
// Each deadline is assigned to its NEAREST meeting uniquely to avoid false matches
// (e.g., Dec 31 vs Jan 27).
func onSameFedMeetingViaFacts(a, b canonical.Instrument, f facts.Set) bool {
	if a.Category != canonical.CategoryFed || b.Category != canonical.CategoryFed {
		return false
	}
	if a.Terms.Subject != fedSubject || b.Terms.Subject != fedSubject {
		return false
	}
	if a.Terms.Deadline == nil || b.Terms.Deadline == nil {
		return false
	}

	// Read fed_meeting_dates from facts.Set (comma-separated dates).
	meetingsFact, ok := f.Get("fed_meeting_dates")
	if !ok {
		return false
	}

	// Parse the comma-separated meeting dates.
	meetingStrs := splitCSV(meetingsFact.Value)
	if len(meetingStrs) == 0 {
		return false
	}

	meetings := make([]time.Time, 0, len(meetingStrs))
	for _, s := range meetingStrs {
		t, err := time.ParseInLocation(facts.DateLayout, s, time.UTC)
		if err != nil {
			continue
		}
		meetings = append(meetings, t)
	}

	if len(meetings) == 0 {
		return false
	}

	// Assign each deadline to its nearest meeting.
	aMeeting := nearestMeeting(*a.Terms.Deadline, meetings)
	bMeeting := nearestMeeting(*b.Terms.Deadline, meetings)

	// Both deadlines must share the same nearest meeting.
	return aMeeting != nil && bMeeting != nil && aMeeting.Equal(*bMeeting)
}

// splitCSV splits a comma-separated value string and trims whitespace.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// maxMeetingDistance is the maximum distance a deadline may be from a meeting
// to be assigned to it. This prevents out-of-calendar deadlines from bypassing
// the 7-day block check (e.g., a market with a 2028 deadline matching a 2027 meeting).
// Set to 30 days to accommodate Polymarket fallback deadlines like Dec 31 for a Dec 9 meeting.
const maxMeetingDistance = 30 * 24 * time.Hour

// nearestMeeting finds the meeting date nearest to the given deadline.
// Returns nil if no meeting is within maxMeetingDistance to prevent
// out-of-calendar deadlines from bypassing the 7-day block check.
func nearestMeeting(deadline time.Time, meetings []time.Time) *time.Time {
	if len(meetings) == 0 {
		return nil
	}

	nearest := meetings[0]
	minGap := abs(deadline.Sub(nearest))

	for i := 1; i < len(meetings); i++ {
		gap := abs(deadline.Sub(meetings[i]))
		if gap < minGap {
			minGap = gap
			nearest = meetings[i]
		}
	}

	// Return nil if the nearest meeting is beyond maxMeetingDistance
	if minGap > maxMeetingDistance {
		return nil
	}

	return &nearest
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// minNonNegativeUpperBound is the minimum upper bound of the target range when
// the range is non-negative (lowest ever 0-0.25%, openmarket.htm). This is used
// for the "no negative range" assumption in deriveNoNegativeRange.
var minNonNegativeUpperBound = decimal.RequireFromString("0.25")

// deriveNoNegativeRange handles level-vs-change Fed pairs where we don't have
// fed_upper_bound for the specific meeting but can still derive implies
// relationships based on the "no negative range" assumption (D-083, KXFED-27JAN).
//
// Math: new upper bound = floor(0.25) + change, so:
// - "No change" (eq 0): new bound = 0.25 + 0 = 0.25
// - "+25 bps" (eq 0.25): new bound >= 0.25 + 0.25 = 0.50
// - "+50+ bps" (gte 0.50): new bound >= 0.25 + 0.50 = 0.75
//
// This is a fallback for when the facts path cannot place the change on the
// ladder (e.g., different meeting from fed_next_meeting). It requires both
// deadlines to resolve on the same meeting to avoid cross-meeting dependencies.
func deriveNoNegativeRange(a, b canonical.Instrument, f facts.Set) (*derivation, derivationNote) {
	level, change, ok := fedSides(a, b)
	if !ok {
		return nil, derivationNote{}
	}

	// Both deadlines must resolve on the same meeting (avoids cross-meeting
	// dependencies: December's change depends on October's decision).
	// Record fed_meeting_dates use per FR-10/D-021.
	meetingsFact, ok := f.Get("fed_meeting_dates")
	if !ok {
		return nil, derivationNote{}
	}
	used := []canonical.FactUse{meetingsFact.Use()}
	if !onSameFedMeetingViaFacts(level, change, f) {
		return nil, derivationNote{facts: used}
	}

	pl, okL := propositionOf(level.Terms)
	pc, okC := propositionOf(change.Terms)
	if !okL || !okC {
		return nil, derivationNote{facts: used}
	}

	// Check if the change is one of the supported buckets: "no change" (eq 0),
	// "+25 bps" (eq 0.25), or "+50+ bps" (gte 0.50).
	var minResultingLevel decimal.Decimal
	var changeBucket string
	switch {
	case pc.iv.lo != nil && pc.iv.hi != nil && pc.iv.lo.Equal(*pc.iv.hi) && pc.iv.lo.IsZero() && !pc.negated:
		// "No change" (eq 0): new upper bound = 0.25 + 0 = 0.25
		minResultingLevel = minNonNegativeUpperBound
		changeBucket = "no change"
	case pc.iv.lo != nil && pc.iv.hi != nil && pc.iv.lo.Equal(*pc.iv.hi) && pc.iv.lo.Equal(decimal.RequireFromString("0.25")) && !pc.negated:
		// "+25 bps" (eq 0.25): new upper bound >= 0.25 + 0.25 = 0.50
		minResultingLevel = decimal.RequireFromString("0.50")
		changeBucket = "+25 bps"
	case pc.iv.lo != nil && pc.iv.lo.Equal(decimal.RequireFromString("0.50")) && pc.iv.hi == nil && !pc.negated:
		// "+50+ bps" (gte 0.50): new upper bound >= 0.25 + 0.50 = 0.75
		minResultingLevel = decimal.RequireFromString("0.75")
		changeBucket = "+50 bps or more"
	default:
		return nil, derivationNote{facts: used}
	}

	// Check if the level threshold is below the minimum resulting level.
	// The level proposition should be "gt X" (positive, lo-open, unbounded above).
	if pl.negated || pl.iv.lo == nil || !pl.iv.loOpen || pl.iv.hi != nil {
		return nil, derivationNote{facts: used}
	}

	// Check if minResultingLevel > pl.iv.lo (the change YES forces level YES)
	if !minResultingLevel.GreaterThan(*pl.iv.lo) {
		return nil, derivationNote{facts: used}
	}

	// Determine direction from fedSides: if a is level and b is change, then
	// change → level means b → a (forward=false).
	forward := a.ID == change.ID

	detail := fmt.Sprintf(
		"on a non-negative target range (upper bound at least %s), %s '%s' YES means the new upper bound is at least %s, "+
			"which forces %s 'above %s' YES; not the reverse (level YES also covers other buckets)",
		minNonNegativeUpperBound, change.ID, changeBucket, minResultingLevel, level.ID, pl.iv.lo)

	return &derivation{
		typ:     canonical.RelImplies,
		forward: forward,
		facts:   used, // records fed_meeting_dates per FR-10/D-021
		detail:  detail,
	}, derivationNote{facts: used}
}
