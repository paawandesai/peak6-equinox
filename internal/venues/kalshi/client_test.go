package kalshi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func TestKalshiNewOptions(t *testing.T) {
	for _, bad := range []string{"ftp://x", "not a url", "http://", "/relative"} {
		if _, err := New(Options{BaseURL: bad}); err == nil {
			t.Errorf("New(%q) accepted", bad)
		}
	}
	a, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	o := a.opts
	if o.BaseURL != DefaultBaseURL || o.Timeout != DefaultTimeout || o.MaxBodyBytes != DefaultMaxBodyBytes ||
		o.Retries != DefaultRetries || o.Backoff != DefaultBackoff || o.MinInterval != DefaultMinInterval ||
		a.hc.Timeout != DefaultTimeout || a.Venue() != canonical.VenueKalshi {
		t.Errorf("defaults = %+v", o)
	}
	if now := o.Now(); now.Location() != time.UTC {
		t.Error("default clock is not UTC")
	}
	a, _ = New(Options{BaseURL: "http://h/x/", Retries: -1, MinInterval: -1})
	if a.opts.BaseURL != "http://h/x" || a.opts.Retries != 0 || a.opts.MinInterval != 0 {
		t.Errorf("options = %+v", a.opts)
	}
}

func TestKalshiRetriesOn429And5xx(t *testing.T) {
	var calls atomic.Int32
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`{"orderbook_fp":{"yes_dollars":[["0.4","1.00"]],"no_dollars":[]}}`))
		}
	})
	a := newTestAdapter(t, s.URL, nil)
	raw, err := a.get(context.Background(), "/markets/X/orderbook")
	if err != nil || calls.Load() != 3 || raw.HTTPStatus != 200 {
		t.Fatalf("get = %v after %d calls (status %d)", err, calls.Load(), raw.HTTPStatus)
	}

	calls.Store(0)
	s2 := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	})
	a = newTestAdapter(t, s2.URL, nil)
	start := time.Now()
	raw, err = a.get(context.Background(), "/x")
	var se statusError
	if !errors.As(err, &se) || se.status != 503 || calls.Load() != 3 {
		t.Errorf("get = %v after %d calls, want 503 after 3", err, calls.Load())
	}
	if raw.HTTPStatus != 503 || string(raw.Body) != `{"error":"down"}` {
		t.Errorf("last raw = %d %q", raw.HTTPStatus, raw.Body)
	}
	// Backoff 1ms then 2ms.
	if time.Since(start) < 3*time.Millisecond {
		t.Error("no backoff between retries")
	}
}

func TestKalshiNoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.NotFound(w, nil)
	})
	a := newTestAdapter(t, s.URL, nil)
	if _, err := a.get(context.Background(), "/x"); err == nil || calls.Load() != 1 {
		t.Errorf("get = %v after %d calls, want one 404", err, calls.Load())
	}
}

func TestKalshiTimeout(t *testing.T) {
	release := make(chan struct{})
	s := serve(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	a := newTestAdapter(t, s.URL, func(o *Options) { o.Timeout = 20 * time.Millisecond; o.Retries = -1 })
	start := time.Now()
	_, err := a.get(context.Background(), "/slow")
	if err == nil || time.Since(start) > 2*time.Second {
		t.Errorf("get = %v after %v, want a timeout", err, time.Since(start))
	}
}

func TestKalshiBodyCap(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
	})
	a := newTestAdapter(t, s.URL, func(o *Options) { o.MaxBodyBytes = 1024 })
	if _, err := a.get(context.Background(), "/big"); !errors.Is(err, errBodyTooLarge) {
		t.Errorf("get = %v, want errBodyTooLarge", err)
	}
	a = newTestAdapter(t, s.URL, func(o *Options) { o.MaxBodyBytes = 2048 })
	if raw, err := a.get(context.Background(), "/exact"); err != nil || len(raw.Body) != 2048 {
		t.Errorf("body at the cap: %v", err)
	}
}

func TestKalshiRedirectToOtherHostRefused(t *testing.T) {
	other := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/same" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		if r.URL.Path == "/target" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		http.Redirect(w, r, other.URL+"/steal", http.StatusFound)
	})
	a := newTestAdapter(t, s.URL, func(o *Options) { o.Retries = -1 })
	if _, err := a.get(context.Background(), "/x"); err == nil || !strings.Contains(err.Error(), "another host") {
		t.Errorf("cross-host redirect: %v", err)
	}
	if _, err := a.get(context.Background(), "/same"); err != nil {
		t.Errorf("same-host redirect: %v", err)
	}
}

func TestKalshiPacing(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) })
	a := newTestAdapter(t, s.URL, func(o *Options) { o.MinInterval = 15 * time.Millisecond })
	start := time.Now()
	for range 4 {
		if _, err := a.get(context.Background(), "/x"); err != nil {
			t.Fatal(err)
		}
	}
	if el := time.Since(start); el < 45*time.Millisecond {
		t.Errorf("4 paced requests took %v, want >= 45ms", el)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.get(ctx, "/x"); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled get = %v", err)
	}
}

const eventsBody = `{"cursor":"%s","events":[{"event_ticker":"E%d","markets":[
 {"ticker":"E%d-A","event_ticker":"E%d","title":"Rung A","close_time":"2026-10-07T00:00:00Z","status":"active"},
 {"ticker":"E%d-MVE","event_ticker":"E%d","title":"Parlay","mve_collection_ticker":"KXMVE-1","status":"active"},
 {"ticker":"","event_ticker":"E%d","title":"No ticker"},
 {"ticker":"E%d-B","event_ticker":"E%d","title":"Rung B","close_time":"2026-10-07T00:00:00Z","status":"active"}]}]}`

func TestKalshiFetchInstrumentsPaginatesAndFilters(t *testing.T) {
	var buf bytes.Buffer
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("series_ticker") != "S" || q.Get("status") != "open" || q.Get("with_nested_markets") != "true" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		page, next := 1, "c/2+="
		if q.Get("cursor") == "c/2+=" {
			page, next = 2, ""
		}
		_, _ = fmt.Fprintf(w, eventsBody, next, page, page, page, page, page, page, page, page)
	})
	a := newTestAdapter(t, s.URL, func(o *Options) { o.Logger = slog.New(slog.NewTextHandler(&buf, nil)) })
	ins, raws, err := a.FetchInstruments(context.Background(), universe(map[canonical.Category][]string{
		canonical.CategoryCrypto: {"S"}, canonical.CategoryFed: {"S"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, i := range ins {
		ids = append(ids, i.ID)
		if !i.Quality.Has(canonical.QualityRecordSkipped) {
			t.Errorf("%s lacks record_skipped for its dropped sibling", i.ID)
		}
		if i.Category != canonical.CategoryCrypto {
			t.Errorf("%s category %s; the first category listing the series wins", i.ID, i.Category)
		}
	}
	if got := strings.Join(ids, ","); got != "kalshi:E1-A,kalshi:E1-B,kalshi:E2-A,kalshi:E2-B" {
		t.Errorf("ids = %s (MVE and ticker-less markets must be dropped)", got)
	}
	if len(raws) != 4 || !strings.HasSuffix(raws[1].Endpoint, "&cursor=c%2F2%2B%3D") {
		t.Errorf("raws = %d, second endpoint %q", len(raws), raws[1].Endpoint)
	}
	if !strings.Contains(buf.String(), "skipping malformed record") || !strings.Contains(buf.String(), "missing ticker") {
		t.Errorf("no log line for the skipped record:\n%s", buf.String())
	}
}

func TestKalshiPaginationGuards(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cursor":"same","markets":[]}`))
	})
	a := newTestAdapter(t, s.URL, nil)
	_, raws, err := a.FetchSettled(context.Background(), universe(map[canonical.Category][]string{canonical.CategoryFed: {"S"}}), time.Time{})
	if err == nil || !strings.Contains(err.Error(), "repeated cursor") || len(raws) != 2 {
		t.Errorf("repeated cursor: %v (%d raws)", err, len(raws))
	}
	if raws[0].Endpoint != "/markets?series_ticker=S&status=settled&limit=200" {
		t.Errorf("endpoint without since = %q", raws[0].Endpoint)
	}

	var n atomic.Int32
	s2 := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"cursor":"c%d","events":[]}`, n.Add(1))
	})
	a = newTestAdapter(t, s2.URL, func(o *Options) { o.MaxEventPages = 3 })
	_, raws, err = a.FetchInstruments(context.Background(), universe(map[canonical.Category][]string{canonical.CategoryFed: {"S"}}))
	if err != nil || len(raws) != 3 {
		t.Errorf("page cap: %v, %d raws; want nil, 3", err, len(raws))
	}

	s3 := serve(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>`)) })
	a = newTestAdapter(t, s3.URL, nil)
	if _, _, err := a.FetchInstruments(context.Background(), universe(map[canonical.Category][]string{canonical.CategoryFed: {"S"}})); err == nil {
		t.Error("non-JSON events page accepted")
	}
	if _, _, err := a.FetchSettled(context.Background(), universe(map[canonical.Category][]string{canonical.CategoryFed: {"S"}}), time.Time{}); err == nil {
		t.Error("non-JSON markets page accepted")
	}
}

func TestKalshiFetchSettledResults(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("min_close_ts") != "1790000000" || r.URL.Query().Get("status") != "settled" {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"cursor":"","markets":[
			{"ticker":"S-Y","title":"y","result":"yes","status":"finalized","settlement_ts":"2026-09-16T18:20:58.459351Z"},
			{"ticker":"S-N","title":"n","result":"no","status":"finalized"},
			{"ticker":"S-V","title":"v","result":"void","status":"finalized"},
			{"ticker":"S-M","title":"m","result":"yes","mve_collection_ticker":"KXMVE"},
			{"ticker":"","title":"x","result":"yes"}]}`))
	})
	a := newTestAdapter(t, s.URL, nil)
	got, _, err := a.FetchSettled(context.Background(), universe(map[canonical.Category][]string{canonical.CategoryFed: {"S"}}), time.Unix(1790000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Outcome != canonical.OutcomeYes || got[1].Outcome != canonical.OutcomeNo {
		t.Fatalf("settled = %+v", got)
	}
	if want := time.Date(2026, 9, 16, 18, 20, 58, 459351000, time.UTC); !got[0].SettledAt.Equal(want) || !got[1].SettledAt.IsZero() {
		t.Errorf("settled_at = %v, %v", got[0].SettledAt, got[1].SettledAt)
	}
}

func TestKalshiFetchQuotesFailures(t *testing.T) {
	s := serve(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/markets/OK/orderbook":
			_, _ = w.Write([]byte(`{"orderbook_fp":{"yes_dollars":[["0.40","3.00"]],"no_dollars":[["0.55","2.00"]]}}`))
		case "/markets/EMPTY/orderbook":
			_, _ = w.Write([]byte(`{"orderbook_fp":{"yes_dollars":[],"no_dollars":[]}}`))
		case "/markets/BAD/orderbook":
			_, _ = w.Write([]byte(`{"orderbook":{}}`))
		default:
			http.NotFound(w, r)
		}
	})
	a := newTestAdapter(t, s.URL, nil)
	mk := func(tk string) canonical.Instrument {
		return canonical.Instrument{ID: "kalshi:" + tk, Venue: canonical.VenueKalshi, VenueMarketID: tk}
	}
	ins := []canonical.Instrument{
		mk("OK"), mk("EMPTY"), mk("BAD"), mk("GONE"),
		{ID: "polymarket:1", Venue: canonical.VenuePolymarket, VenueMarketID: "1"},
		{ID: "kalshi:OK", Venue: canonical.VenueKalshi, VenueMarketID: "../admin"},
	}
	quotes, raws, err := a.FetchQuotes(context.Background(), ins)
	if err != nil {
		t.Fatalf("FetchQuotes: %v", err)
	}
	if len(quotes) != 4 || len(raws) != 4 {
		t.Fatalf("got %d quotes, %d raws; want 4, 4", len(quotes), len(raws))
	}
	if !quotes[0].Quality.OK() || len(quotes[0].YesAsks) != 1 || quotes[0].YesAsks[0].Price.String() != "0.45" {
		t.Errorf("OK quote = %+v", quotes[0])
	}
	for _, q := range quotes[1:] {
		if !q.Quality.Has(canonical.QualityNoBook) || len(q.YesBids)+len(q.YesAsks) != 0 {
			t.Errorf("%s: quality %+v, want no_book", q.InstrumentID, q.Quality)
		}
	}

	_, _, err = a.FetchQuotes(context.Background(), []canonical.Instrument{mk("GONE"), mk("BAD")})
	if err == nil || !strings.Contains(err.Error(), "every order book failed") {
		t.Errorf("all failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	quotes, _, err = a.FetchQuotes(ctx, []canonical.Instrument{mk("OK"), mk("EMPTY")})
	if !errors.Is(err, context.Canceled) || len(quotes) != 2 || !quotes[0].Quality.Has(canonical.QualityNoBook) {
		t.Errorf("cancelled: %v, %d quotes", err, len(quotes))
	}
	if q, _, err := a.FetchQuotes(context.Background(), nil); err != nil || len(q) != 0 {
		t.Errorf("no instruments: %v", err)
	}
}
