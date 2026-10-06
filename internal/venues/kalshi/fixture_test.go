package kalshi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// fixtureDir holds the bodies recorded by `equinox record-fixtures` on 2026-10-06.
var fixtureDir = filepath.Join("..", "..", "..", "testdata", "fixtures", "kalshi")

// Recorded settled-market windows (the min_close_ts the recorder used).
var (
	sinceCrypto = time.Unix(1791235925, 0).UTC() // KXBTCD, 1 day back
	sinceFed    = time.Unix(1783546352, 0).UTC() // KXFED, 90 days back
)

// fixtureServer serves recorded fixtures by venues.FixtureFileName of the
// request URI, and 404 for anything not recorded. It records every request.
type fixtureServer struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func newFixtureServer(t *testing.T) *fixtureServer {
	t.Helper()
	fs := &fixtureServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		fs.requests = append(fs.requests, r.URL.RequestURI())
		fs.mu.Unlock()
		if r.Header.Get("Authorization") != "" {
			http.Error(w, "unexpected auth header", http.StatusBadRequest)
			return
		}
		// FixtureFileName yields a flat, filesystem-safe name, so the path stays in fixtureDir.
		body, err := os.ReadFile(filepath.Join(fixtureDir, venues.FixtureFileName(r.URL.RequestURI()))) //nolint:gosec // see above
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body) //nolint:gosec // test server replaying recorded JSON, not HTML
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fixtureServer) seen() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]string(nil), fs.requests...)
}

var fixedNow = time.Date(2026, 10, 6, 21, 32, 0, 0, time.UTC)

func newTestAdapter(t *testing.T, baseURL string, mod func(*Options)) *Adapter {
	t.Helper()
	o := Options{
		BaseURL:     baseURL,
		Backoff:     time.Millisecond,
		MinInterval: -1,
		Now:         func() time.Time { return fixedNow },
	}
	if mod != nil {
		mod(&o)
	}
	a, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

func universe(cats map[canonical.Category][]string) venues.Universe {
	u := venues.Universe{Venue: canonical.VenueKalshi, Categories: map[canonical.Category]venues.Selector{}}
	for c, s := range cats {
		u.Categories[c] = venues.Selector{Series: s}
	}
	return u
}

func repoUniverse(t *testing.T) venues.Universe {
	t.Helper()
	us, err := venues.LoadUniverse(filepath.Join("..", "..", "..", "config", "universe"))
	if err != nil {
		t.Fatal(err)
	}
	return us.For(canonical.VenueKalshi)
}

func TestKalshiFetchInstrumentsFixtures(t *testing.T) {
	fs := newFixtureServer(t)
	a := newTestAdapter(t, fs.URL, nil)
	ins, raws, err := a.FetchInstruments(context.Background(), repoUniverse(t))
	if err != nil {
		t.Fatalf("FetchInstruments: %v", err)
	}
	// 318 KXBTCD + 390 KXETHD + 247 KXFED markets were recorded, none MVE.
	if len(ins) != 318+390+247 {
		t.Errorf("got %d instruments, want %d", len(ins), 318+390+247)
	}
	wantEndpoints := []string{
		"/events?series_ticker=KXBTCD&status=open&with_nested_markets=true&limit=200",
		"/events?series_ticker=KXETHD&status=open&with_nested_markets=true&limit=200",
		"/events?series_ticker=KXFED&status=open&with_nested_markets=true&limit=200",
	}
	if got := fs.seen(); strings.Join(got, "|") != strings.Join(wantEndpoints, "|") {
		t.Errorf("requests = %v, want %v", got, wantEndpoints)
	}
	if len(raws) != 3 || raws[0].Endpoint != wantEndpoints[0] || raws[0].HTTPStatus != 200 || len(raws[0].Body) == 0 ||
		raws[0].Venue != canonical.VenueKalshi || !raws[0].FetchedAt.Equal(fixedNow) {
		t.Errorf("raws[0] = %+v", raws[0].Endpoint)
	}
	byID := map[string]canonical.Instrument{}
	for _, i := range ins {
		if err := i.Validate(); err != nil {
			t.Errorf("invalid instrument: %v", err)
		}
		if _, dup := byID[i.ID]; dup {
			t.Errorf("duplicate %s", i.ID)
		}
		byID[i.ID] = i
	}
	fed, ok := byID["kalshi:KXFED-26OCT-T3.25"]
	if !ok {
		t.Fatal("kalshi:KXFED-26OCT-T3.25 missing")
	}
	want := canonical.Instrument{
		ID: "kalshi:KXFED-26OCT-T3.25", Venue: canonical.VenueKalshi, VenueMarketID: "KXFED-26OCT-T3.25",
		VenueEventID: "KXFED-26OCT", Category: canonical.CategoryFed,
		Title:          "Will the upper bound of the federal funds rate be above 3.25% following the Fed's Oct 28, 2026 meeting? — Above 3.25%",
		CloseTime:      time.Date(2026, 10, 28, 17, 55, 0, 0, time.UTC),
		ExpirationTime: time.Date(2026, 11, 4, 18, 5, 0, 0, time.UTC),
		Status:         canonical.StatusOpen,
	}
	if fed.ID != want.ID || fed.VenueMarketID != want.VenueMarketID || fed.VenueEventID != want.VenueEventID ||
		fed.Category != want.Category || fed.Title != want.Title || !fed.CloseTime.Equal(want.CloseTime) ||
		!fed.ExpirationTime.Equal(want.ExpirationTime) || fed.Status != want.Status {
		t.Errorf("KXFED-26OCT-T3.25 = %+v", fed)
	}
	if fed.CloseTime.Location() != time.UTC || fed.ExpirationTime.Location() != time.UTC {
		t.Error("times not UTC")
	}
	if !strings.HasPrefix(fed.RulesText, "If the upper bound of the target federal funds rate") ||
		!strings.Contains(fed.RulesText, "\n\nThis market will expire") {
		t.Errorf("rules = %q", fed.RulesText)
	}
	if fed.FeeSchedule.ID != canonical.FeeScheduleKalshiTakerV1 || !fed.FeeSchedule.Params["multiplier"].Equal(decimal.RequireFromString("0.07")) {
		t.Errorf("fee schedule = %+v", fed.FeeSchedule)
	}
	if fed.Terms != canonical.UnknownTerms() || !fed.Quality.OK() {
		t.Errorf("terms %+v quality %+v", fed.Terms, fed.Quality)
	}
	if btc := byID["kalshi:KXBTCD-26OCT0618-T76599.99"]; btc.Category != canonical.CategoryCrypto ||
		!strings.HasSuffix(btc.Title, " — $76,600 or above") {
		t.Errorf("KXBTCD instrument = %q %q", btc.Category, btc.Title)
	}
}

func TestKalshiFetchQuotesFixtures(t *testing.T) {
	fs := newFixtureServer(t)
	a := newTestAdapter(t, fs.URL, nil)
	tickers := []string{
		"KXBTCD-26OCT0618-T76599.99", "KXBTCD-26OCT0618-T76699.99", "KXBTCD-26OCT0618-T76799.99",
		"KXFED-26OCT-T2.75", "KXFED-26OCT-T3.25", "KXFED-26OCT-T4.00",
	}
	var ins []canonical.Instrument
	for _, tk := range tickers {
		ins = append(ins, canonical.Instrument{ID: "kalshi:" + tk, Venue: canonical.VenueKalshi, VenueMarketID: tk})
	}
	quotes, raws, err := a.FetchQuotes(context.Background(), ins)
	if err != nil {
		t.Fatalf("FetchQuotes: %v", err)
	}
	if len(quotes) != len(tickers) || len(raws) != len(tickers) {
		t.Fatalf("got %d quotes, %d raws", len(quotes), len(raws))
	}
	for k, q := range quotes {
		if q.InstrumentID != ins[k].ID || !q.FetchedAt.Equal(fixedNow) || q.VenueTS != nil {
			t.Errorf("quote %d header = %s %v", k, q.InstrumentID, q.FetchedAt)
		}
		if err := q.Validate(); err != nil {
			t.Errorf("quote %s invalid: %v", q.InstrumentID, err)
		}
		if q.Quality.Has(canonical.QualityInvalidPrice) || q.Quality.Has(canonical.QualityInvalidSize) {
			t.Errorf("quote %s quality %+v", q.InstrumentID, q.Quality)
		}
	}
	// KXFED-26OCT-T4.00 was recorded with both sides; check the derivation exactly.
	q := quotes[5]
	d := decimal.RequireFromString
	if bid, _ := q.BestBid(); !bid.Equal(d("0.19")) || !q.YesBids[0].Size.Equal(d("10.25")) {
		t.Errorf("best bid = %v x %v, want 0.19 x 10.25", bid, q.YesBids[0].Size)
	}
	// Best NO bid 0.79 x 8.48 => best YES ask 0.21 x 8.48.
	if ask, _ := q.BestAsk(); !ask.Equal(d("0.21")) || !q.YesAsks[0].Size.Equal(d("8.48")) {
		t.Errorf("best ask = %v x %v, want 0.21 x 8.48", ask, q.YesAsks[0].Size)
	}
	if n := len(q.YesAsks); n != 30 || !q.YesAsks[n-1].Price.Equal(d("0.99")) || !q.YesAsks[n-1].Size.Equal(d("531909.91")) {
		t.Errorf("worst ask = %+v (of %d)", q.YesAsks[n-1], n)
	}
	if len(q.YesBids) != 18 {
		t.Errorf("yes bids = %d levels", len(q.YesBids))
	}
}

func TestKalshiFetchSettledFixtures(t *testing.T) {
	fs := newFixtureServer(t)
	a := newTestAdapter(t, fs.URL, func(o *Options) { o.MaxSettledPages = 2 })
	got, raws, err := a.FetchSettled(context.Background(), universe(map[canonical.Category][]string{canonical.CategoryCrypto: {"KXBTCD"}}), sinceCrypto)
	if err != nil {
		t.Fatalf("FetchSettled crypto: %v", err)
	}
	if len(got) != 400 || len(raws) != 2 {
		t.Errorf("crypto: %d settled, %d raws; want 400, 2", len(got), len(raws))
	}
	if !strings.Contains(raws[1].Endpoint, "&cursor=") {
		t.Errorf("second page endpoint %q has no cursor", raws[1].Endpoint)
	}

	got, _, err = a.FetchSettled(context.Background(), universe(map[canonical.Category][]string{canonical.CategoryFed: {"KXFED"}}), sinceFed)
	if err != nil {
		t.Fatalf("FetchSettled fed: %v", err)
	}
	if len(got) != 11 {
		t.Fatalf("fed: %d settled, want 11", len(got))
	}
	var yes, no int
	for _, s := range got {
		if err := s.Validate(); err != nil {
			t.Errorf("invalid settled: %v", err)
		}
		if s.Instrument.Status != canonical.StatusSettled || s.Instrument.Category != canonical.CategoryFed || s.SettledAt.IsZero() {
			t.Errorf("settled %s: status %s category %s at %v", s.Instrument.ID, s.Instrument.Status, s.Instrument.Category, s.SettledAt)
		}
		switch s.Outcome {
		case canonical.OutcomeYes:
			yes++
		case canonical.OutcomeNo:
			no++
		}
		if s.Instrument.ID == "kalshi:KXFED-26SEP-T5.25" && s.Outcome != canonical.OutcomeNo {
			t.Errorf("KXFED-26SEP-T5.25 outcome = %s, want no", s.Outcome)
		}
	}
	if yes == 0 || no == 0 {
		t.Errorf("outcomes yes=%d no=%d; want both", yes, no)
	}
}

// TestKalshiRecordedFixtureCoverage pins the ticket's fixture minimum: events for
// KXBTCD and KXFED, at least 5 order books, and settled markets for both.
func TestKalshiRecordedFixtureCoverage(t *testing.T) {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	var books int
	has := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		switch {
		case strings.Contains(n, "_orderbook."):
			books++
		case strings.HasPrefix(n, "events_series_ticker-KXBTCD"):
			has["events KXBTCD"] = true
		case strings.HasPrefix(n, "events_series_ticker-KXFED"):
			has["events KXFED"] = true
		case strings.HasPrefix(n, "markets_series_ticker-KXBTCD_status-settled"):
			has["settled KXBTCD"] = true
		case strings.HasPrefix(n, "markets_series_ticker-KXFED_status-settled"):
			has["settled KXFED"] = true
		}
	}
	if books < 5 {
		t.Errorf("%d order book fixtures, want >= 5", books)
	}
	for _, k := range []string{"events KXBTCD", "events KXFED", "settled KXBTCD", "settled KXFED"} {
		if !has[k] {
			t.Errorf("missing fixture: %s", k)
		}
	}
}

func TestKalshiFixtureMissingSeries(t *testing.T) {
	fs := newFixtureServer(t)
	a := newTestAdapter(t, fs.URL, nil)
	u := universe(map[canonical.Category][]string{canonical.CategoryCrypto: {"NOPE"}, canonical.CategoryFed: {"KXFED"}})
	ins, _, err := a.FetchInstruments(context.Background(), u)
	var se statusError
	if err == nil || !errors.As(err, &se) || se.status != 404 || !strings.Contains(err.Error(), "series NOPE") {
		t.Errorf("err = %v, want a 404 for series NOPE", err)
	}
	if len(ins) != 247 {
		t.Errorf("got %d instruments; the KXFED series should still return 247", len(ins))
	}
}
