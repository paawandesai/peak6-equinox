// Package collector runs one fetch round: every registered venue adapter is
// called concurrently, raw bodies and canonical rows are stored under one
// round ID, and one venue failing never stops the other (D-014, D-015, NFR-03).
package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

const (
	// DefaultMaxQuotesPerVenue bounds the order books fetched per venue per
	// round. Kalshi paces requests 100 ms apart (D-056), so 400 books take
	// about 40 s and keep a round under NFR-02's 60 s.
	DefaultMaxQuotesPerVenue = 400
	// finishTimeout bounds the bookkeeping after the fetches, which runs even
	// when the caller's context is already cancelled.
	finishTimeout = 30 * time.Second
	// maxErrorLen caps error text stored in venue_status and the decision log.
	maxErrorLen = 500
)

// ErrRoundInProgress is returned by Run while another round is running.
var ErrRoundInProgress = errors.New("a fetch round is already running")

// Normalizer extracts structured Terms from an instrument's title and rules
// and returns the instrument with Terms (and any Quality flags) filled in.
// M1-07's normalize.Extract is wired here by cmd/equinox; nil leaves the
// adapter's terms (all unknown) in place.
type Normalizer func(canonical.Instrument) canonical.Instrument

// Options configures a Collector. Zero fields take defaults.
type Options struct {
	// MaxQuotesPerVenue caps order books per venue per round (see
	// SelectForQuotes); 0 means DefaultMaxQuotesPerVenue, negative means no cap.
	MaxQuotesPerVenue int
	Normalize         Normalizer
	Logger            *slog.Logger
	// Now overrides the clock (tests). It must return UTC.
	Now func() time.Time
}

// Collector runs fetch rounds. Create it with New; it is safe for concurrent
// use, but only one round runs at a time.
type Collector struct {
	store     *store.Store
	reg       *venues.Registry
	universes venues.Universes
	opts      Options

	mu      sync.Mutex
	running int64 // ID of the round in progress; -1 while it is being created; 0 when idle
}

// New returns a Collector over the given store, adapters and universes.
func New(st *store.Store, reg *venues.Registry, us venues.Universes, opts Options) (*Collector, error) {
	if st == nil {
		return nil, errors.New("collector: store is required")
	}
	if reg == nil || reg.Len() == 0 {
		return nil, errors.New("collector: at least one venue adapter is required")
	}
	if opts.MaxQuotesPerVenue == 0 {
		opts.MaxQuotesPerVenue = DefaultMaxQuotesPerVenue
	}
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Collector{store: st, reg: reg, universes: us, opts: opts}, nil
}

// VenueResult is one venue's outcome in a round: what was stored and what was not.
type VenueResult struct {
	Venue canonical.Venue
	store.VenueStatus
	// InstrumentsFetched is what the adapter returned before validation.
	InstrumentsFetched int `json:"instruments_fetched"`
	// InstrumentsRejected failed validation after normalisation and were not stored.
	InstrumentsRejected int `json:"instruments_rejected"`
	// QuotesRequested is how many books were asked for; QuotesNotRequested is
	// how many stored instruments were left unquoted by the per-venue cap.
	QuotesRequested    int `json:"quotes_requested"`
	QuotesNotRequested int `json:"quotes_not_requested"`
	// QuotesRejected failed validation or referred to an unstored instrument.
	QuotesRejected int `json:"quotes_rejected"`
	RawResponses   int `json:"raw_responses"`
	// StoreErrors counts rows the database refused; the first one is in Error.
	StoreErrors int `json:"store_errors"`
}

// Result is a finished round.
type Result struct {
	RoundID    int64
	Status     store.RoundStatus
	StartedAt  time.Time
	FinishedAt time.Time
	Venues     []VenueResult // in venue order
}

// Duration is the wall time of the round.
func (r Result) Duration() time.Duration { return r.FinishedAt.Sub(r.StartedAt) }

// Summary is the one-line, human-readable account written to the decision log.
func (r Result) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ingest round %d %s in %s:", r.RoundID, r.Status, r.Duration().Round(100*time.Millisecond))
	for i, v := range r.Venues {
		if i > 0 {
			b.WriteString(";")
		}
		fmt.Fprintf(&b, " %s %d instruments, %d quotes in %s", v.Venue, v.Instruments, v.Quotes,
			(time.Duration(v.DurationMS) * time.Millisecond).Round(100*time.Millisecond))
		if !v.OK {
			fmt.Fprintf(&b, " (failed: %s)", v.Error)
		}
	}
	return b.String()
}

// Running reports the round in progress, if any.
func (c *Collector) Running() (int64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running, c.running != 0
}

// Run executes one round. It returns ErrRoundInProgress if another round is
// running, in this process or in any other process sharing the database (a
// Postgres advisory lock is held for the whole round). A venue failing is recorded in the result, not returned as an
// error; the error is for infrastructure failures (the round row could not be
// created or finished). The Result is filled in as far as the run got.
func (c *Collector) Run(ctx context.Context) (Result, error) {
	if !c.claim() {
		return Result{}, ErrRoundInProgress
	}
	defer c.release()
	// The database lock is what actually serialises rounds: other processes
	// (a second Cloud Run instance) share it, while the in-process guard above
	// only adds the running round's ID for the 409 response.
	unlock, acquired, err := c.store.TryLockIngest(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("starting fetch round: %w", err)
	}
	if !acquired {
		return Result{}, ErrRoundInProgress
	}
	defer unlock()

	startedAt := c.opts.Now()
	round, err := c.store.CreateRound(ctx, startedAt)
	if err != nil {
		return Result{}, fmt.Errorf("starting fetch round: %w", err)
	}
	c.setRunning(round.ID)
	log := c.opts.Logger.With("round_id", round.ID)
	log.Info("fetch round started")

	adapters := c.reg.Adapters()
	results := make([]VenueResult, len(adapters))
	var wg sync.WaitGroup
	for i, a := range adapters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = c.runVenue(ctx, round.ID, a)
		}()
	}
	wg.Wait()

	res := Result{RoundID: round.ID, StartedAt: startedAt, FinishedAt: c.opts.Now(), Venues: results}
	res.Status = roundStatus(results)
	// Bookkeeping must happen even if ctx was cancelled mid-round.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), finishTimeout)
	defer cancel()
	statuses := make(map[canonical.Venue]store.VenueStatus, len(results))
	for _, v := range results {
		statuses[v.Venue] = v.VenueStatus
	}
	if err := c.store.FinishRound(fctx, round.ID, res.Status, statuses, res.FinishedAt); err != nil {
		log.Error("finishing round failed", "error", err)
		return res, fmt.Errorf("finishing fetch round %d: %w", round.ID, err)
	}
	payload := map[string]any{"status": res.Status, "duration_ms": res.Duration().Milliseconds(),
		"max_quotes_per_venue": c.opts.MaxQuotesPerVenue, "venues": results}
	if _, err := c.store.AppendLog(fctx, store.LogIngest, strconv.FormatInt(round.ID, 10), res.Summary(), payload, res.FinishedAt); err != nil {
		log.Error("writing ingest decision log failed", "error", err)
		return res, fmt.Errorf("logging fetch round %d: %w", round.ID, err)
	}
	log.Info("fetch round finished", "status", res.Status, "duration_ms", res.Duration().Milliseconds())
	return res, nil
}

func (c *Collector) claim() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running != 0 {
		return false
	}
	c.running = -1
	return true
}

func (c *Collector) setRunning(id int64) {
	c.mu.Lock()
	c.running = id
	c.mu.Unlock()
}

func (c *Collector) release() {
	c.mu.Lock()
	c.running = 0
	c.mu.Unlock()
}

// roundStatus is ok when every venue succeeded, failed when none did, else partial.
func roundStatus(results []VenueResult) store.RoundStatus {
	ok := 0
	for _, v := range results {
		if v.OK {
			ok++
		}
	}
	switch {
	case ok == len(results):
		return store.RoundOK
	case ok == 0:
		return store.RoundFailed
	}
	return store.RoundPartial
}

// venueRun carries one venue's state through a round.
type venueRun struct {
	c       *Collector
	roundID int64
	venue   canonical.Venue
	log     *slog.Logger
	res     VenueResult
	errs    []error
	first   error // first store error
}

// runVenue fetches, normalises and stores one venue's instruments and quotes.
// It never panics on adapter output and never returns early: whatever was
// fetched before a failure is stored, and the failure is recorded.
func (c *Collector) runVenue(ctx context.Context, roundID int64, a venues.Adapter) VenueResult {
	v := a.Venue()
	r := &venueRun{c: c, roundID: roundID, venue: v, log: c.opts.Logger.With("round_id", roundID, "venue", v), res: VenueResult{Venue: v}}
	t0 := c.opts.Now()

	ins, raws, err := a.FetchInstruments(ctx, c.universes.For(v))
	r.storeRaws(ctx, raws)
	if err != nil {
		r.log.Error("fetching instruments failed", "error", err, "instruments_returned", len(ins))
		r.errs = append(r.errs, fmt.Errorf("instruments: %w", err))
	}
	r.res.InstrumentsFetched = len(ins)
	stored := r.storeInstruments(ctx, ins)

	if len(stored) > 0 {
		selected := SelectForQuotes(stored, c.opts.MaxQuotesPerVenue, c.opts.Now())
		r.res.QuotesRequested, r.res.QuotesNotRequested = len(selected), len(stored)-len(selected)
		quotes, raws, err := a.FetchQuotes(ctx, selected)
		r.storeRaws(ctx, raws)
		if err != nil {
			r.log.Error("fetching quotes failed", "error", err, "quotes_returned", len(quotes))
			r.errs = append(r.errs, fmt.Errorf("quotes: %w", err))
		}
		r.storeQuotes(ctx, quotes, stored)
	}

	r.res.DurationMS = c.opts.Now().Sub(t0).Milliseconds()
	if r.res.StoreErrors > 0 {
		r.errs = append(r.errs, fmt.Errorf("storing: %d rows failed, first: %w", r.res.StoreErrors, r.first))
	}
	if len(r.errs) == 0 {
		r.res.OK = true
	} else {
		r.res.Error = truncate(errors.Join(r.errs...).Error(), maxErrorLen)
	}
	r.log.Info("venue finished", "ok", r.res.OK, "instruments", r.res.Instruments, "quotes", r.res.Quotes,
		"raw_responses", r.res.RawResponses, "duration_ms", r.res.DurationMS)
	return r.res
}

func (r *venueRun) storeError(what string, err error) {
	r.res.StoreErrors++
	if r.first == nil {
		r.first = err
	}
	r.log.Error("storing "+what+" failed", "error", err)
}

func (r *venueRun) storeRaws(ctx context.Context, raws []venues.RawResponse) {
	for _, raw := range raws {
		rec := store.RawResponse{RoundID: r.roundID, Venue: raw.Venue, Endpoint: raw.Endpoint,
			HTTPStatus: raw.HTTPStatus, Body: raw.Body, FetchedAt: raw.FetchedAt}
		if !rec.Venue.Valid() {
			rec.Venue = r.venue
		}
		if rec.FetchedAt.IsZero() {
			rec.FetchedAt = r.c.opts.Now()
		}
		if _, err := r.c.store.InsertRawResponse(ctx, rec); err != nil {
			r.storeError("raw response", err)
			continue
		}
		r.res.RawResponses++
	}
}

// storeInstruments normalises, validates and upserts each instrument, and
// returns the ones now in the database.
func (r *venueRun) storeInstruments(ctx context.Context, ins []canonical.Instrument) []canonical.Instrument {
	stored := make([]canonical.Instrument, 0, len(ins))
	seen := make(map[string]bool, len(ins))
	for _, in := range ins {
		if r.c.opts.Normalize != nil {
			in = r.c.opts.Normalize(in)
		}
		if err := in.Validate(); err != nil {
			r.res.InstrumentsRejected++
			r.log.Warn("rejecting instrument", "instrument_id", truncate(in.ID, 64), "error", err)
			continue
		}
		if in.Venue != r.venue {
			r.res.InstrumentsRejected++
			r.log.Warn("rejecting instrument from another venue", "instrument_id", truncate(in.ID, 64))
			continue
		}
		if seen[in.ID] {
			r.res.InstrumentsRejected++
			r.log.Warn("rejecting duplicate instrument", "instrument_id", truncate(in.ID, 64))
			continue
		}
		seen[in.ID] = true
		if err := r.c.store.UpsertInstrument(ctx, r.roundID, in); err != nil {
			r.storeError("instrument", err)
			continue
		}
		r.res.Instruments++
		stored = append(stored, in)
	}
	return stored
}

// storeQuotes validates and inserts each quote whose instrument was stored this round.
func (r *venueRun) storeQuotes(ctx context.Context, quotes []canonical.Quote, stored []canonical.Instrument) {
	known := make(map[string]bool, len(stored))
	for _, in := range stored {
		known[in.ID] = true
	}
	seen := make(map[string]bool, len(quotes))
	for _, q := range quotes {
		if err := q.Validate(); err != nil {
			r.res.QuotesRejected++
			r.log.Warn("rejecting quote", "instrument_id", truncate(q.InstrumentID, 64), "error", err)
			continue
		}
		if !known[q.InstrumentID] || seen[q.InstrumentID] {
			r.res.QuotesRejected++
			r.log.Warn("rejecting quote for an unstored or repeated instrument", "instrument_id", truncate(q.InstrumentID, 64))
			continue
		}
		seen[q.InstrumentID] = true
		if err := r.c.store.InsertQuote(ctx, r.roundID, q); err != nil {
			r.storeError("quote", err)
			continue
		}
		r.res.Quotes++
	}
}

// truncate shortens s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := 0
	for i := range s {
		if i > n {
			break
		}
		cut = i
	}
	return s[:cut] + "…"
}
