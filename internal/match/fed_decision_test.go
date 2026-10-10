package match

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// TestFedDecisionSameMetric tests same-metric Fed decision pairs (D-109).
func TestFedDecisionSameMetric(t *testing.T) {
	deadline1, _ := time.Parse(time.RFC3339, "2026-10-28T18:05:00Z")
	deadline2, _ := time.Parse(time.RFC3339, "2026-10-29T03:59:00Z")

	thresh1 := decimal.RequireFromString("-0.25")
	thresh2 := decimal.RequireFromString("-0.50")

	// Kalshi KXFEDDECISION-26OCT-C26: "Cut >25bps" = change < -0.25
	a := canonical.Instrument{
		ID:       "kalshi:KXFEDDECISION-26OCT-C26",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:          "fed_funds_rate",
			Metric:           "rate_change",
			Comparator:       canonical.ComparatorLT,
			Threshold:        &thresh1,
			Unit:             "percent",
			Deadline:         &deadline1,
			ResolutionSource: "federalreserve_gov",
			Polarity:         canonical.PolarityPositive,
		},
	}

	// Polymarket "decrease by 50+ bps" = change <= -0.50
	b := canonical.Instrument{
		ID:       "polymarket:test",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:          "fed_funds_rate",
			Metric:           "rate_change",
			Comparator:       canonical.ComparatorLTE,
			Threshold:        &thresh2,
			Unit:             "percent",
			Deadline:         &deadline2,
			ResolutionSource: "federalreserve_gov",
			Polarity:         canonical.PolarityPositive,
		},
	}

	// Create facts
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	f, err := facts.New(now,
		facts.Fact{
			ID:     FactFedRateStep,
			Value:  "0.25",
			Source: "https://www.federalreserve.gov/test",
			AsOf:   now,
		},
		facts.Fact{
			ID:     "fed_meeting_dates",
			Value:  "2026-10-28,2026-12-09,2027-01-27",
			Source: "https://www.federalreserve.gov/test",
			AsOf:   now,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	rel := Compare(a, b, f)

	if rel.Type != canonical.RelEquivalentGivenFacts {
		t.Errorf("Expected equivalent_given_facts, got %s: %s", rel.Type, rel.Explanation)
	}

	if !rel.Confidence.GreaterThan(decimal.RequireFromString("0.9")) {
		t.Errorf("Expected high confidence, got %s", rel.Confidence)
	}

	// Check facts used
	hasRateStep := false
	hasMeetingDates := false
	for _, fu := range rel.FactsUsed {
		if fu.Key == FactFedRateStep {
			hasRateStep = true
		}
		if fu.Key == "fed_meeting_dates" {
			hasMeetingDates = true
		}
	}

	if !hasRateStep || !hasMeetingDates {
		t.Errorf("Expected both fed_rate_step and fed_meeting_dates in facts used, got %v", rel.FactsUsed)
	}
}

func TestFedDecisionRealFixtures(t *testing.T) {
	// Test with realistic normalized terms matching the actual fixtures
	deadline1 := time.Date(2026, 10, 28, 18, 5, 0, 0, time.UTC)
	deadline2 := time.Date(2026, 10, 29, 3, 59, 0, 0, time.UTC)

	thresh1 := decimal.RequireFromString("-0.25")
	thresh2 := decimal.RequireFromString("-0.50")

	// Kalshi KXFEDDECISION-26OCT-C26: "Cut >25bps" = lt -0.25
	a := canonical.Instrument{
		ID:       "kalshi:KXFEDDECISION-26OCT-C26",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:          "fed_funds_rate",
			Metric:           "rate_change",
			Comparator:       canonical.ComparatorLT,
			Threshold:        &thresh1,
			Unit:             "percent",
			Deadline:         &deadline1,
			ResolutionSource: "federalreserve_gov",
			Polarity:         canonical.PolarityPositive,
		},
	}

	// Polymarket "decrease by 50+ bps" = lte -0.50
	b := canonical.Instrument{
		ID:       "polymarket:17010377994663817312158123655937348199252960045746746128731746451645055725586",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:          "fed_funds_rate",
			Metric:           "rate_change",
			Comparator:       canonical.ComparatorLTE,
			Threshold:        &thresh2,
			Unit:             "percent",
			Deadline:         &deadline2,
			ResolutionSource: "federalreserve_gov",
			Polarity:         canonical.PolarityPositive,
		},
	}

	// Create facts
	now := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	f, err := facts.New(now,
		facts.Fact{
			ID:     FactFedRateStep,
			Value:  "0.25",
			Source: "https://www.federalreserve.gov/test",
			AsOf:   now,
		},
		facts.Fact{
			ID:     "fed_meeting_dates",
			Value:  "2026-10-28,2026-12-09,2027-01-27",
			Source: "https://www.federalreserve.gov/test",
			AsOf:   now,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	rel := Compare(a, b, f)

	t.Logf("Type: %s", rel.Type)
	t.Logf("Explanation: %s", rel.Explanation)
	t.Logf("Confidence: %s", rel.Confidence)

	if rel.Type != canonical.RelEquivalentGivenFacts {
		t.Errorf("Expected equivalent_given_facts, got %s", rel.Type)
	}
}
