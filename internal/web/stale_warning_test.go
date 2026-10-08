package web

import (
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func TestStaleRoundWarning(t *testing.T) {
	now := time.Date(2026, 10, 7, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		warnMinutes int
		roundAge    time.Duration // age of round snapshot relative to now
		wantWarning string
	}{
		{
			name:        "fresh round: no warning",
			warnMinutes: 10,
			roundAge:    5 * time.Minute,
			wantWarning: "",
		},
		{
			name:        "exactly at threshold: warn",
			warnMinutes: 10,
			roundAge:    10 * time.Minute,
			wantWarning: "Data is 10 minutes old.",
		},
		{
			name:        "stale round: show minutes",
			warnMinutes: 10,
			roundAge:    25 * time.Minute,
			wantWarning: "Data is 25 minutes old.",
		},
		{
			name:        "zero threshold: no warning",
			warnMinutes: 0,
			roundAge:    100 * time.Minute,
			wantWarning: "",
		},
		{
			name:        "negative threshold: no warning",
			warnMinutes: -5,
			roundAge:    100 * time.Minute,
			wantWarning: "",
		},
		{
			name:        "zero snapshot: no warning (fail-closed)",
			warnMinutes: 10,
			roundAge:    0, // will be set to zero snapshot below
			wantWarning: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(Config{StaleRoundWarnMinutes: tt.warnMinutes}, Deps{
				Now: func() time.Time { return now },
			})
			r := store.Round{
				ID:         1,
				StartedAt:  now.Add(-tt.roundAge),
				FinishedAt: now.Add(-tt.roundAge),
				Status:     store.RoundOK,
			}
			if tt.name == "zero snapshot: no warning (fail-closed)" {
				r.StartedAt = time.Time{}
				r.FinishedAt = time.Time{}
			}
			got := s.staleRoundWarning(r)
			if got != tt.wantWarning {
				t.Errorf("staleRoundWarning() = %q, want %q", got, tt.wantWarning)
			}
		})
	}
}
