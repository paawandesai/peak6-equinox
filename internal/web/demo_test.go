package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// newBrokenPool creates a pool with an invalid connection string to trigger DB errors.
func newBrokenPool(ctx context.Context) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
}

// TestDemoModeBanner: table-driven tests for M3-07 demo mode banner behavior.
// Covers: pinned (every page shows banner), unpinned (latest round, no banner),
// zero rounds (fallback banner).
func TestDemoModeBanner(t *testing.T) {
	ctx := context.Background()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	// Seed two rounds
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	round1, err := st.CreateRound(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round1.ID, store.RoundOK, nil, now.Add(-55*time.Minute)); err != nil {
		t.Fatal(err)
	}
	round2, err := st.CreateRound(ctx, now.Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round2.ID, store.RoundOK, nil, now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Create a relationship in round1 for pair page test (round1 will be pinned)
	threshold := decimal.RequireFromString("1")
	insA := canonical.Instrument{
		ID:            "kalshi:TESTA",
		Venue:         canonical.VenueKalshi,
		VenueMarketID: "TESTA",
		Category:      canonical.CategoryPolitics,
		Title:         "Test A",
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: "kalshi_taker_v1"},
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
	insB := canonical.Instrument{
		ID:            "polymarket:TESTB",
		Venue:         canonical.VenuePolymarket,
		VenueMarketID: "TESTB",
		Category:      canonical.CategoryPolitics,
		Title:         "Test B",
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: "polymarket_v1"},
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
	if err := st.UpsertInstrument(ctx, round1.ID, insA); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertInstrument(ctx, round1.ID, insB); err != nil {
		t.Fatal(err)
	}
	rel := canonical.Relationship{
		RoundID:     round1.ID,
		AID:         insA.ID,
		BID:         insB.ID,
		Type:        canonical.RelEquivalent,
		Confidence:  decimal.RequireFromString("0.95"),
		Method:      canonical.MethodRules,
		Explanation: "test",
	}
	relID, err := st.UpsertRelationship(ctx, rel)
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{AppEnv: "ci", PublicRateLimitPerMin: 10000, MinRouteConfidence: decimal.RequireFromString("0.9")}
	deps := Deps{Store: st, Now: func() time.Time { return now }}

	tests := []struct {
		name         string
		setup        func()
		path         string
		expectBanner string // substring to check in response body
	}{
		{
			name:         "pinned round: / shows demo banner",
			setup:        func() { _ = st.PinRound(ctx, round1.ID) },
			path:         "/",
			expectBanner: "Demo mode: data as of",
		},
		{
			name:         "pinned round: /near-misses shows demo banner",
			setup:        func() {},
			path:         "/near-misses",
			expectBanner: "Demo mode: data as of",
		},
		{
			name:         "pinned round: /log shows demo banner",
			setup:        func() {},
			path:         "/log",
			expectBanner: "Demo mode: data as of",
		},
		{
			name:         "pinned round: /eval shows demo banner",
			setup:        func() {},
			path:         "/eval",
			expectBanner: "Demo mode: data as of",
		},
		{
			name:         "pinned round: /pairs/{id} shows demo banner",
			setup:        func() {},
			path:         "/pairs/" + strconv.FormatInt(relID, 10),
			expectBanner: "Demo mode: data as of",
		},
		{
			name:         "unpinned: / shows latest round, no banner",
			setup:        func() { _ = st.UnpinRounds(ctx) },
			path:         "/",
			expectBanner: "", // no banner for unpinned
		},
		{
			name:         "unpinned: /near-misses shows latest round, no banner",
			setup:        func() {},
			path:         "/near-misses",
			expectBanner: "",
		},
		{
			name:         "zero rounds: / shows fallback banner",
			setup:        func() { tdb.Reset(t) },
			path:         "/",
			expectBanner: "No data available yet",
		},
		{
			name:         "zero rounds: /near-misses shows fallback banner",
			setup:        func() {},
			path:         "/near-misses",
			expectBanner: "No data available yet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			s := New(cfg, deps)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status %d, want 200; body: %s", rec.Code, rec.Body.String())
			}

			body := rec.Body.String()
			if tt.expectBanner != "" {
				if !strings.Contains(body, tt.expectBanner) {
					t.Errorf("expected banner substring %q in response", tt.expectBanner)
				}
			} else {
				// No banner expected: should not contain "Demo mode" or "No data available"
				if strings.Contains(body, "Demo mode") || strings.Contains(body, "No data available") {
					t.Error("expected no banner in response")
				}
			}
		})
	}
}

// TestUnpinRemovesBanner: after POST /admin/unpin, public pages lose the demo banner.
func TestUnpinRemovesBanner(t *testing.T) {
	ctx := context.Background()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	// Seed and pin a round
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	round, err := st.CreateRound(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.PinRound(ctx, round.ID); err != nil {
		t.Fatal(err)
	}

	cfg := Config{AppEnv: "ci", PublicRateLimitPerMin: 10000, AdminUser: testUser, AdminPassword: testPass}
	deps := Deps{Store: st, Now: func() time.Time { return now }}
	s := New(cfg, deps)
	handler := s.Handler()

	// Before unpin: / should show demo banner
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Demo mode") {
		t.Error("expected demo banner before unpin")
	}

	// Unpin
	form := url.Values{}
	req = httptest.NewRequest(http.MethodPost, "/admin/unpin", strings.NewReader(form.Encode()))
	req.RemoteAddr = "203.0.113.7:4444" // Required for origin check
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.SetBasicAuth(testUser, testPass)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /admin/unpin status %d, want 303", rec.Code)
	}

	// After unpin: / should not show demo banner
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / status %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Demo mode") {
		t.Error("expected no demo banner after unpin")
	}
}

// TestAdminShowsPinnedRound: GET /admin shows the pinned round ID and timestamp when a round is pinned.
func TestAdminShowsPinnedRound(t *testing.T) {
	ctx := context.Background()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	round, err := st.CreateRound(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := st.PinRound(ctx, round.ID); err != nil {
		t.Fatal(err)
	}

	cfg := Config{AppEnv: "ci", AdminUser: testUser, AdminPassword: testPass}
	deps := Deps{Store: st, Now: func() time.Time { return now }}
	s := New(cfg, deps)

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.SetBasicAuth(testUser, testPass)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, strconv.FormatInt(round.ID, 10)) {
		t.Error("expected admin page to show pinned round ID")
	}
	if !strings.Contains(body, "2026-10-07") {
		t.Error("expected admin page to show pinned round timestamp")
	}
	if !strings.Contains(body, "Unpin") {
		t.Error("expected admin page to show Unpin button")
	}
}

// TestPinnedRoundRejectsOtherRoundPairs: when a round is pinned, /pairs/{id} and
// POST /route reject pairs from other rounds with 404 (NFR-04).
func TestPinnedRoundRejectsOtherRoundPairs(t *testing.T) {
	ctx := context.Background()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	// Seed two rounds with pairs
	round1, err := st.CreateRound(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round1.ID, store.RoundOK, nil, now.Add(-55*time.Minute)); err != nil {
		t.Fatal(err)
	}

	round2, err := st.CreateRound(ctx, now.Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round2.ID, store.RoundOK, nil, now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Create pair in round2 (newer)
	threshold := decimal.RequireFromString("1")
	insA := canonical.Instrument{
		ID:            "kalshi:R2A",
		Venue:         canonical.VenueKalshi,
		VenueMarketID: "R2A",
		Category:      canonical.CategoryPolitics,
		Title:         "Round 2 A",
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: "kalshi_taker_v1"},
		Terms: canonical.Terms{
			Subject: "test", Metric: "result", Comparator: canonical.ComparatorEQ, Threshold: &threshold,
			Unit: canonical.Unknown, DeadlineTZ: canonical.Unknown, ResolutionSource: canonical.Unknown,
			EdgeRules: canonical.Unknown, Polarity: canonical.PolarityPositive,
		},
	}
	insB := canonical.Instrument{
		ID:            "polymarket:R2B",
		Venue:         canonical.VenuePolymarket,
		VenueMarketID: "R2B",
		Category:      canonical.CategoryPolitics,
		Title:         "Round 2 B",
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: "polymarket_v1"},
		Terms: canonical.Terms{
			Subject: "test", Metric: "result", Comparator: canonical.ComparatorEQ, Threshold: &threshold,
			Unit: canonical.Unknown, DeadlineTZ: canonical.Unknown, ResolutionSource: canonical.Unknown,
			EdgeRules: canonical.Unknown, Polarity: canonical.PolarityPositive,
		},
	}
	if err := st.UpsertInstrument(ctx, round2.ID, insA); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertInstrument(ctx, round2.ID, insB); err != nil {
		t.Fatal(err)
	}
	rel := canonical.Relationship{
		RoundID: round2.ID, AID: insA.ID, BID: insB.ID, Type: canonical.RelEquivalent,
		Confidence: decimal.RequireFromString("0.95"), Method: canonical.MethodRules, Explanation: "test",
	}
	relID, err := st.UpsertRelationship(ctx, rel)
	if err != nil {
		t.Fatal(err)
	}

	// Pin round1 (older)
	if err := st.PinRound(ctx, round1.ID); err != nil {
		t.Fatal(err)
	}

	cfg := Config{AppEnv: "ci", PublicRateLimitPerMin: 10000, MinRouteConfidence: decimal.RequireFromString("0.9")}
	deps := Deps{Store: st, Now: func() time.Time { return now }}
	s := New(cfg, deps)
	handler := s.Handler()

	// Test GET /pairs/{id} for newer-round pair: should 404
	req := httptest.NewRequest(http.MethodGet, "/pairs/"+strconv.FormatInt(relID, 10), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /pairs/{id} for newer-round pair: status %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "different round") {
		t.Error("expected 'different round' message")
	}

	// Test POST /route for newer-round pair: should 404
	form := url.Values{
		"pair": {strconv.FormatInt(relID, 10)}, "target": {insA.ID}, "side": {"buy"}, "outcome": {"yes"},
		"quantity": {"10"}, "type": {"market"}, "tif": {"IOC"},
	}
	req = httptest.NewRequest(http.MethodPost, "/route", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /route for newer-round pair: status %d, want 404", rec.Code)
	}
}

// TestUnpinnedAllowsOlderRoundPairs: with no pin, /pairs/{id} and POST /route
// use the pair's own round and return 200 (allows decision-log navigation).
func TestUnpinnedAllowsOlderRoundPairs(t *testing.T) {
	ctx := context.Background()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)

	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

	// Seed older round with pair
	round1, err := st.CreateRound(ctx, now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round1.ID, store.RoundOK, nil, now.Add(-55*time.Minute)); err != nil {
		t.Fatal(err)
	}

	threshold := decimal.RequireFromString("1")
	insA := canonical.Instrument{
		ID:            "kalshi:OLD1",
		Venue:         canonical.VenueKalshi,
		VenueMarketID: "OLD1",
		Category:      canonical.CategoryPolitics,
		Title:         "Old A",
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: "kalshi_taker_v1"},
		Terms: canonical.Terms{
			Subject: "test", Metric: "result", Comparator: canonical.ComparatorEQ, Threshold: &threshold,
			Unit: canonical.Unknown, DeadlineTZ: canonical.Unknown, ResolutionSource: canonical.Unknown,
			EdgeRules: canonical.Unknown, Polarity: canonical.PolarityPositive,
		},
	}
	insB := canonical.Instrument{
		ID:            "polymarket:OLD2",
		Venue:         canonical.VenuePolymarket,
		VenueMarketID: "OLD2",
		Category:      canonical.CategoryPolitics,
		Title:         "Old B",
		Status:        canonical.StatusOpen,
		FeeSchedule:   canonical.FeeSchedule{ID: "polymarket_v1"},
		Terms: canonical.Terms{
			Subject: "test", Metric: "result", Comparator: canonical.ComparatorEQ, Threshold: &threshold,
			Unit: canonical.Unknown, DeadlineTZ: canonical.Unknown, ResolutionSource: canonical.Unknown,
			EdgeRules: canonical.Unknown, Polarity: canonical.PolarityPositive,
		},
	}
	if err := st.UpsertInstrument(ctx, round1.ID, insA); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertInstrument(ctx, round1.ID, insB); err != nil {
		t.Fatal(err)
	}
	rel := canonical.Relationship{
		RoundID: round1.ID, AID: insA.ID, BID: insB.ID, Type: canonical.RelEquivalent,
		Confidence: decimal.RequireFromString("0.95"), Method: canonical.MethodRules, Explanation: "test",
	}
	relID, err := st.UpsertRelationship(ctx, rel)
	if err != nil {
		t.Fatal(err)
	}

	// Seed newer round (becomes latest)
	round2, err := st.CreateRound(ctx, now.Add(-10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round2.ID, store.RoundOK, nil, now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// No pin: latest round is round2, but older-round pair should still work
	cfg := Config{AppEnv: "ci", PublicRateLimitPerMin: 10000, MinRouteConfidence: decimal.RequireFromString("0.9")}
	deps := Deps{Store: st, Now: func() time.Time { return now }}
	s := New(cfg, deps)
	handler := s.Handler()

	// Test GET /pairs/{id} for older-round pair: should 200
	req := httptest.NewRequest(http.MethodGet, "/pairs/"+strconv.FormatInt(relID, 10), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /pairs/{id} for older-round pair (unpinned): status %d, want 200", rec.Code)
	}

	// Test POST /route for older-round pair: should 200
	form := url.Values{
		"pair": {strconv.FormatInt(relID, 10)}, "target": {insA.ID}, "side": {"buy"}, "outcome": {"yes"},
		"quantity": {"10"}, "type": {"market"}, "tif": {"IOC"},
	}
	req = httptest.NewRequest(http.MethodPost, "/route", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("POST /route for older-round pair (unpinned): status %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
}

// TestMiddlewareCurrentRoundDBError: when CurrentRound fails with a non-NotFound
// DB error, the middleware renders 503 instead of continuing.
func TestMiddlewareCurrentRoundDBError(t *testing.T) {
	// Use a pool with invalid connection to trigger DB error
	ctx := context.Background()
	pool, err := newBrokenPool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	st := store.New(pool)
	cfg := Config{AppEnv: "ci", PublicRateLimitPerMin: 10000}
	deps := Deps{Store: st, Now: func() time.Time { return time.Now() }}
	s := New(cfg, deps)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503 when CurrentRound fails", rec.Code)
	}
}
