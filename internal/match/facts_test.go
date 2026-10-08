package match

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

var (
	octMeeting = time.Date(2026, 10, 28, 18, 5, 0, 0, time.UTC)
	octClose   = time.Date(2026, 10, 29, 3, 59, 0, 0, time.UTC)
	factsNow   = time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
)

func fedFact(id, value string) facts.Fact {
	return facts.Fact{ID: id, Value: value, Source: "https://www.federalreserve.gov/x", AsOf: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)}
}

func fedFacts(t testing.TB, fs ...facts.Fact) facts.Set {
	t.Helper()
	if fs == nil {
		fs = []facts.Fact{fedFact(FactFedUpperBound, "4.00"), fedFact(FactFedNextMeeting, "2026-10-28"), fedFact(FactFedRateStep, "0.25")}
	}
	s, err := facts.New(factsNow, fs...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func fedInstrument(id, metric string, cmp canonical.Comparator, threshold string, deadline time.Time) canonical.Instrument {
	dl := deadline
	return canonical.Instrument{
		ID: id, Venue: canonical.VenueOf(id), VenueMarketID: "X", Category: canonical.CategoryFed,
		Title: "fed", RulesText: "r", CloseTime: deadline, Status: canonical.StatusOpen,
		Terms: canonical.Terms{
			Subject: fedSubject, Metric: metric, Comparator: cmp, Threshold: dec(threshold), Unit: "percent",
			Deadline: &dl, DeadlineTZ: "America/New_York", ResolutionSource: "federalreserve_gov",
			EdgeRules: canonical.Unknown, Polarity: canonical.PolarityPositive,
		},
		FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
	}
}

func fedLevel(id, threshold string) canonical.Instrument {
	return fedInstrument(id, fedMetricUpperBound, canonical.ComparatorGT, threshold, octMeeting)
}

func fedChange(id string, cmp canonical.Comparator, threshold string) canonical.Instrument {
	in := fedInstrument(id, fedMetricChange, cmp, threshold, octClose)
	in.Quality.Add(canonical.QualityDeadlineDefaulted, "deadline", "close time")
	return in
}

func TestFedFactClosedPair(t *testing.T) {
	a, b := fedLevel("kalshi:L425", "4.25"), fedChange("polymarket:C50", canonical.ComparatorGTE, "0.5")
	rel := Compare(a, b, fedFacts(t))
	if rel.Type != canonical.RelEquivalentGivenFacts {
		t.Fatalf("type = %s: %s", rel.Type, rel.Explanation)
	}
	// 0.97 − 0.04 (edge rules unknown on both sides); the meeting fact replaces the deadline deductions.
	if !rel.Confidence.Equal(decimal.RequireFromString("0.93")) {
		t.Errorf("confidence = %s", rel.Confidence)
	}
	ids := []string{}
	for _, fu := range rel.FactsUsed {
		ids = append(ids, fu.Key)
	}
	if strings.Join(ids, ",") != "fed_upper_bound,fed_next_meeting,fed_rate_step" {
		t.Errorf("facts used = %v", ids)
	}
	for _, want := range []string{"equivalent_given_facts", "fed_upper_bound=4.00", "2026-10-28", "gte 4.5", "the same set", "metric (rate_upper_bound vs rate_change)"} {
		if !strings.Contains(rel.Explanation, want) {
			t.Errorf("explanation %q lacks %q", rel.Explanation, want)
		}
	}
	if rel.BasisRisk == nil {
		t.Fatal("no basis risk")
	}
	names := map[string]decimal.Decimal{}
	for _, c := range rel.BasisRisk.Components {
		names[c.Name] = c.Score
	}
	if !names[BasisFactDependency].Equal(decimal.RequireFromString("0.15")) || !names[BasisGridRounding].Equal(basisGrid) ||
		!names[BasisCutoffUncertain].Equal(basisCutoffUnsure) || !names[BasisSourceDiff].IsZero() {
		t.Errorf("components = %+v", rel.BasisRisk.Components)
	}
	rel.RoundID = 1
	if err := rel.Validate(); err != nil {
		t.Error(err)
	}
}

func TestFedRuleOtherOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		level   string
		cmp     canonical.Comparator
		change  string
		want    canonical.RelationshipType
		facts   int
		explain string
	}{
		{"exact hike inside the ladder", "4.00", canonical.ComparatorEQ, "0.25", canonical.RelImplies, 2, "forces YES on kalshi:L"},
		{"deep cut is the complement", "3.50", canonical.ComparatorLTE, "-0.5", canonical.RelComplement, 2, "pays exactly when NO on"},
		{"hold inside a lower ladder", "3.75", canonical.ComparatorEQ, "0", canonical.RelImplies, 2, "eq 4"},
		{"hike below the strike", "4.50", canonical.ComparatorEQ, "0.25", canonical.RelRelated, 3, "neither coincide"},
		{"between bucket on the grid", "4.00", canonical.ComparatorBetween, "0.25", canonical.RelImplies, 2, "between 4.25 and 4.75"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, b := fedLevel("kalshi:L", tc.level), fedChange("polymarket:C", tc.cmp, tc.change)
			if tc.cmp == canonical.ComparatorBetween {
				b.Terms.ThresholdHigh = dec("0.75")
			}
			rel := Compare(a, b, fedFacts(t))
			if rel.Type != tc.want || len(rel.FactsUsed) != tc.facts || !strings.Contains(rel.Explanation, tc.explain) {
				t.Errorf("type %s with %d facts: %s", rel.Type, len(rel.FactsUsed), rel.Explanation)
			}
			if rel.Type == canonical.RelRelated && (rel.BasisRisk != nil || !NearMiss(rel)) {
				t.Errorf("related with facts consulted: basis %v, near miss %v", rel.BasisRisk, NearMiss(rel))
			}
		})
	}
}

func TestFedRuleMissingOrBadFacts(t *testing.T) {
	a, b := fedLevel("kalshi:L", "4.25"), fedChange("polymarket:C", canonical.ComparatorGTE, "0.5")
	cases := []struct {
		name    string
		set     facts.Set
		facts   int
		explain string
	}{
		{"no facts", facts.Set{}, 0, "without fact fed_upper_bound"},
		{"no meeting", fedFacts(t, fedFact(FactFedUpperBound, "4.00")), 1, "without fact fed_next_meeting"},
		{"bad level", fedFacts(t, fedFact(FactFedUpperBound, "four"), fedFact(FactFedNextMeeting, "2026-10-28")), 1, "is not a number"},
		{"bad meeting", fedFacts(t, fedFact(FactFedUpperBound, "4.00"), fedFact(FactFedNextMeeting, "October")), 2, "is not a date"},
		{"other meeting", fedFacts(t, fedFact(FactFedUpperBound, "4.00"), fedFact(FactFedNextMeeting, "2026-12-09"), fedFact(FactFedRateStep, "0.25")), 2, "depends on the prior decision"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rel := Compare(a, b, tc.set)
			if rel.Type != canonical.RelRelated || len(rel.FactsUsed) != tc.facts || !strings.Contains(rel.Explanation, tc.explain) {
				t.Errorf("type %s, facts %d, %q", rel.Type, len(rel.FactsUsed), rel.Explanation)
			}
			if !rel.Confidence.Equal(confRelatedKnown) {
				t.Errorf("confidence %s", rel.Confidence)
			}
		})
	}
	// Without a usable grid the pair is still a true implication (gte 4.50
	// forces gt 4.25) on the two facts it does have; the step is not named.
	for name, set := range map[string]facts.Set{
		"no step":  fedFacts(t, fedFact(FactFedUpperBound, "4.00"), fedFact(FactFedNextMeeting, "2026-10-28")),
		"bad step": fedFacts(t, fedFact(FactFedUpperBound, "4.00"), fedFact(FactFedNextMeeting, "2026-10-28"), fedFact(FactFedRateStep, "0")),
	} {
		rel := Compare(a, b, set)
		if rel.Type != canonical.RelImplies || len(rel.FactsUsed) != 2 || strings.Contains(rel.Explanation, FactFedRateStep) {
			t.Errorf("%s: type %s, facts %d, %q", name, rel.Type, len(rel.FactsUsed), rel.Explanation)
		}
	}
	// When only the grid could have decided, its absence is named.
	far := fedLevel("kalshi:L450", "4.50")
	exact := fedChange("polymarket:C25", canonical.ComparatorEQ, "0.25")
	for name, tc := range map[string]struct {
		set     facts.Set
		explain string
	}{
		"no step":  {fedFacts(t, fedFact(FactFedUpperBound, "4.00"), fedFact(FactFedNextMeeting, "2026-10-28")), "fed_rate_step (the rate grid) is not available"},
		"bad step": {fedFacts(t, fedFact(FactFedUpperBound, "4.00"), fedFact(FactFedNextMeeting, "2026-10-28"), fedFact(FactFedRateStep, "-1")), "is not a positive number"},
	} {
		rel := Compare(far, exact, tc.set)
		if rel.Type != canonical.RelRelated || len(rel.FactsUsed) != 2 || !strings.Contains(rel.Explanation, tc.explain) {
			t.Errorf("%s: type %s, facts %d, %q", name, rel.Type, len(rel.FactsUsed), rel.Explanation)
		}
	}
}

func TestFedRuleNeedsBothSidesOnTheMeeting(t *testing.T) {
	a, b := fedLevel("kalshi:L", "4.25"), fedChange("polymarket:C", canonical.ComparatorGTE, "0.5")
	early := octMeeting.Add(-23 * time.Hour) // the day before: still within 24 h of a, but before the meeting day
	b.Terms.Deadline = &early
	rel := Compare(a, b, fedFacts(t))
	if rel.Type != canonical.RelRelated || !strings.Contains(rel.Explanation, "polymarket:C resolves on 2026-10-27") {
		t.Errorf("%s: %s", rel.Type, rel.Explanation)
	}
	if onMeeting(nil, octMeeting) {
		t.Error("nil deadline on meeting")
	}
}

func TestFedRuleIgnoresOtherCategoriesAndMetrics(t *testing.T) {
	a, b := fedLevel("kalshi:L", "4.25"), fedChange("polymarket:C", canonical.ComparatorGTE, "0.5")
	b.Terms.Metric = "rate_lower_bound"
	if rel := Compare(a, b, fedFacts(t)); rel.Type != canonical.RelRelated || len(rel.FactsUsed) != 0 {
		t.Errorf("unknown metric pair: %s %v", rel.Type, rel.FactsUsed)
	}
	a, b = pair() // crypto
	b.Terms.Metric = "price_high"
	if rel := Compare(a, b, fedFacts(t)); rel.Type != canonical.RelRelated || len(rel.FactsUsed) != 0 {
		t.Errorf("crypto metric pair: %s %v", rel.Type, rel.FactsUsed)
	}
}

// TestShippedFactsCarryTheMatcherIds: config/facts.yaml holds every fact the
// Fed rule consults, with the shapes it expects.
func TestShippedFactsCarryTheMatcherIds(t *testing.T) {
	s, err := facts.LoadAt("../../config/facts.yaml", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{FactFedUpperBound, FactFedNextMeeting, FactFedRateStep, "fed_meeting_dates"} {
		if _, ok := s.Get(id); !ok {
			t.Errorf("config/facts.yaml lacks %s", id)
		}
	}
	if f, _ := s.Get(FactFedUpperBound); f.Value != "" {
		if _, err := f.Decimal(); err != nil {
			t.Error(err)
		}
	}
	if f, _ := s.Get(FactFedNextMeeting); f.Value != "" {
		if _, err := time.Parse(facts.DateLayout, f.Value); err != nil {
			t.Error(err)
		}
	}
	if f, _ := s.Get(FactFedRateStep); f.Value != "" {
		if d, err := f.Decimal(); err != nil || !d.IsPositive() {
			t.Errorf("step %q: %v", f.Value, err)
		}
	}
	if f, _ := s.Get("fed_meeting_dates"); f.Value != "" {
		dates := splitCSV(f.Value)
		if len(dates) == 0 {
			t.Errorf("fed_meeting_dates is empty")
		}
		for _, d := range dates {
			if _, err := time.Parse(facts.DateLayout, d); err != nil {
				t.Errorf("fed_meeting_dates entry %q: %v", d, err)
			}
		}
	}
}

// TestNoNegativeRangeBucketBoundaries tests that the bucket math is correct:
// "no change" vs gt 0.25 is NOT implies, but "+25 bps" vs gt 0.25 IS implies.
func TestNoNegativeRangeBucketBoundaries(t *testing.T) {
	f := fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27"))

	// Level: KXFED-27JAN-T0.25 ("above 0.25%")
	level := canonical.Instrument{
		ID:       "kalshi:KXFED-27JAN-T0.25",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:    "fed_funds_rate",
			Metric:     "rate_upper_bound",
			Comparator: canonical.ComparatorGT,
			Threshold:  ptr(decimal.RequireFromString("0.25")),
			Unit:       "percent",
			Deadline:   ptr(time.Date(2027, 1, 27, 19, 0, 0, 0, time.UTC)),
			Polarity:   canonical.PolarityPositive,
		},
	}

	// Change: "no change" (eq 0) → new bound = 0.25, NOT > 0.25 → NOT implies
	noChange := canonical.Instrument{
		ID:       "polymarket:nochange",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:    "fed_funds_rate",
			Metric:     "rate_change",
			Comparator: canonical.ComparatorEQ,
			Threshold:  ptr(decimal.Zero),
			Unit:       "percent",
			Deadline:   ptr(time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC)),
			Polarity:   canonical.PolarityPositive,
		},
	}

	d, _ := deriveNoNegativeRange(level, noChange, f)
	if d != nil {
		t.Errorf("no change vs gt 0.25: expected nil, got %v", d.typ)
	}

	// Change: "+25 bps" (eq 0.25) → new bound >= 0.50 > 0.25 → implies
	plus25 := canonical.Instrument{
		ID:       "polymarket:plus25",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:    "fed_funds_rate",
			Metric:     "rate_change",
			Comparator: canonical.ComparatorEQ,
			Threshold:  ptr(decimal.RequireFromString("0.25")),
			Unit:       "percent",
			Deadline:   ptr(time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC)),
			Polarity:   canonical.PolarityPositive,
		},
	}

	d, _ = deriveNoNegativeRange(level, plus25, f)
	if d == nil || d.typ != canonical.RelImplies {
		t.Errorf("+25 bps vs gt 0.25: expected implies, got %v", d)
	}

	// "+50+ bps" (gte 0.50) → new bound >= 0.75 > 0.25 → implies
	plus50 := canonical.Instrument{
		ID:       "polymarket:plus50",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:    "fed_funds_rate",
			Metric:     "rate_change",
			Comparator: canonical.ComparatorGTE,
			Threshold:  ptr(decimal.RequireFromString("0.50")),
			Unit:       "percent",
			Deadline:   ptr(time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC)),
			Polarity:   canonical.PolarityPositive,
		},
	}

	d, _ = deriveNoNegativeRange(level, plus50, f)
	if d == nil || d.typ != canonical.RelImplies {
		t.Errorf("+50+ bps vs gt 0.25: expected implies, got %v", d)
	}
}

// TestNoNegativeRangeArgumentOrder tests that both argument orders work.
func TestNoNegativeRangeArgumentOrder(t *testing.T) {
	f := fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27"))

	level := canonical.Instrument{
		ID:       "kalshi:KXFED-27JAN-T0.50",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:    "fed_funds_rate",
			Metric:     "rate_upper_bound",
			Comparator: canonical.ComparatorGT,
			Threshold:  ptr(decimal.RequireFromString("0.50")),
			Unit:       "percent",
			Deadline:   ptr(time.Date(2027, 1, 27, 19, 0, 0, 0, time.UTC)),
			Polarity:   canonical.PolarityPositive,
		},
	}

	change := canonical.Instrument{
		ID:       "polymarket:plus50",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:    "fed_funds_rate",
			Metric:     "rate_change",
			Comparator: canonical.ComparatorGTE,
			Threshold:  ptr(decimal.RequireFromString("0.50")),
			Unit:       "percent",
			Deadline:   ptr(time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC)),
			Polarity:   canonical.PolarityPositive,
		},
	}

	// (level, change) order
	d1, _ := deriveNoNegativeRange(level, change, f)
	if d1 == nil || d1.typ != canonical.RelImplies {
		t.Errorf("(level, change): expected implies, got %v", d1)
	}
	if d1 != nil && d1.forward {
		t.Errorf("(level, change): expected forward=false (change → level), got forward=%v", d1.forward)
	}

	// (change, level) order
	d2, _ := deriveNoNegativeRange(change, level, f)
	if d2 == nil || d2.typ != canonical.RelImplies {
		t.Errorf("(change, level): expected implies, got %v", d2)
	}
	if d2 != nil && !d2.forward {
		t.Errorf("(change, level): expected forward=true (change → level), got forward=%v", d2.forward)
	}
}

// TestSameFedMeetingViaFacts tests that pairs are correctly assigned to meetings.
func TestSameFedMeetingViaFacts(t *testing.T) {
	f := fedFacts(t, fedFact("fed_meeting_dates", "2026-12-09,2027-01-27"))

	// Dec 9 meeting
	dec9 := canonical.Instrument{
		ID:       "kalshi:KXFED-26DEC-T2.75",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:  "fed_funds_rate",
			Deadline: ptr(time.Date(2026, 12, 9, 14, 0, 0, 0, time.UTC)),
		},
	}

	// Dec 31 fallback (should be nearest to Dec 9)
	dec31 := canonical.Instrument{
		ID:       "polymarket:dec",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:  "fed_funds_rate",
			Deadline: ptr(time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC)),
		},
	}

	// Jan 27 meeting
	jan27 := canonical.Instrument{
		ID:       "kalshi:KXFED-27JAN-T0.00",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:  "fed_funds_rate",
			Deadline: ptr(time.Date(2027, 1, 27, 19, 0, 0, 0, time.UTC)),
		},
	}

	// Same meeting: Dec 9 and Dec 31 should both map to Dec 9
	if !onSameFedMeetingViaFacts(dec9, dec31, f) {
		t.Errorf("Dec 9 vs Dec 31: expected same meeting (both nearest to Dec 9)")
	}

	// Different meetings: Dec 31 vs Jan 27
	if onSameFedMeetingViaFacts(dec31, jan27, f) {
		t.Errorf("Dec 31 vs Jan 27: expected different meetings (Dec 31 nearest to Dec 9, Jan 27 nearest to Jan 27)")
	}

	// Same meeting: both on Jan 27
	jan27b := jan27
	jan27b.ID = "polymarket:jan"
	jan27b.Terms.Deadline = ptr(time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC))
	if !onSameFedMeetingViaFacts(jan27, jan27b, f) {
		t.Errorf("Jan 27 vs Jan 27: expected same meeting")
	}
}

func ptr[T any](v T) *T { return &v }

// TestDeriveNoNegativeRangeCoverage tests error paths in deriveNoNegativeRange:
// not a Fed level/change pair, no proposition, unknown bucket, wrong level shape.
func TestDeriveNoNegativeRangeCoverage(t *testing.T) {
	jan27 := time.Date(2027, 1, 27, 19, 0, 0, 0, time.UTC)
	jan27b := time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC)
	f := fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27"))

	// Not a level/change pair (both are levels)
	level1 := fedLevel("kalshi:L1", "0.25")
	level1.Terms.Deadline = &jan27
	level2 := fedLevel("kalshi:L2", "0.50")
	level2.Terms.Deadline = &jan27b

	d, note := deriveNoNegativeRange(level1, level2, f)
	if d != nil {
		t.Errorf("both levels: expected nil, got %v", d.typ)
	}
	if len(note.facts) != 0 {
		t.Errorf("both levels: expected no facts, got %v", note.facts)
	}

	// No proposition (malformed terms)
	level := fedLevel("kalshi:L", "0.25")
	level.Terms.Deadline = &jan27
	change := fedChange("polymarket:C", canonical.ComparatorEQ, "0.25")
	change.Terms.Deadline = &jan27b
	change.Terms.Threshold = nil // Makes propositionOf fail

	d, note = deriveNoNegativeRange(level, change, f)
	if d != nil {
		t.Errorf("no proposition: expected nil, got %v", d.typ)
	}
	if len(note.facts) != 1 || note.facts[0].Key != "fed_meeting_dates" {
		t.Errorf("no proposition: expected [fed_meeting_dates], got %v", note.facts)
	}

	// Unknown bucket (not one of the supported buckets)
	level = fedLevel("kalshi:L", "0.25")
	level.Terms.Deadline = &jan27
	unknownBucket := fedChange("polymarket:C", canonical.ComparatorEQ, "0.10") // 10 bps, not supported
	unknownBucket.Terms.Deadline = &jan27b

	d, note = deriveNoNegativeRange(level, unknownBucket, f)
	if d != nil {
		t.Errorf("unknown bucket: expected nil, got %v", d.typ)
	}
	if len(note.facts) != 1 || note.facts[0].Key != "fed_meeting_dates" {
		t.Errorf("unknown bucket: expected [fed_meeting_dates], got %v", note.facts)
	}

	// Wrong level shape (bounded above, not "gt X")
	wrongLevel := fedInstrument("kalshi:WL", "rate_upper_bound", canonical.ComparatorBetween, "0.25", jan27)
	wrongLevel.Terms.ThresholdHigh = ptr(decimal.RequireFromString("0.50"))
	wrongLevel.Terms.Deadline = &jan27
	change = fedChange("polymarket:C", canonical.ComparatorEQ, "0.25")
	change.Terms.Deadline = &jan27b

	d, note = deriveNoNegativeRange(wrongLevel, change, f)
	if d != nil {
		t.Errorf("wrong level shape: expected nil, got %v", d.typ)
	}
	if len(note.facts) != 1 || note.facts[0].Key != "fed_meeting_dates" {
		t.Errorf("wrong level shape: expected [fed_meeting_dates], got %v", note.facts)
	}

	// Level threshold not forced by bucket (minResultingLevel <= pl.iv.lo)
	highLevel := fedLevel("kalshi:HL", "0.75")
	highLevel.Terms.Deadline = &jan27
	change = fedChange("polymarket:C", canonical.ComparatorEQ, "0.25") // +25 → 0.50, not > 0.75
	change.Terms.Deadline = &jan27b

	d, note = deriveNoNegativeRange(highLevel, change, f)
	if d != nil {
		t.Errorf("not forced: expected nil, got %v", d.typ)
	}
	if len(note.facts) != 1 || note.facts[0].Key != "fed_meeting_dates" {
		t.Errorf("not forced: expected [fed_meeting_dates], got %v", note.facts)
	}
}

// TestDeriveWithFactsFallsBackToNoNegativeRange tests that deriveWithFacts
// calls deriveNoNegativeRange as a fallback when fed_upper_bound is missing
// or when the meeting check fails.
func TestDeriveWithFactsFallsBackToNoNegativeRange(t *testing.T) {
	jan27 := time.Date(2027, 1, 27, 19, 0, 0, 0, time.UTC)
	jan27b := time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC)

	// Case 1: fed_upper_bound missing, falls back to no-negative-range
	fNoUpperBound := fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27"))
	level := fedLevel("kalshi:L", "0.25")
	level.Terms.Deadline = &jan27
	change := fedChange("polymarket:C", canonical.ComparatorEQ, "0.25")
	change.Terms.Deadline = &jan27b

	rel := Compare(level, change, fNoUpperBound)
	if rel.Type != canonical.RelImplies {
		t.Errorf("no fed_upper_bound: expected implies, got %s: %s", rel.Type, rel.Explanation)
	}
	found := false
	for _, fu := range rel.FactsUsed {
		if fu.Key == "fed_meeting_dates" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FactsUsed missing fed_meeting_dates: %v", rel.FactsUsed)
	}

	// Case 2: different meeting from fed_next_meeting, falls back to no-negative-range
	// Set fed_next_meeting to Dec 9, but instruments are on Jan 27
	fWrongMeeting := fedFacts(t, fedFact(FactFedUpperBound, "4.00"), fedFact(FactFedNextMeeting, "2026-12-09"),
		fedFact("fed_meeting_dates", "2027-01-27"))
	rel2 := Compare(level, change, fWrongMeeting)
	if rel2.Type != canonical.RelImplies {
		t.Errorf("wrong meeting: expected implies, got %s: %s", rel2.Type, rel2.Explanation)
	}
	found = false
	for _, fu := range rel2.FactsUsed {
		if fu.Key == "fed_meeting_dates" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FactsUsed missing fed_meeting_dates: %v", rel2.FactsUsed)
	}
}

// TestNoNegativeRangeRecordsFedMeetingDates tests that deriveNoNegativeRange
// records fed_meeting_dates in its fact list per FR-10/D-021.
func TestNoNegativeRangeRecordsFedMeetingDates(t *testing.T) {
	jan27 := time.Date(2027, 1, 27, 19, 0, 0, 0, time.UTC)
	f := fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27"))

	// "+25 bps" implies "gt 0.25" because new bound >= 0.50 > 0.25
	level := fedLevel("kalshi:L", "0.25")
	level.Terms.Deadline = &jan27
	change := fedChange("polymarket:C", canonical.ComparatorEQ, "0.25")
	jan27Close := time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC)
	change.Terms.Deadline = &jan27Close

	d, note := deriveNoNegativeRange(level, change, f)
	if d == nil || d.typ != canonical.RelImplies {
		t.Fatalf("expected implies, got %v", d)
	}
	if len(d.facts) != 1 || d.facts[0].Key != "fed_meeting_dates" {
		t.Errorf("d.facts = %v, expected [fed_meeting_dates]", d.facts)
	}
	if len(note.facts) != 1 || note.facts[0].Key != "fed_meeting_dates" {
		t.Errorf("note.facts = %v, expected [fed_meeting_dates]", note.facts)
	}
}

// TestBlockFedExceptionRecordsFedMeetingDates tests that when block()'s Fed
// exception allows a pair through, fed_meeting_dates is recorded per FR-10/D-021.
func TestBlockFedExceptionRecordsFedMeetingDates(t *testing.T) {
	f := fedFacts(t, fedFact("fed_meeting_dates", "2026-12-09"))

	// Dec 9 and Dec 31 are > 7 days apart but should be unblocked by Fed exception
	dec9Date := time.Date(2026, 12, 9, 14, 0, 0, 0, time.UTC)
	dec31Date := time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC)
	dec9 := fedLevel("kalshi:D9", "2.75")
	dec9.Terms.Deadline = &dec9Date
	dec31 := fedChange("polymarket:D31", canonical.ComparatorEQ, "0")
	dec31.Terms.Deadline = &dec31Date

	r, blockFacts := block(dec9, dec31, f)
	if r != nil {
		t.Errorf("expected unblocked, got %v", r)
	}
	if len(blockFacts) != 1 || blockFacts[0].Key != "fed_meeting_dates" {
		t.Errorf("blockFacts = %v, expected [fed_meeting_dates]", blockFacts)
	}

	// Now verify Compare() includes it in the final relationship
	rel := Compare(dec9, dec31, f)
	found := false
	for _, fu := range rel.FactsUsed {
		if fu.Key == "fed_meeting_dates" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Compare().FactsUsed = %v, missing fed_meeting_dates", rel.FactsUsed)
	}
}

// TestOnSameFedMeetingViaFactsCoverage tests error paths in onSameFedMeetingViaFacts:
// missing fact, empty CSV, bad CSV, parse errors.
func TestOnSameFedMeetingViaFactsCoverage(t *testing.T) {
	jan27 := time.Date(2027, 1, 27, 19, 0, 0, 0, time.UTC)
	jan27b := time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC)
	a := fedLevel("kalshi:A", "0.50")
	a.Terms.Deadline = &jan27
	b := fedChange("polymarket:B", canonical.ComparatorEQ, "0.25")
	b.Terms.Deadline = &jan27b

	// Missing fed_meeting_dates fact
	if onSameFedMeetingViaFacts(a, b, facts.Set{}) {
		t.Error("expected false when fed_meeting_dates missing")
	}

	// Empty CSV (can't create a fact with empty value, so test splitCSV directly)
	if len(splitCSV("")) != 0 {
		t.Error("expected splitCSV to return empty for empty string")
	}
	if len(splitCSV("  ,  , ")) != 0 {
		t.Error("expected splitCSV to return empty for whitespace-only string")
	}

	// Bad CSV: all dates unparseable
	fBad := fedFacts(t, fedFact("fed_meeting_dates", "not-a-date,also-bad"))
	if onSameFedMeetingViaFacts(a, b, fBad) {
		t.Error("expected false when all dates unparseable")
	}

	// Mixed good/bad dates: should use the parseable ones
	fMixed := fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27,bad-date,2026-12-09"))
	if !onSameFedMeetingViaFacts(a, b, fMixed) {
		t.Error("expected true when parseable dates exist (both near 2027-01-27)")
	}

	// Non-Fed category
	aCrypto := a
	aCrypto.Category = canonical.CategoryCrypto
	if onSameFedMeetingViaFacts(aCrypto, b, fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27"))) {
		t.Error("expected false for non-Fed category")
	}

	// Wrong subject
	aWrong := a
	aWrong.Terms.Subject = "other"
	if onSameFedMeetingViaFacts(aWrong, b, fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27"))) {
		t.Error("expected false for wrong subject")
	}

	// Nil deadline
	aNil := a
	aNil.Terms.Deadline = nil
	if onSameFedMeetingViaFacts(aNil, b, fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27"))) {
		t.Error("expected false for nil deadline")
	}
}

// TestNearestMeetingDistanceCap tests that nearestMeeting returns nil when
// the nearest meeting is beyond maxMeetingDistance, preventing out-of-calendar
// deadlines from bypassing the 7-day block check.
func TestNearestMeetingDistanceCap(t *testing.T) {
	dec9 := time.Date(2026, 12, 9, 0, 0, 0, 0, time.UTC)
	jan27 := time.Date(2027, 1, 27, 0, 0, 0, 0, time.UTC)

	// Within maxMeetingDistance: should return the nearest meeting
	nearDeadline := time.Date(2026, 12, 10, 0, 0, 0, 0, time.UTC) // 1 day after Dec 9
	nearest := nearestMeeting(nearDeadline, []time.Time{dec9, jan27})
	if nearest == nil || !nearest.Equal(dec9) {
		t.Errorf("nearDeadline: expected Dec 9, got %v", nearest)
	}

	// Dec 31 (22 days from Dec 9): within maxMeetingDistance (30 days)
	dec31 := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	nearest = nearestMeeting(dec31, []time.Time{dec9, jan27})
	if nearest == nil || !nearest.Equal(dec9) {
		t.Errorf("dec31: expected Dec 9, got %v", nearest)
	}

	// Use a single meeting to test the distance cap clearly
	singleMeeting := []time.Time{dec9}

	// Exactly at maxMeetingDistance edge: should return the meeting
	edgeDeadline := dec9.Add(maxMeetingDistance)
	nearest = nearestMeeting(edgeDeadline, singleMeeting)
	if nearest == nil || !nearest.Equal(dec9) {
		t.Errorf("edgeDeadline: expected Dec 9, got %v", nearest)
	}

	// Beyond maxMeetingDistance: should return nil
	farDeadline := dec9.Add(maxMeetingDistance + time.Hour)
	nearest = nearestMeeting(farDeadline, singleMeeting)
	if nearest != nil {
		t.Errorf("farDeadline: expected nil, got %v", nearest)
	}

	// Way outside the calendar (> 30 days from any meeting)
	wayOut := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC)
	nearest = nearestMeeting(wayOut, singleMeeting)
	if nearest != nil {
		t.Errorf("wayOut: expected nil, got %v", nearest)
	}

	// Empty meetings
	if nearestMeeting(nearDeadline, []time.Time{}) != nil {
		t.Error("expected nil for empty meetings")
	}
}

// TestCompareLevelDec9VsDec31Related tests at the Compare level that Dec 9 vs Dec 31
// (unblocked by Fed exception) produces a related verdict with fed_meeting_dates recorded.
func TestCompareLevelDec9VsDec31Related(t *testing.T) {
	f := fedFacts(t, fedFact("fed_meeting_dates", "2026-12-09"))

	dec9Date := time.Date(2026, 12, 9, 14, 0, 0, 0, time.UTC)
	dec31Date := time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC)
	dec9 := fedLevel("kalshi:D9", "2.75")
	dec9.Terms.Deadline = &dec9Date
	dec31 := fedChange("polymarket:D31", canonical.ComparatorEQ, "0.25")
	dec31.Terms.Deadline = &dec31Date

	rel := Compare(dec9, dec31, f)

	// Should be related because Fed exception unblocks it but metrics differ
	if rel.Type != canonical.RelRelated {
		t.Errorf("expected related, got %s: %s", rel.Type, rel.Explanation)
	}

	// Should record fed_meeting_dates
	found := false
	for _, fu := range rel.FactsUsed {
		if fu.Key == "fed_meeting_dates" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("FactsUsed = %v, missing fed_meeting_dates", rel.FactsUsed)
	}
}

// TestNoNegativeRangeT0Bucket tests the "no change" (eq 0) bucket case,
// which produces a new upper bound of exactly 0.25 and should NOT force
// a "gt 0.25" level.
func TestNoNegativeRangeT0Bucket(t *testing.T) {
	jan27 := time.Date(2027, 1, 27, 19, 0, 0, 0, time.UTC)
	jan27b := time.Date(2027, 1, 27, 14, 0, 0, 0, time.UTC)
	f := fedFacts(t, fedFact("fed_meeting_dates", "2027-01-27"))

	level := fedLevel("kalshi:L", "0.00")
	level.Terms.Deadline = &jan27
	noChange := fedChange("polymarket:C", canonical.ComparatorEQ, "0")
	noChange.Terms.Deadline = &jan27b

	// no change → new bound = 0.25; level is gt 0.00 → implies (0.25 > 0.00)
	d, _ := deriveNoNegativeRange(level, noChange, f)
	if d == nil || d.typ != canonical.RelImplies {
		t.Errorf("no change vs gt 0.00: expected implies, got %v", d)
	}

	// But vs gt 0.25: no change → 0.25 NOT > 0.25 → should NOT imply
	level25 := fedLevel("kalshi:L25", "0.25")
	level25.Terms.Deadline = &jan27
	d, _ = deriveNoNegativeRange(level25, noChange, f)
	if d != nil {
		t.Errorf("no change vs gt 0.25: expected nil, got %v", d.typ)
	}
}
