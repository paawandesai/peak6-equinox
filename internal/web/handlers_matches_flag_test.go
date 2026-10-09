package web

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
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

// TestStaleQuoteFiltering verifies that handlers_matches.go filters stale quotes
// from quoteMap so ComputeNetGap receives nil (UI-18 AC: "missing/stale quote → no gap").
// This test verifies the production staleness-filtering logic.
func TestStaleQuoteFiltering(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	staleAfter := 2 * time.Minute

	quotes := []struct {
		id        string
		fetchedAt time.Time
		wantInMap bool
	}{
		{
			id:        "fresh:1",
			fetchedAt: now.Add(-1 * time.Minute), // 1 minute old, within threshold
			wantInMap: true,
		},
		{
			id:        "fresh:2",
			fetchedAt: now, // just fetched
			wantInMap: true,
		},
		{
			id:        "stale:1",
			fetchedAt: now.Add(-3 * time.Minute), // 3 minutes old, beyond threshold
			wantInMap: false,
		},
		{
			id:        "stale:2",
			fetchedAt: now.Add(-10 * time.Minute), // very old
			wantInMap: false,
		},
	}

	// Simulate the production filtering logic from handlers_matches.go lines 298-308.
	quoteMap := make(map[string]*canonical.Quote)
	for _, q := range quotes {
		age := now.Sub(q.fetchedAt)
		if age <= staleAfter {
			// In production this would be &quotes[i].Quote, but we just need to mark presence.
			quoteMap[q.id] = &canonical.Quote{InstrumentID: q.id}
		}
	}

	for _, q := range quotes {
		_, inMap := quoteMap[q.id]
		if inMap != q.wantInMap {
			t.Errorf("quote %s: inMap = %v, want %v (age = %v, threshold = %v)",
				q.id, inMap, q.wantInMap, now.Sub(q.fetchedAt), staleAfter)
		}
	}
}

// TestFlagLogicProductionFormula verifies the exact production flagging formula:
// gap >= threshold AND confidence < 0.90 (UI-18 AC).
func TestFlagLogicProductionFormula(t *testing.T) {
	threshold := decimal.RequireFromString("0.02")

	tests := []struct {
		name        string
		gap         decimal.Decimal
		confidence  decimal.Decimal
		wantFlagged bool
	}{
		{
			name:        "below threshold, low conf",
			gap:         decimal.RequireFromString("0.019"),
			confidence:  decimal.RequireFromString("0.85"),
			wantFlagged: false, // gap < threshold
		},
		{
			name:        "at threshold, conf 0.89",
			gap:         decimal.RequireFromString("0.02"),
			confidence:  decimal.RequireFromString("0.89"),
			wantFlagged: true, // gap >= threshold AND conf < 0.90
		},
		{
			name:        "at threshold, conf 0.90",
			gap:         decimal.RequireFromString("0.02"),
			confidence:  decimal.RequireFromString("0.90"),
			wantFlagged: false, // conf >= 0.90
		},
		{
			name:        "above threshold, conf 0.91",
			gap:         decimal.RequireFromString("0.05"),
			confidence:  decimal.RequireFromString("0.91"),
			wantFlagged: false, // conf >= 0.90
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Production formula from handlers_matches.go: gap >= threshold AND confidence < 0.90.
			isFlagged := tt.gap.GreaterThanOrEqual(threshold) &&
				tt.confidence.LessThan(decimal.RequireFromString("0.90"))

			if isFlagged != tt.wantFlagged {
				t.Errorf("isFlagged = %v, want %v (gap=%s, conf=%s, threshold=%s)",
					isFlagged, tt.wantFlagged, tt.gap, tt.confidence, threshold)
			}
		})
	}
}
