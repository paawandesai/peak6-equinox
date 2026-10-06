package security

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/polymarket"
)

// AB-02: a hostile or broken Polymarket returns malformed, huge or
// out-of-range data. The body read is capped, invalid records are rejected and
// flagged, the rest still return, and nothing panics.

const ab02PolyMaxBody = 10 << 20 // MAX_BODY_BYTES default

var ab02PolyUniverse = venues.Universe{Venue: canonical.VenuePolymarket, Categories: map[canonical.Category]venues.Selector{
	canonical.CategoryFed: {TagSlugs: []string{"fed-rates"}},
}}

// ab02PolyGoodMarket is a valid YES/NO binary with tokens 111 (YES) / 222 (NO).
const ab02PolyGoodMarket = `{"question":"Will the Fed cut in December?","description":"rules","conditionId":"0xabc",` +
	`"endDate":"2026-12-10T19:00:00Z","active":true,"closed":false,` +
	`"outcomes":"[\"Yes\", \"No\"]","outcomePrices":"[\"0.4\", \"0.6\"]","clobTokenIds":"[\"111\", \"222\"]"}`

func ab02PolyAdapter(t *testing.T, gamma, clob http.HandlerFunc, maxPages int) *polymarket.Adapter {
	t.Helper()
	nf := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }
	if gamma == nil {
		gamma = nf
	}
	if clob == nil {
		clob = nf
	}
	gs, cs := httptest.NewServer(gamma), httptest.NewServer(clob)
	t.Cleanup(gs.Close)
	t.Cleanup(cs.Close)
	a, err := polymarket.New(polymarket.Options{
		GammaURL: gs.URL, ClobURL: cs.URL, Timeout: 5 * time.Second, MaxBodyBytes: ab02PolyMaxBody,
		Retries: -1, MaxPages: maxPages, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func ab02PolyEvents(markets ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "0" {
			_, _ = io.WriteString(w, "[]")
			return
		}
		_, _ = io.WriteString(w, `[{"slug":"fed-dec","markets":[`+strings.Join(markets, ",")+`]}]`)
	}
}

// TestAB02_PolymarketHugeBody streams a 50 MB events page: the read stops at
// the cap with an error, and the body is neither stored nor parsed.
func TestAB02_PolymarketHugeBody(t *testing.T) {
	gamma := func(w http.ResponseWriter, _ *http.Request) {
		chunk := []byte(strings.Repeat(`{"slug":"x"},`, 1<<12))
		_, _ = w.Write([]byte("["))
		for written := 0; written < 50<<20; written += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return // the client stopped reading at the cap
			}
		}
	}
	a := ab02PolyAdapter(t, gamma, nil, 1)
	ins, raws, err := a.FetchInstruments(context.Background(), ab02PolyUniverse)
	if err == nil || !strings.Contains(err.Error(), "size cap") {
		t.Fatalf("err = %v, want a size-cap error", err)
	}
	if len(ins) != 0 || len(raws) != 0 {
		t.Errorf("oversize body produced %d instruments and %d stored bodies", len(ins), len(raws))
	}
}

// TestAB02_PolymarketHugeBook: an oversize book is a failed request flagged
// no_book, not a crash.
func TestAB02_PolymarketHugeBook(t *testing.T) {
	clob := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"asset_id":"111","bids":[`)
		lvl := []byte(strings.Repeat(`{"price":"0.5","size":"1"},`, 1<<12))
		for written := 0; written < 12<<20; written += len(lvl) {
			if _, err := w.Write(lvl); err != nil {
				return
			}
		}
	}
	a := ab02PolyAdapter(t, nil, clob, 1)
	quotes, _, err := a.FetchQuotes(context.Background(), []canonical.Instrument{{ID: "polymarket:111"}})
	if err == nil || len(quotes) != 1 || !quotes[0].Quality.Has(canonical.QualityNoBook) {
		t.Fatalf("quotes = %+v err = %v", quotes, err)
	}
}

// TestAB02_PolymarketHostileBookLevels: NaN, price 1.5, negative and zero
// sizes, exponent notation and garbage are rejected and flagged; the valid
// level survives.
func TestAB02_PolymarketHostileBookLevels(t *testing.T) {
	book := `{"asset_id":"111","timestamp":"1791322179740","bids":[` +
		`{"price":"NaN","size":"10"},{"price":"1.5","size":"10"},{"price":"-0.1","size":"10"},` +
		`{"price":"0.3","size":"-5"},{"price":"0.3","size":"0"},{"price":"1e-1","size":"1"},` +
		`{"price":"0.2","size":"1e999999999"},{"price":"Infinity","size":"1"},{"price":"","size":""},` +
		`{"price":"0.25","size":"4"}],"asks":[{"price":"1.0000001","size":"1"}]}`
	clob := func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, book) }
	a := ab02PolyAdapter(t, nil, clob, 1)
	quotes, _, err := a.FetchQuotes(context.Background(), []canonical.Instrument{{ID: "polymarket:111"}})
	if err != nil || len(quotes) != 1 {
		t.Fatalf("quotes = %d err = %v", len(quotes), err)
	}
	q := quotes[0]
	if err := q.Validate(); err != nil {
		t.Fatalf("hostile book produced an invalid quote: %v", err)
	}
	if len(q.YesBids) != 1 || q.YesBids[0].Price.String() != "0.25" || len(q.YesAsks) != 0 {
		t.Errorf("bids = %v asks = %v, want only the 0.25 bid", q.YesBids, q.YesAsks)
	}
	for _, f := range []canonical.QualityFlag{canonical.QualityInvalidPrice, canonical.QualityInvalidSize, canonical.QualityRecordSkipped} {
		if !q.Quality.Has(f) {
			t.Errorf("quality %+v lacks %s", q.Quality, f)
		}
	}
}

// TestAB02_PolymarketMalformedMarkets: malformed markets are skipped and the
// valid one in the same page still returns, flagged.
func TestAB02_PolymarketMalformedMarkets(t *testing.T) {
	bad := []string{
		`{"question":"q","conditionId":"0x1","outcomes":"[\"Yes\", \"No\"]","clobTokenIds":"[\"1\", \"2\"]","closed":"yes"}`,
		`{"question":"q","conditionId":"0x1","outcomes":"[\"Yes\", \"No\"]","clobTokenIds":"[\"<script>\", \"2\"]"}`,
		`{"question":"q","conditionId":"0x1","outcomes":"[\"Yes\", \"No\"]","clobTokenIds":"[\"` + strings.Repeat("9", 200) + `\", \"2\"]"}`,
		`{"question":"q","conditionId":"0x1","outcomes":"[\"Yes\", \"No\"]","clobTokenIds":"[\"1\", \"2\", \"3\"]"}`,
		`{"question":"q","conditionId":"0x1","outcomes":"NaN","clobTokenIds":"[\"1\", \"2\"]"}`,
		`[1,2,3]`,
		`null`,
	}
	a := ab02PolyAdapter(t, ab02PolyEvents(append(bad, ab02PolyGoodMarket)...), nil, 1)
	ins, _, err := a.FetchInstruments(context.Background(), ab02PolyUniverse)
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 1 || ins[0].ID != "polymarket:111" {
		t.Fatalf("instruments = %+v, want only the valid market", ins)
	}
	if err := ins[0].Validate(); err != nil {
		t.Fatal(err)
	}
	if !ins[0].Quality.Has(canonical.QualityRecordSkipped) {
		t.Errorf("survivor not flagged: %+v", ins[0].Quality)
	}
}

// TestAB02_PolymarketDeeplyNested: deeply nested JSON in an events page or a
// book is rejected without a stack overflow.
func TestAB02_PolymarketDeeplyNested(t *testing.T) {
	deep := strings.Repeat("[", 200_000) + strings.Repeat("]", 200_000)
	gamma := func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, deep) }
	clob := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"asset_id":"111","extra":`+deep+`,"bids":[{"price":"0.5","size":"1"}]}`)
	}
	a := ab02PolyAdapter(t, gamma, clob, 1)
	if _, _, err := a.FetchInstruments(context.Background(), ab02PolyUniverse); err == nil {
		t.Error("deeply nested events page accepted")
	}
	quotes, _, err := a.FetchQuotes(context.Background(), []canonical.Instrument{{ID: "polymarket:111"}})
	if err != nil || len(quotes) != 1 || !quotes[0].Quality.Has(canonical.QualityNoBook) {
		t.Fatalf("quotes = %+v err = %v", quotes, err)
	}

	// Nesting inside a market's double-encoded field.
	nested := `{"question":"q","conditionId":"0x1","outcomes":"` + strings.Repeat("[", 3000) + `","clobTokenIds":"[\"1\", \"2\"]"}`
	a = ab02PolyAdapter(t, ab02PolyEvents(nested, ab02PolyGoodMarket), nil, 1)
	ins, _, err := a.FetchInstruments(context.Background(), ab02PolyUniverse)
	if err != nil || len(ins) != 1 {
		t.Fatalf("ins = %d err = %v", len(ins), err)
	}
}

// TestAB02_PolymarketHostileSettled: settled markets with out-of-range,
// NaN or split final prices are skipped, never mapped to an outcome.
func TestAB02_PolymarketHostileSettled(t *testing.T) {
	settled := func(token, prices string) string {
		return `{"question":"q","conditionId":"0x` + token + `","closed":true,"endDate":"2026-12-10T19:00:00Z",` +
			`"outcomes":"[\"Yes\", \"No\"]","outcomePrices":"` + prices + `","clobTokenIds":"[\"` + token + `\", \"9` + token + `\"]"}`
	}
	gamma := ab02PolyEvents(
		settled("1", `[\"1.5\", \"-0.5\"]`),
		settled("2", `[\"NaN\", \"0\"]`),
		settled("3", `[\"0.5\", \"0.5\"]`),
		settled("4", `[\"1\", \"1\"]`),
		settled("5", `[\"1\"]`),
		settled("6", `[\"0\", \"1\"]`),
	)
	a := ab02PolyAdapter(t, gamma, nil, 1)
	out, _, err := a.FetchSettled(context.Background(), ab02PolyUniverse, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Instrument.ID != "polymarket:6" || out[0].Outcome != canonical.OutcomeNo {
		t.Fatalf("settled = %+v, want only market 6 = no", out)
	}
	if !out[0].Instrument.Quality.Has(canonical.QualityAmbiguousOutcome) {
		t.Errorf("survivor not flagged ambiguous_outcome siblings: %+v", out[0].Instrument.Quality)
	}
}

// TestAB02_PolymarketEndlessPagination: a venue that always returns a full
// page cannot keep the adapter paging forever.
func TestAB02_PolymarketEndlessPagination(t *testing.T) {
	var hits atomic.Int64
	gamma := func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "["+strings.Repeat(`{"slug":"x","markets":[]},`, 49)+`{"slug":"x","markets":[]}]`)
	}
	a := ab02PolyAdapter(t, gamma, nil, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, _, err := a.FetchInstruments(ctx, ab02PolyUniverse); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != polymarket.DefaultMaxPages {
		t.Errorf("made %d requests, want the %d-page cap", hits.Load(), polymarket.DefaultMaxPages)
	}
}

// TestAB02_PolymarketWrongTokenBook: a book for a different token than the
// one requested is not attributed to the instrument.
func TestAB02_PolymarketWrongTokenBook(t *testing.T) {
	clob := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"asset_id":"999","bids":[{"price":"0.9","size":"100"}],"asks":[]}`)
	}
	a := ab02PolyAdapter(t, nil, clob, 1)
	quotes, _, err := a.FetchQuotes(context.Background(), []canonical.Instrument{{ID: "polymarket:111"}})
	if err != nil || len(quotes) != 1 || len(quotes[0].YesBids) != 0 || !quotes[0].Quality.Has(canonical.QualityNoBook) {
		t.Fatalf("quotes = %+v err = %v", quotes, err)
	}
}

// TestAB02_PolymarketServerErrorIsAnError: a venue outage surfaces as an
// error the collector can record, never a panic or silent success.
func TestAB02_PolymarketServerErrorIsAnError(t *testing.T) {
	gamma := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"type":"internal error","error":"internal server error"}`)
	}
	a := ab02PolyAdapter(t, gamma, nil, 1)
	_, raws, err := a.FetchInstruments(context.Background(), ab02PolyUniverse)
	if err == nil || errors.Is(err, context.Canceled) || len(raws) != 1 || raws[0].HTTPStatus != http.StatusInternalServerError {
		t.Fatalf("err = %v raws = %+v", err, raws)
	}
}

// TestAB02_PolymarketOffHostRedirect: a Gamma or CLOB response that redirects
// to another origin is refused without contacting it or retrying, so data can
// never be attributed to a Polymarket endpoint it did not come from.
func TestAB02_PolymarketOffHostRedirect(t *testing.T) {
	var evilHits, apiHits atomic.Int64
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		evilHits.Add(1)
		_, _ = io.WriteString(w, `[{"slug":"evil","markets":[`+ab02PolyGoodMarket+`]}]`)
	}))
	t.Cleanup(evil.Close)
	redirect := func(w http.ResponseWriter, r *http.Request) {
		apiHits.Add(1)
		http.Redirect(w, r, evil.URL+r.URL.RequestURI(), http.StatusFound) //nolint:gosec // the hostile redirect under test
	}
	gs, cs := httptest.NewServer(http.HandlerFunc(redirect)), httptest.NewServer(http.HandlerFunc(redirect))
	t.Cleanup(gs.Close)
	t.Cleanup(cs.Close)
	a, err := polymarket.New(polymarket.Options{
		GammaURL: gs.URL, ClobURL: cs.URL, Timeout: 5 * time.Second, MaxBodyBytes: ab02PolyMaxBody,
		Backoff: time.Millisecond, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	ins, _, err := a.FetchInstruments(context.Background(), ab02PolyUniverse)
	if err == nil || !strings.Contains(err.Error(), "redirect to another origin refused") || len(ins) != 0 {
		t.Fatalf("instruments = %+v err = %v, want the redirect refused", ins, err)
	}
	quotes, _, err := a.FetchQuotes(context.Background(), []canonical.Instrument{{ID: "polymarket:111"}})
	if err == nil || len(quotes) != 1 || len(quotes[0].YesBids) != 0 || !quotes[0].Quality.Has(canonical.QualityNoBook) {
		t.Fatalf("quotes = %+v err = %v, want no_book and an error", quotes, err)
	}
	if evilHits.Load() != 0 {
		t.Errorf("the off-host server was contacted %d times", evilHits.Load())
	}
	if apiHits.Load() != 2 {
		t.Errorf("API hit %d times, want 2 (one per request, no retries)", apiHits.Load())
	}
}
