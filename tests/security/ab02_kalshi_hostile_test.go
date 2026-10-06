package security

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/kalshi"
)

// AB-02: a venue returns malformed, huge or out-of-range data. The body read
// is capped, invalid records are rejected and flagged, the rest still return,
// and nothing panics.

// endlessReader yields an unbounded stream of bytes.
type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	return len(p), nil
}

func hostileKalshi(t *testing.T) *kalshi.Adapter {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events":
			switch r.URL.Query().Get("series_ticker") {
			case "HUGE":
				// 50 MB body: must be cut at the cap, never read in full.
				_, _ = io.Copy(w, io.LimitReader(endlessReader{}, 50<<20))
			case "NESTED":
				_, _ = w.Write([]byte(`{"events":` + strings.Repeat("[", 100000) + strings.Repeat("]", 100000) + `}`))
			default:
				_, _ = w.Write([]byte(`{"cursor":"","events":[{"event_ticker":"OK","markets":[
					{"ticker":"OK-1","event_ticker":"OK","title":"Good","close_time":"2026-10-07T00:00:00Z","status":"active"},
					{"ticker":"","event_ticker":"OK","title":"No ticker"},
					{"ticker":"OK-2","event_ticker":"OK","title":"<script>alert(1)</script>","close_time":"NaN","status":"active"},
					{"ticker":"OK-3","title":` + strings.Repeat(`[`, 5000) + strings.Repeat(`]`, 5000) + `}]}]}`))
			}
		case "/markets/HOSTILE/orderbook":
			_, _ = w.Write([]byte(`{"orderbook_fp":{
				"yes_dollars":[["NaN","10.00"],["1.5","10.00"],["0.40","-5.00"],["1e999999999","1"],["0.35","NaN"],["0.30","7.00"]],
				"no_dollars":[["0.60","Infinity"],["-0.1","3.00"],["0.55","2.00"]]}}`))
		case "/markets/NESTED/orderbook":
			_, _ = w.Write([]byte(strings.Repeat(`{"a":`, 200000)))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	a, err := kalshi.New(kalshi.Options{
		BaseURL: s.URL, MaxBodyBytes: 10 << 20, Timeout: 10 * time.Second,
		Retries: -1, MinInterval: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAB02_KalshiHugeBodyIsCapped(t *testing.T) {
	a := hostileKalshi(t)
	u := venues.Universe{Venue: canonical.VenueKalshi, Categories: map[canonical.Category]venues.Selector{
		canonical.CategoryCrypto: {Series: []string{"HUGE"}},
		canonical.CategoryFed:    {Series: []string{"GOOD"}},
	}}
	ins, raws, err := a.FetchInstruments(context.Background(), u)
	if err == nil || !strings.Contains(err.Error(), "size cap") || !strings.Contains(err.Error(), "series HUGE") {
		t.Fatalf("err = %v, want the HUGE series to fail on the size cap", err)
	}
	for _, r := range raws {
		if len(r.Body) > 10<<20 {
			t.Errorf("raw body of %d bytes kept", len(r.Body))
		}
	}
	// The round continues: the GOOD series still returns its two valid markets.
	if len(ins) != 2 || ins[0].ID != "kalshi:OK-1" || ins[1].ID != "kalshi:OK-2" {
		t.Errorf("instruments = %+v", ins)
	}
}

func TestAB02_KalshiMalformedRecordsSkippedAndFlagged(t *testing.T) {
	a := hostileKalshi(t)
	u := venues.Universe{Venue: canonical.VenueKalshi, Categories: map[canonical.Category]venues.Selector{
		canonical.CategoryFed: {Series: []string{"GOOD"}},
	}}
	ins, _, err := a.FetchInstruments(context.Background(), u)
	if err != nil {
		t.Fatal(err)
	}
	// OK-1 is clean. OK-2 keeps its (escaped-at-render) title but its NaN close time is flagged.
	// The ticker-less market and the deeply nested one are dropped.
	if len(ins) != 2 {
		t.Fatalf("instruments = %+v", ins)
	}
	for _, i := range ins {
		if err := i.Validate(); err != nil {
			t.Errorf("invalid instrument returned: %v", err)
		}
		if !i.Quality.Has(canonical.QualityRecordSkipped) {
			t.Errorf("%s lacks the record_skipped flag", i.ID)
		}
	}
	if !ins[1].Quality.Has(canonical.QualityInvalidTime) || !ins[1].CloseTime.IsZero() {
		t.Errorf("NaN close time not flagged: %+v", ins[1].Quality)
	}
}

func TestAB02_KalshiNaNNegativeAndOutOfRangeLevels(t *testing.T) {
	a := hostileKalshi(t)
	quotes, _, err := a.FetchQuotes(context.Background(), []canonical.Instrument{
		{ID: "kalshi:HOSTILE", Venue: canonical.VenueKalshi, VenueMarketID: "HOSTILE"},
	})
	if err != nil {
		t.Fatal(err)
	}
	q := quotes[0]
	if err := q.Validate(); err != nil {
		t.Fatalf("quote with hostile levels is invalid: %v", err)
	}
	// Only ["0.30","7.00"] (YES) and ["0.55","2.00"] (NO => YES ask 0.45) survive.
	if len(q.YesBids) != 1 || q.YesBids[0].Price.String() != "0.3" || len(q.YesAsks) != 1 || q.YesAsks[0].Price.String() != "0.45" {
		t.Errorf("bids %+v asks %+v", q.YesBids, q.YesAsks)
	}
	for _, f := range []canonical.QualityFlag{canonical.QualityInvalidPrice, canonical.QualityInvalidSize} {
		if !q.Quality.Has(f) {
			t.Errorf("quality lacks %s: %+v", f, q.Quality)
		}
	}
	for _, l := range append(q.YesBids, q.YesAsks...) {
		if canonical.ValidatePrice(l.Price) != nil || !l.Size.IsPositive() {
			t.Errorf("bad level kept: %+v", l)
		}
	}
}

func TestAB02_KalshiDeeplyNestedJSON(t *testing.T) {
	a := hostileKalshi(t)
	quotes, _, err := a.FetchQuotes(context.Background(), []canonical.Instrument{
		{ID: "kalshi:NESTED", Venue: canonical.VenueKalshi, VenueMarketID: "NESTED"},
		{ID: "kalshi:HOSTILE", Venue: canonical.VenueKalshi, VenueMarketID: "HOSTILE"},
	})
	if err != nil {
		t.Fatalf("one bad book must not fail the batch: %v", err)
	}
	if len(quotes) != 2 || !quotes[0].Quality.Has(canonical.QualityNoBook) || quotes[1].Quality.Has(canonical.QualityNoBook) {
		t.Errorf("quotes = %+v", quotes)
	}
	u := venues.Universe{Venue: canonical.VenueKalshi, Categories: map[canonical.Category]venues.Selector{
		canonical.CategoryFed: {Series: []string{"NESTED"}},
	}}
	if _, _, err := a.FetchInstruments(context.Background(), u); err == nil || errors.Is(err, context.Canceled) {
		t.Errorf("deeply nested events body: err = %v, want a decode error", err)
	}
}
