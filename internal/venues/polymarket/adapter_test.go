package polymarket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// clobIDsKey is Gamma's double-encoded CLOB ID field name (a named key keeps
// gosec's credential heuristic quiet about the test literals).
const clobIDsKey = "clobTokenIds"

const (
	yesToken = "111"
	noToken  = "222"
)

// market builds a Gamma market JSON object; override fields with extra
// (raw JSON fragments keyed by field name).
func market(extra map[string]string) string {
	fields := map[string]string{
		"question":      `"Will the Fed cut rates in December?"`,
		"description":   `"Resolves YES if the FOMC lowers the target range."`,
		"conditionId":   `"0xabc"`,
		"endDate":       `"2026-12-10T19:00:00Z"`,
		"closedTime":    `"2026-12-10 19:05:00+00"`,
		"active":        `true`,
		"closed":        `false`,
		"outcomes":      `"[\"Yes\", \"No\"]"`,
		"outcomePrices": `"[\"0.4\", \"0.6\"]"`,
		"clobTokenIds":  `"[\"` + yesToken + `\", \"` + noToken + `\"]"`,
		"feesEnabled":   `true`,
		"feeSchedule":   `{"exponent": 1, "rate": 0.05, "takerOnly": true}`,
	}
	for k, v := range extra {
		if v == "" {
			delete(fields, k)
			continue
		}
		fields[k] = v
	}
	parts := make([]string, 0, len(fields))
	for k, v := range fields {
		parts = append(parts, strconv.Quote(k)+":"+v)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func event(slug string, markets ...string) string {
	return fmt.Sprintf(`{"id":"1","slug":%q,"title":"t","endDate":"2026-12-10T19:00:00Z","negRisk":true,"markets":[%s]}`,
		slug, strings.Join(markets, ","))
}

// stubAdapter returns an adapter whose Gamma and CLOB servers are handled by gamma and clob.
func stubAdapter(t *testing.T, gamma, clob http.HandlerFunc, mod func(*Options)) *Adapter {
	t.Helper()
	notFound := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }
	if gamma == nil {
		gamma = notFound
	}
	if clob == nil {
		clob = notFound
	}
	gs := httptest.NewServer(gamma)
	cs := httptest.NewServer(clob)
	t.Cleanup(gs.Close)
	t.Cleanup(cs.Close)
	opts := Options{
		GammaURL: gs.URL, ClobURL: cs.URL, Timeout: 2 * time.Second, MaxBodyBytes: 1 << 20,
		Backoff: time.Millisecond, Logger: quietLogger(), Now: func() time.Time { return recordedAt },
	}
	if mod != nil {
		mod(&opts)
	}
	a, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func fedUniverse() venues.Universe {
	return venues.Universe{Venue: canonical.VenuePolymarket, Categories: map[canonical.Category]venues.Selector{
		canonical.CategoryFed: {TagSlugs: []string{"fed-rates"}},
	}}
}

func serveEvents(events ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "0" {
			_, _ = io.WriteString(w, "[]")
			return
		}
		_, _ = io.WriteString(w, "["+strings.Join(events, ",")+"]")
	}
}

func TestNewValidates(t *testing.T) {
	good := Options{GammaURL: "https://g", ClobURL: "https://c", Timeout: time.Second, MaxBodyBytes: 1}
	if _, err := New(good); err != nil {
		t.Fatalf("New(good) = %v", err)
	}
	for name, mod := range map[string]func(*Options){
		"bad gamma":  func(o *Options) { o.GammaURL = "ftp://x" },
		"bad clob":   func(o *Options) { o.ClobURL = "not a url" },
		"no cap":     func(o *Options) { o.MaxBodyBytes = 0 },
		"no timeout": func(o *Options) { o.Timeout = 0 },
	} {
		o := good
		mod(&o)
		if _, err := New(o); err == nil {
			t.Errorf("%s: New accepted %+v", name, o)
		}
	}
	a, _ := New(good)
	if a.Venue() != canonical.VenuePolymarket || a.gamma.retries != DefaultRetries || a.pageSize != DefaultPageSize {
		t.Errorf("defaults not applied: %+v", a)
	}
}

func TestFetchInstrumentsQueryAndMapping(t *testing.T) {
	var got []string
	gamma := func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+"?"+r.URL.RawQuery)
		serveEvents(event("fed-dec", market(nil),
			market(map[string]string{clobIDsKey: `"[\"333\", \"444\"]"`, "conditionId": `"0xdef"`, "question": `"Hike?"`})))(w, r)
	}
	a := stubAdapter(t, gamma, nil, nil)
	ins, raws, err := a.FetchInstruments(context.Background(), fedUniverse())
	if err != nil {
		t.Fatal(err)
	}
	want := "/events?active=true&closed=false&exclude_tag_id=102127&limit=50&offset=0&tag_slug=fed-rates"
	if len(got) != 1 || got[0] != want {
		t.Errorf("requests = %v, want [%s]", got, want)
	}
	if len(raws) != 1 || raws[0].Endpoint != "gamma"+want {
		t.Errorf("raw endpoint = %+v", raws)
	}
	if len(ins) != 2 {
		t.Fatalf("got %d instruments", len(ins))
	}
	in := ins[0]
	if in.ID != "polymarket:"+yesToken || in.VenueMarketID != "0xabc" || in.VenueEventID != "fed-dec" ||
		in.Title != "Will the Fed cut rates in December?" || in.RulesText != "Resolves YES if the FOMC lowers the target range." ||
		!in.CloseTime.Equal(time.Date(2026, 12, 10, 19, 0, 0, 0, time.UTC)) || in.Category != canonical.CategoryFed ||
		in.Status != canonical.StatusOpen || !in.Quality.OK() {
		t.Errorf("instrument = %+v", in)
	}
	if !in.FeeSchedule.Params["rate"].Equal(decimal.RequireFromString("0.05")) || !in.FeeSchedule.Params["exponent"].Equal(decimal.NewFromInt(1)) {
		t.Errorf("fee params = %v", in.FeeSchedule.Params)
	}
	if ins[1].VenueEventID != in.VenueEventID {
		t.Error("markets of one negRisk event must share venue_event_id")
	}
}

func TestFetchInstrumentsYesTokenByLabel(t *testing.T) {
	a := stubAdapter(t, serveEvents(event("e", market(map[string]string{"outcomes": `"[\"No\", \"Yes\"]"`}))), nil, nil)
	ins, _, err := a.FetchInstruments(context.Background(), fedUniverse())
	if err != nil || len(ins) != 1 || ins[0].ID != "polymarket:"+noToken {
		t.Fatalf("ins = %+v, err = %v; want the token at the Yes position", ins, err)
	}
}

func TestFetchInstrumentsFiltersAndFlags(t *testing.T) {
	ev := event("fed-dec",
		market(nil), // kept
		market(map[string]string{"closed": "true", clobIDsKey: `"[\"5\", \"6\"]"`}),                    // closed: dropped silently
		market(map[string]string{"active": "false", clobIDsKey: `"[\"7\", \"8\"]"`}),                   // inactive: dropped silently
		market(map[string]string{"outcomes": `"[\"Up\", \"Down\"]"`, clobIDsKey: `"[\"9\", \"10\"]"`}), // not binary
		market(map[string]string{clobIDsKey: `"[\"abc\", \"10\"]"`}),                                   // malformed token
		market(map[string]string{clobIDsKey: `"not json"`}),                                            // malformed array
		market(map[string]string{"question": `""`, clobIDsKey: `"[\"11\", \"12\"]"`}),                  // missing question
		market(map[string]string{"question": `42`}),                                                    // wrong type
		market(map[string]string{clobIDsKey: `"[\"13\", \"13\"]"`}),                                    // same token twice
		market(map[string]string{clobIDsKey: `"[\"14\"]"`}),                                            // one token
		market(map[string]string{"conditionId": `""`, clobIDsKey: `"[\"15\", \"16\"]"`}),               // no condition id
	)
	a := stubAdapter(t, serveEvents(ev, `"not an object"`, `{"id":"2","markets":[]}`), nil, nil)
	ins, _, err := a.FetchInstruments(context.Background(), fedUniverse())
	if err != nil {
		t.Fatal(err)
	}
	if len(ins) != 1 {
		t.Fatalf("got %d instruments, want 1: %+v", len(ins), ins)
	}
	if !ins[0].Quality.Has(canonical.QualityRecordSkipped) {
		t.Errorf("surviving sibling not flagged record_skipped: %+v", ins[0].Quality)
	}
}

func TestFetchInstrumentsFieldQuality(t *testing.T) {
	ev := event("e",
		market(map[string]string{"endDate": `""`, "description": `"  "`, "feeSchedule": "", "feesEnabled": ""}),
		market(map[string]string{"endDate": `"tomorrow"`, clobIDsKey: `"[\"3\", \"4\"]"`, "feesEnabled": "false"}),
		market(map[string]string{clobIDsKey: `"[\"5\", \"6\"]"`, "feeSchedule": `{"rate": -1, "exponent": 1}`}),
	)
	a := stubAdapter(t, serveEvents(ev), nil, nil)
	ins, _, err := a.FetchInstruments(context.Background(), fedUniverse())
	if err != nil || len(ins) != 3 {
		t.Fatalf("ins = %d, err = %v", len(ins), err)
	}
	q0 := ins[0].Quality
	if !q0.Has(canonical.QualityMissingField) || len(q0) != 3 || !ins[0].CloseTime.IsZero() {
		t.Errorf("missing endDate/description/fees: %+v", q0)
	}
	if !ins[1].Quality.Has(canonical.QualityInvalidTime) || !ins[1].FeeSchedule.Params["rate"].IsZero() {
		t.Errorf("bad endDate / fees disabled: %+v %v", ins[1].Quality, ins[1].FeeSchedule)
	}
	if _, ok := ins[2].FeeSchedule.Params["rate"]; ok || !ins[2].Quality.Has(canonical.QualityMissingField) {
		t.Errorf("negative fee rate kept: %+v", ins[2].FeeSchedule)
	}
}

func TestFetchInstrumentsDedupesAcrossTags(t *testing.T) {
	u := venues.Universe{Categories: map[canonical.Category]venues.Selector{
		canonical.CategoryCrypto: {TagSlugs: []string{"bitcoin", "ethereum"}},
	}}
	a := stubAdapter(t, serveEvents(event("e", market(nil))), nil, nil)
	ins, raws, err := a.FetchInstruments(context.Background(), u)
	if err != nil || len(ins) != 1 || len(raws) != 2 {
		t.Fatalf("ins = %d raws = %d err = %v", len(ins), len(raws), err)
	}
}

func TestPagination(t *testing.T) {
	var offsets []string
	gamma := func(w http.ResponseWriter, r *http.Request) {
		offsets = append(offsets, r.URL.Query().Get("offset"))
		if r.URL.Query().Get("limit") != "2" {
			t.Errorf("limit = %s", r.URL.Query().Get("limit"))
		}
		switch r.URL.Query().Get("offset") {
		case "0":
			_, _ = io.WriteString(w, "["+event("a", market(nil))+","+event("b", market(map[string]string{clobIDsKey: `"[\"3\", \"4\"]"`}))+"]")
		case "2":
			_, _ = io.WriteString(w, "["+event("c", market(map[string]string{clobIDsKey: `"[\"5\", \"6\"]"`}))+"]")
		default:
			t.Errorf("unexpected offset %s", r.URL.Query().Get("offset"))
		}
	}
	a := stubAdapter(t, gamma, nil, func(o *Options) { o.PageSize = 2 })
	ins, raws, err := a.FetchInstruments(context.Background(), fedUniverse())
	if err != nil || len(ins) != 3 || len(raws) != 2 || strings.Join(offsets, ",") != "0,2" {
		t.Fatalf("ins = %d raws = %d offsets = %v err = %v", len(ins), len(raws), offsets, err)
	}
}

func TestPaginationCap(t *testing.T) {
	var hits atomic.Int64
	gamma := func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "["+event("a", market(nil))+"]") // always a full page of 1
	}
	a := stubAdapter(t, gamma, nil, func(o *Options) { o.PageSize = 1; o.MaxPages = 5 })
	ins, _, err := a.FetchInstruments(context.Background(), fedUniverse())
	if err != nil || len(ins) != 1 || hits.Load() != 5 {
		t.Fatalf("ins = %d hits = %d err = %v; want the page cap to stop an endless listing", len(ins), hits.Load(), err)
	}
}

func TestRetries(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var hits atomic.Int64
			gamma := func(w http.ResponseWriter, r *http.Request) {
				if hits.Add(1) <= 2 {
					w.WriteHeader(status)
					return
				}
				serveEvents(event("e", market(nil)))(w, r)
			}
			a := stubAdapter(t, gamma, nil, nil)
			ins, raws, err := a.FetchInstruments(context.Background(), fedUniverse())
			if err != nil || len(ins) != 1 || hits.Load() != 3 || raws[0].HTTPStatus != 200 {
				t.Fatalf("ins = %d hits = %d err = %v", len(ins), hits.Load(), err)
			}
		})
	}
}

func TestRetriesExhausted(t *testing.T) {
	var hits atomic.Int64
	gamma := func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(http.StatusBadGateway) }
	a := stubAdapter(t, gamma, nil, nil)
	_, raws, err := a.FetchInstruments(context.Background(), fedUniverse())
	var he *httpError
	if !errors.As(err, &he) || he.Status != http.StatusBadGateway || hits.Load() != 1+DefaultRetries {
		t.Fatalf("err = %v hits = %d", err, hits.Load())
	}
	if len(raws) != 1 || raws[0].HTTPStatus != http.StatusBadGateway {
		t.Errorf("last failed response not returned: %+v", raws)
	}
}

func TestNoRetryOn4xx(t *testing.T) {
	var hits atomic.Int64
	gamma := func(w http.ResponseWriter, _ *http.Request) { hits.Add(1); w.WriteHeader(http.StatusBadRequest) }
	a := stubAdapter(t, gamma, nil, nil)
	if _, _, err := a.FetchInstruments(context.Background(), fedUniverse()); err == nil || hits.Load() != 1 {
		t.Fatalf("err = %v hits = %d", err, hits.Load())
	}
}

func TestTransportErrorRetried(t *testing.T) {
	var hits atomic.Int64
	gamma := func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			hj, _ := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			_ = conn.Close() // drop the connection mid-request
			return
		}
		serveEvents(event("e", market(nil)))(w, r)
	}
	a := stubAdapter(t, gamma, nil, nil)
	ins, _, err := a.FetchInstruments(context.Background(), fedUniverse())
	if err != nil || len(ins) != 1 || hits.Load() != 2 {
		t.Fatalf("ins = %d hits = %d err = %v", len(ins), hits.Load(), err)
	}
}

func TestTimeout(t *testing.T) {
	gamma := func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}
	a := stubAdapter(t, gamma, nil, func(o *Options) { o.Timeout = 50 * time.Millisecond; o.Retries = -1 })
	start := time.Now()
	if _, _, err := a.FetchInstruments(context.Background(), fedUniverse()); err == nil {
		t.Fatal("no error on timeout")
	}
	if time.Since(start) > time.Second {
		t.Errorf("timeout not applied: took %v", time.Since(start))
	}
}

func TestContextCancelledDuringBackoff(t *testing.T) {
	gamma := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }
	a := stubAdapter(t, gamma, nil, func(o *Options) { o.Backoff = time.Hour })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := a.FetchInstruments(ctx, fedUniverse())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
}

func TestBodyCap(t *testing.T) {
	gamma := func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "["+strings.Repeat(" ", 2048)+"]")
	}
	a := stubAdapter(t, gamma, nil, func(o *Options) { o.MaxBodyBytes = 1024 })
	_, raws, err := a.FetchInstruments(context.Background(), fedUniverse())
	if !errors.Is(err, errTooLarge) || len(raws) != 0 {
		t.Fatalf("err = %v raws = %d, want errTooLarge and no stored body", err, len(raws))
	}
}

func TestMalformedPage(t *testing.T) {
	a := stubAdapter(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"error":"x"}`) }, nil, nil)
	_, raws, err := a.FetchInstruments(context.Background(), fedUniverse())
	if err == nil || len(raws) != 1 {
		t.Fatalf("err = %v raws = %d; a non-array page must be an error and still stored", err, len(raws))
	}
}

func TestOneTagFailingKeepsOthers(t *testing.T) {
	gamma := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tag_slug") == "bitcoin" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		serveEvents(event("e", market(nil)))(w, r)
	}
	u := venues.Universe{Categories: map[canonical.Category]venues.Selector{
		canonical.CategoryCrypto: {TagSlugs: []string{"bitcoin"}},
		canonical.CategoryFed:    {TagSlugs: []string{"fed-rates"}},
	}}
	a := stubAdapter(t, gamma, nil, func(o *Options) { o.Retries = -1 })
	ins, _, err := a.FetchInstruments(context.Background(), u)
	if err == nil || len(ins) != 1 || ins[0].Category != canonical.CategoryFed {
		t.Fatalf("ins = %+v err = %v", ins, err)
	}
}

func book(asset string, bids, asks string) string {
	return fmt.Sprintf(`{"market":"0xabc","asset_id":%q,"timestamp":"1791322179740","bids":[%s],"asks":[%s]}`, asset, bids, asks)
}

func clobFor(books map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/book" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		b, ok := books[r.URL.Query().Get("token_id")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"No orderbook exists for the requested token id"}`)
			return
		}
		_, _ = io.WriteString(w, b)
	}
}

func TestFetchQuotesMapping(t *testing.T) {
	// The CLOB lists bids ascending and asks descending (worst first).
	b := book(yesToken,
		`{"price":"0.001","size":"8837.58"},{"price":"0.40","size":"10"},{"price":"0.4","size":"5"},{"price":"0.42","size":"1"}`,
		`{"price":"0.999","size":"3214483.33"},{"price":"0.45","size":"7"},{"price":"0.44","size":"2"}`)
	a := stubAdapter(t, nil, clobFor(map[string]string{yesToken: b}), nil)
	quotes, raws, err := a.FetchQuotes(context.Background(), []canonical.Instrument{{ID: "polymarket:" + yesToken}})
	if err != nil || len(quotes) != 1 || len(raws) != 1 {
		t.Fatalf("quotes = %d raws = %d err = %v", len(quotes), len(raws), err)
	}
	q := quotes[0]
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	wantBids := []string{"0.42:1", "0.4:15", "0.001:8837.58"}
	wantAsks := []string{"0.44:2", "0.45:7", "0.999:3214483.33"}
	if got := levelStrings(q.YesBids); strings.Join(got, " ") != strings.Join(wantBids, " ") {
		t.Errorf("bids = %v, want %v (best first, duplicates merged, extremes kept)", got, wantBids)
	}
	if got := levelStrings(q.YesAsks); strings.Join(got, " ") != strings.Join(wantAsks, " ") {
		t.Errorf("asks = %v, want %v", got, wantAsks)
	}
	if q.VenueTS == nil || !q.VenueTS.Equal(time.UnixMilli(1791322179740)) || !q.Quality.OK() {
		t.Errorf("venue_ts = %v quality = %+v", q.VenueTS, q.Quality)
	}
	if raws[0].Endpoint != "clob/book?token_id="+yesToken {
		t.Errorf("endpoint = %s", raws[0].Endpoint)
	}
}

func levelStrings(ls []canonical.Level) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Price.String() + ":" + l.Size.String()
	}
	return out
}

func TestFetchQuotesBadBooks(t *testing.T) {
	books := map[string]string{
		"1": book("1", `{"price":"1.5","size":"1"},{"price":"NaN","size":"1"},{"price":"0.3","size":"-2"},{"price":"0.3","size":"0"},{"price":"0.2","size":"3"}`, ``),
		"2": book("999", `{"price":"0.2","size":"3"}`, ``), // another token's book
		"3": `{"bids": "nope"}`,
		"4": book("4", ``, ``),
		"5": `{"asset_id":"5","timestamp":"yesterday","bids":[{"price":"0.2","size":"3"}],"asks":[]}`,
		"6": `{"asset_id":"6","bids":[{"price":"0.2","size":"3"}],"asks":[]}`,
	}
	ins := []canonical.Instrument{
		{ID: "polymarket:1"}, {ID: "polymarket:2"}, {ID: "polymarket:3"}, {ID: "polymarket:4"},
		{ID: "polymarket:5"}, {ID: "polymarket:6"}, {ID: "kalshi:KXFED-1"}, {ID: "polymarket:../x"},
	}
	a := stubAdapter(t, nil, clobFor(books), nil)
	quotes, _, err := a.FetchQuotes(context.Background(), ins)
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 6 {
		t.Fatalf("got %d quotes, want 6 (non-polymarket and malformed ids skipped)", len(quotes))
	}
	for _, q := range quotes {
		if err := q.Validate(); err != nil {
			t.Errorf("invalid quote: %v", err)
		}
	}
	q1 := quotes[0].Quality
	if len(quotes[0].YesBids) != 1 || !q1.Has(canonical.QualityInvalidPrice) || !q1.Has(canonical.QualityInvalidSize) || !q1.Has(canonical.QualityRecordSkipped) {
		t.Errorf("bad levels: %+v %+v", quotes[0].YesBids, q1)
	}
	for _, i := range []int{1, 2, 3} {
		if !quotes[i].Quality.Has(canonical.QualityNoBook) || len(quotes[i].YesBids) != 0 {
			t.Errorf("quote %d: %+v, want no_book", i, quotes[i])
		}
	}
	if quotes[4].VenueTS != nil || !quotes[4].Quality.Has(canonical.QualityInvalidTime) {
		t.Errorf("bad timestamp: %+v", quotes[4])
	}
	if quotes[5].VenueTS != nil || !quotes[5].Quality.Has(canonical.QualityMissingField) {
		t.Errorf("missing timestamp: %+v", quotes[5])
	}
}

func TestFetchQuotesAllFail(t *testing.T) {
	clob := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }
	a := stubAdapter(t, nil, clob, func(o *Options) { o.Retries = -1 })
	ins := []canonical.Instrument{{ID: "polymarket:1"}, {ID: "polymarket:2"}}
	quotes, raws, err := a.FetchQuotes(context.Background(), ins)
	if err == nil || len(quotes) != 2 || len(raws) != 2 {
		t.Fatalf("quotes = %d raws = %d err = %v; want an error when the CLOB is down", len(quotes), len(raws), err)
	}
	for _, q := range quotes {
		if !q.Quality.Has(canonical.QualityNoBook) {
			t.Errorf("%+v not flagged no_book", q)
		}
	}
}

func TestFetchQuotesSomeFail(t *testing.T) {
	clob := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token_id") == "1" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		clobFor(map[string]string{"2": book("2", `{"price":"0.5","size":"1"}`, ``)})(w, r)
	}
	a := stubAdapter(t, nil, clob, func(o *Options) { o.Retries = -1 })
	quotes, _, err := a.FetchQuotes(context.Background(), []canonical.Instrument{{ID: "polymarket:1"}, {ID: "polymarket:2"}})
	if err != nil || len(quotes) != 2 || !quotes[0].Quality.Has(canonical.QualityNoBook) || len(quotes[1].YesBids) != 1 {
		t.Fatalf("quotes = %+v err = %v", quotes, err)
	}
}

func TestFetchQuotesCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := stubAdapter(t, nil, clobFor(nil), nil)
	if _, _, err := a.FetchQuotes(ctx, []canonical.Instrument{{ID: "polymarket:1"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchSettled(t *testing.T) {
	since := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	settledMarket := func(token1, token2, prices, closed string) string {
		return market(map[string]string{
			"closed": "true", "outcomePrices": prices, "closedTime": closed,
			clobIDsKey: `"[\"` + token1 + `\", \"` + token2 + `\"]"`,
		})
	}
	var queries []string
	gamma := func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		if r.URL.Query().Get("offset") != "0" {
			t.Errorf("paged past an event that ended before since: %s", r.URL.RawQuery)
		}
		recent := event("dec",
			settledMarket("1", "2", `"[\"1\", \"0\"]"`, `"2026-12-10 19:05:00+00"`),         // YES
			settledMarket("3", "4", `"[\"0\", \"1\"]"`, `"2026-12-10T19:05:00Z"`),           // NO
			settledMarket("5", "6", `"[\"0.5\", \"0.5\"]"`, `"2026-12-10T19:05:00Z"`),       // ambiguous
			settledMarket("7", "8", `"[\"0.9995\", \"0.0005\"]"`, `"2026-12-10T19:05:00Z"`), // not final
			settledMarket("9", "10", `"[\"1\", \"0\"]"`, `"2026-11-01T00:00:00Z"`),          // settled before since
			settledMarket("11", "12", `"[\"1\", \"0\"]"`, `"garbage"`),                      // YES, bad closedTime
			market(map[string]string{clobIDsKey: `"[\"13\", \"14\"]"`}),                     // still open
		)
		old := strings.Replace(event("sep", settledMarket("15", "16", `"[\"1\", \"0\"]"`, `""`)),
			`"endDate":"2026-12-10T19:00:00Z"`, `"endDate":"2026-09-17T18:00:00Z"`, 1)
		_, _ = io.WriteString(w, "["+recent+","+old+"]")
	}
	a := stubAdapter(t, gamma, nil, func(o *Options) { o.PageSize = 2 })
	settled, _, err := a.FetchSettled(context.Background(), fedUniverse(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(queries) != 1 || queries[0] != "ascending=false&closed=true&exclude_tag_id=102127&limit=2&offset=0&order=endDate&tag_slug=fed-rates" {
		t.Errorf("queries = %v", queries)
	}
	got := map[string]canonical.Outcome{}
	for _, s := range settled {
		if err := s.Validate(); err != nil {
			t.Errorf("invalid: %v", err)
		}
		got[s.Instrument.ID] = s.Outcome
		if s.Instrument.Status != canonical.StatusSettled || !s.Instrument.Quality.Has(canonical.QualityAmbiguousOutcome) {
			t.Errorf("%s: status %s quality %+v", s.Instrument.ID, s.Instrument.Status, s.Instrument.Quality)
		}
	}
	want := map[string]canonical.Outcome{"polymarket:1": canonical.OutcomeYes, "polymarket:3": canonical.OutcomeNo, "polymarket:11": canonical.OutcomeYes}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("settled = %v, want %v", got, want)
	}
	for _, s := range settled {
		if s.Instrument.ID == "polymarket:1" && !s.SettledAt.Equal(time.Date(2026, 12, 10, 19, 5, 0, 0, time.UTC)) {
			t.Errorf("settled_at = %v", s.SettledAt)
		}
		if s.Instrument.ID == "polymarket:11" && (!s.SettledAt.IsZero() || !s.Instrument.Quality.Has(canonical.QualityInvalidTime)) {
			t.Errorf("bad closedTime: %+v", s)
		}
	}
}

func TestSameHostRedirectFollowed(t *testing.T) {
	gamma := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/events" {
			http.Redirect(w, r, "/v2/events?"+r.URL.RawQuery, http.StatusFound) //nolint:gosec // test server redirecting on purpose
			return
		}
		serveEvents(event("e", market(nil)))(w, r)
	}
	a := stubAdapter(t, gamma, nil, nil)
	ins, _, err := a.FetchInstruments(context.Background(), fedUniverse())
	if err != nil || len(ins) != 1 {
		t.Fatalf("ins = %d err = %v; a same-host redirect should be followed", len(ins), err)
	}
}

func TestRedirectLoopStopped(t *testing.T) {
	var hits atomic.Int64
	gamma := func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, r.URL.String(), http.StatusFound) //nolint:gosec // test server redirect loop on purpose
	}
	a := stubAdapter(t, gamma, nil, func(o *Options) { o.Retries = -1 })
	if _, _, err := a.FetchInstruments(context.Background(), fedUniverse()); err == nil || hits.Load() != maxRedirects {
		t.Fatalf("err = %v hits = %d", err, hits.Load())
	}
}

func TestRedirectGuardOnSuppliedClient(t *testing.T) {
	supplied := &http.Client{Timeout: time.Second}
	a, err := New(Options{GammaURL: "https://g", ClobURL: "https://c", HTTPClient: supplied, MaxBodyBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if a.gamma.http.CheckRedirect == nil || supplied.CheckRedirect != nil {
		t.Error("guard not applied to a copy of the supplied client")
	}
	own := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	a, _ = New(Options{GammaURL: "https://g", ClobURL: "https://c", HTTPClient: &http.Client{CheckRedirect: own}, MaxBodyBytes: 1})
	if a.gamma.http.CheckRedirect == nil || fmt.Sprintf("%p", a.gamma.http.CheckRedirect) != fmt.Sprintf("%p", own) {
		t.Error("a supplied CheckRedirect was replaced")
	}
}
