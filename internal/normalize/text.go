package normalize

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// maxTextBytes bounds how much of each text the regular expressions scan, so
// hostile venue text cannot make extraction slow (AB-07).
const maxTextBytes = 20000

// source is one piece of instrument text, scanned in priority order: rules
// first (the binding document), then the title.
type source struct {
	name string
	text string
}

func sourcesOf(in canonical.Instrument) []source {
	return []source{{"rules", clip(in.RulesText)}, {"title", clip(in.Title)}}
}

func clip(s string) string {
	if len(s) > maxTextBytes {
		return s[:maxTextBytes]
	}
	return s
}

// Amounts: "$76,600", "76599.99", "$150k", "1.5k". The digit counts are
// bounded so a hostile string cannot build a huge decimal.
const amountPattern = `(\$)?\s?(\d{1,3}(?:,\d{3})+|\d{1,12})(\.\d{1,8})?\s?([kK]\b)?`

var amountRe = regexp.MustCompile(amountPattern)

// amount is one parsed money-like number.
type amount struct {
	value   decimal.Decimal
	dollar  bool // a $ sign was present
	percent bool // a % sign followed the number
}

// parseAmount converts the submatches of amountRe into an amount. The matched
// text is followed by rest (used to spot a trailing %).
func parseAmount(sub []string, rest string) (amount, bool) {
	if len(sub) < 5 {
		return amount{}, false
	}
	digits := strings.ReplaceAll(sub[2], ",", "")
	if len(digits) > 12 {
		return amount{}, false
	}
	d, err := decimal.NewFromString(digits + sub[3])
	if err != nil {
		return amount{}, false
	}
	if sub[4] != "" {
		d = d.Mul(decimal.NewFromInt(1000))
	}
	return amount{value: d, dollar: sub[1] == "$", percent: strings.HasPrefix(strings.TrimSpace(rest), "%")}, true
}

// amountsIn returns every amount in text, in order.
func amountsIn(text string) []amount {
	var out []amount
	for _, loc := range amountRe.FindAllStringSubmatchIndex(text, -1) {
		sub := submatches(text, loc)
		if a, ok := parseAmount(sub, text[loc[1]:]); ok {
			out = append(out, a)
		}
	}
	return out
}

// submatches turns FindAllStringSubmatchIndex output into strings ("" for unmatched groups).
func submatches(text string, loc []int) []string {
	out := make([]string, len(loc)/2)
	for i := range out {
		if loc[2*i] >= 0 {
			out[i] = text[loc[2*i]:loc[2*i+1]]
		}
	}
	return out
}

// Comparator phrases. Inclusive phrases are listed before their exclusive
// stems so that, at the same position, "or above" wins over "above" and
// "equal to or greater than" wins over "greater than".
const (
	gtePhrases = `at or above|or above|or higher|or more|or greater|equal to or (?:greater|higher|more) than|` +
		`(?:greater|higher|more) than or equal to|at least|no less than`
	ltePhrases = `at or below|or below|or lower|or less|equal to or (?:less|lower) than|` +
		`(?:less|lower) than or equal to|at most|no more than`
	gtPhrases = `above|higher than|greater than|more than|exceeds?|over`
	ltPhrases = `below|lower than|less than|under`
)

var (
	// phraseAmountRe: "<phrase> [the price of] <amount>", e.g. "above 76599.99", "less than $76,000".
	phraseAmountRe = regexp.MustCompile(`(?i)\b(` + gtePhrases + `|` + ltePhrases + `|` + gtPhrases + `|` + ltPhrases +
		`)\s+(?:the\s+)?(?:price\s+of\s+)?(` + amountPattern + `)`)
	// amountPhraseRe: "<amount> or above", e.g. "$76,600 or above", "$150k or higher".
	amountPhraseRe = regexp.MustCompile(`(?i)(` + amountPattern + `)\s+(or above|or higher|or more|or greater|or below|or lower|or less)\b`)
	// phraseTitleRe: "<phrase> the price specified in the title" (Polymarket style); the amount is in the title.
	phraseTitleRe = regexp.MustCompile(`(?i)\b(` + gtePhrases + `|` + ltePhrases + `|` + gtPhrases + `|` + ltPhrases +
		`)\s+the\s+(?:price|value|level|amount|number)\s+(?:specified|stated|listed|mentioned|given)\s+in\s+the\s+title`)
	// betweenRe: "between $76,000 and $78,000".
	betweenRe = regexp.MustCompile(`(?i)\bbetween\s+(` + amountPattern + `)\s+and\s+(` + amountPattern + `)`)
	// negationRe: a negation just before a comparator phrase flips polarity.
	negationRe = regexp.MustCompile(`(?i)\b(?:not|never|no|fails?\s+to|failed\s+to|won't|will\s+not|does\s+not|doesn't|is\s+not|isn't)\s*$`)

	gteRe = regexp.MustCompile(`(?i)^(?:` + gtePhrases + `)$`)
	lteRe = regexp.MustCompile(`(?i)^(?:` + ltePhrases + `)$`)
	gtRe  = regexp.MustCompile(`(?i)^(?:` + gtPhrases + `)$`)
	ltRe  = regexp.MustCompile(`(?i)^(?:` + ltPhrases + `)$`)
)

// negationWindow bounds how far before a phrase a negation is looked for, so
// the check is constant-time per match however long the text is.
const negationWindow = 40

// negated reports whether a negation ends just before position pos.
func negated(text string, pos int) bool {
	return negationRe.MatchString(text[max(0, pos-negationWindow):pos])
}

// comparatorOf maps a matched phrase to its comparator.
func comparatorOf(phrase string) canonical.Comparator {
	phrase = strings.TrimSpace(phrase)
	switch {
	case gteRe.MatchString(phrase):
		return canonical.ComparatorGTE
	case lteRe.MatchString(phrase):
		return canonical.ComparatorLTE
	case gtRe.MatchString(phrase):
		return canonical.ComparatorGT
	case ltRe.MatchString(phrase):
		return canonical.ComparatorLT
	}
	return canonical.ComparatorUnknown
}

// condition is one comparator with its threshold(s) as stated in a text.
type condition struct {
	comparator canonical.Comparator
	threshold  *decimal.Decimal // nil when the text defers to the title
	high       *decimal.Decimal // upper bound for between
	percent    bool
	negated    bool
}

// key identifies a condition for de-duplication.
func (c condition) key() string {
	k := string(c.comparator)
	if c.threshold != nil {
		k += "|" + c.threshold.String()
	}
	if c.high != nil {
		k += "|" + c.high.String()
	}
	if c.negated {
		k += "|neg"
	}
	return k
}

// conditionsIn finds every comparator statement in text, in order.
func conditionsIn(text string) []condition {
	var out []condition
	for _, loc := range betweenRe.FindAllStringSubmatchIndex(text, -1) {
		sub := submatches(text, loc)
		lo, okLo := parseAmount(sub[1:6], "")
		hi, okHi := parseAmount(sub[6:11], text[loc[1]:])
		if okLo && okHi && !hi.value.LessThan(lo.value) {
			out = append(out, condition{comparator: canonical.ComparatorBetween, threshold: &lo.value, high: &hi.value,
				percent: hi.percent, negated: negated(text, loc[0])})
		}
	}
	for _, loc := range phraseAmountRe.FindAllStringSubmatchIndex(text, -1) {
		sub := submatches(text, loc)
		a, ok := parseAmount(sub[2:7], text[loc[1]:])
		if !ok {
			continue
		}
		out = append(out, condition{comparator: comparatorOf(sub[1]), threshold: &a.value, percent: a.percent,
			negated: negated(text, loc[0])})
	}
	for _, loc := range amountPhraseRe.FindAllStringSubmatchIndex(text, -1) {
		sub := submatches(text, loc)
		a, ok := parseAmount(sub[1:6], "")
		if !ok {
			continue
		}
		out = append(out, condition{comparator: comparatorOf(sub[6]), threshold: &a.value, percent: a.percent,
			negated: negated(text, loc[0])})
	}
	for _, loc := range phraseTitleRe.FindAllStringSubmatchIndex(text, -1) {
		sub := submatches(text, loc)
		out = append(out, condition{comparator: comparatorOf(sub[1]), negated: negated(text, loc[0])})
	}
	return out
}

// Zone abbreviations venues use, mapped to IANA names so DST resolves correctly.
var zones = map[string]string{
	"et": "America/New_York", "est": "America/New_York", "edt": "America/New_York",
	"ct": "America/Chicago", "cst": "America/Chicago", "cdt": "America/Chicago",
	"mt": "America/Denver", "mst": "America/Denver", "mdt": "America/Denver",
	"pt": "America/Los_Angeles", "pst": "America/Los_Angeles", "pdt": "America/Los_Angeles",
	"utc": "UTC", "gmt": "UTC",
}

const zonePattern = `(ET|EST|EDT|CT|CST|CDT|MT|MST|MDT|PT|PST|PDT|UTC|GMT)\b(?:\s+time(?:zone)?)?`

var (
	// clockRe: "5 PM EDT", "6pm EDT", "11:59PM ET", "12:00 in the ET timezone".
	clockRe = regexp.MustCompile(`(?i)\b(\d{1,2})(?::(\d{2}))?\s*(a\.?m\.?|p\.?m\.?)?\s*(?:in\s+the\s+)?` + zonePattern)
	// wordClockRe: "noon ET", "midnight ET".
	wordClockRe = regexp.MustCompile(`(?i)\b(noon|midnight)\s+(?:in\s+the\s+)?` + zonePattern)
)

// clock is a time of day in a named zone.
type clock struct {
	hour, minute int
	zone         string // IANA name
}

func (c clock) key() string {
	return strconv.Itoa(c.hour) + ":" + strconv.Itoa(c.minute) + "@" + c.zone
}

// clocksIn finds every "time + zone" statement in text. A time with neither
// am/pm nor minutes is ambiguous and skipped.
func clocksIn(text string) []clock {
	var out []clock
	for _, sub := range clockRe.FindAllStringSubmatch(text, -1) {
		hour, _ := strconv.Atoi(sub[1])
		minute := 0
		if sub[2] != "" {
			minute, _ = strconv.Atoi(sub[2])
		}
		meridiem := strings.ToLower(strings.ReplaceAll(sub[3], ".", ""))
		switch {
		case meridiem == "" && sub[2] == "":
			continue // "5 ET": ambiguous
		case meridiem == "" && hour > 23:
			continue
		case meridiem != "" && (hour < 1 || hour > 12):
			continue
		case meridiem == "am" && hour == 12:
			hour = 0
		case meridiem == "pm" && hour != 12:
			hour += 12
		}
		if minute > 59 {
			continue
		}
		out = append(out, clock{hour: hour, minute: minute, zone: zones[strings.ToLower(sub[4])]})
	}
	for _, sub := range wordClockRe.FindAllStringSubmatch(text, -1) {
		c := clock{zone: zones[strings.ToLower(sub[2])]}
		if strings.EqualFold(sub[1], "noon") {
			c.hour = 12
		}
		out = append(out, c)
	}
	return out
}

var months = map[string]time.Month{
	"jan": time.January, "feb": time.February, "mar": time.March, "apr": time.April, "may": time.May, "jun": time.June,
	"jul": time.July, "aug": time.August, "sep": time.September, "oct": time.October, "nov": time.November, "dec": time.December,
}

var (
	// monthDayRe: "Oct 6, 2026", "October 7", "December 31st, 2026".
	monthDayRe = regexp.MustCompile(`(?i)\b(jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|` +
		`sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?)\.?\s+(\d{1,2})(?:st|nd|rd|th)?(?:,?\s+(\d{4}))?\b`)
	// isoDateRe: "2026-10-06".
	isoDateRe = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
)

// calendarDate is a month and day with an optional year (0 when not stated).
type calendarDate struct {
	year  int
	month time.Month
	day   int
}

func (d calendarDate) key() string {
	return strconv.Itoa(d.year) + "-" + strconv.Itoa(int(d.month)) + "-" + strconv.Itoa(d.day)
}

// valid reports whether the day exists in the month (any year when year is 0).
func (d calendarDate) valid() bool {
	y := d.year
	if y == 0 {
		y = 2000 // a leap year, so Feb 29 is allowed when the year is unknown
	}
	t := time.Date(y, d.month, d.day, 0, 0, 0, 0, time.UTC)
	return d.day >= 1 && t.Month() == d.month && t.Day() == d.day
}

// datesIn finds every calendar date in text, in order.
func datesIn(text string) []calendarDate {
	var out []calendarDate
	for _, sub := range monthDayRe.FindAllStringSubmatch(text, -1) {
		d := calendarDate{month: months[strings.ToLower(sub[1])[:3]]}
		d.day, _ = strconv.Atoi(sub[2])
		if sub[3] != "" {
			d.year, _ = strconv.Atoi(sub[3])
		}
		if d.valid() {
			out = append(out, d)
		}
	}
	for _, sub := range isoDateRe.FindAllStringSubmatch(text, -1) {
		y, _ := strconv.Atoi(sub[1])
		m, _ := strconv.Atoi(sub[2])
		d := calendarDate{year: y, month: time.Month(m)}
		d.day, _ = strconv.Atoi(sub[3])
		if m >= 1 && m <= 12 && d.valid() {
			out = append(out, d)
		}
	}
	return out
}

// single returns the one distinct item in items (by key), or false when there
// are none or several. Several distinct statements of the same field are
// ambiguous, and ambiguity is recorded as unknown.
func single[T any](items []T, key func(T) string) (T, bool, bool) {
	var (
		zero  T
		first T
		seen  string
	)
	for i, it := range items {
		k := key(it)
		if i == 0 {
			first, seen = it, k
			continue
		}
		if k != seen {
			return zero, false, true
		}
	}
	if len(items) == 0 {
		return zero, false, false
	}
	return first, true, false
}

// firstStated scans the sources in order and returns the first one that states
// the field exactly once. A source that states it several ways is ambiguous,
// and the scan stops there: a later source must not override the binding text.
func firstStated[T any](srcs []source, find func(string) []T, key func(T) string) (val T, ok bool, ambiguousIn string) {
	for _, s := range srcs {
		v, found, ambiguous := single(find(s.text), key)
		if ambiguous {
			var zero T
			return zero, false, s.name
		}
		if found {
			return v, true, ""
		}
	}
	var zero T
	return zero, false, ""
}
