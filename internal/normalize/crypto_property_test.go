package normalize

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// formatMoney renders d the ways venues write amounts: with $ and thousands
// separators, as a plain decimal, or with a k suffix when it is a whole
// number of thousands.
func formatMoney(t *rapid.T, d decimal.Decimal) string {
	whole := d.Truncate(0)
	frac := d.Sub(whole)
	styles := []string{"plain", "commas", "dollar_commas"}
	if frac.IsZero() && whole.Mod(decimal.NewFromInt(1000)).IsZero() && !whole.IsZero() {
		styles = append(styles, "k", "dollar_k")
	}
	switch rapid.SampledFrom(styles).Draw(t, "style") {
	case "commas":
		return withCommas(d)
	case "dollar_commas":
		return "$" + withCommas(d)
	case "k":
		return whole.Div(decimal.NewFromInt(1000)).String() + "k"
	case "dollar_k":
		return "$" + whole.Div(decimal.NewFromInt(1000)).String() + "k"
	}
	return d.String()
}

func withCommas(d decimal.Decimal) string {
	s := d.String()
	intPart, fracPart, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if fracPart != "" {
		b.WriteString("." + fracPart)
	}
	return b.String()
}

// TestCryptoThresholdRoundTripProperty: any amount, however formatted, parses
// back to the same decimal with the comparator its phrase names.
func TestCryptoThresholdRoundTripProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cents := rapid.Int64Range(0, 999_999_999_99).Draw(t, "cents")
		d := decimal.NewFromInt(cents).Div(decimal.NewFromInt(100))
		phrase := rapid.SampledFrom([]struct {
			text string
			comp canonical.Comparator
			post bool // amount comes before the phrase
		}{
			{"above", canonical.ComparatorGT, false}, {"higher than", canonical.ComparatorGT, false},
			{"greater than", canonical.ComparatorGT, false}, {"below", canonical.ComparatorLT, false},
			{"less than", canonical.ComparatorLT, false}, {"at or above", canonical.ComparatorGTE, false},
			{"equal to or greater than", canonical.ComparatorGTE, false}, {"at least", canonical.ComparatorGTE, false},
			{"equal to or lower than", canonical.ComparatorLTE, false}, {"at most", canonical.ComparatorLTE, false},
			{"or above", canonical.ComparatorGTE, true}, {"or higher", canonical.ComparatorGTE, true},
			{"or below", canonical.ComparatorLTE, true}, {"or lower", canonical.ComparatorLTE, true},
		}).Draw(t, "phrase")
		money := formatMoney(t, d)
		var rules string
		if phrase.post {
			rules = fmt.Sprintf("Resolves Yes if the price is %s %s at 5 PM ET on Oct 6, 2026.", money, phrase.text)
		} else {
			rules = fmt.Sprintf("Resolves Yes if the price is %s %s at 5 PM ET on Oct 6, 2026.", phrase.text, money)
		}
		got := Extract(crypto("Bitcoin price on Oct 6, 2026?", rules, time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC)))
		if got.Comparator != phrase.comp {
			t.Fatalf("%q: comparator %q, want %q", rules, got.Comparator, phrase.comp)
		}
		if got.Threshold == nil || !got.Threshold.Equal(d) {
			t.Fatalf("%q: threshold %s, want %s", rules, str(got.Threshold), d)
		}
		if got.Polarity != canonical.PolarityPositive {
			t.Fatalf("%q: polarity %q", rules, got.Polarity)
		}
	})
}

// TestCryptoBetweenRoundTripProperty: "between A and B" keeps both bounds.
func TestCryptoBetweenRoundTripProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lo := decimal.NewFromInt(rapid.Int64Range(0, 10_000_000).Draw(t, "lo"))
		hi := lo.Add(decimal.NewFromInt(rapid.Int64Range(0, 10_000_000).Draw(t, "span")))
		title := fmt.Sprintf("Will the price of Bitcoin be between %s and %s on October 7?", formatMoney(t, lo), formatMoney(t, hi))
		got := Extract(crypto(title, polyBucketRules, closeNoon7))
		if got.Comparator != canonical.ComparatorBetween || got.Threshold == nil || got.ThresholdHigh == nil ||
			!got.Threshold.Equal(lo) || !got.ThresholdHigh.Equal(hi) {
			t.Fatalf("%q: got %s %s/%s", title, got.Comparator, str(got.Threshold), str(got.ThresholdHigh))
		}
	})
}

var zoneAbbrevs = map[string][]string{
	"America/New_York":    {"ET", "EST", "EDT"},
	"America/Chicago":     {"CT", "CST", "CDT"},
	"America/Los_Angeles": {"PT", "PST", "PDT"},
	"UTC":                 {"UTC", "GMT"},
}

// TestCryptoDeadlineRoundTripProperty: a deadline written as venues write
// them (12-hour clock + zone abbreviation + month-name date) parses back to the
// same UTC instant, in the rules or in the title, with or without a year.
func TestCryptoDeadlineRoundTripProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		zone := rapid.SampledFrom([]string{"America/New_York", "America/Chicago", "America/Los_Angeles", "UTC"}).Draw(t, "zone")
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		base := time.Date(2024, 1, 1, 0, 0, 0, 0, loc)
		minutes := rapid.Int64Range(0, 6*365*24*60).Draw(t, "minutes")
		local := base.Add(time.Duration(minutes) * time.Minute)
		local = time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, loc)
		if local.Hour() == 0 && !rapid.Bool().Draw(t, "keep_midnight") {
			return // 12 AM is written as "12 AM"; nothing special, but keep the generator balanced
		}
		abbrev := rapid.SampledFrom(zoneAbbrevs[zone]).Draw(t, "abbrev")
		clockText := rapid.SampledFrom([]string{"3:04 PM", "3:04PM", "3:04 pm", "3PM"}).Draw(t, "clock")
		if local.Minute() != 0 && clockText == "3PM" {
			clockText = "3:04 PM"
		}
		withYear := rapid.Bool().Draw(t, "with_year")
		dateText := rapid.SampledFrom([]string{"Jan 2, 2006", "January 2, 2006", "January 2"}).Draw(t, "date")
		if !withYear {
			dateText = "January 2"
		} else if dateText == "January 2" {
			dateText = "Jan 2, 2006"
		}
		stmt := fmt.Sprintf("at %s %s on %s", local.Format(clockText), abbrev, local.Format(dateText))
		closeTime := local.UTC().Add(time.Duration(rapid.Int64Range(-60, 60).Draw(t, "close_skew")) * time.Minute)

		var in canonical.Instrument
		if rapid.Bool().Draw(t, "in_rules") {
			in = crypto("Bitcoin price?", "If BRTI is above 100000 "+stmt+", then Yes.", closeTime)
		} else {
			in = crypto("Bitcoin above $100,000 "+stmt+"?", "", closeTime)
		}
		out := Normalize(in)
		got := out.Terms.Deadline
		if got == nil || !got.Equal(local) {
			t.Fatalf("%q (close %s): deadline %v, want %v", stmt, closeTime, got, local.UTC())
		}
		if out.Terms.DeadlineTZ != zone {
			t.Fatalf("%q: tz %q, want %q", stmt, out.Terms.DeadlineTZ, zone)
		}
		if out.Quality.Has(canonical.QualityDeadlineDefaulted) {
			t.Fatalf("%q: deadline was defaulted: %v", stmt, out.Quality)
		}
		if withYear == out.Quality.Has(QualityDeadlineYearInferred) {
			t.Fatalf("%q: year flag %v with withYear=%v", stmt, out.Quality, withYear)
		}
	})
}

// TestCryptoExtractNeverPanicsProperty: arbitrary text always yields valid terms.
func TestCryptoExtractNeverPanicsProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := crypto(rapid.String().Draw(t, "title"), rapid.String().Draw(t, "rules"), closeOct6)
		if err := Normalize(in).Terms.Validate(); err != nil {
			t.Fatal(err)
		}
	})
}

// FuzzExtractCrypto is the fuzz target for the crypto text parser.
func FuzzExtractCrypto(f *testing.F) {
	f.Add("Bitcoin price on Oct 6, 2026? — $76,600 or above", kalshiBTCRules)
	f.Add("Will the price of Bitcoin be above $74,000 on October 7?", polyAboveRules)
	f.Add("Will Bitcoin hit $150k by September 30?", polyHitRules)
	f.Add("", "")
	f.Fuzz(func(t *testing.T, title, rules string) {
		out := Normalize(crypto(title, rules, closeOct6))
		if err := out.Terms.Validate(); err != nil {
			t.Fatal(err)
		}
		if out.Terms.Deadline == nil {
			t.Fatal("deadline must default to the close time")
		}
	})
}
