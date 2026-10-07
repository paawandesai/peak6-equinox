package normalize

import (
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Real venue text recorded on 2026-10-06 (testdata/fixtures: KXFED open and
// settled markets, Polymarket fed-rates events). Kalshi rules are the primary
// and secondary rules joined the way the Kalshi adapter joins them.
const (
	kxfedOctRules = "If the upper bound of the target federal funds rate published on the Federal Reserve's official website is greater than 2.75% " +
		"following the Federal Reserve's Oct 28, 2026 meeting, then the market resolves to Yes.\n\n" +
		"This market will expire the first 2:05 PM ET following the release of a Federal Reserve statement for their Oct 28, 2026 meeting " +
		"or one week following the last day of that meeting."
	kxfedDecRules = "If the upper bound of the target federal funds rate published on the Federal Reserve's official website is greater than 3.00% " +
		"following the Federal Reserve's Dec 9, 2026 meeting, then the market resolves to Yes.\n\n" +
		"This market will expire the first 2:05 PM ET following the release of a Federal Reserve statement for their Dec 9, 2026 meeting " +
		"or one week following the last day of that meeting."
	kxfedSepRules = "If the upper bound of the target federal funds rate published on the Federal Reserve's official website is greater than 5.25% " +
		"following the Federal Reserve's Sep 16, 2026 meeting, then the market resolves to Yes.\n\n" +
		"This market will expire the first 2:05 PM ET following the release of a Federal Reserve statement for their Sep 16, 2026 meeting " +
		"or one week following the last day of that meeting."
	polyDecisionRules = "The FED interest rates are defined in this market by the upper bound of the target federal funds range. " +
		"The decisions on the target federal funds range are made by the Federal Open Market Committee (FOMC) meetings.\n\n" +
		"This market will resolve to the amount of basis points the upper bound of the target federal funds rate is changed by versus " +
		"the level it was prior to the Federal Reserve's October 2026 meeting.\n\n" +
		"If the target federal funds rate is changed to a level not expressed in the displayed options, the change will be rounded up to " +
		"the nearest 25 and will resolve to the relevant bracket. (e.g. if there's a cut/increase of 12.5 bps it will be considered to be 25 bps)\n\n" +
		"The resolution source for this market is the FOMC’s statement after its meeting scheduled for October 27-28, 2026 according to the " +
		"official calendar: https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm.\n\n" +
		"The level and change of the target federal funds rate is also published at the official website of the Federal Reserve at " +
		"https://www.federalreserve.gov/monetarypolicy/openmarket.htm.\n\n" +
		"This market may resolve as soon as the FOMC’s statement for their October meeting with relevant data is issued. If no statement is " +
		"released by the end date of the next scheduled meeting, this market will resolve to the \"No change\" bracket."
	polyEndOfYearRules = "The FED rate is defined in this market by the upper bound of the target federal funds range. The decisions on the target " +
		"federal fund range are made by the Federal Open Market Committee (FOMC) meetings.\n\n" +
		"This market will resolve according to the upper bound of the Federal Reserve’s target federal funds range after the December 2026 " +
		"Federal Open Market Committee (FOMC) meeting, currently scheduled for December 8-9, 2026.\n\n" +
		"If no FOMC decision on the target federal funds range for their December meeting has been issued by December 31, 2026, 11:59 PM ET, " +
		"this market will resolve according to the upper bound of the target federal funds range at that time.\n\n" +
		"The upper bound of the target federal funds range will be rounded to the nearest 25 basis points for resolution of this market. " +
		"If the upper bound of the target federal funds range falls exactly between two listed options, it will be rounded away from zero " +
		"(e.g. if the upper bound is 2.875, with listed options of 3.0 & 2.75, this market will resolve to 3.0).\n\n" +
		"The primary resolution source for this market will be official information from the Federal Reserve (https://www.federalreserve.gov/monetarypolicy/openmarket.htm)."
	polyCutCountRules = "This market will resolve according to the exact amount of cuts of 25 basis points in 2026 by the Fed (including any cuts " +
		"made during the December meeting).\n\nEmergency rate cuts outside of scheduled FOMC meetings will also count toward the total number of " +
		"cuts in 2026. This market will remain open until December 31, 2026, 11:59 PM ET, to account for any such emergency actions.\n\n" +
		"For example, if the Fed cuts rates by 50 bps after a meeting, it would be considered 2 cuts (of 25 bps each).\n\n" +
		"The resolution source for this market will be FOMC statements after meetings scheduled in 2026 according to the official calendar: " +
		"https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm."
	polyReachRules = "The FED interest rates are defined in this market by the lower or the upper bound of the target federal funds range.\n\n" +
		"This market will resolve to “Yes” if the lower or the upper bound of the target federal funds rate reaches the specified level at any " +
		"point by December 31, 2026, 12:59 PM ET. Otherwise, this market will resolve to “No.”\n\n" +
		"The resolution source for this market is the official website of the Federal Reserve at:\nhttps://www.federalreserve.gov/monetarypolicy/openmarket.htm."
	polySequenceRules = "The FED interest rates are defined in this market by the upper bound of the target federal funds rate.\n\n" +
		"This market will resolve according to the decisions made by the next three Federal Open Market Committee (FOMC) meetings: " +
		"October 27 to 28; December 8 to 9; and January 26 to 27.\n\nA qualifying hike occurs when the new upper bound of the target federal " +
		"funds rate is higher compared to the level it was prior to the respective meeting.\n\n" +
		"The resolution source for this market is the FOMC’s statement after its meetings:\nhttps://www.federalreserve.gov/monetarypolicy/fomccalendars.htm"
	polyTreasuryRules = "This market will resolve to \"Yes\" if the Treasury 10-year yield is lower than the listed value for any date between " +
		"November 11, 2025 and December 31, 2026. Otherwise this market will resolve to \"No\".\n\nThe resolution source for this market is the " +
		"Department of the treasury, specially the data listed under \"Daily Treasury Par Yield Curve Rates\" for the column \"10 Yr\"."
	polyPowellRules = "This market will resolve to “Yes” if Jerome Powell ceases to hold a position on the Federal Reserve Board of Governors " +
		"for any period of time between this market's creation and the listed date, 11:59 PM ET. Otherwise, this market will resolve to “No”.\n\n" +
		"The resolution source for this market will be information from the U.S. Government; however, a consensus of credible reporting will also suffice."
)

var (
	kxfedOctClose  = time.Date(2026, 10, 28, 17, 55, 0, 0, time.UTC)
	kxfedDecClose  = time.Date(2026, 12, 9, 18, 55, 0, 0, time.UTC)
	kxfedSepClose  = time.Date(2026, 9, 16, 17, 55, 0, 0, time.UTC)
	polyOctClose   = time.Date(2026, 10, 29, 3, 59, 0, 0, time.UTC)
	polyYearClose  = time.Date(2027, 1, 1, 4, 59, 0, 0, time.UTC)
	kxfedOctDecide = utc(2026, 10, 28, 18, 5) // 2:05 PM EDT
	kxfedDecDecide = utc(2026, 12, 9, 19, 5)  // 2:05 PM EST
	kxfedSepDecide = utc(2026, 9, 16, 18, 5)  // 2:05 PM EDT
)

func fed(title, rules string, closeTime time.Time) canonical.Instrument {
	in := crypto(title, rules, closeTime)
	in.Category = canonical.CategoryFed
	return in
}

func checkTerms(t *testing.T, out canonical.Instrument, w want) {
	t.Helper()
	got := out.Terms
	if err := got.Validate(); err != nil {
		t.Errorf("terms invalid: %v", err)
	}
	if got.Subject != w.subject || got.Metric != w.metric || got.Comparator != w.comparator ||
		!decEqual(got.Threshold, w.threshold) || !decEqual(got.ThresholdHigh, w.high) || got.Unit != w.unit ||
		got.DeadlineTZ != w.tz || got.ResolutionSource != w.source || got.Polarity != w.polarity {
		t.Errorf("terms = %s %s %s %s..%s %s tz=%s src=%s pol=%s\n want %s %s %s %s..%s %s tz=%s src=%s pol=%s",
			got.Subject, got.Metric, got.Comparator, str(got.Threshold), str(got.ThresholdHigh), got.Unit, got.DeadlineTZ, got.ResolutionSource, got.Polarity,
			w.subject, w.metric, w.comparator, str(w.threshold), str(w.high), w.unit, w.tz, w.source, w.polarity)
	}
	switch {
	case (got.Deadline == nil) != (w.deadline == nil):
		t.Errorf("deadline = %v, want %v", got.Deadline, w.deadline)
	case got.Deadline != nil && !got.Deadline.Equal(*w.deadline):
		t.Errorf("deadline = %v, want %v", got.Deadline.UTC(), w.deadline.UTC())
	}
	for _, f := range w.flags {
		if !out.Quality.Has(f) {
			t.Errorf("missing quality flag %q in %v", f, out.Quality)
		}
	}
	if len(w.flags) == 0 && !out.Quality.OK() {
		t.Errorf("unexpected quality flags %v", out.Quality)
	}
}

func TestExtractFedRealVenueText(t *testing.T) {
	cases := []struct {
		name string
		in   canonical.Instrument
		want want
	}{
		// Kalshi KXFED ladders: a level of the upper bound after a dated meeting (recorded fixtures).
		{"kalshi oct ladder", fed("Will the upper bound of the federal funds rate be above 2.75% following the Fed's Oct 28, 2026 meeting?",
			kxfedOctRules, kxfedOctClose), want{
			SubjectFedFunds, MetricRateUpperBound, canonical.ComparatorGT, dec("2.75"), nil, UnitPercent,
			kxfedOctDecide, "America/New_York", SourceFederalReserve, canonical.PolarityPositive, nil}},
		{"kalshi dec ladder (EST)", fed("Will the upper bound of the federal funds rate be above 3.00% following the Fed's Dec 9, 2026 meeting?",
			kxfedDecRules, kxfedDecClose), want{
			SubjectFedFunds, MetricRateUpperBound, canonical.ComparatorGT, dec("3.00"), nil, UnitPercent,
			kxfedDecDecide, "America/New_York", SourceFederalReserve, canonical.PolarityPositive, nil}},
		{"kalshi sep ladder (settled)", fed("Will the upper bound of the federal funds rate be above 5.25% following the Fed's Sep 16, 2026 meeting?",
			kxfedSepRules, kxfedSepClose), want{
			SubjectFedFunds, MetricRateUpperBound, canonical.ComparatorGT, dec("5.25"), nil, UnitPercent,
			kxfedSepDecide, "America/New_York", SourceFederalReserve, canonical.PolarityPositive, nil}},
		// Polymarket "Fed Decision in October?" buckets: the rules resolve to the bucket named in the title; no time is
		// stated, so the deadline is the close time (flagged). The meeting is October 27-28 → both venues agree on Oct 28.
		{"polymarket 25 bps increase", fed("Will the Fed increase interest rates by 25 bps after the October 2026 meeting?", polyDecisionRules, polyOctClose), want{
			SubjectFedFunds, MetricRateChange, canonical.ComparatorEQ, dec("0.25"), nil, UnitPercent,
			&polyOctClose, canonical.Unknown, SourceFederalReserve, canonical.PolarityPositive, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		{"polymarket 50+ bps increase", fed("Will the Fed increase interest rates by 50+ bps after the October 2026 meeting?", polyDecisionRules, polyOctClose), want{
			SubjectFedFunds, MetricRateChange, canonical.ComparatorGTE, dec("0.5"), nil, UnitPercent,
			&polyOctClose, canonical.Unknown, SourceFederalReserve, canonical.PolarityPositive, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		{"polymarket 25 bps decrease", fed("Will the Fed decrease interest rates by 25 bps after the October 2026 meeting?", polyDecisionRules, polyOctClose), want{
			SubjectFedFunds, MetricRateChange, canonical.ComparatorEQ, dec("-0.25"), nil, UnitPercent,
			&polyOctClose, canonical.Unknown, SourceFederalReserve, canonical.PolarityPositive, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		{"polymarket 50+ bps decrease", fed("Will the Fed decrease interest rates by 50+ bps after the October 2026 meeting?", polyDecisionRules, polyOctClose), want{
			SubjectFedFunds, MetricRateChange, canonical.ComparatorLTE, dec("-0.5"), nil, UnitPercent,
			&polyOctClose, canonical.Unknown, SourceFederalReserve, canonical.PolarityPositive, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		{"polymarket no change", fed("Will there be no change in Fed interest rates after the October 2026 meeting?", polyDecisionRules, polyOctClose), want{
			SubjectFedFunds, MetricRateChange, canonical.ComparatorEQ, dec("0"), nil, UnitPercent,
			&polyOctClose, canonical.Unknown, SourceFederalReserve, canonical.PolarityPositive, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		// Polymarket end-of-year level: eq on the upper bound; the rules state two dates, so the deadline is the close time.
		{"polymarket end-of-year level", fed("Will the upper bound of the target federal funds rate be 2.25% at the end of 2026?", polyEndOfYearRules, polyYearClose), want{
			SubjectFedFunds, MetricRateUpperBound, canonical.ComparatorEQ, dec("2.25"), nil, UnitPercent,
			&polyYearClose, canonical.Unknown, SourceFederalReserve, canonical.PolarityPositive, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		// Adversarial and off-topic text: unknown, never a guess.
		{"cut count: example clause is not a condition", fed("Will 1 Fed rate cut happen in 2026?", polyCutCountRules, polyYearClose), want{
			SubjectFedFunds, canonical.Unknown, canonical.ComparatorUnknown, nil, nil, canonical.Unknown,
			utc(2027, 1, 1, 4, 59), "America/New_York", SourceFederalReserve, canonical.PolarityUnknown, nil}},
		{"path-dependent reach on either bound", fed("Will the Fed’s upper bound reach 5.0% or higher before 2027?", polyReachRules, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)), want{
			SubjectFedFunds, canonical.Unknown, canonical.ComparatorGTE, dec("5.0"), nil, UnitPercent,
			utc(2026, 12, 31, 17, 59), "America/New_York", SourceFederalReserve, canonical.PolarityPositive, nil}},
		{"decision sequence", fed("Will the Fed Hike–Pause–Hike in the next three decisions (Oct–Dec–Jan)?", polySequenceRules, time.Date(2027, 1, 27, 23, 59, 0, 0, time.UTC)), want{
			SubjectFedFunds, canonical.Unknown, canonical.ComparatorUnknown, nil, nil, canonical.Unknown,
			utc(2027, 1, 27, 23, 59), canonical.Unknown, SourceFederalReserve, canonical.PolarityUnknown, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		{"treasury yield carries the fed tag but is not the fed rate", fed("Will the 10-year Treasury yield dip below 4.0% before 2027?", polyTreasuryRules, time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)), want{
			canonical.Unknown, canonical.Unknown, canonical.ComparatorLT, dec("4.0"), nil, UnitPercent,
			utc(2026, 12, 31, 0, 0), canonical.Unknown, "treasury_gov", canonical.PolarityPositive, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}}},
		{"fed personnel", fed("Jerome Powell out from Fed Board by December 31?", polyPowellRules, polyYearClose), want{
			canonical.Unknown, canonical.Unknown, canonical.ComparatorUnknown, nil, nil, canonical.Unknown,
			utc(2027, 1, 1, 4, 59), "America/New_York", canonical.Unknown, canonical.PolarityUnknown, []canonical.QualityFlag{QualityDeadlineYearInferred}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkTerms(t, Normalize(tc.in), tc.want)
		})
	}
}

// TestExtractFedLevelVsChange: the same meeting stated as a level and as a
// change gives different metrics with thresholds in one unit (percentage
// points), and a text that states both is ambiguous.
func TestExtractFedLevelVsChange(t *testing.T) {
	rules := "If the upper bound of the target federal funds rate is greater than 4.25% following the Federal Reserve's Oct 28, 2026 meeting, " +
		"then the market resolves to Yes. Expires 2:05 PM ET on Oct 28, 2026."
	cases := []struct {
		name          string
		title, rules  string
		metric        string
		comparator    canonical.Comparator
		threshold     string
		ambiguousFlag bool
	}{
		{"level in rules", "Fed funds rate after Oct meeting?", rules, MetricRateUpperBound, canonical.ComparatorGT, "4.25", false},
		{"level in title only", "Will the upper bound of the fed funds rate be above 4.25% after the Oct 28, 2026 FOMC meeting?",
			"Resolves per the Federal Reserve statement. Expires 2:05 PM ET on Oct 28, 2026.", MetricRateUpperBound, canonical.ComparatorGT, "4.25", false},
		{"level at or above", "Fed?", "Yes if the upper bound of the federal funds rate is at or above 4.25% following the Oct 28, 2026 FOMC meeting at 2:05 PM ET.",
			MetricRateUpperBound, canonical.ComparatorGTE, "4.25", false},
		{"level or lower", "Fed?", "Yes if the upper bound of the federal funds rate is 4.25% or lower following the Oct 28, 2026 FOMC meeting at 2:05 PM ET.",
			MetricRateUpperBound, canonical.ComparatorLTE, "4.25", false},
		{"level between", "Fed?", "Yes if the upper bound of the federal funds rate is between 4.00% and 4.25% following the Oct 28, 2026 FOMC meeting at 2:05 PM ET.",
			MetricRateUpperBound, canonical.ComparatorBetween, "4", false},
		{"change by bps in rules", "Fed?", "Yes if the FOMC raises the target federal funds rate by 25 bps at its Oct 28, 2026 meeting (2:05 PM ET).",
			MetricRateChange, canonical.ComparatorEQ, "0.25", false},
		{"change by percent in rules", "Fed?", "Yes if the FOMC raises the target federal funds rate by 0.25% at its Oct 28, 2026 meeting (2:05 PM ET).",
			MetricRateChange, canonical.ComparatorEQ, "0.25", false},
		{"change by basis points, cut", "Fed?", "Yes if the Fed cuts rates by 50 basis points at its Oct 28, 2026 meeting (2:05 PM ET).",
			MetricRateChange, canonical.ComparatorEQ, "-0.5", false},
		{"change at least", "Fed?", "Yes if the Fed cuts rates by at least 50 bps at its Oct 28, 2026 meeting (2:05 PM ET).",
			MetricRateChange, canonical.ComparatorLTE, "-0.5", false},
		{"change or more", "Fed?", "Yes if the Fed hikes rates by 25 bps or more at its Oct 28, 2026 meeting (2:05 PM ET).",
			MetricRateChange, canonical.ComparatorGTE, "0.25", false},
		{"change more than", "Fed?", "Yes if the Fed cuts rates by more than 25 bps at its Oct 28, 2026 meeting (2:05 PM ET).",
			MetricRateChange, canonical.ComparatorLT, "-0.25", false},
		{"change noun form", "25 bps increase", "Fed Decision in October? Resolves to the amount of basis points the upper bound is changed by at the Oct 28, 2026 meeting (2:05 PM ET).",
			MetricRateChange, canonical.ComparatorEQ, "0.25", false},
		{"deferral with unparsed title bucket", "Fed Decision in October? — Other",
			"Resolves to the amount of basis points the upper bound is changed by at the Oct 28, 2026 meeting (2:05 PM ET).",
			MetricRateChange, canonical.ComparatorUnknown, "", false},
		{"both level and change is ambiguous", "Fed?",
			"Yes if the Fed cuts rates by 25 bps so that the upper bound is below 4.25% following the Oct 28, 2026 meeting (2:05 PM ET).",
			canonical.Unknown, canonical.ComparatorUnknown, "", true},
		{"two levels in the rules is ambiguous", "Fed?",
			"Yes if the upper bound of the federal funds rate is above 4.25% or above 4.50% following the Oct 28, 2026 meeting (2:05 PM ET).",
			canonical.Unknown, canonical.ComparatorUnknown, "", true},
		{"two changes in the rules is ambiguous", "Fed?",
			"Yes if the Fed cuts rates by 25 bps or cuts rates by 50 bps at its Oct 28, 2026 meeting (2:05 PM ET).",
			canonical.Unknown, canonical.ComparatorUnknown, "", true},
		{"lower bound is not the upper bound", "Fed?",
			"Yes if the lower bound of the federal funds rate is above 4.00% following the Oct 28, 2026 meeting (2:05 PM ET).",
			canonical.Unknown, canonical.ComparatorGT, "4.00", false},
		{"level without the bound named", "Fed?",
			"Yes if the federal funds rate is above 4.25% following the Oct 28, 2026 meeting (2:05 PM ET).",
			canonical.Unknown, canonical.ComparatorGT, "4.25", false},
		{"unparsed qualifier stays unknown", "Fed?", "Yes if the Fed cuts rates by up to 50 bps at its Oct 28, 2026 meeting (2:05 PM ET).",
			canonical.Unknown, canonical.ComparatorUnknown, "", false},
		{"level without a unit stays unknown", "Fed?", "Yes if the upper bound of the federal funds rate is above 4.25 following the Oct 28, 2026 meeting (2:05 PM ET).",
			canonical.Unknown, canonical.ComparatorUnknown, "", false},
		{"quoted no change names a bracket", "Fed?", "Otherwise this market resolves to the \"No change\" bracket after the Oct 28, 2026 meeting (2:05 PM ET).",
			canonical.Unknown, canonical.ComparatorUnknown, "", false},
		{"unquoted no change is the condition", "Fed?", "Yes if there is no change to the fed funds rate at the Oct 28, 2026 FOMC meeting (2:05 PM ET).",
			MetricRateChange, canonical.ComparatorEQ, "0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Normalize(fed(tc.title, tc.rules, kxfedOctClose))
			got := out.Terms
			if got.Metric != tc.metric || got.Comparator != tc.comparator {
				t.Errorf("metric/comparator = %s/%s, want %s/%s", got.Metric, got.Comparator, tc.metric, tc.comparator)
			}
			if tc.threshold == "" {
				if got.Threshold != nil {
					t.Errorf("threshold = %s, want nil", got.Threshold)
				}
			} else if !decEqual(got.Threshold, dec(tc.threshold)) || got.Unit != UnitPercent {
				t.Errorf("threshold = %s %s, want %s percent", str(got.Threshold), got.Unit, tc.threshold)
			}
			if out.Quality.Has(QualityTermsAmbiguous) != tc.ambiguousFlag {
				t.Errorf("terms_ambiguous = %v, want %v (%v)", !tc.ambiguousFlag, tc.ambiguousFlag, out.Quality)
			}
			if got.Subject != SubjectFedFunds {
				t.Errorf("subject = %q", got.Subject)
			}
		})
	}
}

func TestExtractFedMeetingDates(t *testing.T) {
	closeTime := time.Date(2026, 10, 28, 17, 55, 0, 0, time.UTC)
	cases := []struct {
		name     string
		rules    string
		deadline *time.Time
		tz       string
		flags    []canonical.QualityFlag
	}{
		{"single date with time", "Above 4.25% following the Oct 28, 2026 meeting. Expires 2:05 PM ET following the statement for the Oct 28, 2026 meeting.",
			utc(2026, 10, 28, 18, 5), "America/New_York", nil},
		{"range resolves on the last day", "Above 4.25% after the meeting scheduled for October 27-28, 2026. Expires 2:05 PM ET.",
			utc(2026, 10, 28, 18, 5), "America/New_York", nil},
		{"range with 'to'", "Above 4.25% after the meeting scheduled for October 27 to 28, 2026. Expires 2:05 PM ET.",
			utc(2026, 10, 28, 18, 5), "America/New_York", nil},
		{"range with en dash", "Above 4.25% after the meeting scheduled for October 27–28, 2026. Expires 2:05 PM ET.",
			utc(2026, 10, 28, 18, 5), "America/New_York", nil},
		{"range without a year is inferred from the close", "Above 4.25% after the meeting scheduled for October 27-28. Expires 2:05 PM ET.",
			utc(2026, 10, 28, 18, 5), "America/New_York", []canonical.QualityFlag{QualityDeadlineYearInferred}},
		{"date without a time defaults to the close", "Above 4.25% after the meeting scheduled for October 27-28, 2026.",
			&closeTime, canonical.Unknown, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}},
		{"month without a day defaults to the close", "Above 4.25% after the October 2026 meeting at 2:05 PM ET.",
			&closeTime, canonical.Unknown, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}},
		{"two dates is ambiguous", "Above 4.25% after the October 27-28, 2026 meeting or by November 4, 2026 at 2:05 PM ET.",
			&closeTime, canonical.Unknown, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}},
		{"a date far from the close is a misread", "Above 4.25% after the meeting on Oct 28, 2027 at 2:05 PM ET.",
			&closeTime, canonical.Unknown, []canonical.QualityFlag{canonical.QualityDeadlineDefaulted}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Normalize(fed("Fed funds rate after Oct 2026 meeting?", "Upper bound of the federal funds rate. "+tc.rules, closeTime))
			got := out.Terms
			if got.Deadline == nil || !got.Deadline.Equal(*tc.deadline) || got.DeadlineTZ != tc.tz {
				t.Errorf("deadline = %v %s, want %v %s", got.Deadline, got.DeadlineTZ, tc.deadline.UTC(), tc.tz)
			}
			for _, f := range tc.flags {
				if !out.Quality.Has(f) {
					t.Errorf("missing flag %q in %v", f, out.Quality)
				}
			}
			if len(tc.flags) == 0 && !out.Quality.OK() {
				t.Errorf("unexpected flags %v", out.Quality)
			}
		})
	}
}

func TestExtractFedPolarityAndSource(t *testing.T) {
	negative := Normalize(fed("Will the Fed not raise rates by 25 bps at the Oct 28, 2026 meeting?",
		"Yes if the FOMC does not raise the target rate by 25 bps at its Oct 28, 2026 meeting (2:05 PM ET), per the Federal Reserve statement.", kxfedOctClose))
	if negative.Terms.Polarity != canonical.PolarityNegative || negative.Terms.Comparator != canonical.ComparatorEQ ||
		!decEqual(negative.Terms.Threshold, dec("0.25")) {
		t.Errorf("negated change: %+v", negative.Terms)
	}
	sources := map[string]string{
		"The resolution source for this market is the FOMC's statement; credible reporting may also be used.": SourceFederalReserve,
		"Per federalreserve.gov.":                                                       SourceFederalReserve,
		"Per the Federal Reserve's official website.":                                   SourceFederalReserve,
		"Per official announcements from the Federal Reserve or credible news sources.": canonical.Unknown,
		"Per the CME FedWatch tool.":                                                    "cme_fedwatch",
		"Per a consensus of credible reporting.":                                        "credible_reporting",
		"No source named.":                                                              canonical.Unknown,
	}
	for rules, want := range sources {
		got := Normalize(fed("Fed funds rate after Oct meeting?", rules, kxfedOctClose)).Terms.ResolutionSource
		if got != want {
			t.Errorf("%q: source %q, want %q", rules, got, want)
		}
	}
}

func TestExtractFedHostileTextDoesNotPanic(t *testing.T) {
	huge := strings.Repeat("above 4.25% or above 4.50% cut by 25 bps 50+ bps increase no change October 27-28, 2026 2:05 PM ET ", 2000)
	in := fed(huge, huge+"\x00\xff\xfe", kxfedOctClose)
	out := Normalize(in)
	if err := out.Terms.Validate(); err != nil {
		t.Fatal(err)
	}
	if out.Terms.Comparator != canonical.ComparatorUnknown || out.Terms.Metric != canonical.Unknown {
		t.Errorf("repeated contradictory conditions gave %q %q", out.Terms.Metric, out.Terms.Comparator)
	}
	if out.Terms.Deadline == nil {
		t.Error("no deadline defaulted")
	}
}

func TestRegistry(t *testing.T) {
	got := Registered()
	want := map[canonical.Category]bool{canonical.CategoryCrypto: true, canonical.CategoryFed: true, canonical.CategoryPolitics: true}
	for _, c := range got {
		delete(want, c)
	}
	if len(want) != 0 {
		t.Errorf("Registered() = %v, missing %v", got, want)
	}

	// A new category dispatches through the registry without touching extract.go.
	const testCat canonical.Category = "registry_test"
	called := 0
	Register(testCat, func(canonical.Instrument) (canonical.Terms, canonical.Quality) {
		called++
		terms := canonical.UnknownTerms()
		terms.Subject = "from_registry"
		var q canonical.Quality
		q.Add(QualityTermsAmbiguous, "subject", "test")
		return terms, q
	})
	in := crypto("x", "y", closeOct6)
	in.Category = testCat
	out := Normalize(in)
	if called != 1 || out.Terms.Subject != "from_registry" || !out.Quality.Has(QualityTermsAmbiguous) {
		t.Errorf("registered extractor not used: called=%d terms=%+v quality=%v", called, out.Terms, out.Quality)
	}
	if Extract(in).Subject != "from_registry" {
		t.Error("Extract bypassed the registry")
	}

	for name, fn := range map[string]func(){
		"duplicate": func() {
			Register(testCat, func(canonical.Instrument) (canonical.Terms, canonical.Quality) { return canonical.UnknownTerms(), nil })
		},
		"nil": func() { Register("registry_nil", nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register %s did not panic", name)
				}
			}()
			fn()
		}()
	}
}
