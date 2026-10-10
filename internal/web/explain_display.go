package web

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Display-only rewrites of the matcher's stored text (M6 P4). The matcher
// writes Go durations ("526h54m0s") and, in related explanations, always
// says "(within 168h0m0s)" even when a Fed meeting exception let a pair
// through from outside the 7-day blocking window (internal/match block.go).
// The stored relationship is never changed; these helpers only change how
// it reads.

// factFedMeetingDates is the fact whose use in blocking lets a Fed pair whose
// deadlines are more than BlockWindow apart be compared anyway (match
// block.go: both deadlines sit nearest the same FOMC meeting).
const factFedMeetingDates = "fed_meeting_dates"

// factFedUpperBound is the fact the Fed level-vs-change rule consults to
// place a change bucket on the level ladder (match facts.go).
const factFedUpperBound = "fed_upper_bound"

var (
	// withinRe matches the matcher's "deadlines <gap> apart (within <window>)".
	withinRe = regexp.MustCompile(`deadlines (\S+) apart \(within (\S+)\)`)
	// goDurRe matches the Go duration strings the matcher writes after
	// rounding to the minute, such as "526h54m0s" or "45m0s".
	goDurRe = regexp.MustCompile(`\b(?:\d+h)?\d+m\d+s\b`)
)

// humanSpan formats a duration for people: "21 days 22 h", "5 h 30 min",
// "45 min". Days drop the minutes; a zero span reads "0 min".
func humanSpan(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	days := int(d / (24 * time.Hour))
	hours := int(d % (24 * time.Hour) / time.Hour)
	mins := int(d % time.Hour / time.Minute)
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%d day%s %d h", days, pluralS(days), hours)
	case days > 0:
		return fmt.Sprintf("%d day%s", days, pluralS(days))
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%d h %d min", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%d h", hours)
	}
	return fmt.Sprintf("%d min", mins)
}

// pluralS returns "s" if n != 1, else "".
func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// hasFact reports whether key is among the facts the relationship used.
func hasFact(used []canonical.FactUse, key string) bool {
	for _, fu := range used {
		if fu.Key == key {
			return true
		}
	}
	return false
}

// readableExplanation rewrites a stored explanation for display: the
// "deadlines X apart (within W)" clause says "within" only when it is true,
// and when the gap is outside the window it says why the pair was compared
// (the Fed meeting exception, recorded as fact fed_meeting_dates). Every
// other Go duration is reformatted with humanSpan.
func readableExplanation(expl string, used []canonical.FactUse) string {
	out := withinRe.ReplaceAllStringFunc(expl, func(m string) string {
		sub := withinRe.FindStringSubmatch(m)
		gap, err1 := time.ParseDuration(sub[1])
		window, err2 := time.ParseDuration(sub[2])
		if err1 != nil || err2 != nil {
			return m
		}
		return deadlineClause(gap, window, used)
	})
	return humanizeDurations(out)
}

// deadlineClause states a deadline gap against a window truthfully.
func deadlineClause(gap, window time.Duration, used []canonical.FactUse) string {
	head := fmt.Sprintf("deadlines %s apart", humanSpan(gap))
	if gap <= window {
		return fmt.Sprintf("%s (within %s)", head, humanSpan(window))
	}
	if hasFact(used, factFedMeetingDates) {
		return fmt.Sprintf("%s (window %s; compared anyway because fact %s puts both deadlines on the same FOMC meeting)",
			head, humanSpan(window), factFedMeetingDates)
	}
	return fmt.Sprintf("%s (window %s; the stored record does not say why the pair was compared)", head, humanSpan(window))
}

// humanizeDurations replaces Go duration strings in s with humanSpan.
func humanizeDurations(s string) string {
	return goDurRe.ReplaceAllStringFunc(s, func(m string) string {
		d, err := time.ParseDuration(m)
		if err != nil {
			return m
		}
		return humanSpan(d)
	})
}

// factNote extracts the derivation note the matcher appends to a related
// explanation (" Facts: <reason>."), or "" when there is none. Related
// explanations put it after the veto list and before any "Uncertain:" tail.
func factNote(expl string) string {
	_, after, ok := strings.Cut(expl, " Facts: ")
	if !ok {
		return ""
	}
	if before, _, cut := strings.Cut(after, " Uncertain: "); cut {
		after = before
	}
	return strings.TrimSuffix(strings.TrimSpace(after), ".")
}

// factDidNotClose reports whether a related pair is one an external fact was
// consulted for but did not close: either no veto fired, or the Fed
// level-vs-change rule consulted fed_upper_bound (recorded in FactsUsed) and
// the pair still stayed related, e.g. a December level contract against a
// change market when fed_upper_bound is the level before October.
func factDidNotClose(r canonical.Relationship) bool {
	if len(r.Vetoes) == 0 {
		return true
	}
	return r.Type == canonical.RelRelated && hasFact(r.FactsUsed, factFedUpperBound)
}

// diffValue returns the diff entry for field, if any.
func diffValue(diff []canonical.FieldDiff, field string) (canonical.FieldDiff, bool) {
	for _, d := range diff {
		if d.Field == field {
			return d, true
		}
	}
	return canonical.FieldDiff{}, false
}

// termGap returns "how close" a near miss was, with units: a threshold gap in
// the contracts' unit (USD, percentage points with bps for rates), or the
// time between deadlines. Thresholds of different metrics (a rate level and a
// rate change) are not comparable, so no number is shown for them.
func termGap(diff []canonical.FieldDiff) string {
	if m, ok := diffValue(diff, "metric"); ok && m.Status == canonical.DiffDiffer {
		return "n/a: different measures (" + m.A + " vs " + m.B + ")"
	}
	if th, ok := diffValue(diff, "threshold"); ok && th.Status == canonical.DiffDiffer {
		if g, ok := thresholdGap(th, diff); ok {
			return g
		}
	}
	if dl, ok := diffValue(diff, "deadline"); ok && dl.Status == canonical.DiffDiffer {
		ta, err1 := time.Parse(time.RFC3339, dl.A)
		tb, err2 := time.Parse(time.RFC3339, dl.B)
		if err1 == nil && err2 == nil {
			return "deadlines " + humanSpan(ta.Sub(tb)) + " apart"
		}
	}
	return "—"
}

// thresholdGap renders the absolute threshold difference with its unit.
func thresholdGap(th canonical.FieldDiff, diff []canonical.FieldDiff) (string, bool) {
	a, err1 := decimal.NewFromString(th.A)
	b, err2 := decimal.NewFromString(th.B)
	if err1 != nil || err2 != nil {
		return "", false
	}
	gap := a.Sub(b).Abs()
	unit, ok := diffValue(diff, "unit")
	if !ok || unit.Status != canonical.DiffMatch {
		if ok && unit.Status == canonical.DiffDiffer {
			return gap.String() + " (units differ: " + unit.A + " vs " + unit.B + ")", true
		}
		return gap.String() + " (unit unknown)", true
	}
	subject, _ := diffValue(diff, "subject")
	return withUnit(gap, unit.A, subject.A), true
}

// withUnit formats a threshold gap in its unit. Percent gaps are percentage
// points; for the Fed funds rate they are also shown in basis points.
func withUnit(gap decimal.Decimal, unit, subject string) string {
	switch unit {
	case "USD":
		return gap.StringFixed(2) + " USD"
	case "percent":
		s := gap.String() + " pp"
		if subject == "fed_funds_rate" {
			s += " (" + gap.Mul(decimal.NewFromInt(100)).String() + " bps)"
		}
		return s
	case "bps":
		return gap.String() + " bps"
	}
	return gap.String() + " " + unit
}
