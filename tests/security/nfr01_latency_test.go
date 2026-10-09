package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

// TestNFR01_PageLatencyP95 measures server-side p95 latency for page renders
// and routing simulation (NFR-01: pages <500ms, routing <300ms, issue #80).
func TestNFR01_PageLatencyP95(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping latency measurement in short mode")
	}

	ctx := context.Background()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	// Seed a round with minimal data for realistic page rendering
	round, pairID, instrumentID := seedRoundForLatency(ctx, t, st)

	cfg := web.Config{
		AppEnv:                "ci",
		PublicRateLimitPerMin: 10000,
		MinRouteConfidence:    decimal.RequireFromString("0.9"),
		BasisPenaltyMult:      decimal.RequireFromString("0.01"),
		StaleAfter:            120 * time.Second,
		SkewWarn:              5 * time.Second,
		SkewMax:               30 * time.Second,
		MinBookDepth:          decimal.RequireFromString("1"),
	}
	deps := web.Deps{
		Store: st,
		Now:   func() time.Time { return round.StartedAt.Add(1 * time.Second) },
	}
	s := web.New(cfg, deps)
	handler := s.Handler()

	// Measure page renders over N requests
	const numRequests = 20 // Sample size for p95 calculation

	pageTests := []struct {
		name   string
		method string
		path   string
		target time.Duration // NFR-01 target
	}{
		{"GET /", "GET", "/", 500 * time.Millisecond},
		{"GET /pairs/{id}", "GET", "/pairs/" + strconv.FormatInt(pairID, 10), 500 * time.Millisecond},
	}

	for _, tc := range pageTests {
		t.Run(tc.name, func(t *testing.T) {
			durations := make([]time.Duration, numRequests)

			for i := 0; i < numRequests; i++ {
				req := httptest.NewRequest(tc.method, tc.path, nil)
				w := httptest.NewRecorder()

				start := time.Now()
				handler.ServeHTTP(w, req)
				durations[i] = time.Since(start)

				if w.Code != http.StatusOK {
					t.Fatalf("request %d to %s: got status %d", i, tc.path, w.Code)
				}
			}

			p95 := percentile(durations, 0.95)
			median := percentile(durations, 0.50)
			mean := meanDuration(durations)
			t.Logf("%s: mean=%v, p50=%v, p95=%v (NFR-01 target <%v)", tc.name, mean, median, p95, tc.target)

			if p95 > tc.target {
				t.Logf("WARNING: p95 %v exceeds NFR-01 target %v", p95, tc.target)
			}
		})
	}

	// Measure routing simulation separately
	t.Run("POST /route", func(t *testing.T) {
		durations := make([]time.Duration, numRequests)
		form := url.Values{
			"target":     {instrumentID},
			"side":       {"buy"},
			"outcome":    {"yes"},
			"quantity":   {"100"},
			"order_type": {"market"},
			"tif":        {"ioc"},
		}

		for i := 0; i < numRequests; i++ {
			req := httptest.NewRequest("POST", "/route", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()

			start := time.Now()
			handler.ServeHTTP(w, req)
			durations[i] = time.Since(start)

			if w.Code != http.StatusOK {
				t.Fatalf("request %d to POST /route: got status %d", i, w.Code)
			}
		}

		p95 := percentile(durations, 0.95)
		median := percentile(durations, 0.50)
		mean := meanDuration(durations)
		target := 300 * time.Millisecond
		t.Logf("POST /route: mean=%v, p50=%v, p95=%v (NFR-01 target <%v)", mean, median, p95, target)

		if p95 > target {
			t.Logf("WARNING: p95 %v exceeds NFR-01 target %v", p95, target)
		}
	})
}

// seedRoundForLatency creates a minimal round with instruments and relationships for latency testing.
func seedRoundForLatency(ctx context.Context, t *testing.T, st *store.Store) (store.Round, int64, string) {
	t.Helper()

	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

	// Create a round
	round, err := st.CreateRound(ctx, now)
	if err != nil {
		t.Fatalf("creating round: %v", err)
	}

	// Insert two instruments
	threshold := decimal.RequireFromString("50")
	aIns := canonical.Instrument{
		ID:            "kalshi:KXLATENCY-A",
		Venue:         canonical.VenueKalshi,
		VenueMarketID: "KXLATENCY-A",
		Category:      canonical.CategoryCrypto,
		Title:         "Latency Test A",
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: "kalshi_taker_v1", Verified: true},
		Terms: canonical.Terms{
			Subject:          "btc",
			Metric:           "price",
			Comparator:       canonical.ComparatorGTE,
			Threshold:        &threshold,
			Unit:             "k",
			DeadlineTZ:       canonical.Unknown,
			ResolutionSource: canonical.Unknown,
			EdgeRules:        canonical.Unknown,
			Polarity:         canonical.PolarityPositive,
		},
	}
	bIns := canonical.Instrument{
		ID:            "polymarket:200",
		Venue:         canonical.VenuePolymarket,
		VenueMarketID: "latency-market",
		Category:      canonical.CategoryCrypto,
		Title:         "Latency Test B",
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: "polymarket_v1", Verified: true},
		Terms: canonical.Terms{
			Subject:          "btc",
			Metric:           "price",
			Comparator:       canonical.ComparatorGTE,
			Threshold:        &threshold,
			Unit:             "k",
			DeadlineTZ:       canonical.Unknown,
			ResolutionSource: canonical.Unknown,
			EdgeRules:        canonical.Unknown,
			Polarity:         canonical.PolarityPositive,
		},
	}

	if err := st.UpsertInstrument(ctx, round.ID, aIns); err != nil {
		t.Fatalf("inserting instrument A: %v", err)
	}
	if err := st.UpsertInstrument(ctx, round.ID, bIns); err != nil {
		t.Fatalf("inserting instrument B: %v", err)
	}

	// Insert quotes for both instruments
	aQuote := canonical.Quote{
		InstrumentID: aIns.ID,
		YesBids:      []canonical.Level{{Price: decimal.RequireFromString("0.50"), Size: decimal.RequireFromString("100")}},
		YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.52"), Size: decimal.RequireFromString("100")}},
		FetchedAt:    now,
	}
	bQuote := canonical.Quote{
		InstrumentID: bIns.ID,
		YesBids:      []canonical.Level{{Price: decimal.RequireFromString("0.48"), Size: decimal.RequireFromString("150")}},
		YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.49"), Size: decimal.RequireFromString("150")}},
		FetchedAt:    now,
	}

	if err := st.InsertQuote(ctx, round.ID, aQuote); err != nil {
		t.Fatalf("inserting quote A: %v", err)
	}
	if err := st.InsertQuote(ctx, round.ID, bQuote); err != nil {
		t.Fatalf("inserting quote B: %v", err)
	}

	// Create a relationship between the instruments
	rel := canonical.Relationship{
		RoundID:     round.ID,
		AID:         aIns.ID,
		BID:         bIns.ID,
		Type:        canonical.RelEquivalent,
		Confidence:  decimal.RequireFromString("0.95"),
		Method:      canonical.MethodRules,
		Explanation: "Test instruments for latency measurement",
	}

	relID, err := st.UpsertRelationship(ctx, rel)
	if err != nil {
		t.Fatalf("inserting relationship: %v", err)
	}

	// Finish the round
	venueStatus := map[canonical.Venue]store.VenueStatus{
		canonical.VenueKalshi:     {OK: true, Instruments: 1, Quotes: 1, DurationMS: 100},
		canonical.VenuePolymarket: {OK: true, Instruments: 1, Quotes: 1, DurationMS: 100},
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, venueStatus, now.Add(5*time.Second)); err != nil {
		t.Fatalf("finishing round: %v", err)
	}

	return round, relID, aIns.ID
}

// percentile returns the value at the given percentile (0.0 to 1.0) from a slice of durations.
func percentile(data []time.Duration, p float64) time.Duration {
	if len(data) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(data))
	copy(sorted, data)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	index := int(float64(len(sorted)) * p)
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

// meanDuration returns the arithmetic mean of a slice of durations.
func meanDuration(data []time.Duration) time.Duration {
	if len(data) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range data {
		sum += d
	}
	return sum / time.Duration(len(data))
}
