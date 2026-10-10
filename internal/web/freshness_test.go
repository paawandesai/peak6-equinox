package web

import (
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestQuoteFreshState(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	warnAfter := 60 * time.Second
	staleAfter := 120 * time.Second

	tests := []struct {
		name      string
		fetchedAt time.Time
		isPinned  bool
		want      FreshState
	}{
		{
			name:      "fresh: within warn window",
			fetchedAt: now.Add(-59 * time.Second),
			isPinned:  false,
			want:      FreshStateFresh,
		},
		{
			name:      "warn: at warn boundary",
			fetchedAt: now.Add(-60 * time.Second),
			isPinned:  false,
			want:      FreshStateWarn,
		},
		{
			name:      "warn: between warn and stale",
			fetchedAt: now.Add(-90 * time.Second),
			isPinned:  false,
			want:      FreshStateWarn,
		},
		{
			name:      "stale: at stale boundary",
			fetchedAt: now.Add(-121 * time.Second),
			isPinned:  false,
			want:      FreshStateStale,
		},
		{
			name:      "stale: well beyond stale",
			fetchedAt: now.Add(-300 * time.Second),
			isPinned:  false,
			want:      FreshStateStale,
		},
		{
			name:      "pinned: overrides stale",
			fetchedAt: now.Add(-300 * time.Second),
			isPinned:  true,
			want:      FreshStatePinned,
		},
		{
			name:      "pinned: overrides warn",
			fetchedAt: now.Add(-90 * time.Second),
			isPinned:  true,
			want:      FreshStatePinned,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := quoteFreshState(canonical.VenueKalshi, tt.fetchedAt, now, tt.isPinned, warnAfter, staleAfter)
			if got != tt.want {
				t.Errorf("quoteFreshState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWarnThresholdForVenue(t *testing.T) {
	s := &Server{
		cfg: Config{
			KalshiWarnAfter:     60 * time.Second,
			PolymarketWarnAfter: 90 * time.Second,
			StaleAfter:          120 * time.Second,
		},
	}

	tests := []struct {
		venue canonical.Venue
		want  time.Duration
	}{
		{canonical.VenueKalshi, 60 * time.Second},
		{canonical.VenuePolymarket, 90 * time.Second},
		{canonical.Venue("unknown"), 120 * time.Second}, // fallback
	}

	for _, tt := range tests {
		t.Run(string(tt.venue), func(t *testing.T) {
			got := s.warnThresholdForVenue(tt.venue)
			if got != tt.want {
				t.Errorf("warnThresholdForVenue(%q) = %v, want %v", tt.venue, got, tt.want)
			}
		})
	}
}
