package normalize

import (
	"regexp"
	"strings"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Fed funds rate markets come in two shapes (domain.md): Kalshi ladders on the
// level of the upper bound after a meeting ("above 4.25% following the Fed's
// Oct 28, 2026 meeting") and Polymarket buckets on the change decided at the
// meeting ("increase interest rates by 25 bps after the October 2026 meeting").
//
// Field sources (D-060 precedence: rules first, title second):
//   - subject: SubjectFedFunds when the text names the Fed / FOMC / federal
//     funds rate and the title is not about something else that carries the
//     venue's fed tag (Treasury yields, Fed personnel, dissent counts, odds
//     listings). Otherwise unknown.
//   - metric: MetricRateUpperBound when a level condition on the upper bound
//     is stated and MetricRateChange when a change condition is stated or the
//     rules resolve "to the amount of basis points ... changed by" (the bucket
//     is then read from the title). A text that states both, names the lower
//     bound, or is path-dependent ("at any point", "reach", "before 2027"), a
//     count ("2 rate cuts in 2026") or a sequence ("Hike-Pause-Hike") is
//     unknown: the matcher must never pair those with a meeting-level ladder.
//   - comparator, threshold, unit: every threshold is in percentage points
//     (UnitPercent), the one unit this extractor produces. A level "4.25%" is
//     4.25. A change is signed, + for a hike: "25 bps increase" is eq 0.25,
//     "50+ bps decrease" is lte -0.50, "no change" is eq 0. "N+", "at least
//     N" and "N or more" are gte in magnitude; "more than N" is gt. Other
//     qualifiers ("up to", "at most") are not parsed and stay unknown.
//   - deadline: the meeting date stated in the rules (the last day of a
//     "October 27-28, 2026" range) with the time and zone the rules state
//     (Kalshi: "2:05 PM ET"). Without a full date + time + zone the close time
//     is used and flagged deadline_defaulted. Several dates is ambiguous.
//   - resolution source: federalreserve_gov for the Federal Reserve's website
//     and FOMC statement aliases; other named sources get their own IDs;
//     several candidates without an authority cue is unknown.

// Subject and metrics this extractor produces.
const (
	SubjectFedFunds      = "fed_funds_rate"   // the FOMC's target federal funds rate range
	MetricRateUpperBound = "rate_upper_bound" // the upper bound of the target range after the meeting, in percent
	MetricRateChange     = "rate_change"      // the signed change of the upper bound at the meeting, in percentage points
)

// SourceFederalReserve is the resolution source ID for federalreserve.gov and
// FOMC statements.
const SourceFederalReserve = "federalreserve_gov"

func init() { Register(canonical.CategoryFed, extractFed) }

// bpsPerPercent converts basis points to percentage points.
var bpsPerPercent = decimal.NewFromInt(100)

const (
	fedMonthPattern = `(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|` +
		`sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)`
	// pctPattern: a rate level such as "4.25%" or "0.00 %". Levels are small,
	// so the digits are bounded tightly.
	pctPattern = `(\d{1,3}(?:\.\d{1,4})?)\s?%`
	// bpsPattern: a change size, "25 bps", "50+ bps", "25 basis points", "0.25%".
	bpsPattern = `(\d{1,3}(?:\.\d{1,2})?)(\+)?\s?(bps|bp|basis\s+points?|%|percentage\s+points?)`
	upWords    = `increases?|increased|increasing|raises?|raised|raising|hikes?|hiked|hiking`
	downWords  = `decreases?|decreased|decreasing|cuts?|cutting|lowers?|lowered|lowering|reduces?|reduced|reducing`
	changeNoun = `increase|decrease|cut|hike|raise|reduction|rise`
	postPhrase = `or above|or higher|or more|or greater|or below|or lower|or less`
)

var (
	// fedAliasRe: the text is about the Fed's target rate.
	fedAliasRe = regexp.MustCompile(`(?i)\b(?:fed|fomc|federal\s+reserve|federal\s+funds|fed\s+funds|federal\s+open\s+market)\b`)
	// fedOffTopicRe: title words that mean the market is not about the target rate itself.
	fedOffTopicRe = regexp.MustCompile(`(?i)\b(?:treasury|yields?|powell|cook|waller|governors?|chair(?:man|woman)?|dissents?|dissenting|` +
		`jail|charged|indict\w*|fired?|fires|resign\w*|nominat\w*|confirm\w*|odds|favou?red|fedwatch|probabilit\w*|balance\s+sheet|qe|qt)\b`)
	// fedPathRe: the proposition is about the path of the rate over a window, not its level after one meeting.
	fedPathRe = regexp.MustCompile(`(?i)\b(?:at\s+any\s+point|any\s+point\s+by|reach(?:es|ed)?|hits?|hit\s+before|emergency)\b`)
	// fedCountRe: the proposition counts cuts or hikes.
	fedCountRe = regexp.MustCompile(`(?i)\b(?:how\s+many|(?:\d+|no|zero|one|two|three|four)\s+(?:fed\s+)?rate\s+(?:cuts?|hikes?)\s+(?:happen|occur|in|by)|` +
		`(?:number|amount)\s+of\s+(?:rate\s+)?(?:cuts|hikes))\b`)
	// fedSequenceRe: the proposition is a sequence of decisions.
	fedSequenceRe = regexp.MustCompile(`(?i)\b(?:next\s+(?:two|three|four|\d)\s+decisions|pause)\b`)
	// upperBoundRe / lowerBoundRe: which bound of the target range the text resolves on.
	upperBoundRe = regexp.MustCompile(`(?i)\bupper\s+bound\b`)
	lowerBoundRe = regexp.MustCompile(`(?i)\blower\s+bound\b`)
	// changeDeferralRe: the rules resolve to the bucket named in the title.
	changeDeferralRe = regexp.MustCompile(`(?i)\bamount\s+of\s+basis\s+points\b[^.]{0,120}\bchanged\s+by\b`)

	// Level conditions.
	levelPhraseRe = regexp.MustCompile(`(?i)\b(` + gtePhrases + `|` + ltePhrases + `|` + gtPhrases + `|` + ltPhrases +
		`)\s+(?:the\s+)?(?:level\s+of\s+)?` + pctPattern)
	levelPostRe    = regexp.MustCompile(`(?i)\b` + pctPattern + `\s+(` + postPhrase + `)\b`)
	levelBetweenRe = regexp.MustCompile(`(?i)\bbetween\s+` + pctPattern + `\s+and\s+` + pctPattern)
	levelEqRe      = regexp.MustCompile(`(?i)\b(?:be|equal\s+to|equals)\s+` + pctPattern)
	levelEqPostRe  = regexp.MustCompile(`(?i)^\s*(?:` + postPhrase + `)\b`)

	// Change conditions.
	changeByRe = regexp.MustCompile(`(?i)\b(` + upWords + `|` + downWords + `)\s+(?:(?:the|its|their|interest|fed|target|federal|funds|rates?|range)\s+){0,6}` +
		`by\s+(at\s+least\s+|more\s+than\s+)?` + bpsPattern + `(\s+or\s+more)?`)
	changeNounRe = regexp.MustCompile(`(?i)\b` + bpsPattern + `\s+(` + changeNoun + `)\b`)
	noChangeRe   = regexp.MustCompile(`(?i)(["“'‘]?)\b(?:no\s+change|unchanged|holds?\s+(?:rates?\s+)?steady)\b`)
	upWordRe     = regexp.MustCompile(`(?i)^(?:` + upWords + `|increase|hike|raise|rise)$`)

	// exampleRe: a condition inside an illustration ("for example, if the Fed
	// cuts rates by 50 bps ...") is not the market's condition.
	exampleRe = regexp.MustCompile(`(?i)\b(?:for\s+example|e\.g\.|such\s+as|for\s+instance)\b[^.]*$`)

	// Meeting dates: "October 27-28, 2026", "Jan 26 to 27, 2027" resolve on the last day.
	meetingRangeRe = regexp.MustCompile(`(?i)\b` + fedMonthPattern + `\.?\s+(\d{1,2})(?:st|nd|rd|th)?\s*(?:-|–|—|to|through)\s*(\d{1,2})(?:st|nd|rd|th)?(?:,?\s+(\d{4}))?\b`)
)

// fedSourceNames maps resolution-source text to canonical IDs.
var fedSourceNames = []sourceName{
	{regexp.MustCompile(`(?i)federalreserve\.gov|federal\s+reserve['’]?s?\s+(?:official\s+)?website|official\s+website\s+of\s+the\s+federal\s+reserve|` +
		`fomc['’]?s?\s+statements?|federal\s+reserve\s+statement|official\s+(?:information|announcements?)\s+from\s+the\s+federal\s+reserve`), SourceFederalReserve},
	{regexp.MustCompile(`(?i)credible\s+(?:news\s+)?(?:sources?|reporting)`), "credible_reporting"},
	{regexp.MustCompile(`(?i)\bfedwatch\b`), "cme_fedwatch"},
	{regexp.MustCompile(`(?i)treasury\.gov|department\s+of\s+the\s+treasury`), "treasury_gov"},
	{regexp.MustCompile(`(?i)predictionmarketodds`), "predictionmarketodds"},
	{regexp.MustCompile(`(?i)\bu\.?s\.?\s+government\b`), "us_government"},
}

func extractFed(in canonical.Instrument) (canonical.Terms, canonical.Quality) {
	var q canonical.Quality
	t := canonical.UnknownTerms()
	srcs := sourcesOf(in)
	rules, title := srcs[0].text, srcs[1].text
	all := rules + "\n" + title

	t.Subject = fedSubject(title, all)
	level, levelOK, levelAmbiguous := firstStated(srcs, levelConditionsIn, condition.key)
	change, changeOK, changeAmbiguous := firstStated(srcs, changeConditionsIn, condition.key)
	switch {
	case levelAmbiguous != "":
		q.Add(QualityTermsAmbiguous, "comparator", "the "+levelAmbiguous+" text states more than one rate level")
	case changeAmbiguous != "":
		q.Add(QualityTermsAmbiguous, "comparator", "the "+changeAmbiguous+" text states more than one rate change")
	case levelOK && changeOK:
		q.Add(QualityTermsAmbiguous, "metric", "the text states both a rate level and a rate change")
	case changeOK:
		fedSetCondition(&t, change)
	case levelOK:
		fedSetCondition(&t, level)
	}
	ambiguous := levelAmbiguous != "" || changeAmbiguous != "" || (levelOK && changeOK)
	if !ambiguous {
		t.Metric = fedMetric(title, rules, levelOK, changeOK)
	}
	textDeadline(&t, &q, in.CloseTime, srcs, meetingDatesIn)
	t.ResolutionSource = namedSource(all, fedSourceNames, nil)
	return t, q
}

// fedSubject returns SubjectFedFunds when the text names the Fed and the title
// is about the rate itself, else unknown.
func fedSubject(title, all string) string {
	if fedOffTopicRe.MatchString(title) || !fedAliasRe.MatchString(all) {
		return canonical.Unknown
	}
	return SubjectFedFunds
}

// fedMetric decides whether the market is on the level of the upper bound or
// on the change decided at the meeting, or unknown.
func fedMetric(title, rules string, levelOK, changeOK bool) string {
	if fedOffTopicRe.MatchString(title) || fedCountRe.MatchString(title) || fedCountRe.MatchString(rules) ||
		fedSequenceRe.MatchString(title) || fedPathRe.MatchString(title) || fedPathRe.MatchString(rules) {
		return canonical.Unknown
	}
	switch {
	case changeOK || changeDeferralRe.MatchString(rules):
		return MetricRateChange
	case levelOK && upperBoundRe.MatchString(rules+"\n"+title) && !lowerBoundRe.MatchString(rules+"\n"+title):
		return MetricRateUpperBound
	}
	return canonical.Unknown
}

// fedSetCondition copies a parsed condition into t. Every fed threshold is in
// percentage points.
func fedSetCondition(t *canonical.Terms, c condition) {
	t.Comparator = c.comparator
	t.Threshold = c.threshold
	t.ThresholdHigh = c.high
	t.Unit = UnitPercent
	t.Polarity = canonical.PolarityPositive
	if c.negated {
		t.Polarity = canonical.PolarityNegative
	}
}

// exampleWindow bounds how far before a condition an illustration cue is
// looked for, so the check is constant-time per match.
const exampleWindow = 60

// illustrative reports whether the condition at pos sits in an example clause.
func illustrative(text string, pos int) bool {
	return exampleRe.MatchString(text[max(0, pos-exampleWindow):pos])
}

// parsePercent converts a bounded digit string to a decimal.
func parsePercent(digits string) (decimal.Decimal, bool) {
	d, err := decimal.NewFromString(digits)
	return d, err == nil
}

// changeSpans returns the byte ranges of every change statement in text, so
// the percent inside "cut rates by at least 0.25%" is not also read as a level.
func changeSpans(text string) [][]int {
	spans := changeByRe.FindAllStringIndex(text, -1)
	return append(spans, changeNounRe.FindAllStringIndex(text, -1)...)
}

// overlaps reports whether [start, end) intersects any span.
func overlaps(spans [][]int, start, end int) bool {
	for _, s := range spans {
		if start < s[1] && s[0] < end {
			return true
		}
	}
	return false
}

// levelConditionsIn finds every rate-level statement in text, in order:
// "greater than 2.75%", "5.0% or higher", "between 3.25% and 3.50%", "be 2.25%".
func levelConditionsIn(text string) []condition {
	var out []condition
	spans := changeSpans(text)
	for _, loc := range levelBetweenRe.FindAllStringSubmatchIndex(text, -1) {
		if illustrative(text, loc[0]) || overlaps(spans, loc[0], loc[1]) {
			continue
		}
		sub := submatches(text, loc)
		lo, okLo := parsePercent(sub[1])
		hi, okHi := parsePercent(sub[2])
		if okLo && okHi && !hi.LessThan(lo) {
			out = append(out, condition{comparator: canonical.ComparatorBetween, threshold: &lo, high: &hi, percent: true,
				negated: negated(text, loc[0])})
		}
	}
	for _, loc := range levelPhraseRe.FindAllStringSubmatchIndex(text, -1) {
		if illustrative(text, loc[0]) || overlaps(spans, loc[0], loc[1]) {
			continue
		}
		sub := submatches(text, loc)
		if d, ok := parsePercent(sub[2]); ok {
			out = append(out, condition{comparator: comparatorOf(sub[1]), threshold: &d, percent: true, negated: negated(text, loc[0])})
		}
	}
	for _, loc := range levelPostRe.FindAllStringSubmatchIndex(text, -1) {
		if illustrative(text, loc[0]) || overlaps(spans, loc[0], loc[1]) {
			continue
		}
		sub := submatches(text, loc)
		if d, ok := parsePercent(sub[1]); ok {
			out = append(out, condition{comparator: comparatorOf(sub[2]), threshold: &d, percent: true, negated: negated(text, loc[0])})
		}
	}
	for _, loc := range levelEqRe.FindAllStringSubmatchIndex(text, -1) {
		if illustrative(text, loc[0]) || overlaps(spans, loc[0], loc[1]) {
			continue
		}
		sub := submatches(text, loc)
		if levelEqPostRe.MatchString(text[loc[1]:]) {
			continue // "be 4.25% or higher" is the post-phrase form
		}
		if d, ok := parsePercent(sub[1]); ok {
			out = append(out, condition{comparator: canonical.ComparatorEQ, threshold: &d, percent: true, negated: negated(text, loc[0])})
		}
	}
	return out
}

// changeConditionsIn finds every rate-change statement in text, in order:
// "increase interest rates by 25 bps", "cut rates by at least 50 basis
// points", "50+ bps decrease", "no change". The threshold is the signed change
// in percentage points.
func changeConditionsIn(text string) []condition {
	var out []condition
	for _, loc := range changeByRe.FindAllStringSubmatchIndex(text, -1) {
		if illustrative(text, loc[0]) {
			continue
		}
		sub := submatches(text, loc)
		// sub: 1 direction, 2 "at least "/"more than ", 3 digits, 4 "+", 5 unit, 6 " or more"
		magnitude, ok := changeMagnitude(sub[3], sub[5])
		if !ok {
			continue
		}
		comp := canonical.ComparatorEQ
		switch {
		case strings.HasPrefix(strings.ToLower(strings.TrimSpace(sub[2])), "more"):
			comp = canonical.ComparatorGT
		case sub[2] != "" || sub[4] != "" || sub[6] != "":
			comp = canonical.ComparatorGTE
		}
		out = append(out, signedChange(sub[1], comp, magnitude, negated(text, loc[0])))
	}
	for _, loc := range changeNounRe.FindAllStringSubmatchIndex(text, -1) {
		if illustrative(text, loc[0]) {
			continue
		}
		sub := submatches(text, loc)
		// sub: 1 digits, 2 "+", 3 unit, 4 noun
		magnitude, ok := changeMagnitude(sub[1], sub[3])
		if !ok {
			continue
		}
		comp := canonical.ComparatorEQ
		if sub[2] != "" {
			comp = canonical.ComparatorGTE
		}
		out = append(out, signedChange(sub[4], comp, magnitude, negated(text, loc[0])))
	}
	for _, loc := range noChangeRe.FindAllStringSubmatchIndex(text, -1) {
		sub := submatches(text, loc)
		if sub[1] != "" || illustrative(text, loc[0]) {
			continue // a quoted "No change" names a bracket, it does not state the condition
		}
		zero := decimal.Zero
		out = append(out, condition{comparator: canonical.ComparatorEQ, threshold: &zero, percent: true, negated: negated(text, loc[0])})
	}
	return out
}

// changeMagnitude converts a change size to percentage points: bps are
// divided by 100, percent and percentage points are used as written.
func changeMagnitude(digits, unit string) (decimal.Decimal, bool) {
	d, err := decimal.NewFromString(digits)
	if err != nil {
		return decimal.Zero, false
	}
	u := strings.ToLower(unit)
	if strings.HasPrefix(u, "bp") || strings.HasPrefix(u, "basis") {
		d = d.Div(bpsPerPercent)
	}
	return d, true
}

// signedChange builds a change condition: a hike is positive, a cut negative,
// and the comparator flips with the sign ("50+ bps decrease" is lte -0.50).
func signedChange(direction string, comp canonical.Comparator, magnitude decimal.Decimal, neg bool) condition {
	if upWordRe.MatchString(direction) {
		return condition{comparator: comp, threshold: &magnitude, percent: true, negated: neg}
	}
	value := magnitude.Neg()
	switch comp {
	case canonical.ComparatorGTE:
		comp = canonical.ComparatorLTE
	case canonical.ComparatorGT:
		comp = canonical.ComparatorLT
	}
	return condition{comparator: comp, threshold: &value, percent: true, negated: neg}
}

// meetingDatesIn finds every calendar date in text, reading a meeting range
// such as "October 27-28, 2026" as its last day (the decision day).
func meetingDatesIn(text string) []calendarDate {
	return datesIn(meetingRangeRe.ReplaceAllString(text, "$1 $3, $4"))
}
