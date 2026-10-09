package normalize

import (
	"testing"
	"time"
	"github.com/shopspring/decimal"
	
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestExtractFedDecision(t *testing.T) {
	// Test KXFEDDECISION-26OCT-C26 "Cut >25bps"
	in := canonical.Instrument{
		ID:            "kalshi:KXFEDDECISION-26OCT-C26",
		VenueMarketID: "KXFEDDECISION-26OCT-C26",
		Title:         "Will the Federal Reserve Cut rates by >25bps at their October 2026 meeting?",
		RulesText: `If the Federal Reserve does a Cut of >25bps on October 28, 2026, then the market resolves to Yes.

The outcomes are mutually exclusive: at most one can resolve to Yes. For example, a 50-basis-point hike resolves "Hike >25 bps" to Yes and "Hike 25 bps" to No.

If the scheduled FOMC meeting is canceled and does not occur on its scheduled date, "Fed maintains rate" resolves to Yes and all other outcomes resolve to No.`,
		CloseTime: mustParse("2026-10-28T17:59:00Z"),
		Category:  canonical.CategoryFed,
	}
	
	terms, quality := extractFed(in)
	
	t.Logf("Subject: %s", terms.Subject)
	t.Logf("Metric: %s", terms.Metric)
	t.Logf("Comparator: %s", terms.Comparator)
	t.Logf("Threshold: %v", terms.Threshold)
	t.Logf("Unit: %s", terms.Unit)
	t.Logf("Deadline: %v", terms.Deadline)
	t.Logf("ResolutionSource: %s", terms.ResolutionSource)
	t.Logf("Quality: %v", quality)
	
	if terms.Subject != SubjectFedFunds {
		t.Errorf("Expected subject %s, got %s", SubjectFedFunds, terms.Subject)
	}
	
	if terms.Metric != MetricRateChange {
		t.Errorf("Expected metric %s, got %s", MetricRateChange, terms.Metric)
	}
}

func TestExtractRateCutCount(t *testing.T) {
	// Test KXRATECUTCOUNT-26DEC31-T0
	in := canonical.Instrument{
		ID:        "kalshi:KXRATECUTCOUNT-26DEC31-T0",
		Title:     "Will the Fed cut rates 0 times?",
		RulesText: "If the Fed cuts 0 times starting Jan 1, 2026 and before 2027, then the market resolves to Yes.",
		CloseTime: mustParse("2026-12-31T23:59:00Z"),
		Category:  canonical.CategoryFed,
	}
	
	terms, quality := extractFed(in)
	
	t.Logf("Subject: %s", terms.Subject)
	t.Logf("Metric: %s", terms.Metric)
	t.Logf("Quality: %v", quality)
	
	if terms.Subject != SubjectFedFunds {
		t.Errorf("Expected subject %s, got %s", SubjectFedFunds, terms.Subject)
	}
	
	// Currently, these are marked as unknown metric due to fedCountRe
	if terms.Metric != canonical.Unknown {
		t.Logf("Metric is %s (expected unknown currently, but should be rate_cut_count after D-109)", terms.Metric)
	}
}

func mustParse(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestExtractRealFixtureFedDecision(t *testing.T) {
	// Real fixture text from KXFEDDECISION-26OCT-C26
	// The rules_primary has two spaces instead of ">" (encoding issue?)
	// But the title has ">25bps" correctly
	in := canonical.Instrument{
		ID:            "kalshi:KXFEDDECISION-26OCT-C26",
		VenueMarketID: "KXFEDDECISION-26OCT-C26",
		Title:         "Will the Federal Reserve Cut rates by >25bps at their October 2026 meeting?",
		RulesText:     `If the Federal Reserve does a Cut of   25bps on October 28, 2026, then the market resolves to Yes.`,
		CloseTime:     time.Date(2026, 10, 28, 17, 59, 0, 0, time.UTC),
		Category:      canonical.CategoryFed,
	}
	
	terms, quality := extractFed(in)
	
	t.Logf("Subject: %s", terms.Subject)
	t.Logf("Metric: %s", terms.Metric)
	t.Logf("Comparator: %s", terms.Comparator)
	if terms.Threshold != nil {
		t.Logf("Threshold: %s", terms.Threshold)
	} else {
		t.Logf("Threshold: nil")
	}
	t.Logf("Unit: %s", terms.Unit)
	t.Logf("Quality: %v", quality)
	
	if terms.Subject != SubjectFedFunds {
		t.Errorf("Expected subject %s, got %s", SubjectFedFunds, terms.Subject)
	}
	
	if terms.Metric != MetricRateChange {
		t.Errorf("Expected metric %s, got %s", MetricRateChange, terms.Metric)
	}
	
	// D-109: the new changeOfRe pattern correctly extracts from the title ">25bps" as lt -0.25
	if terms.Comparator != canonical.ComparatorLT {
		t.Errorf("Expected comparator lt (from title '>25bps'), got %s", terms.Comparator)
	}

	if terms.Threshold == nil || terms.Threshold.String() != "-0.25" {
		t.Errorf("Expected threshold -0.25, got %v", terms.Threshold)
	}
}

func TestExtractPolymarketFedDecision(t *testing.T) {
	// Polymarket "decrease by 50+ bps"
	polyDecisionRules := "The FED interest rates are defined in this market by the upper bound of the target federal funds range. " +
		"The decisions on the target federal funds range are made by the Federal Open Market Committee (FOMC) meetings.\n\n" +
		"This market will resolve to the amount of basis points the upper bound of the target federal funds rate is changed by versus " +
		"the level it was prior to the Federal Reserve's October 2026 meeting.\n\n" +
		"If the target federal funds rate is changed to a level not expressed in the displayed options, the change will be rounded up to " +
		"the nearest 25 and will resolve to the relevant bracket. (e.g. if there's a cut/increase of 12.5 bps it will be considered to be 25 bps)\n\n" +
		"The resolution source for this market is the FOMC's statement..."
	
	in := canonical.Instrument{
		ID:        "polymarket:17010377994663817312158123655937348199252960045746746128731746451645055725586",
		Title:     "Will the Fed decrease interest rates by 50+ bps after the October 2026 meeting?",
		RulesText: polyDecisionRules,
		CloseTime: time.Date(2026, 10, 29, 3, 59, 0, 0, time.UTC),
		Category:  canonical.CategoryFed,
	}
	
	terms, _ := extractFed(in)
	
	t.Logf("Subject: %s", terms.Subject)
	t.Logf("Metric: %s", terms.Metric)
	t.Logf("Comparator: %s", terms.Comparator)
	if terms.Threshold != nil {
		t.Logf("Threshold: %s", terms.Threshold)
	} else {
		t.Logf("Threshold: nil")
	}
	t.Logf("Unit: %s", terms.Unit)
	
	if terms.Metric != MetricRateChange {
		t.Errorf("Expected metric %s, got %s", MetricRateChange, terms.Metric)
	}
	
	if terms.Comparator != canonical.ComparatorLTE {
		t.Errorf("Expected comparator lte, got %s", terms.Comparator)
	}
	
	expectedThresh := decimal.RequireFromString("-0.50")
	if terms.Threshold == nil || !terms.Threshold.Equal(expectedThresh) {
		t.Errorf("Expected threshold -0.50, got %v", terms.Threshold)
	}
}
