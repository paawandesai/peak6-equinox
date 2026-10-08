package security

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/collector"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

// TestNFR04_DemoZeroOutbound: when a round is pinned, all public pages and the
// router use only stored data and make 0 outbound venue calls (NFR-04). This test
// seeds a round, pins it, wraps venue adapters with PanicOnCallAdapter, and makes
// requests to all public pages and POST /route. If any handler tries to trigger a
// venue call, the adapter will panic and fail the test.
func TestNFR04_DemoZeroOutbound(t *testing.T) {
	ctx := context.Background()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	// Seed a round with minimal data for the test
	round, relID := seedRoundForDemo(ctx, t, st)

	// Pin the round
	if err := st.PinRound(ctx, round.ID); err != nil {
		t.Fatalf("pinning round: %v", err)
	}

	// Build server with PanicOnCallAdapter-wrapped venues in the collector
	// NFR-04: if any handler tries to trigger an ingest, the panic proves venue calls happened
	kalshiMock := &mockAdapter{venue: canonical.VenueKalshi}
	polymarketMock := &mockAdapter{venue: canonical.VenuePolymarket}
	reg, err := venues.NewRegistry(
		venues.NewPanicOnCallAdapter(kalshiMock),
		venues.NewPanicOnCallAdapter(polymarketMock),
	)
	if err != nil {
		t.Fatalf("creating registry: %v", err)
	}

	coll, err := collector.New(st, reg, venues.Universes{}, collector.Options{})
	if err != nil {
		t.Fatalf("creating collector: %v", err)
	}

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
		Store:    st,
		Ingester: coll,
		Now:      func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) },
	}
	s := web.New(cfg, deps)
	handler := s.Handler()

	// Test each public page
	tests := []struct {
		method string
		path   string
	}{
		{"GET", "/"},
		{"GET", "/near-misses"},
		{"GET", "/log"},
		{"GET", "/eval"},
		{"GET", "/pairs/" + strconv.FormatInt(relID, 10)},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status %d, want 200", rec.Code)
			}

			body := rec.Body.String()
			if !strings.Contains(body, "Demo mode") {
				t.Error("expected demo banner in response")
			}
			if !strings.Contains(body, "Unpin") {
				t.Error("expected Unpin link in demo banner")
			}
		})
	}

	// Test POST /route
	t.Run("POST /route", func(t *testing.T) {
		form := url.Values{}
		form.Set("pair", strconv.FormatInt(relID, 10))
		form.Set("target", "kalshi:KXTEST-YES")
		form.Set("side", "buy")
		form.Set("outcome", "yes")
		form.Set("quantity", "10")
		form.Set("type", "market")
		form.Set("tif", "IOC")

		req := httptest.NewRequest(http.MethodPost, "/route", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
		}

		body := rec.Body.String()
		if !strings.Contains(body, "Chosen") || !strings.Contains(body, "Decision") {
			t.Error("expected routing decision in response")
		}
	})
}

// TestNFR04_NoRoundFallback: when no round exists, pages show the fallback banner.
func TestNFR04_NoRoundFallback(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	cfg := web.Config{AppEnv: "ci", PublicRateLimitPerMin: 10000}
	deps := web.Deps{
		Store: st,
		Now:   func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) },
	}
	s := web.New(cfg, deps)
	handler := s.Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "No data available yet") {
		t.Error("expected no-round fallback banner")
	}
	if !strings.Contains(body, "/admin") {
		t.Error("expected /admin link in fallback banner")
	}
}

// mockAdapter is a minimal adapter for testing (used before wrapping with PanicOnCallAdapter).
type mockAdapter struct {
	venue canonical.Venue
}

func (m *mockAdapter) Venue() canonical.Venue { return m.venue }

func (m *mockAdapter) FetchInstruments(_ context.Context, _ venues.Universe) ([]canonical.Instrument, []venues.RawResponse, error) {
	return nil, nil, nil
}

func (m *mockAdapter) FetchQuotes(_ context.Context, _ []canonical.Instrument) ([]canonical.Quote, []venues.RawResponse, error) {
	return nil, nil, nil
}

func (m *mockAdapter) FetchSettled(_ context.Context, _ venues.Universe, _ time.Time) ([]canonical.Settled, []venues.RawResponse, error) {
	return nil, nil, nil
}

// seedRoundForDemo creates a minimal round with one relationship for testing demo mode.
func seedRoundForDemo(ctx context.Context, t *testing.T, st *store.Store) (store.Round, int64) {
	t.Helper()

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	// Create a round
	round, err := st.CreateRound(ctx, now)
	if err != nil {
		t.Fatalf("creating round: %v", err)
	}

	// Insert two instruments
	threshold := decimal.RequireFromString("1")
	aIns := canonical.Instrument{
		ID:            "kalshi:KXTEST-YES",
		Venue:         canonical.VenueKalshi,
		VenueMarketID: "KXTEST-YES",
		Category:      canonical.CategoryPolitics,
		Title:         "Test Event YES",
		Status:        canonical.StatusOpen,
		FeeSchedule: canonical.FeeSchedule{
			ID: "kalshi_taker_v1",
		},
		Terms: canonical.Terms{
			Subject:          "test",
			Metric:           "result",
			Comparator:       canonical.ComparatorEQ,
			Threshold:        &threshold,
			Unit:             canonical.Unknown,
			DeadlineTZ:       canonical.Unknown,
			ResolutionSource: canonical.Unknown,
			EdgeRules:        canonical.Unknown,
			Polarity:         canonical.PolarityPositive,
		},
	}
	bIns := canonical.Instrument{
		ID:            "polymarket:100",
		Venue:         canonical.VenuePolymarket,
		VenueMarketID: "test-market",
		Category:      canonical.CategoryPolitics,
		Title:         "Test Event YES",
		Status:        canonical.StatusOpen,
		FeeSchedule: canonical.FeeSchedule{
			ID: "polymarket_v1",
		},
		Terms: canonical.Terms{
			Subject:          "test",
			Metric:           "result",
			Comparator:       canonical.ComparatorEQ,
			Threshold:        &threshold,
			Unit:             canonical.Unknown,
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

	// Insert quotes
	aQuote := canonical.Quote{
		InstrumentID: aIns.ID,
		YesBids:      []canonical.Level{{Price: decimal.RequireFromString("0.50"), Size: decimal.RequireFromString("100")}},
		YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.51"), Size: decimal.RequireFromString("100")}},
		FetchedAt:    now,
	}
	bQuote := canonical.Quote{
		InstrumentID: bIns.ID,
		YesBids:      []canonical.Level{{Price: decimal.RequireFromString("0.49"), Size: decimal.RequireFromString("100")}},
		YesAsks:      []canonical.Level{{Price: decimal.RequireFromString("0.50"), Size: decimal.RequireFromString("100")}},
		FetchedAt:    now,
	}
	if err := st.InsertQuote(ctx, round.ID, aQuote); err != nil {
		t.Fatalf("inserting quote A: %v", err)
	}
	if err := st.InsertQuote(ctx, round.ID, bQuote); err != nil {
		t.Fatalf("inserting quote B: %v", err)
	}

	// Create a relationship
	rel := canonical.Relationship{
		RoundID:     round.ID,
		AID:         aIns.ID,
		BID:         bIns.ID,
		Type:        canonical.RelEquivalent,
		Confidence:  decimal.RequireFromString("0.95"),
		Method:      canonical.MethodRules,
		Explanation: "Test equivalent pair",
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
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, venueStatus, now.Add(time.Second)); err != nil {
		t.Fatalf("finishing round: %v", err)
	}

	// Reload the round to get the finished state
	round, err = st.GetRound(ctx, round.ID)
	if err != nil {
		t.Fatalf("reloading round: %v", err)
	}

	return round, relID
}
