package web

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Plain contract names (ui/plain-language). A contract is named the way a
// person would say it, from its parsed terms: "Fed rate above 3.50% after
// Oct 28" or "Fed cuts 50+ bps in October". When a term the name needs is
// unknown, or the shape has no template, the venue's own title is used
// (trimmed), so a name is never guessed. Term values mirror internal/normalize.

const (
	termFedFunds   = "fed_funds_rate"
	termRateLevel  = "rate_upper_bound"
	termRateChange = "rate_change"
	termPrice      = "price"
	termPriceHigh  = "price_high"
	termPriceLow   = "price_low"
	termPercent    = "percent"
	termUSD        = "USD"
	maxTitleName   = 70
)

// contractName is the plain name of one contract in its long and short forms.
type contractName struct {
	Long  string // "Fed rate above 3.50% after Oct 28"
	Short string // "Fed above 3.50%"
	// FromTerms is false when the name fell back to the venue title.
	FromTerms bool
}

// nameContract names an instrument. A zero Instrument (not loaded) falls back
// to its truncated ID, which only happens for decisions whose instruments were
// deleted.
func nameContract(in canonical.Instrument) contractName {
	if long, short, ok := namedFromTerms(in.Terms); ok {
		return contractName{Long: long, Short: short, FromTerms: true}
	}
	t := titleName(in.Title)
	if t == "" {
		t = truncateID(in.ID)
	}
	return contractName{Long: t, Short: t}
}

// venueName is a venue's display name: "Kalshi", "Polymarket".
func venueName(v canonical.Venue) string { return venueLabel(v) }

// namedFromTerms builds a name from parsed terms for the shapes it knows.
func namedFromTerms(t canonical.Terms) (long, short string, ok bool) {
	if t.Polarity != canonical.PolarityPositive || t.Threshold == nil {
		return "", "", false
	}
	switch {
	case t.Subject == termFedFunds && t.Metric == termRateLevel && t.Unit == termPercent:
		return fedLevelName(t)
	case t.Subject == termFedFunds && t.Metric == termRateChange && t.Unit == termPercent:
		return fedChangeName(t)
	case t.Unit == termUSD && (t.Metric == termPrice || t.Metric == termPriceHigh || t.Metric == termPriceLow):
		return cryptoName(t)
	}
	return "", "", false
}

// levelPhrase words a comparison against a level: "above 3.50%".
func levelPhrase(c canonical.Comparator, lo string, hi *decimal.Decimal, unitFn func(decimal.Decimal) string) (string, bool) {
	switch c {
	case canonical.ComparatorGT:
		return "above " + lo, true
	case canonical.ComparatorGTE:
		return lo + " or higher", true
	case canonical.ComparatorLT:
		return "below " + lo, true
	case canonical.ComparatorLTE:
		return lo + " or lower", true
	case canonical.ComparatorEQ:
		return "at " + lo, true
	case canonical.ComparatorBetween:
		if hi == nil {
			return "", false
		}
		return "between " + lo + " and " + unitFn(*hi), true
	}
	return "", false
}

func pctText(d decimal.Decimal) string { return d.StringFixed(2) + "%" }

func usdText(d decimal.Decimal) string {
	if d.Equal(d.Truncate(0)) {
		return "$" + groupThousands(d.StringFixed(0))
	}
	return "$" + groupThousands(d.StringFixed(2))
}

func fedLevelName(t canonical.Terms) (string, string, bool) {
	p, ok := levelPhrase(t.Comparator, pctText(*t.Threshold), t.ThresholdHigh, pctText)
	if !ok {
		return "", "", false
	}
	long := "Fed rate " + p
	if t.Deadline != nil {
		long += " after " + venueDate(*t.Deadline, t.DeadlineTZ).Format("Jan 2")
	}
	return long, "Fed " + p, true
}

// bpsText words a change in percentage points as basis points: 0.5 → "50".
func bpsText(d decimal.Decimal) string { return d.Abs().Mul(hundred).StringFixed(0) }

func fedChangeName(t canonical.Terms) (string, string, bool) {
	th := *t.Threshold
	var long, short string
	switch {
	case t.Comparator == canonical.ComparatorEQ && th.IsZero():
		long, short = "Fed holds rates", "Fed hold"
	case t.Comparator == canonical.ComparatorEQ:
		long, short = fedMove(th)+" "+bpsText(th)+" bps", "Fed "+signWord(th)+bpsText(th)+" bps"
	case (t.Comparator == canonical.ComparatorGTE && th.IsPositive()) || (t.Comparator == canonical.ComparatorLTE && th.IsNegative()):
		long, short = fedMove(th)+" "+bpsText(th)+"+ bps", "Fed "+signWord(th)+bpsText(th)+" bps"
	case (t.Comparator == canonical.ComparatorGT && th.IsPositive()) || (t.Comparator == canonical.ComparatorLT && th.IsNegative()):
		long, short = fedMove(th)+" more than "+bpsText(th)+" bps", "Fed "+signWord(th)+">"+bpsText(th)+" bps"
	default:
		return "", "", false
	}
	if t.Deadline != nil {
		long += " in " + venueDate(*t.Deadline, t.DeadlineTZ).Format("January")
	}
	return long, short, true
}

func fedMove(th decimal.Decimal) string {
	if th.IsNegative() {
		return "Fed cuts"
	}
	return "Fed raises"
}

func signWord(th decimal.Decimal) string {
	if th.IsNegative() {
		return "−"
	}
	return "+"
}

func cryptoName(t canonical.Terms) (string, string, bool) {
	if t.Subject == "" || t.Subject == canonical.Unknown {
		return "", "", false
	}
	lo := usdText(*t.Threshold)
	var p string
	switch {
	case t.Metric == termPriceHigh && (t.Comparator == canonical.ComparatorGT || t.Comparator == canonical.ComparatorGTE):
		p = "reaches " + lo
	case t.Metric == termPriceLow && (t.Comparator == canonical.ComparatorLT || t.Comparator == canonical.ComparatorLTE):
		p = "dips to " + lo
	case t.Metric == termPrice:
		var ok bool
		if p, ok = levelPhrase(t.Comparator, lo, t.ThresholdHigh, usdText); !ok {
			return "", "", false
		}
	default:
		return "", "", false
	}
	subject := strings.ToUpper(t.Subject)
	long := subject + " " + p
	if t.Deadline != nil {
		word := " on "
		if t.Metric != termPrice {
			word = " by "
		}
		long += word + venueDate(*t.Deadline, t.DeadlineTZ).Format("Jan 2")
	}
	return long, subject + " " + p, true
}

// venueDate puts a UTC deadline in the venue's stated zone, defaulting to US
// Eastern (both venues are US-facing); UTC when no zone database is present.
func venueDate(t time.Time, tz string) time.Time {
	if tz == "" || tz == canonical.Unknown {
		tz = "America/New_York"
	}
	loc, err := loadLocation(tz)
	if err != nil {
		return t.UTC()
	}
	return t.In(loc)
}

// titleName trims a venue title into a name: no leading "Will", no trailing
// "?", Kalshi's " — Above 4.25%" option suffix dropped when the title already
// says it, and long titles cut at a word boundary.
func titleName(title string) string {
	t := strings.TrimSpace(title)
	if i := strings.Index(t, " — "); i > 0 {
		t = strings.TrimSpace(t[:i])
	}
	t = strings.TrimSuffix(t, "?")
	if strings.HasPrefix(t, "Will ") {
		t = capitalize(strings.TrimPrefix(t, "Will "))
	}
	if r := []rune(t); len(r) > maxTitleName {
		cut := string(r[:maxTitleName])
		if i := strings.LastIndexByte(cut, ' '); i > maxTitleName/2 {
			cut = cut[:i]
		}
		t = strings.TrimRight(cut, " ,;:") + "…"
	}
	return t
}
