package web

import (
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// Plain-language formatting for the presenter (ui/plain-language): money in
// cents and dollars, counts with thousands separators, and small numbers as
// words. Every helper returns a plain string; templates still escape it.

var (
	hundred = decimal.NewFromInt(100)
)

// centsText formats a per-contract price in cents: whole cents drop the
// decimals ("1¢"), anything else keeps two places ("0.80¢", "0.38¢").
func centsText(d decimal.Decimal) string {
	c := d.Mul(hundred)
	if c.Equal(c.Truncate(0)) {
		return groupThousands(c.StringFixed(0)) + "¢"
	}
	return groupThousands(c.StringFixed(2)) + "¢"
}

// signedCentsText is a per-contract gap in cents with an explicit sign:
// "+0.27¢" or "−0.27¢".
func signedCentsText(d decimal.Decimal) string {
	if d.IsNegative() {
		return "−" + centsText(d.Abs())
	}
	return "+" + centsText(d)
}

// dollarsText formats a total in dollars to the cent: "$0.80", "$1,234.50".
func dollarsText(d decimal.Decimal) string {
	sign := ""
	if d.IsNegative() {
		sign = "−"
		d = d.Abs()
	}
	return sign + "$" + groupThousands(d.StringFixed(2))
}

// perDollarText formats a per-$1-payout cost in dollars: two places when that
// is exact, else four, so $1.0019 never rounds to a misleading "$1.00".
func perDollarText(d decimal.Decimal) string {
	if d.Equal(d.Round(2)) {
		return "$" + d.StringFixed(2)
	}
	return "$" + d.StringFixed(4)
}

// countText formats a contract count: whole contracts rounded down, with
// thousands separators ("8,912" from 8912.33).
func countText(d decimal.Decimal) string {
	return groupThousands(d.Floor().StringFixed(0))
}

// groupThousands inserts commas into the integer part of a decimal string.
func groupThousands(s string) string {
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	if len(intPart) <= 3 {
		return sign + intPart + frac
	}
	var b strings.Builder
	lead := len(intPart) % 3
	if lead > 0 {
		b.WriteString(intPart[:lead])
	}
	for i := lead; i < len(intPart); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(intPart[i : i+3])
	}
	return sign + b.String() + frac
}

var smallNumbers = [...]string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}

// numberWord spells 0 to 10 as a word and leaves larger numbers as digits.
func numberWord(n int) string {
	if n >= 0 && n < len(smallNumbers) {
		return smallNumbers[n]
	}
	return strconv.Itoa(n)
}

// capitalize upper-cases the first letter of s.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// plural picks the singular or plural form for n.
func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return singular
	}
	return pluralForm
}

// percentText formats a probability in [0,1] as a whole percent ("99%"), with
// "under 1%" and "over 99%" at the extremes so a tiny price never reads as 0%.
func percentText(p decimal.Decimal) string {
	pct := p.Mul(hundred)
	switch {
	case pct.IsPositive() && pct.LessThan(one):
		return "under 1%"
	case pct.LessThan(hundred) && pct.GreaterThan(decimal.NewFromInt(99)):
		return "over 99%"
	}
	return pct.Round(0).StringFixed(0) + "%"
}

// truncateID shortens a long instrument ID for visible text: the first six
// and last four characters of the key around an ellipsis
// ("polymarket:182462…9498"). Short IDs are returned whole.
func truncateID(id string) string {
	prefix, key := "", id
	if i := strings.IndexByte(id, ':'); i >= 0 {
		prefix, key = id[:i+1], id[i+1:]
	}
	if len(key) <= 14 {
		return id
	}
	return prefix + key[:6] + "…" + key[len(key)-4:]
}
