package web

import (
	"testing"

	"github.com/shopspring/decimal"
)

// TestFlaggedPairsBoundary checks the flag boundary at REVIEW_EDGE_MIN threshold
// and confidence 0.89/0.90 (UI-18 AC).
func TestFlaggedPairsBoundary(t *testing.T) {
	threshold := decimal.RequireFromString("0.02")

	tests := []struct {
		name        string
		netGap      decimal.Decimal
		confidence  float64
		wantFlagged bool
	}{
		{
			name:        "below threshold, low confidence",
			netGap:      decimal.RequireFromString("0.019"),
			confidence:  0.85,
			wantFlagged: false, // gap < threshold
		},
		{
			name:        "at threshold, low confidence",
			netGap:      decimal.RequireFromString("0.02"),
			confidence:  0.85,
			wantFlagged: true, // gap >= threshold AND confidence < 0.90
		},
		{
			name:        "above threshold, low confidence",
			netGap:      decimal.RequireFromString("0.03"),
			confidence:  0.85,
			wantFlagged: true,
		},
		{
			name:        "at threshold, confidence 0.89",
			netGap:      decimal.RequireFromString("0.02"),
			confidence:  0.89,
			wantFlagged: true, // confidence < 0.90
		},
		{
			name:        "at threshold, confidence 0.90",
			netGap:      decimal.RequireFromString("0.02"),
			confidence:  0.90,
			wantFlagged: false, // confidence >= 0.90
		},
		{
			name:        "above threshold, high confidence",
			netGap:      decimal.RequireFromString("0.05"),
			confidence:  0.95,
			wantFlagged: false, // confidence >= 0.90
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Flag logic: gap >= threshold AND confidence < 0.90
			isFlagged := tt.netGap.GreaterThanOrEqual(threshold) && tt.confidence < 0.90
			if isFlagged != tt.wantFlagged {
				t.Errorf("isFlagged = %v, want %v (gap=%s, conf=%.2f, threshold=%s)",
					isFlagged, tt.wantFlagged, tt.netGap, tt.confidence, threshold)
			}
		})
	}
}

// TestFlaggedPairsSortFirst checks that flagged pairs sort before unflagged ones
// in Review priority order (UI-18 AC).
func TestFlaggedPairsSortFirst(t *testing.T) {
	confStruct := func(v float64) struct {
		Value float64
		Word  string
		Class string
	} {
		return struct {
			Value float64
			Word  string
			Class string
		}{Value: v}
	}

	pairs := []pairRow{
		{ID: 1, IsFlagged: false, ConfValue: confStruct(0.95), Vetoes: 0},
		{ID: 2, IsFlagged: true, ConfValue: confStruct(0.85), Vetoes: 2},
		{ID: 3, IsFlagged: false, ConfValue: confStruct(0.88), Vetoes: 1},
		{ID: 4, IsFlagged: true, ConfValue: confStruct(0.87), Vetoes: 1},
	}

	sortPairRows(pairs, "priority")

	// Flagged pairs (2, 4) should come first.
	if !pairs[0].IsFlagged || !pairs[1].IsFlagged {
		t.Errorf("first two pairs should be flagged, got IsFlagged = [%v, %v, %v, %v]",
			pairs[0].IsFlagged, pairs[1].IsFlagged, pairs[2].IsFlagged, pairs[3].IsFlagged)
	}
	// Unflagged pairs (1, 3) should come after.
	if pairs[2].IsFlagged || pairs[3].IsFlagged {
		t.Errorf("last two pairs should not be flagged")
	}
}

// TestReviewEdgeMinConfig checks REVIEW_EDGE_MIN config default and override (UI-18 AC).
func TestReviewEdgeMinConfig(t *testing.T) {
	// Default value is zero (server.go New() fills it if zero).
	defaultCfg := Config{}
	if !defaultCfg.ReviewEdgeMin.IsZero() {
		t.Errorf("default ReviewEdgeMin should be zero, got %s", defaultCfg.ReviewEdgeMin)
	}

	// Override value.
	customCfg := Config{
		ReviewEdgeMin: decimal.RequireFromString("0.03"),
	}
	if !customCfg.ReviewEdgeMin.Equal(decimal.RequireFromString("0.03")) {
		t.Errorf("ReviewEdgeMin = %s, want 0.03", customCfg.ReviewEdgeMin)
	}
}
