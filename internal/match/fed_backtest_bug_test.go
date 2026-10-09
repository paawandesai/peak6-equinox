package match

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// TestFedDecisionH25DoesNotImply50Plus tests the backtest bug:
// H25 (exact 25bps hike) should NOT imply 50+ (50 or more bps hike).
// These are mutually exclusive buckets.
func TestFedDecisionH25DoesNotImply50Plus(t *testing.T) {
	deadline1 := time.Date(2026, 9, 16, 18, 5, 0, 0, time.UTC)
	deadline2 := time.Date(2026, 9, 17, 3, 59, 0, 0, time.UTC)

	// Kalshi KXFEDDECISION-26SEP-H25: "Hike 25bps" = eq 0.25
	thresh1 := decimal.RequireFromString("0.25")
	a := canonical.Instrument{
		ID:       "kalshi:KXFEDDECISION-26SEP-H25",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:          "fed_funds_rate",
			Metric:           "rate_change",
			Comparator:       canonical.ComparatorEQ,
			Threshold:        &thresh1,
			Unit:             "percent",
			Deadline:         &deadline1,
			ResolutionSource: "federalreserve_gov",
			Polarity:         canonical.PolarityPositive,
		},
	}

	// Polymarket "increase 50+ bps" = gte 0.50
	thresh2 := decimal.RequireFromString("0.50")
	b := canonical.Instrument{
		ID:       "polymarket:88912926533493988427719291698947688154042720958310632316541141466409683822293",
		Category: canonical.CategoryFed,
		Terms: canonical.Terms{
			Subject:          "fed_funds_rate",
			Metric:           "rate_change",
			Comparator:       canonical.ComparatorGTE,
			Threshold:        &thresh2,
			Unit:             "percent",
			Deadline:         &deadline2,
			ResolutionSource: "federalreserve_gov",
			Polarity:         canonical.PolarityPositive,
		},
	}

	// Create facts
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	f, err := facts.New(now,
		facts.Fact{
			ID:     FactFedRateStep,
			Value:  "0.25",
			Source: "https://www.federalreserve.gov/test",
			AsOf:   now,
		},
		facts.Fact{
			ID:     "fed_meeting_dates",
			Value:  "2026-09-16,2026-10-28,2026-12-09",
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

	// eq 0.25 (exact 25bps) and gte 0.50 (50+ bps) are DISJOINT intervals.
	// They should be "related" at most, NOT "implies".
	if rel.Type == canonical.RelImplies {
		t.Errorf("BUG: H25 (eq 0.25) should NOT imply 50+ (gte 0.50) - they are mutually exclusive buckets!")
		t.Errorf("Backtest shows: H25 settled YES, 50+ settled NO - a true implies would have both YES")
	}

	// The correct relationship is "related" (disjoint intervals on same measurement)
	if rel.Type != canonical.RelRelated {
		t.Errorf("Expected related (disjoint intervals), got %s", rel.Type)
	}
}
