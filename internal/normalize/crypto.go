package normalize

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Crypto threshold markets: "BTC above $X at time T on date D, per source S".
//
// Field sources (D-060):
//   - subject: the asset named in the title; several assets (ETH/BTC) is unknown.
//   - metric: "price" (the price at the deadline instant), "price_high" or
//     "price_low" (the extreme over the window, for "hit"/"reach"/"dip to"
//     markets), cross-checked against the rules' "High"/"Low"/"Close" wording.
//   - comparator and threshold: the rules text first, then the title. When the
//     rules defer to "the price specified in the title", the title's single $
//     amount is used.
//   - deadline: date, time and zone from the rules first, then the title. A
//     missing year is the one nearest the close time (flagged). Without a full
//     date + time + zone, the close time is used and flagged.
//   - resolution source: a named index or exchange the text makes
//     authoritative; several candidates is unknown.

// Metrics this extractor produces.
const (
	MetricPrice     = "price"      // the price at the deadline instant
	MetricPriceHigh = "price_high" // the highest price reached by the deadline
	MetricPriceLow  = "price_low"  // the lowest price reached by the deadline
)

// Threshold units this package produces.
const (
	UnitUSD     = "USD"     // $ amounts
	UnitPercent = "percent" // percentages and (for fed) percentage points
	UnitCount   = "count"   // integer counts (e.g., number of rate cuts)
)

func init() { Register(canonical.CategoryCrypto, extractCrypto) }

// Deadline sanity window: a parsed deadline further than this from the close
// time is a misread, and the close time is used instead.
const deadlineSanityWindow = 7 * 24 * time.Hour

// assetAliases maps title words to canonical subjects.
var assetAliases = []struct {
	re      *regexp.Regexp
	subject string
}{
	{regexp.MustCompile(`(?i)\b(?:bitcoin|btc)\b`), "BTC"},
	{regexp.MustCompile(`(?i)\b(?:ethereum|ether|eth)\b`), "ETH"},
}

var (
	// disqualifierRe: title words that mean the market is not about the asset's price.
	disqualifierRe = regexp.MustCompile(`(?i)\b(?:dominance|volatility|difficulty|hash\s?rate|market\s+cap|etf|reserve|` +
		`green\s+days|red\s+days|fees?|transactions?|supply|company|treasury|up\s+or\s+down|flip|bankrupt\w*)\b`)
	// highVerbRe / lowVerbRe: path-dependent markets ("hit $150k by", "dip to $60,000 by").
	highVerbRe = regexp.MustCompile(`(?i)\b(?:hit|hits|reach|reaches|touch|touches)\s+\$`)
	lowVerbRe  = regexp.MustCompile(`(?i)\b(?:dip|dips|fall|falls|drop|drops)\s+(?:to|below)\s+\$`)
	// priceWordRe: the title talks about the price itself.
	priceWordRe = regexp.MustCompile(`(?i)\bprice\b`)
	// comparatorDollarRe: "above $74,000" in a title also means price.
	comparatorDollarRe = regexp.MustCompile(`(?i)\b(?:above|below|over|under|higher than|lower than|greater than|less than|` +
		`more than|at least|at most|between)\s+\$`)
	// rulesPriceKindRe: which price the rules read from the source's candles.
	rulesPriceKindRe = regexp.MustCompile(`(?i)final\s+["“]?(high|low|close)["”]?\s+price`)
)

// sourceNames maps text to canonical resolution-source IDs. Index and pair
// suffixes make "CF Benchmarks BRTI" distinct from "CF Benchmarks ERTI".
var sourceNames = []sourceName{
	{regexp.MustCompile(`(?i)\bcf\s+benchmarks\b`), "cf_benchmarks"},
	{regexp.MustCompile(`(?i)\bbinance\b`), "binance"},
	{regexp.MustCompile(`(?i)\bcoinbase\b`), "coinbase"},
	{regexp.MustCompile(`(?i)\bkraken\b`), "kraken"},
	{regexp.MustCompile(`(?i)\bbitstamp\b`), "bitstamp"},
	{regexp.MustCompile(`(?i)\bgemini\b`), "gemini"},
	{regexp.MustCompile(`(?i)\bcoingecko\b`), "coingecko"},
	{regexp.MustCompile(`(?i)\bcoinmarketcap\b`), "coinmarketcap"},
	{regexp.MustCompile(`(?i)\bchainlink\b`), "chainlink"},
	{regexp.MustCompile(`(?i)\bpyth\b`), "pyth"},
	{regexp.MustCompile(`(?i)\bbloomberg\b`), "bloomberg"},
	{regexp.MustCompile(`(?i)\bokx\b`), "okx"},
	{regexp.MustCompile(`(?i)\bbybit\b`), "bybit"},
}

var sourceSuffixes = []sourceSuffix{
	{"cf_benchmarks", "_brti", regexp.MustCompile(`(?i)\bBRTI\b|bitcoin\s+real[- ]time\s+index`)},
	{"cf_benchmarks", "_erti", regexp.MustCompile(`(?i)\bERTI\b|ethereum\s+real[- ]time\s+index`)},
	{"binance", "_btc_usdt", regexp.MustCompile(`(?i)\bBTC\s*/\s*USDT\b`)},
	{"binance", "_eth_usdt", regexp.MustCompile(`(?i)\bETH\s*/\s*USDT\b`)},
}

var (
	// authorityBeforeRe: cue words ending just before a source name.
	authorityBeforeRe = regexp.MustCompile(`(?i)(?:resolution\s+source[^.]{0,60}?\b(?:is|will\s+be)|based\s+on|according\s+to|` +
		`published\s+by|determined\s+by|provided\s+by|reported\s+by)\s+(?:the\s+)?$`)
	// authorityAfterRe: a source name followed by an index name.
	authorityAfterRe = regexp.MustCompile(`(?i)^['’]?s?\s+[\w\s()-]{0,60}?\bindex\b`)
)

func extractCrypto(in canonical.Instrument) (canonical.Terms, canonical.Quality) {
	var q canonical.Quality
	t := canonical.UnknownTerms()
	srcs := sourcesOf(in)
	title := srcs[1].text
	rules := srcs[0].text

	t.Subject = cryptoSubject(title)
	t.Metric = cryptoMetric(title, rules)
	cryptoCondition(&t, &q, srcs)
	textDeadline(&t, &q, in.CloseTime, srcs, datesIn)
	t.ResolutionSource = cryptoSource(rules + "\n" + title)
	if strings.Contains(title, "$") || strings.Contains(rules, "$") {
		if t.Unit == canonical.Unknown {
			t.Unit = UnitUSD
		}
	}
	return t, q
}

// cryptoSubject returns the one asset the title names, else unknown.
func cryptoSubject(title string) string {
	found := ""
	for _, a := range assetAliases {
		if !a.re.MatchString(title) {
			continue
		}
		if found != "" {
			return canonical.Unknown // two assets (e.g. "ETH/BTC")
		}
		found = a.subject
	}
	if found == "" {
		return canonical.Unknown
	}
	return found
}

// cryptoMetric decides which price the market is about, or unknown.
func cryptoMetric(title, rules string) string {
	if disqualifierRe.MatchString(title) {
		return canonical.Unknown
	}
	metric := canonical.Unknown
	switch {
	case highVerbRe.MatchString(title):
		metric = MetricPriceHigh
	case lowVerbRe.MatchString(title):
		metric = MetricPriceLow
	case priceWordRe.MatchString(title) || comparatorDollarRe.MatchString(title):
		metric = MetricPrice
	}
	// The rules name the candle field they read; it must agree with the title.
	kinds := map[string]bool{}
	for _, m := range rulesPriceKindRe.FindAllStringSubmatch(rules, -1) {
		kinds[strings.ToLower(m[1])] = true
	}
	if len(kinds) > 1 {
		return canonical.Unknown
	}
	for kind := range kinds {
		want := map[string]string{"high": MetricPriceHigh, "low": MetricPriceLow, "close": MetricPrice}[kind]
		if metric != canonical.Unknown && metric != want {
			return canonical.Unknown
		}
		if metric == canonical.Unknown && cryptoSubjectLike(title) {
			metric = want
		}
	}
	return metric
}

// cryptoSubjectLike reports whether the title names an asset at all.
func cryptoSubjectLike(title string) bool {
	for _, a := range assetAliases {
		if a.re.MatchString(title) {
			return true
		}
	}
	return false
}

// cryptoCondition sets comparator, threshold(s), unit and polarity from the
// first text that states the condition exactly once.
func cryptoCondition(t *canonical.Terms, q *canonical.Quality, srcs []source) {
	c, ok, ambiguousIn := firstStated(srcs, conditionsIn, condition.key)
	if ambiguousIn != "" {
		q.Add(QualityTermsAmbiguous, "comparator", "the "+ambiguousIn+" text states more than one condition")
		return
	}
	if !ok {
		return
	}
	if c.threshold == nil {
		// The rules defer to the title: use the title's single $ amount.
		var dollars []amount
		for _, a := range amountsIn(srcs[1].text) {
			if a.dollar {
				dollars = append(dollars, a)
			}
		}
		a, found, ambiguous := single(dollars, func(a amount) string { return a.value.String() })
		if ambiguous {
			q.Add(QualityTermsAmbiguous, "threshold", "the title has several $ amounts")
		}
		if !found || ambiguous {
			return
		}
		c.threshold, c.percent = &a.value, a.percent
	}
	t.Comparator = c.comparator
	t.Threshold = c.threshold
	t.ThresholdHigh = c.high
	if c.percent {
		t.Unit = UnitPercent
	}
	t.Polarity = canonical.PolarityPositive
	if c.negated {
		t.Polarity = canonical.PolarityNegative
	}
}

// textDeadline sets Deadline and DeadlineTZ from the first text that states a
// date (found by dates) and a time with a zone. Without a complete, consistent
// date + time + zone the close time is used and flagged (D-060).
func textDeadline(t *canonical.Terms, q *canonical.Quality, closeTime time.Time, srcs []source,
	dates func(string) []calendarDate) {
	date, dateOK, dateAmbiguous := firstStated(srcs, dates, calendarDate.key)
	clk, clockOK, clockAmbiguous := firstStated(srcs, clocksIn, clock.key)
	var missing []string
	if dateAmbiguous != "" {
		missing = append(missing, "the "+dateAmbiguous+" text states several dates")
	} else if !dateOK {
		missing = append(missing, "no date stated")
	}
	if clockAmbiguous != "" {
		missing = append(missing, "the "+clockAmbiguous+" text states several times")
	} else if !clockOK {
		missing = append(missing, "no time with a timezone stated")
	}
	if len(missing) == 0 {
		if date.year == 0 {
			if closeTime.IsZero() {
				missing = append(missing, "no year stated and no close time to infer it from")
			} else {
				date.year = nearestYear(date, clk, closeTime)
				q.Add(QualityDeadlineYearInferred, "deadline", fmt.Sprintf("year %d taken from the close time", date.year))
			}
		}
	}
	if len(missing) == 0 {
		loc, err := time.LoadLocation(clk.zone)
		if err != nil {
			missing = append(missing, "unknown timezone "+clk.zone)
		} else {
			deadline := time.Date(date.year, date.month, date.day, clk.hour, clk.minute, 0, 0, loc).UTC()
			if !closeTime.IsZero() && absDuration(deadline.Sub(closeTime)) > deadlineSanityWindow {
				missing = append(missing, fmt.Sprintf("parsed deadline %s is more than 7 days from the close time",
					deadline.Format(time.RFC3339)))
			} else {
				t.Deadline, t.DeadlineTZ = &deadline, clk.zone
				return
			}
		}
	}
	detail := strings.Join(missing, "; ")
	if closeTime.IsZero() {
		q.Add(canonical.QualityMissingField, "deadline", detail+"; no close time to default to")
		return
	}
	d := closeTime.UTC()
	t.Deadline = &d
	q.Add(canonical.QualityDeadlineDefaulted, "deadline", detail+"; using close time")
}

// nearestYear picks the year (close year ± 1) that puts the date closest to the close time.
func nearestYear(date calendarDate, clk clock, closeTime time.Time) int {
	loc, err := time.LoadLocation(clk.zone)
	if err != nil {
		loc = time.UTC
	}
	best, bestGap := closeTime.Year(), time.Duration(-1)
	for y := closeTime.Year() - 1; y <= closeTime.Year()+1; y++ {
		if !(calendarDate{year: y, month: date.month, day: date.day}).valid() {
			continue
		}
		gap := absDuration(time.Date(y, date.month, date.day, clk.hour, clk.minute, 0, 0, loc).Sub(closeTime))
		if bestGap < 0 || gap < bestGap {
			best, bestGap = y, gap
		}
	}
	return best
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// cryptoSource returns the one resolution source the text makes authoritative.
func cryptoSource(text string) string {
	return namedSource(text, sourceNames, sourceSuffixes)
}

// sourceName is one resolution-source alias and the canonical ID it maps to.
type sourceName struct {
	re *regexp.Regexp
	id string
}

// sourceSuffix refines a base source ID when the text also names an index or
// pair ("CF Benchmarks" + "BRTI" is cf_benchmarks_brti).
type sourceSuffix struct {
	base, suffix string
	re           *regexp.Regexp
}

// namedSource returns the one resolution source the text makes authoritative
// (named after a cue such as "the resolution source is", "based on", or
// followed by "... Index"). With no authoritative name, a single mentioned
// source counts; several candidates is unknown.
func namedSource(text string, names []sourceName, suffixes []sourceSuffix) string {
	authoritative := map[string]bool{}
	mentioned := map[string]bool{}
	for _, s := range names {
		for _, loc := range s.re.FindAllStringIndex(text, -1) {
			id := s.id
			for _, suf := range suffixes {
				if suf.base == s.id && suf.re.MatchString(text) {
					id += suf.suffix
					break
				}
			}
			mentioned[id] = true
			before := text[max(0, loc[0]-120):loc[0]]
			after := text[loc[1]:min(len(text), loc[1]+80)]
			if authorityBeforeRe.MatchString(before) || authorityAfterRe.MatchString(after) {
				authoritative[id] = true
			}
		}
	}
	if len(authoritative) == 1 {
		return onlyKey(authoritative)
	}
	if len(authoritative) == 0 && len(mentioned) == 1 {
		return onlyKey(mentioned)
	}
	return canonical.Unknown
}

func onlyKey(m map[string]bool) string {
	for k := range m {
		return k
	}
	return canonical.Unknown
}
