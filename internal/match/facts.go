package match

import (
	"fmt"
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
	base, baseOK := f.Get(FactFedUpperBound)
	if !baseOK {
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
