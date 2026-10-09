package web

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"
)

// TestIsQuoteFresh tests production staleness filtering (handlers_matches.go line 304).
func TestIsQuoteFresh(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	staleAfter := 2 * time.Minute

	tests := []struct {
		name      string
		fetchedAt time.Time
		want      bool
	}{
		{
			name:      "fresh: just now",
			fetchedAt: now,
			want:      true,
		},
		{
			name:      "fresh: 1 minute old (within threshold)",
			fetchedAt: now.Add(-1 * time.Minute),
			want:      true,
		},
		{
			name:      "fresh: exactly at threshold",
			fetchedAt: now.Add(-2 * time.Minute),
			want:      true, // age <= staleAfter
		},
		{
			name:      "stale: 1 second beyond threshold",
			fetchedAt: now.Add(-2*time.Minute - time.Second),
			want:      false,
		},
		{
			name:      "stale: 3 minutes old",
			fetchedAt: now.Add(-3 * time.Minute),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isQuoteFresh(tt.fetchedAt, now, staleAfter)
			if got != tt.want {
				t.Errorf("isQuoteFresh(%v, %v, %v) = %v, want %v",
					tt.fetchedAt, now, staleAfter, got, tt.want)
			}
		})
	}
}

// TestShouldFlagForReview tests production flag predicate (handlers_matches.go line 483).
func TestShouldFlagForReview(t *testing.T) {
	threshold := decimal.RequireFromString("0.02")

	tests := []struct {
		name       string
		netGap     NetGap
		confidence string
		want       bool
	}{
		{
			name:       "no value: no flag",
			netGap:     NetGap{HasValue: false, Reason: "no quote"},
			confidence: "0.85",
			want:       false,
		},
		{
			name:       "gap below threshold: no flag",
			netGap:     NetGap{HasValue: true, Value: decimal.RequireFromString("0.019")},
			confidence: "0.85",
			want:       false,
		},
		{
			name:       "gap at threshold, conf 0.89: flag",
			netGap:     NetGap{HasValue: true, Value: decimal.RequireFromString("0.02")},
			confidence: "0.89",
			want:       true, // gap >= threshold AND conf < 0.90
		},
		{
			name:       "gap at threshold, conf exactly 0.90: no flag",
			netGap:     NetGap{HasValue: true, Value: decimal.RequireFromString("0.02")},
			confidence: "0.90",
			want:       false, // conf >= 0.90
		},
		{
			name:       "gap above threshold, conf 0.91: no flag",
			netGap:     NetGap{HasValue: true, Value: decimal.RequireFromString("0.05")},
			confidence: "0.91",
			want:       false, // conf >= 0.90
		},
		{
			name:       "negative gap (abs >= threshold), low conf: flag",
			netGap:     NetGap{HasValue: true, Value: decimal.RequireFromString("-0.03")},
			confidence: "0.85",
			want:       true, // uses Abs()
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldFlagForReview(tt.netGap, decimal.RequireFromString(tt.confidence), threshold)
			if got != tt.want {
				t.Errorf("shouldFlagForReview(gap=%v, conf=%s, threshold=%s) = %v, want %v",
					tt.netGap.Value, tt.confidence, threshold, got, tt.want)
			}
		})
	}
}

// TestShouldFlagForReview_Boundaries uses property-based testing for confidence boundaries.
func TestShouldFlagForReview_Boundaries(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate gap >= threshold (always eligible by gap criterion).
		gapCents := rapid.IntRange(2, 100).Draw(rt, "gapCents") // 2 to 100 cents
		gap := NetGap{
			HasValue: true,
			Value:    decimal.NewFromInt(int64(gapCents)).Div(decimal.NewFromInt(100)),
		}

		// Generate confidence in [0.80, 0.95] with high precision.
		confInt := rapid.IntRange(80, 95).Draw(rt, "confInt") // 0.80 to 0.95
		confidence := decimal.NewFromInt(int64(confInt)).Div(decimal.NewFromInt(100))

		threshold := decimal.RequireFromString("0.02")

		flagged := shouldFlagForReview(gap, confidence, threshold)

		// Property: flagged iff confidence < 0.90
		expectedFlag := confidence.LessThan(decimal.RequireFromString("0.90"))

		if flagged != expectedFlag {
			rt.Fatalf("gap=%s, conf=%s: flagged=%v, want=%v",
				gap.Value, confidence, flagged, expectedFlag)
		}
	})
}

// TestShouldFlagForReview_GapBoundaries uses property-based testing for gap boundaries.
func TestShouldFlagForReview_GapBoundaries(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate gap in [0, 0.10] (0 to 10 cents).
		gapCents := rapid.IntRange(0, 10).Draw(rt, "gapCents")
		gap := NetGap{
			HasValue: true,
			Value:    decimal.NewFromInt(int64(gapCents)).Div(decimal.NewFromInt(100)),
		}

		// Fix confidence < 0.90 so gap is the only variable.
		confidence := decimal.RequireFromString("0.85")
		threshold := decimal.RequireFromString("0.02")

		flagged := shouldFlagForReview(gap, confidence, threshold)

		// Property: flagged iff gap.Abs() >= threshold (and conf < 0.90, which is fixed true).
		expectedFlag := gap.Value.Abs().GreaterThanOrEqual(threshold)

		if flagged != expectedFlag {
			rt.Fatalf("gap=%s (cents=%d), conf=0.85: flagged=%v, want=%v",
				gap.Value, gapCents, flagged, expectedFlag)
		}
	})
}

// TestSortPairRows_FlaggedFirst tests production sort order: flagged pairs first (UI-18 AC).
func TestSortPairRows_FlaggedFirst(t *testing.T) {
	confVal := func(v float64) struct {
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
		{ID: 1, IsFlagged: false, ConfValue: confVal(0.95), ReviewPriority: 0.05},
		{ID: 2, IsFlagged: true, ConfValue: confVal(0.85), ReviewPriority: 0.15},
		{ID: 3, IsFlagged: false, ConfValue: confVal(0.88), ReviewPriority: 0.12},
		{ID: 4, IsFlagged: true, ConfValue: confVal(0.87), ReviewPriority: 0.13},
	}

	// Call production sortPairRows with "priority" order.
	sortPairRows(pairs, "priority")

	// Flagged pairs (2, 4) should sort before unflagged pairs (1, 3).
	if !pairs[0].IsFlagged || !pairs[1].IsFlagged {
		t.Errorf("first two pairs should be flagged, got IsFlagged = [%v, %v, %v, %v]",
			pairs[0].IsFlagged, pairs[1].IsFlagged, pairs[2].IsFlagged, pairs[3].IsFlagged)
	}
	if pairs[2].IsFlagged || pairs[3].IsFlagged {
		t.Errorf("last two pairs should not be flagged, got IsFlagged = [%v, %v]",
			pairs[2].IsFlagged, pairs[3].IsFlagged)
	}

	// Within flagged group, should sort by ReviewPriority (lower = closer to 0.90 = higher priority).
	// Pair 4 (priority 0.13) should come before pair 2 (priority 0.15) since 0.13 < 0.15.
	if pairs[0].ID != 4 || pairs[1].ID != 2 {
		t.Errorf("flagged pairs should sort by ReviewPriority, got IDs [%d, %d], want [4, 2]", pairs[0].ID, pairs[1].ID)
	}
}
