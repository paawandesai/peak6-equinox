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

// TestFedChangeRoundTripProperty: a change of N bps, however the venue
// writes it (bps, basis points, or the equivalent percent; "by N" or "N
// increase"; hike or cut; exact, "+", "at least" or "or more"), parses to the
// signed change in percentage points with the comparator the phrase names.
func TestFedChangeRoundTripProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bps := rapid.Int64Range(1, 300).Draw(t, "bps")
		magnitude := decimal.NewFromInt(bps).Div(bpsPerPercent)
		up := rapid.Bool().Draw(t, "up")
		unit := rapid.SampledFrom([]string{"bps", "bp", "basis points", "percent"}).Draw(t, "unit")
		amount := fmt.Sprintf("%d", bps)
		if unit == "percent" {
			amount, unit = magnitude.String(), "%"
		}
		qualifier := rapid.SampledFrom([]string{"", "+", "at least", "or more", "more than"}).Draw(t, "qualifier")
		form := rapid.SampledFrom([]string{"by", "noun"}).Draw(t, "form")
		if form == "noun" && qualifier != "" && qualifier != "+" {
			qualifier = ""
		}
		var phrase string
		var verb string
		noun := "decrease"
		if up {
			verb, noun = rapid.SampledFrom([]string{"raises", "hikes", "increases"}).Draw(t, "verb"), rapid.SampledFrom([]string{"increase", "hike"}).Draw(t, "noun")
		} else {
			verb = rapid.SampledFrom([]string{"cuts", "lowers", "decreases"}).Draw(t, "verb")
		}
		switch {
		case form == "noun":
			phrase = fmt.Sprintf("%s%s %s %s", amount, qualifier, unit, noun)
		case qualifier == "+":
			phrase = fmt.Sprintf("%s rates by %s+ %s", verb, amount, unit)
		case qualifier == "or more":
			phrase = fmt.Sprintf("%s rates by %s %s or more", verb, amount, unit)
		default:
			phrase = strings.TrimSpace(fmt.Sprintf("%s rates by %s %s %s", verb, qualifier, amount, unit))
		}
		rules := fmt.Sprintf("Yes if the Fed %s at its Oct 28, 2026 meeting (2:05 PM ET).", phrase)
		got := Extract(fed("Fed decision in October?", rules, kxfedOctClose))

		wantValue := magnitude
		wantComp := canonical.ComparatorEQ
		switch qualifier {
		case "+", "at least", "or more":
			wantComp = canonical.ComparatorGTE
		case "more than":
			wantComp = canonical.ComparatorGT
		}
		if !up {
			wantValue = magnitude.Neg()
			switch wantComp {
			case canonical.ComparatorGTE:
				wantComp = canonical.ComparatorLTE
			case canonical.ComparatorGT:
				wantComp = canonical.ComparatorLT
			}
		}
		if got.Metric != MetricRateChange || got.Comparator != wantComp || got.Threshold == nil || !got.Threshold.Equal(wantValue) || got.Unit != UnitPercent {
			t.Fatalf("%q: got %s %s %s %s, want %s %s %s percent", rules, got.Metric, got.Comparator, str(got.Threshold), got.Unit,
				MetricRateChange, wantComp, wantValue)
		}
	})
}

// TestFedBpsPercentEquivalenceProperty: "N bps" and "N/100 %" are the same change.
func TestFedBpsPercentEquivalenceProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		bps := rapid.Int64Range(1, 500).Draw(t, "bps")
		pct := decimal.NewFromInt(bps).Div(bpsPerPercent)
		asBps := changeConditionsIn(fmt.Sprintf("raises rates by %d bps", bps))
		asPct := changeConditionsIn(fmt.Sprintf("raises rates by %s%%", pct))
		if len(asBps) != 1 || len(asPct) != 1 || !asBps[0].threshold.Equal(*asPct[0].threshold) || !asBps[0].threshold.Equal(pct) {
			t.Fatalf("%d bps parsed as %v, %s%% as %v", bps, asBps, pct, asPct)
		}
	})
}

// TestFedLevelRoundTripProperty: a rate level with up to two decimals parses
// back exactly with the comparator its phrase names.
func TestFedLevelRoundTripProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hundredths := rapid.Int64Range(0, 2500).Draw(t, "hundredths")
		level := decimal.NewFromInt(hundredths).Div(bpsPerPercent)
		text := rapid.SampledFrom([]string{level.String(), level.StringFixed(2)}).Draw(t, "format")
		phrase := rapid.SampledFrom([]struct {
			text string
			comp canonical.Comparator
			post bool
		}{
			{"above", canonical.ComparatorGT, false}, {"greater than", canonical.ComparatorGT, false},
			{"below", canonical.ComparatorLT, false}, {"less than", canonical.ComparatorLT, false},
			{"at or above", canonical.ComparatorGTE, false}, {"at least", canonical.ComparatorGTE, false},
			{"at or below", canonical.ComparatorLTE, false}, {"at most", canonical.ComparatorLTE, false},
			{"or higher", canonical.ComparatorGTE, true}, {"or above", canonical.ComparatorGTE, true},
			{"or lower", canonical.ComparatorLTE, true}, {"or below", canonical.ComparatorLTE, true},
			{"be", canonical.ComparatorEQ, false},
		}).Draw(t, "phrase")
		var cond string
		if phrase.post {
			cond = text + "% " + phrase.text
		} else {
			cond = phrase.text + " " + text + "%"
		}
		rules := fmt.Sprintf("Yes if the upper bound of the target federal funds rate will %s following the Fed's Oct 28, 2026 meeting (2:05 PM ET).", cond)
		got := Extract(fed("Fed funds rate after Oct 2026 meeting?", rules, kxfedOctClose))
		if got.Metric != MetricRateUpperBound || got.Comparator != phrase.comp || got.Threshold == nil || !got.Threshold.Equal(level) || got.Unit != UnitPercent {
			t.Fatalf("%q: got %s %s %s %s, want %s %s %s percent", rules, got.Metric, got.Comparator, str(got.Threshold), got.Unit,
				MetricRateUpperBound, phrase.comp, level)
		}
	})
}

// TestFedMeetingDateRoundTripProperty: a meeting written as a single day or a
// two-day range, with a stated time and zone, parses to the last day's instant.
func TestFedMeetingDateRoundTripProperty(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	rapid.Check(t, func(t *rapid.T) {
		year := rapid.IntRange(2024, 2030).Draw(t, "year")
		month := time.Month(rapid.IntRange(1, 12).Draw(t, "month"))
		daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
		last := rapid.IntRange(2, daysInMonth).Draw(t, "last")
		first := last - 1
		long := rapid.Bool().Draw(t, "long")
		name := month.String()
		if !long {
			name = name[:3]
		}
		var date string
		switch rapid.SampledFrom([]string{"single", "dash", "en-dash", "to"}).Draw(t, "style") {
		case "single":
			date = fmt.Sprintf("%s %d, %d", name, last, year)
		case "dash":
			date = fmt.Sprintf("%s %d-%d, %d", name, first, last, year)
		case "en-dash":
			date = fmt.Sprintf("%s %d–%d, %d", name, first, last, year)
		default:
			date = fmt.Sprintf("%s %d to %d, %d", name, first, last, year)
		}
		hour := rapid.IntRange(1, 12).Draw(t, "hour")
		minute := rapid.IntRange(0, 59).Draw(t, "minute")
		meridiem := rapid.SampledFrom([]string{"AM", "PM"}).Draw(t, "meridiem")
		h24 := hour % 12
		if meridiem == "PM" {
			h24 += 12
		}
		want := time.Date(year, month, last, h24, minute, 0, 0, ny).UTC()
		closeTime := want.Add(-time.Duration(rapid.IntRange(0, 48).Draw(t, "closeHoursBefore")) * time.Hour)
		rules := fmt.Sprintf("Yes if the upper bound of the federal funds rate is above 4.25%% following the meeting scheduled for %s. Expires %d:%02d %s ET.",
			date, hour, minute, meridiem)
		out := Normalize(fed("Fed funds rate after the meeting?", rules, closeTime))
		got := out.Terms.Deadline
		if got == nil || !got.Equal(want) || out.Terms.DeadlineTZ != "America/New_York" || !out.Quality.OK() {
			t.Fatalf("%q: deadline %v %s %v, want %v", rules, got, out.Terms.DeadlineTZ, out.Quality, want)
		}
	})
}

// TestFedExtractNeverPanicsProperty: any text, including hostile bytes, gives valid Terms.
func TestFedExtractNeverPanicsProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		title := rapid.String().Draw(t, "title")
		rules := rapid.String().Draw(t, "rules")
		out := Normalize(fed(title, rules, kxfedOctClose))
		if err := out.Terms.Validate(); err != nil {
			t.Fatalf("invalid terms for %q / %q: %v", title, rules, err)
		}
	})
}

func FuzzExtractFed(f *testing.F) {
	f.Add("Will the upper bound of the federal funds rate be above 2.75% following the Fed's Oct 28, 2026 meeting?", kxfedOctRules)
	f.Add("Will the Fed increase interest rates by 25 bps after the October 2026 meeting?", polyDecisionRules)
	f.Add("Will there be no change in Fed interest rates after the October 2026 meeting?", polyDecisionRules)
	f.Add("", "")
	f.Fuzz(func(t *testing.T, title, rules string) {
		out := Normalize(fed(title, rules, kxfedOctClose))
		if err := out.Terms.Validate(); err != nil {
			t.Fatal(err)
		}
	})
}
