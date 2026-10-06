package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/fixtureserver"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/kalshi"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/polymarket"
)

var (
	ctx = context.Background()
	t0  = time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)
)

// fakeAdapter is a scripted venues.Adapter.
type fakeAdapter struct {
	venue     canonical.Venue
	ins       []canonical.Instrument
	insRaws   []venues.RawResponse
	insErr    error
	quoteFn   func(sel []canonical.Instrument) []canonical.Quote
	quoteErr  error
	block     chan struct{} // FetchInstruments waits on it (or ctx, unless ignoreCtx) when set
	ignoreCtx bool

	mu       sync.Mutex
	quoteReq []canonical.Instrument
}

func (f *fakeAdapter) Venue() canonical.Venue { return f.venue }

func (f *fakeAdapter) FetchInstruments(ctx context.Context, _ venues.Universe) ([]canonical.Instrument, []venues.RawResponse, error) {
	if f.block != nil {
		if f.ignoreCtx {
			<-f.block
		} else {
			select {
			case <-f.block:
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			}
		}
	}
	return f.ins, f.insRaws, f.insErr
}

func (f *fakeAdapter) FetchQuotes(_ context.Context, sel []canonical.Instrument) ([]canonical.Quote, []venues.RawResponse, error) {
	f.mu.Lock()
	f.quoteReq = append([]canonical.Instrument(nil), sel...)
	f.mu.Unlock()
	var quotes []canonical.Quote
	if f.quoteFn != nil {
		quotes = f.quoteFn(sel)
	}
	raws := make([]venues.RawResponse, 0, len(sel))
	for _, in := range sel {
		raws = append(raws, venues.RawResponse{Venue: f.venue, Endpoint: "/book/" + in.VenueMarketID, HTTPStatus: 200, Body: []byte(`{"book":1}`), FetchedAt: t0})
	}
	return quotes, raws, f.quoteErr
}

func (f *fakeAdapter) FetchSettled(context.Context, venues.Universe, time.Time) ([]canonical.Settled, []venues.RawResponse, error) {
	return nil, nil, errors.New("not used")
}

func (f *fakeAdapter) requested() []canonical.Instrument {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.quoteReq
}

func instrument(v canonical.Venue, key string, cat canonical.Category, closeAt time.Time) canonical.Instrument {
	return canonical.Instrument{
		ID: canonical.InstrumentID(v, key), Venue: v, VenueMarketID: key, VenueEventID: "EVT", Category: cat,
		Title: "Will " + key + " happen?", RulesText: "rules", Terms: canonical.UnknownTerms(), CloseTime: closeAt,
		Status: canonical.StatusOpen, FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
	}
}

func instruments(v canonical.Venue, n int) []canonical.Instrument {
	out := make([]canonical.Instrument, 0, n)
	for i := range n {
		out = append(out, instrument(v, fmt.Sprintf("M%03d", i), canonical.CategoryCrypto, t0.Add(time.Duration(i)*time.Hour)))
	}
	return out
}

func quote(id string) canonical.Quote {
	return canonical.Quote{InstrumentID: id, FetchedAt: t0,
		YesBids: []canonical.Level{{Price: decimal.RequireFromString("0.4"), Size: decimal.NewFromInt(10)}},
		YesAsks: []canonical.Level{{Price: decimal.RequireFromString("0.6"), Size: decimal.NewFromInt(5)}}}
}

func quotesFor(sel []canonical.Instrument) []canonical.Quote {
	out := make([]canonical.Quote, 0, len(sel))
	for _, in := range sel {
		out = append(out, quote(in.ID))
	}
	return out
}

func rawPage(v canonical.Venue) []venues.RawResponse {
	return []venues.RawResponse{{Venue: v, Endpoint: "/events?x=1", HTTPStatus: 200, Body: []byte(strings.Repeat(`{"page":1}`, 50)), FetchedAt: t0}}
}

func okAdapter(v canonical.Venue, n int) *fakeAdapter {
	return &fakeAdapter{venue: v, ins: instruments(v, n), insRaws: rawPage(v), quoteFn: quotesFor}
}

func newCollector(t *testing.T, opts Options, adapters ...venues.Adapter) (*Collector, *store.Store) {
	t.Helper()
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	reg, err := venues.NewRegistry(adapters...)
	if err != nil {
		t.Fatal(err)
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return t0 }
	}
	c, err := New(st, reg, venues.Universes{}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, st
}

func TestNewRejectsMissingDeps(t *testing.T) {
	reg, _ := venues.NewRegistry(okAdapter(canonical.VenueKalshi, 1))
	if _, err := New(nil, reg, nil, Options{}); err == nil {
		t.Error("nil store accepted")
	}
	empty, _ := venues.NewRegistry()
	if _, err := New(store.New(nil), empty, nil, Options{}); err == nil {
		t.Error("empty registry accepted")
	}
	if _, err := New(store.New(nil), nil, nil, Options{}); err == nil {
		t.Error("nil registry accepted")
	}
	c, err := New(store.New(nil), reg, nil, Options{})
	if err != nil || c.opts.MaxQuotesPerVenue != DefaultMaxQuotesPerVenue || c.opts.Logger == nil || c.opts.Now == nil {
		t.Errorf("defaults not applied: %+v, %v", c.opts, err)
	}
	if id, running := c.Running(); running || id != 0 {
		t.Errorf("Running on idle = %d %v", id, running)
	}
}

func TestRunBothVenuesOK(t *testing.T) {
	k := okAdapter(canonical.VenueKalshi, 5)
	p := okAdapter(canonical.VenuePolymarket, 3)
	c, st := newCollector(t, Options{}, k, p)

	res, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != store.RoundOK || res.RoundID == 0 || !res.StartedAt.Equal(t0) || len(res.Venues) != 2 {
		t.Fatalf("result = %+v", res)
	}
	want := map[canonical.Venue]int{canonical.VenueKalshi: 5, canonical.VenuePolymarket: 3}
	for _, v := range res.Venues {
		if !v.OK || v.Instruments != want[v.Venue] || v.Quotes != want[v.Venue] || v.RawResponses != 1+want[v.Venue] ||
			v.QuotesRequested != want[v.Venue] || v.QuotesNotRequested != 0 || v.InstrumentsRejected != 0 || v.Error != "" {
			t.Errorf("%s = %+v", v.Venue, v)
		}
	}
	round, err := st.GetRound(ctx, res.RoundID)
	if err != nil || round.Status != store.RoundOK || round.FinishedAt.IsZero() || !round.VenueStatus[canonical.VenueKalshi].OK ||
		round.VenueStatus[canonical.VenueKalshi].Instruments != 5 || round.VenueStatus[canonical.VenuePolymarket].Quotes != 3 {
		t.Errorf("stored round = %+v, %v", round, err)
	}
	raws, err := st.ListRawResponses(ctx, res.RoundID)
	if err != nil || len(raws) != 2+5+3 {
		t.Fatalf("raw responses = %d, %v", len(raws), err)
	}
	if string(raws[0].Body) != strings.Repeat(`{"page":1}`, 50) {
		t.Errorf("raw body round-trip: %.40q", raws[0].Body)
	}
	ins, err := st.ListInstruments(ctx, store.InstrumentFilter{SeenInRound: res.RoundID})
	if err != nil || len(ins) != 8 {
		t.Fatalf("instruments = %d, %v", len(ins), err)
	}
	quotes, err := st.ListQuotes(ctx, res.RoundID)
	if err != nil || len(quotes) != 8 {
		t.Fatalf("quotes = %d, %v", len(quotes), err)
	}
	if bid := quotes[0].BestBid; !bid.Valid || !bid.Decimal.Equal(decimal.RequireFromString("0.4")) {
		t.Errorf("quote best bid = %+v", bid)
	}
	logs, err := st.ListLog(ctx, store.LogFilter{Kind: store.LogIngest})
	if err != nil || len(logs) != 1 {
		t.Fatalf("ingest log entries = %d, %v", len(logs), err)
	}
	if logs[0].Ref != fmt.Sprint(res.RoundID) || !strings.Contains(logs[0].Summary, "kalshi 5 instruments, 5 quotes") || !strings.Contains(logs[0].Summary, " ok ") {
		t.Errorf("log entry = %+v", logs[0])
	}
	var payload struct {
		Status string `json:"status"`
		Venues []struct {
			Venue string `json:"Venue"`
			OK    bool   `json:"ok"`
		} `json:"venues"`
	}
	if err := json.Unmarshal(logs[0].Payload, &payload); err != nil || payload.Status != "ok" || len(payload.Venues) != 2 || !payload.Venues[0].OK {
		t.Errorf("log payload = %s (%v)", logs[0].Payload, err)
	}
	latest, err := st.LatestRound(ctx)
	if err != nil || latest.ID != res.RoundID {
		t.Errorf("LatestRound = %+v, %v", latest, err)
	}
}

func TestRunOneVenueFailsIsPartial(t *testing.T) {
	k := okAdapter(canonical.VenueKalshi, 4)
	p := &fakeAdapter{venue: canonical.VenuePolymarket, insErr: errors.New("GET /events: HTTP status 500"),
		insRaws: []venues.RawResponse{{Venue: canonical.VenuePolymarket, Endpoint: "/events", HTTPStatus: 500, Body: []byte("boom"), FetchedAt: t0}}}
	c, st := newCollector(t, Options{}, k, p)

	res, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != store.RoundPartial {
		t.Fatalf("status = %s, want partial", res.Status)
	}
	kr, pr := res.Venues[0], res.Venues[1]
	if !kr.OK || kr.Instruments != 4 || kr.Quotes != 4 {
		t.Errorf("kalshi = %+v", kr)
	}
	if pr.OK || !strings.Contains(pr.Error, "HTTP status 500") || pr.Instruments != 0 || pr.RawResponses != 1 || pr.QuotesRequested != 0 {
		t.Errorf("polymarket = %+v", pr)
	}
	if p.requested() != nil {
		t.Error("quotes were requested for a venue with no instruments")
	}
	round, _ := st.GetRound(ctx, res.RoundID)
	if round.Status != store.RoundPartial || round.VenueStatus[canonical.VenuePolymarket].OK || round.VenueStatus[canonical.VenuePolymarket].Error == "" {
		t.Errorf("stored round = %+v", round)
	}
	quotes, _ := st.ListQuotes(ctx, res.RoundID)
	if len(quotes) != 4 {
		t.Errorf("quotes stored = %d, want the 4 kalshi quotes", len(quotes))
	}
	raws, _ := st.ListRawResponses(ctx, res.RoundID)
	var failedRaw int
	for _, r := range raws {
		if r.HTTPStatus == 500 && string(r.Body) == "boom" {
			failedRaw++
		}
	}
	if failedRaw != 1 {
		t.Errorf("the failed venue's raw 500 body was not kept (%d)", failedRaw)
	}
	if logs, _ := st.ListLog(ctx, store.LogFilter{Kind: store.LogIngest}); len(logs) != 1 || !strings.Contains(logs[0].Summary, "polymarket 0 instruments, 0 quotes") || !strings.Contains(logs[0].Summary, "failed: instruments: GET /events: HTTP status 500") {
		t.Errorf("log = %+v", logs)
	}
}

func TestRunBothVenuesFail(t *testing.T) {
	k := &fakeAdapter{venue: canonical.VenueKalshi, insErr: errors.New("kalshi down")}
	p := &fakeAdapter{venue: canonical.VenuePolymarket, insErr: errors.New("polymarket down")}
	c, st := newCollector(t, Options{}, k, p)
	res, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != store.RoundFailed || res.Venues[0].OK || res.Venues[1].OK {
		t.Errorf("result = %+v", res)
	}
	round, _ := st.GetRound(ctx, res.RoundID)
	if round.Status != store.RoundFailed {
		t.Errorf("stored status = %s", round.Status)
	}
	if _, err := st.LatestRound(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a failed round became the latest round: %v", err)
	}
	if logs, _ := st.ListLog(ctx, store.LogFilter{Kind: store.LogIngest}); len(logs) != 1 || !strings.Contains(logs[0].Summary, "failed in") {
		t.Errorf("log = %+v", logs)
	}
}

// A venue that returns some instruments together with an error (one series
// failed) keeps what it got but is not OK, so the round is partial.
func TestRunVenueWithPartialDataIsNotOK(t *testing.T) {
	k := okAdapter(canonical.VenueKalshi, 3)
	k.insErr = errors.New("kalshi series KXETHD: HTTP status 503")
	k.quoteErr = errors.New("kalshi quotes: context deadline exceeded")
	p := okAdapter(canonical.VenuePolymarket, 2)
	c, st := newCollector(t, Options{}, k, p)
	res, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != store.RoundPartial {
		t.Errorf("status = %s", res.Status)
	}
	kr := res.Venues[0]
	if kr.OK || kr.Instruments != 3 || kr.Quotes != 3 || !strings.Contains(kr.Error, "KXETHD") || !strings.Contains(kr.Error, "quotes: kalshi quotes") {
		t.Errorf("kalshi = %+v", kr)
	}
	if quotes, _ := st.ListQuotes(ctx, res.RoundID); len(quotes) != 5 {
		t.Errorf("quotes stored = %d, want 5", len(quotes))
	}
}

func TestRunNormalizesAndRejectsBadRecords(t *testing.T) {
	k := okAdapter(canonical.VenueKalshi, 4)
	k.ins = append(k.ins,
		k.ins[0], // duplicate ID
		instrument(canonical.VenuePolymarket, "WRONG", canonical.CategoryCrypto, t0), // another venue's instrument
		canonical.Instrument{ID: "kalshi:NO-TITLE", Venue: canonical.VenueKalshi, VenueMarketID: "NO-TITLE", Category: canonical.CategoryCrypto, Terms: canonical.UnknownTerms(), Status: canonical.StatusOpen, FeeSchedule: canonical.FeeSchedule{ID: "x"}},
	)
	k.quoteFn = func(sel []canonical.Instrument) []canonical.Quote {
		qs := quotesFor(sel)
		bad := quote(sel[0].ID)
		bad.YesBids = []canonical.Level{{Price: decimal.NewFromInt(2), Size: decimal.NewFromInt(1)}}
		return append(qs, quote("kalshi:NEVER-STORED"), qs[0], bad)
	}
	normalized := 0
	c, st := newCollector(t, Options{Normalize: func(in canonical.Instrument) canonical.Instrument {
		normalized++
		in.Terms.Subject = "BTC"
		if in.VenueMarketID == "M003" {
			in.Terms.Comparator = "bogus" // the normaliser misbehaved; the record is rejected, never panics
		}
		return in
	}}, k)
	res, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	kr := res.Venues[0]
	if normalized != 7 || kr.InstrumentsFetched != 7 || kr.Instruments != 3 || kr.InstrumentsRejected != 4 {
		t.Errorf("normalized %d, kalshi = %+v", normalized, kr)
	}
	if kr.Quotes != 3 || kr.QuotesRejected != 3 || !kr.OK {
		t.Errorf("kalshi quotes = %+v", kr)
	}
	ins, _ := st.ListInstruments(ctx, store.InstrumentFilter{Venue: canonical.VenueKalshi})
	if len(ins) != 3 || ins[0].Terms.Subject != "BTC" {
		t.Errorf("stored instruments = %+v", ins)
	}
	if res.Status != store.RoundOK {
		t.Errorf("status = %s; rejected records are flagged, not failures", res.Status)
	}
}

func TestRunCapsQuotesPerVenue(t *testing.T) {
	k := okAdapter(canonical.VenueKalshi, 30)
	c, _ := newCollector(t, Options{MaxQuotesPerVenue: 10}, k)
	res, err := c.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	kr := res.Venues[0]
	if len(k.requested()) != 10 || kr.QuotesRequested != 10 || kr.QuotesNotRequested != 20 || kr.Quotes != 10 || kr.Instruments != 30 {
		t.Errorf("requested %d, kalshi = %+v", len(k.requested()), kr)
	}
	if got := k.requested()[0].VenueMarketID; got != "M000" {
		t.Errorf("first quoted = %s, want the soonest-closing M000", got)
	}

	k2 := okAdapter(canonical.VenueKalshi, 30)
	c2, _ := newCollector(t, Options{MaxQuotesPerVenue: -1}, k2)
	if _, err := c2.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(k2.requested()) != 30 {
		t.Errorf("negative cap requested %d, want all 30", len(k2.requested()))
	}
}

func TestRunRefusesConcurrentRounds(t *testing.T) {
	k := okAdapter(canonical.VenueKalshi, 1)
	k.block = make(chan struct{})
	c, _ := newCollector(t, Options{}, k)
	done := make(chan Result, 1)
	go func() {
		res, err := c.Run(ctx)
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if id, running := c.Running(); running && id > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("round never started")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := c.Run(ctx); !errors.Is(err, ErrRoundInProgress) {
		t.Errorf("second Run = %v, want ErrRoundInProgress", err)
	}
	close(k.block)
	res := <-done
	if id, running := c.Running(); running || id != 0 {
		t.Errorf("Running after finish = %d %v", id, running)
	}
	if res.Status != store.RoundOK {
		t.Errorf("status = %s", res.Status)
	}
	// And a new round runs fine afterwards.
	if _, err := c.Run(ctx); err != nil {
		t.Errorf("third Run: %v", err)
	}
}

// Another process (a second Cloud Run instance) holding the database lock
// is refused too, and no round row is created for the refused attempt.
func TestRunRefusesRoundHeldByAnotherProcess(t *testing.T) {
	k := okAdapter(canonical.VenueKalshi, 1)
	c, st := newCollector(t, Options{}, k)
	unlock, acquired, err := st.TryLockIngest(ctx)
	if err != nil || !acquired {
		t.Fatalf("taking the ingest lock: %v %v", acquired, err)
	}
	if _, err := c.Run(ctx); !errors.Is(err, ErrRoundInProgress) {
		t.Errorf("Run while another process holds the lock = %v, want ErrRoundInProgress", err)
	}
	if rounds, _ := st.ListRounds(ctx, 10); len(rounds) != 0 {
		t.Errorf("a refused attempt created %d round row(s)", len(rounds))
	}
	if _, running := c.Running(); running {
		t.Error("collector marked running after a refused attempt")
	}
	unlock()
	res, err := c.Run(ctx)
	if err != nil || res.Status != store.RoundOK {
		t.Errorf("Run after the other process released = %+v, %v", res, err)
	}
	// The lock is released after the round, so the next round proceeds.
	if _, acquired, _ := st.TryLockIngest(ctx); !acquired {
		t.Error("ingest lock still held after Run returned")
	}
}

func TestRunCancelledContextStillFinishesRound(t *testing.T) {
	k := okAdapter(canonical.VenueKalshi, 2)
	c, st := newCollector(t, Options{}, k)
	cctx, cancel := context.WithCancel(ctx)
	// Cancel after the round row exists: the fake ignores ctx, so the stores fail.
	k.block = make(chan struct{})
	k.ignoreCtx = true
	go func() {
		for {
			if id, running := c.Running(); running && id > 0 {
				cancel()
				close(k.block)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	res, err := c.Run(cctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	kr := res.Venues[0]
	if res.Status != store.RoundFailed || kr.OK || kr.StoreErrors == 0 || !strings.Contains(kr.Error, "storing:") {
		t.Errorf("result = %+v", res)
	}
	round, err := st.GetRound(ctx, res.RoundID)
	if err != nil || round.Status != store.RoundFailed {
		t.Errorf("round not finished after cancel: %+v %v", round, err)
	}
	if logs, _ := st.ListLog(ctx, store.LogFilter{Kind: store.LogIngest}); len(logs) != 1 {
		t.Errorf("ingest log entries = %d", len(logs))
	}
}

func TestRunStoreUnavailable(t *testing.T) {
	st := store.New(tdb.Pool(t)) // real pool, but the round is created on a dead context
	reg, _ := venues.NewRegistry(okAdapter(canonical.VenueKalshi, 1))
	c, err := New(st, reg, venues.Universes{}, Options{Now: func() time.Time { return t0 }})
	if err != nil {
		t.Fatal(err)
	}
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Run(dead); err == nil || !strings.Contains(err.Error(), "starting fetch round") {
		t.Errorf("Run with a dead context = %v", err)
	}
	if _, running := c.Running(); running {
		t.Error("collector still marked running after a failed start")
	}
}

func TestSummary(t *testing.T) {
	r := Result{RoundID: 7, Status: store.RoundPartial, StartedAt: t0, FinishedAt: t0.Add(41*time.Second + 240*time.Millisecond), Venues: []VenueResult{
		{Venue: canonical.VenueKalshi, VenueStatus: store.VenueStatus{OK: true, Instruments: 955, Quotes: 400, DurationMS: 41000}},
		{Venue: canonical.VenuePolymarket, VenueStatus: store.VenueStatus{OK: false, Error: "instruments: HTTP status 500", DurationMS: 300}},
	}}
	want := "ingest round 7 partial in 41.2s: kalshi 955 instruments, 400 quotes in 41s; polymarket 0 instruments, 0 quotes in 300ms (failed: instruments: HTTP status 500)"
	if got := r.Summary(); got != want {
		t.Errorf("Summary =\n%s\nwant\n%s", got, want)
	}
	if r.Duration() != 41*time.Second+240*time.Millisecond {
		t.Errorf("Duration = %s", r.Duration())
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("héllo wörld", 6); got != "héllo…" && got != "héll…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate = %q", got)
	}
}

// TestRunRealAdaptersAgainstFixtures is the offline integration round: both
// real adapters through the fixture server, as `equinox seed` does.
func TestRunRealAdaptersAgainstFixtures(t *testing.T) {
	fsrv, err := fixtureserver.New(filepath.Join("..", "..", "testdata", "fixtures"), nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(fsrv.Handler())
	t.Cleanup(srv.Close)
	kURL, gURL, cURL := fixtureserver.VenueURLs(srv.URL)
	now := func() time.Time { return t0 }
	ka, err := kalshi.New(kalshi.Options{BaseURL: kURL, MinInterval: -1, Retries: -1, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	pa, err := polymarket.New(polymarket.Options{GammaURL: gURL, ClobURL: cURL, Timeout: 5 * time.Second, MaxBodyBytes: 10 << 20, Retries: -1, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	us, err := venues.LoadUniverse(filepath.Join("..", "..", "config", "universe"))
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	reg, _ := venues.NewRegistry(ka, pa)
	c, err := New(st, reg, us, Options{Logger: slog.New(slog.DiscardHandler), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != store.RoundOK {
		t.Fatalf("status = %s: %+v", res.Status, res.Venues)
	}
	for _, v := range res.Venues {
		if !v.OK || v.Instruments < 100 || v.QuotesRequested != DefaultMaxQuotesPerVenue || v.Quotes != DefaultMaxQuotesPerVenue || v.InstrumentsRejected != 0 {
			t.Errorf("%s = %+v", v.Venue, v)
		}
	}
	// The recorded order books are among the quoted instruments (soonest close
	// first, categories interleaved), so routing has books to walk.
	quotes, err := st.ListQuotes(ctx, res.RoundID)
	if err != nil {
		t.Fatal(err)
	}
	withBook := map[canonical.Venue]int{}
	for _, q := range quotes {
		if !q.Quality.Has(canonical.QualityNoBook) {
			withBook[canonical.VenueOf(q.InstrumentID)]++
		}
	}
	// Kalshi's recorded books (today's KXBTCD ladder and the next Fed meeting)
	// are the soonest-closing, so they are quoted. Polymarket's recorded books
	// are the venue's most popular, long-dated markets, which the priority rule
	// leaves for later; the quoted set is the soonest-closing one instead.
	if withBook[canonical.VenueKalshi] < 10 {
		t.Errorf("quotes with a book = %v, want the recorded Kalshi books", withBook)
	}
	quoted := map[string]bool{}
	for _, q := range quotes {
		quoted[q.InstrumentID] = true
	}
	ins, _ := st.ListInstruments(ctx, store.InstrumentFilter{Venue: canonical.VenuePolymarket, Category: canonical.CategoryCrypto, Limit: 10000})
	var latestQuoted, soonestUnquoted time.Time
	for _, in := range ins {
		if in.CloseTime.Before(t0) {
			continue // already closed; quoted after the open ones
		}
		switch {
		case quoted[in.ID] && in.CloseTime.After(latestQuoted):
			latestQuoted = in.CloseTime
		case !quoted[in.ID] && (soonestUnquoted.IsZero() || in.CloseTime.Before(soonestUnquoted)):
			soonestUnquoted = in.CloseTime
		}
	}
	if latestQuoted.IsZero() || soonestUnquoted.IsZero() || latestQuoted.After(soonestUnquoted) {
		t.Errorf("polymarket crypto: latest quoted close %v is after the soonest unquoted close %v", latestQuoted, soonestUnquoted)
	}
	if fsrv.Hits() < 800 {
		t.Errorf("fixture server hits = %d", fsrv.Hits())
	}
	if _, err := st.GetQuote(ctx, res.RoundID, "kalshi:KXFED-26OCT-T3.25"); err != nil {
		t.Errorf("recorded KXFED-26OCT-T3.25 book not quoted: %v", err)
	}
}
