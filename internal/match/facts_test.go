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
	for _, id := range []string{FactFedUpperBound, FactFedNextMeeting, FactFedRateStep} {
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
}
