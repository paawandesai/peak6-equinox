package match

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
)

// TestFedDecisionGridBugVariants tests different interpretations
// of "50+" to find which one causes the backtest bug.
func TestFedDecisionGridBugVariants(t *testing.T) {
	deadline1 := time.Date(2026, 9, 16, 18, 5, 0, 0, time.UTC)
	deadline2 := time.Date(2026, 9, 17, 3, 59, 0, 0, time.UTC)

	// Kalshi H25: eq 0.25
	thresh1 := decimal.RequireFromString("0.25")
	h25 := canonical.Instrument{
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

	testCases := []struct {
		name       string
		comparator canonical.Comparator
		threshold  string
		shouldFail bool
	}{
		{"gte_0.50", canonical.ComparatorGTE, "0.50", false}, // Should be related (disjoint)
		{"gt_0.25", canonical.ComparatorGT, "0.25", true},    // This might be the bug!
		{"gt_0.49", canonical.ComparatorGT, "0.49", false},   // Should be related
		{"lte_0.50", canonical.ComparatorLTE, "0.50", false}, // Wrong direction but test it
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			thresh := decimal.RequireFromString(tc.threshold)
			poly50plus := canonical.Instrument{
				ID:       "polymarket:88912926533493988427719291698947688154042720958310632316541141466409683822293",
				Category: canonical.CategoryFed,
				Terms: canonical.Terms{
					Subject:          "fed_funds_rate",
					Metric:           "rate_change",
					Comparator:       tc.comparator,
					Threshold:        &thresh,
					Unit:             "percent",
					Deadline:         &deadline2,
					ResolutionSource: "federalreserve_gov",
					Polarity:         canonical.PolarityPositive,
				},
			}

			rel := Compare(h25, poly50plus, f)
			t.Logf("%s: Type=%s, Explanation=%s", tc.name, rel.Type, rel.Explanation)

			if tc.shouldFail && rel.Type == canonical.RelImplies {
				t.Errorf("FOUND THE BUG! H25 (eq 0.25) should NOT imply 50+ (%s %s)", tc.comparator, tc.threshold)
				t.Errorf("This is the case causing the backtest DISAGREED")
			}
		})
	}
}
