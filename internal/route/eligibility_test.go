package route

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestIsEligible(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	baseCfg := EligibilityConfig{
		Snapshot:     now,
		StaleAfter:   120 * time.Second,
		SkewWarn:     5 * time.Second,
		SkewMax:      30 * time.Second,
		MinBookDepth: decimal.NewFromInt(1),
	}

	tests := []struct {
		name     string
		quote    canonical.Quote
		cfg      EligibilityConfig
		wantOK   bool
		wantReas []string // list of expected reasons
	}{
		{
			name: "happy path: fresh quote with book",
			quote: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now.Add(-10 * time.Second),
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("10")}},
				YesAsks:      []canonical.Level{{Price: dec("0.6"), Size: dec("10")}},
			},
			cfg:      baseCfg,
			wantOK:   true,
			wantReas: nil,
		},
		{
			name: "stale: older than StaleAfter",
			quote: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now.Add(-130 * time.Second),
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("10")}},
				YesAsks:      []canonical.Level{{Price: dec("0.6"), Size: dec("10")}},
			},
			cfg:      baseCfg,
			wantOK:   false,
			wantReas: []string{ReasonStale},
		},
		{
			name: "stale: missing FetchedAt",
			quote: canonical.Quote{
				InstrumentID: "test:3",
				FetchedAt:    time.Time{},
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("10")}},
				YesAsks:      []canonical.Level{{Price: dec("0.6"), Size: dec("10")}},
			},
			cfg:      baseCfg,
			wantOK:   false,
			wantReas: []string{ReasonStale},
		},
		{
			name: "missing book: Quality flag",
			quote: canonical.Quote{
				InstrumentID: "test:4",
				FetchedAt:    now.Add(-10 * time.Second),
				Quality:      canonical.Quality{{Flag: canonical.QualityNoBook}},
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("10")}},
			},
			cfg:      baseCfg,
			wantOK:   false,
			wantReas: []string{ReasonNoBook},
		},
		{
			name: "missing book: both sides empty",
			quote: canonical.Quote{
				InstrumentID: "test:5",
				FetchedAt:    now.Add(-10 * time.Second),
				YesBids:      []canonical.Level{},
				YesAsks:      []canonical.Level{},
			},
			cfg:      baseCfg,
			wantOK:   false,
			wantReas: []string{ReasonNoBook},
		},
		{
			name: "insufficient depth: total below MinBookDepth",
			quote: canonical.Quote{
				InstrumentID: "test:6",
				FetchedAt:    now.Add(-10 * time.Second),
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("0.3")}},
				YesAsks:      []canonical.Level{{Price: dec("0.6"), Size: dec("0.4")}},
			},
			cfg:      baseCfg,
			wantOK:   false,
			wantReas: []string{ReasonInsufficientDepth},
		},
		{
			name: "multiple failures: stale + no book",
			quote: canonical.Quote{
				InstrumentID: "test:7",
				FetchedAt:    now.Add(-200 * time.Second),
				YesBids:      []canonical.Level{},
				YesAsks:      []canonical.Level{},
			},
			cfg:      baseCfg,
			wantOK:   false,
			wantReas: []string{ReasonStale, ReasonNoBook},
		},
		{
			name: "edge: exactly at StaleAfter is still fresh",
			quote: canonical.Quote{
				InstrumentID: "test:8",
				FetchedAt:    now.Add(-120 * time.Second),
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("10")}},
				YesAsks:      []canonical.Level{{Price: dec("0.6"), Size: dec("10")}},
			},
			cfg:      baseCfg,
			wantOK:   true,
			wantReas: nil,
		},
		{
			name: "edge: exactly at MinBookDepth is eligible",
			quote: canonical.Quote{
				InstrumentID: "test:9",
				FetchedAt:    now.Add(-10 * time.Second),
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("0.5")}},
				YesAsks:      []canonical.Level{{Price: dec("0.6"), Size: dec("0.5")}},
			},
			cfg:      baseCfg,
			wantOK:   true,
			wantReas: nil,
		},
		{
			name: "one side only is fine if depth is sufficient",
			quote: canonical.Quote{
				InstrumentID: "test:10",
				FetchedAt:    now.Add(-10 * time.Second),
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("10")}},
				YesAsks:      []canonical.Level{},
			},
			cfg:      baseCfg,
			wantOK:   true,
			wantReas: nil,
		},
		{
			name: "snapshot staleness: 121s is stale (NFR-09, D-077)",
			quote: canonical.Quote{
				InstrumentID: "test:11",
				FetchedAt:    now.Add(-121 * time.Second),
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("10")}},
				YesAsks:      []canonical.Level{{Price: dec("0.6"), Size: dec("10")}},
			},
			cfg:      baseCfg,
			wantOK:   false,
			wantReas: []string{ReasonStale},
		},
		{
			name: "zero snapshot: fail closed (NFR-09, D-077)",
			quote: canonical.Quote{
				InstrumentID: "test:12",
				FetchedAt:    now.Add(-10 * time.Second),
				YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("10")}},
				YesAsks:      []canonical.Level{{Price: dec("0.6"), Size: dec("10")}},
			},
			cfg: EligibilityConfig{
				Snapshot:     time.Time{}, // zero
				StaleAfter:   120 * time.Second,
				SkewWarn:     5 * time.Second,
				SkewMax:      30 * time.Second,
				MinBookDepth: decimal.NewFromInt(1),
			},
			wantOK:   false,
			wantReas: []string{ReasonStale},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOK, gotExs := IsEligible(tt.quote, tt.cfg)
			if gotOK != tt.wantOK {
				t.Errorf("IsEligible() gotOK = %v, want %v", gotOK, tt.wantOK)
			}
			if len(gotExs) != len(tt.wantReas) {
				t.Fatalf("IsEligible() got %d exclusions, want %d: %+v", len(gotExs), len(tt.wantReas), gotExs)
			}
			for i, wantReason := range tt.wantReas {
				if gotExs[i].Reason != wantReason {
					t.Errorf("exclusion[%d].Reason = %q, want %q (detail: %s)", i, gotExs[i].Reason, wantReason, gotExs[i].Detail)
				}
			}
		})
	}
}

func TestSkew(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	baseCfg := EligibilityConfig{
		StaleAfter:   120 * time.Second,
		SkewWarn:     5 * time.Second,
		SkewMax:      30 * time.Second,
		MinBookDepth: decimal.NewFromInt(1),
	}

	tests := []struct {
		name             string
		q1               canonical.Quote
		q2               canonical.Quote
		cfg              EligibilityConfig
		wantExcludeOlder bool
		wantWarnings     int
	}{
		{
			name: "no skew: same time",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now,
			},
			cfg:              baseCfg,
			wantExcludeOlder: false,
			wantWarnings:     0,
		},
		{
			name: "small skew: under SkewWarn",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now.Add(-3 * time.Second),
			},
			cfg:              baseCfg,
			wantExcludeOlder: false,
			wantWarnings:     0,
		},
		{
			name: "medium skew: between SkewWarn and SkewMax",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now.Add(-10 * time.Second),
			},
			cfg:              baseCfg,
			wantExcludeOlder: false,
			wantWarnings:     1,
		},
		{
			name: "large skew: above SkewMax",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now.Add(-35 * time.Second),
			},
			cfg:              baseCfg,
			wantExcludeOlder: true,
			wantWarnings:     0,
		},
		{
			name: "edge: exactly at SkewWarn",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now.Add(-5 * time.Second),
			},
			cfg:              baseCfg,
			wantExcludeOlder: false,
			wantWarnings:     0,
		},
		{
			name: "edge: exactly at SkewMax",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now.Add(-30 * time.Second),
			},
			cfg:              baseCfg,
			wantExcludeOlder: false,
			wantWarnings:     1,
		},
		{
			name: "missing FetchedAt on q1",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    time.Time{},
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now,
			},
			cfg:              baseCfg,
			wantExcludeOlder: false,
			wantWarnings:     0,
		},
		{
			name: "missing FetchedAt on q2",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    time.Time{},
			},
			cfg:              baseCfg,
			wantExcludeOlder: false,
			wantWarnings:     0,
		},
		{
			name: "negative skew: q2 is newer",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now.Add(-40 * time.Second),
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now,
			},
			cfg:              baseCfg,
			wantExcludeOlder: true,
			wantWarnings:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotExclude, gotWarnings := Skew(tt.q1, tt.q2, tt.cfg)
			if gotExclude != tt.wantExcludeOlder {
				t.Errorf("Skew() excludeOlder = %v, want %v", gotExclude, tt.wantExcludeOlder)
			}
			if len(gotWarnings) != tt.wantWarnings {
				t.Errorf("Skew() warnings = %v, want %d warnings", gotWarnings, tt.wantWarnings)
			}
		})
	}
}

func TestSkewSymmetry(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate two random timestamps.
		now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
		offset1 := rapid.Int64Range(-1000, 1000).Draw(t, "offset1")
		offset2 := rapid.Int64Range(-1000, 1000).Draw(t, "offset2")
		t1 := now.Add(time.Duration(offset1) * time.Second)
		t2 := now.Add(time.Duration(offset2) * time.Second)

		q1 := canonical.Quote{
			InstrumentID: "test:A",
			FetchedAt:    t1,
		}
		q2 := canonical.Quote{
			InstrumentID: "test:B",
			FetchedAt:    t2,
		}

		cfg := EligibilityConfig{
			SkewWarn: 5 * time.Second,
			SkewMax:  30 * time.Second,
		}

		exclude1, warn1 := Skew(q1, q2, cfg)
		exclude2, warn2 := Skew(q2, q1, cfg)

		// The exclusion result should be the same (either both true or both false).
		if exclude1 != exclude2 {
			t.Fatalf("Skew is not symmetric: Skew(q1, q2) = %v, Skew(q2, q1) = %v", exclude1, exclude2)
		}

		// The warnings should also be symmetric in count (if not in content).
		if len(warn1) != len(warn2) {
			t.Fatalf("Warning count is not symmetric: Skew(q1, q2) = %d warnings, Skew(q2, q1) = %d warnings", len(warn1), len(warn2))
		}
	})
}

func TestOlderQuote(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		q1   canonical.Quote
		q2   canonical.Quote
		want string
	}{
		{
			name: "q1 is older",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now.Add(-10 * time.Second),
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now,
			},
			want: "test:1",
		},
		{
			name: "q2 is older",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now.Add(-10 * time.Second),
			},
			want: "test:2",
		},
		{
			name: "same time",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now,
			},
			want: "",
		},
		{
			name: "q1 missing FetchedAt",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    time.Time{},
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    now,
			},
			want: "",
		},
		{
			name: "q2 missing FetchedAt",
			q1: canonical.Quote{
				InstrumentID: "test:1",
				FetchedAt:    now,
			},
			q2: canonical.Quote{
				InstrumentID: "test:2",
				FetchedAt:    time.Time{},
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := OlderQuote(tt.q1, tt.q2)
			if got != tt.want {
				t.Errorf("OlderQuote() = %q, want %q", got, tt.want)
			}
		})
	}
}

// dec is a helper to create decimal values in tests.
func dec(s string) decimal.Decimal {
	return decimal.RequireFromString(s)
}

func TestSkewWithRouteIntegration(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cfg := EligibilityConfig{
		SkewWarn: 5 * time.Second,
		SkewMax:  30 * time.Second,
	}

	q1 := canonical.Quote{InstrumentID: "test:1", FetchedAt: now}
	q2 := canonical.Quote{InstrumentID: "test:2", FetchedAt: now.Add(-10 * time.Second)}

	exclude, warnings := Skew(q1, q2, cfg)
	if exclude {
		t.Errorf("10s skew should not exclude")
	}
	if len(warnings) == 0 {
		t.Errorf("10s skew should warn, got warnings=%v", warnings)
	}
	t.Logf("10s skew: exclude=%v, warnings=%v", exclude, warnings)
}
