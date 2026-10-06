package kalshi

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// seriesRef is one configured series and the category it belongs to.
type seriesRef struct {
	series   string
	category canonical.Category
}

// seriesOf lists u's Kalshi series in a stable order: categories in
// canonical order, series as listed in the universe file.
func seriesOf(u venues.Universe) []seriesRef {
	var out []seriesRef
	for _, c := range canonical.Categories() {
		for _, s := range u.Categories[c].Series {
			out = append(out, seriesRef{series: s, category: c})
		}
	}
	return out
}

// eventsEndpoint is the open-events request for a series; cursor is empty on the first page.
func (a *Adapter) eventsEndpoint(series, cursor string) string {
	e := "/events?series_ticker=" + url.QueryEscape(series) +
		"&status=open&with_nested_markets=true&limit=" + strconv.Itoa(a.opts.EventsPageLimit)
	if cursor != "" {
		e += "&cursor=" + url.QueryEscape(cursor)
	}
	return e
}

// settledEndpoint is the settled-markets request for a series closed at or after since.
func (a *Adapter) settledEndpoint(series string, since time.Time, cursor string) string {
	e := "/markets?series_ticker=" + url.QueryEscape(series) + "&status=settled"
	if !since.IsZero() {
		e += "&min_close_ts=" + strconv.FormatInt(since.Unix(), 10)
	}
	e += "&limit=" + strconv.Itoa(a.opts.SettledPageLimit)
	if cursor != "" {
		e += "&cursor=" + url.QueryEscape(cursor)
	}
	return e
}

// orderbookEndpoint is the order-book request for one market ticker.
func orderbookEndpoint(ticker string) string {
	return "/markets/" + url.PathEscape(ticker) + "/orderbook"
}

// paginate fetches pages of one cursor-paginated listing, calling handle on
// each 2xx body; handle returns the next cursor ("" ends the listing). It
// stops at maxPages or when the venue repeats a cursor.
func (a *Adapter) paginate(ctx context.Context, maxPages int, endpoint func(cursor string) string,
	handle func(raw venues.RawResponse) (string, error)) ([]venues.RawResponse, error) {
	var (
		raws   []venues.RawResponse
		cursor string
		seen   = map[string]bool{}
	)
	for page := 0; page < maxPages; page++ {
		ep := endpoint(cursor)
		raw, err := a.get(ctx, ep)
		if raw.Body != nil {
			raws = append(raws, raw)
		}
		if err != nil {
			return raws, err
		}
		next, err := handle(raw)
		if err != nil {
			return raws, fmt.Errorf("GET %s: %w", ep, err)
		}
		if next == "" {
			return raws, nil
		}
		if seen[next] {
			return raws, fmt.Errorf("GET %s: the venue repeated cursor %q", ep, truncate(next, 64))
		}
		seen[next] = true
		cursor = next
	}
	a.log.Warn("pagination cap reached; later pages not fetched", "endpoint", endpoint(""), "max_pages", maxPages)
	return raws, nil
}

// logSkips logs each dropped record.
func (a *Adapter) logSkips(endpoint string, skips []skip) {
	for _, s := range skips {
		a.log.Warn("skipping malformed record", "endpoint", endpoint, "ticker", truncate(s.ticker, 64), "reason", truncate(s.reason, 200))
	}
}

// FetchInstruments returns the open, non-MVE markets of every configured series.
// A series that fails is logged and the others are still returned, with the
// failures joined into the error.
func (a *Adapter) FetchInstruments(ctx context.Context, u venues.Universe) ([]canonical.Instrument, []venues.RawResponse, error) {
	var (
		out  []canonical.Instrument
		raws []venues.RawResponse
		errs []error
		seen = map[string]bool{}
	)
	for _, sr := range seriesOf(u) {
		r, err := a.paginate(ctx, a.opts.MaxEventPages,
			func(c string) string { return a.eventsEndpoint(sr.series, c) },
			func(raw venues.RawResponse) (string, error) {
				p, err := parseEvents(raw.Body)
				if err != nil {
					return "", err
				}
				a.logSkips(raw.Endpoint, p.skipped)
				for _, ev := range p.events {
					for _, ins := range a.eventInstruments(raw.Endpoint, ev, sr.category) {
						if !seen[ins.ID] {
							seen[ins.ID] = true
							out = append(out, ins)
						}
					}
				}
				return p.cursor, nil
			})
		raws = append(raws, r...)
		if err != nil {
			a.log.Error("fetching series failed", "series", sr.series, "error", err)
			errs = append(errs, fmt.Errorf("kalshi series %s: %w", sr.series, err))
		}
	}
	return out, raws, errors.Join(errs...)
}

// eventInstruments maps one event's markets, dropping MVE markets and
// malformed ones. When a market is dropped as malformed, its siblings carry a
// record_skipped flag so a ladder with a missing rung is visible downstream.
func (a *Adapter) eventInstruments(endpoint string, ev event, cat canonical.Category) []canonical.Instrument {
	skips := ev.skipped
	var ins []canonical.Instrument
	for _, m := range ev.markets {
		if m.isMVE() {
			continue
		}
		i, err := toInstrument(m, cat)
		if err != nil {
			skips = append(skips, skip{ticker: m.Ticker, reason: err.Error()})
			continue
		}
		ins = append(ins, i)
	}
	a.logSkips(endpoint, skips)
	if len(skips) > 0 {
		detail := fmt.Sprintf("%d malformed market(s) dropped from event %s", len(skips), truncate(ev.ticker, 64))
		for k := range ins {
			ins[k].Quality.Add(canonical.QualityRecordSkipped, "event", detail)
		}
	}
	return ins
}

// FetchQuotes fetches the order book of each Kalshi instrument. YES bids are
// taken directly; YES asks are derived as 1 − NO bid at each level (D-010).
// A book that cannot be fetched or decoded gives a quote flagged no_book. The
// error is non-nil only if the context ends or every book failed.
func (a *Adapter) FetchQuotes(ctx context.Context, ins []canonical.Instrument) ([]canonical.Quote, []venues.RawResponse, error) {
	type result struct {
		quote canonical.Quote
		raw   venues.RawResponse
		err   error
	}
	results := make([]result, len(ins))
	sem := make(chan struct{}, a.opts.QuoteConcurrency)
	var wg sync.WaitGroup
	for k, in := range ins {
		if in.Venue != canonical.VenueKalshi || canonical.ValidateInstrumentID(in.ID) != nil ||
			in.ID != canonical.InstrumentID(canonical.VenueKalshi, in.VenueMarketID) {
			results[k].err = fmt.Errorf("instrument %q is not a valid Kalshi instrument", truncate(in.ID, 64))
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				q := canonical.Quote{InstrumentID: in.ID, FetchedAt: a.opts.Now()}
				q.Quality.Add(canonical.QualityNoBook, "orderbook", ctx.Err().Error())
				results[k] = result{quote: q, err: ctx.Err()}
				return
			}
			q, raw, err := a.fetchQuote(ctx, in)
			results[k] = result{quote: q, raw: raw, err: err}
		}()
	}
	wg.Wait()
	var (
		quotes []canonical.Quote
		raws   []venues.RawResponse
		failed int
		errs   []error
	)
	for k, r := range results {
		if r.raw.Body != nil {
			raws = append(raws, r.raw)
		}
		if r.quote.InstrumentID == "" {
			// Not a Kalshi instrument: nothing to quote.
			a.log.Warn("skipping instrument", "instrument_id", truncate(ins[k].ID, 64), "error", r.err)
			continue
		}
		if r.err != nil {
			failed++
			errs = append(errs, r.err)
		}
		quotes = append(quotes, r.quote)
	}
	if err := ctx.Err(); err != nil {
		return quotes, raws, fmt.Errorf("kalshi quotes: %w", err)
	}
	if failed > 0 && failed == len(quotes) {
		return quotes, raws, fmt.Errorf("kalshi quotes: every order book failed: %w", errors.Join(errs...))
	}
	return quotes, raws, nil
}

// fetchQuote fetches one order book. It always returns a quote for in; on
// failure the quote has no levels and a no_book flag, and err says why.
func (a *Adapter) fetchQuote(ctx context.Context, in canonical.Instrument) (canonical.Quote, venues.RawResponse, error) {
	ep := orderbookEndpoint(in.VenueMarketID)
	raw, err := a.get(ctx, ep)
	fetched := raw.FetchedAt
	if fetched.IsZero() {
		fetched = a.opts.Now()
	}
	q := canonical.Quote{InstrumentID: in.ID, FetchedAt: fetched}
	if err != nil {
		a.log.Warn("order book fetch failed", "endpoint", ep, "error", err)
		q.Quality.Add(canonical.QualityNoBook, "orderbook", err.Error())
		return q, raw, err
	}
	b, err := parseOrderbook(raw.Body)
	if err != nil {
		a.log.Warn("order book rejected", "endpoint", ep, "error", err)
		q.Quality.Add(canonical.QualityNoBook, "orderbook", err.Error())
		return q, raw, fmt.Errorf("GET %s: %w", ep, err)
	}
	if !b.quality.OK() {
		a.log.Warn("order book had malformed levels; they were skipped", "endpoint", ep, "issues", len(b.quality))
	}
	q.YesBids = b.yesBids
	q.YesAsks = yesAsks(b.noBids)
	q.Quality = append(q.Quality, b.quality...)
	if len(q.YesBids) == 0 && len(q.YesAsks) == 0 {
		q.Quality.Add(canonical.QualityNoBook, "orderbook", "both sides are empty")
	}
	if err := q.Validate(); err != nil {
		// Unreachable for parsed books; kept so a bad quote never leaves the adapter.
		a.log.Error("derived quote failed validation", "endpoint", ep, "error", err)
		q.YesBids, q.YesAsks = nil, nil
		q.Quality.Add(canonical.QualityNoBook, "orderbook", err.Error())
	}
	return q, raw, nil
}

// FetchSettled returns markets of every configured series that settled YES or
// NO and closed at or after since. Markets with any other result are logged and skipped.
func (a *Adapter) FetchSettled(ctx context.Context, u venues.Universe, since time.Time) ([]canonical.Settled, []venues.RawResponse, error) {
	var (
		out  []canonical.Settled
		raws []venues.RawResponse
		errs []error
	)
	for _, sr := range seriesOf(u) {
		r, err := a.paginate(ctx, a.opts.MaxSettledPages,
			func(c string) string { return a.settledEndpoint(sr.series, since, c) },
			func(raw venues.RawResponse) (string, error) {
				p, err := parseMarkets(raw.Body)
				if err != nil {
					return "", err
				}
				s, skips := settledFrom(p.markets, sr.category)
				out = append(out, s...)
				a.logSkips(raw.Endpoint, append(p.skipped, skips...))
				return p.cursor, nil
			})
		raws = append(raws, r...)
		if err != nil {
			a.log.Error("fetching settled series failed", "series", sr.series, "error", err)
			errs = append(errs, fmt.Errorf("kalshi settled %s: %w", sr.series, err))
		}
	}
	return out, raws, errors.Join(errs...)
}

// settledFrom maps settled markets, skipping MVE markets silently and
// malformed or non-binary results with a reason.
func settledFrom(ms []market, cat canonical.Category) ([]canonical.Settled, []skip) {
	var (
		out   []canonical.Settled
		skips []skip
	)
	for _, m := range ms {
		if m.isMVE() {
			continue
		}
		ins, err := toInstrument(m, cat)
		if err != nil {
			skips = append(skips, skip{ticker: m.Ticker, reason: err.Error()})
			continue
		}
		outcome, ok := mapResult(m.Result)
		if !ok {
			skips = append(skips, skip{ticker: m.Ticker, reason: fmt.Sprintf("%s: result %q is not yes or no", canonical.QualityAmbiguousOutcome, truncate(m.Result, 32))})
			continue
		}
		ins.Status = canonical.StatusSettled
		s := canonical.Settled{Instrument: ins, Outcome: outcome}
		s.SettledAt = parseTime(&s.Instrument.Quality, "settlement_ts", m.SettlementTS, false)
		out = append(out, s)
	}
	return out, skips
}
