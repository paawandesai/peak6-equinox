package normalize

import (
	"testing"
	"time"
	
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestExtractFedDecision(t *testing.T) {
	// Test KXFEDDECISION-26OCT-C26 "Cut >25bps"
	in := canonical.Instrument{
		ID:        "kalshi:KXFEDDECISION-26OCT-C26",
		Title:     "Will the Federal Reserve Cut rates by >25bps at their October 2026 meeting?",
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
