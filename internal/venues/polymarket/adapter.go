// Package polymarket is the Polymarket venue adapter. It reads event and
// market metadata from the public Gamma API and order books from the public
// CLOB API (no auth), and maps them into canonical types. Every Instrument is
// the YES outcome token of a YES/NO binary market.
package polymarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// Defaults for Options fields left zero.
const (
	DefaultRetries     = 2
	DefaultBackoff     = 500 * time.Millisecond
	DefaultPageSize    = 50
	DefaultMaxPages    = 40
	DefaultConcurrency = 8
)

// upOrDownTagID is Gamma's "up-or-down" tag (verified 2026-10-06). Those
// events are 5-minute to daily "Up"/"Down" markets, which are not YES/NO
// binaries; excluding them server-side keeps each sweep small (D-057).
const upOrDownTagID = "102127"

// Options configures an Adapter.
type Options struct {
	GammaURL string // POLYMARKET_GAMMA_URL
	ClobURL  string // POLYMARKET_CLOB_URL
	// HTTPClient is used if set; otherwise a client with Timeout is built.
	// Either way, redirects that leave the API host are refused unless the
	// supplied client sets its own CheckRedirect.
	HTTPClient   *http.Client
	Timeout      time.Duration // HTTP_TIMEOUT_SECONDS
	MaxBodyBytes int64         // MAX_BODY_BYTES
	// Retries is the number of retries on 429/5xx/transport errors: 0 means
	// DefaultRetries, a negative value means none.
	Retries int
	// Backoff is the first retry delay; it doubles on each retry.
	Backoff time.Duration
	// PageSize is the Gamma page size (limit); MaxPages caps pages per tag.
	PageSize int
	MaxPages int
	// Concurrency is the number of order books fetched in parallel.
	Concurrency int
	Logger      *slog.Logger
	Now         func() time.Time
}

// Adapter implements venues.Adapter for Polymarket.
type Adapter struct {
	gamma       *client
	clob        *client
	pageSize    int
	maxPages    int
	concurrency int
	logger      *slog.Logger
}

var _ venues.Adapter = (*Adapter)(nil)

// New validates opts, fills defaults and returns an Adapter.
func New(opts Options) (*Adapter, error) {
	gammaURL, err := parseBaseURL("gamma", opts.GammaURL)
	if err != nil {
		return nil, err
	}
	clobURL, err := parseBaseURL("clob", opts.ClobURL)
	if err != nil {
		return nil, err
	}
	if opts.MaxBodyBytes <= 0 {
		return nil, errors.New("polymarket: MaxBodyBytes must be positive")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	var hc *http.Client
	if opts.HTTPClient != nil {
		c := *opts.HTTPClient // copy, so the caller's client is not modified
		hc = &c
	} else {
		if opts.Timeout <= 0 {
			return nil, errors.New("polymarket: Timeout must be positive")
		}
		hc = &http.Client{Timeout: opts.Timeout}
	}
	if hc.CheckRedirect == nil {
		hc.CheckRedirect = sameHostRedirect
	}
	retries := opts.Retries
	switch {
	case retries == 0:
		retries = DefaultRetries
	case retries < 0:
		retries = 0
	}
	backoff := orDefault(opts.Backoff, DefaultBackoff)
	logger := opts.Logger.With("venue", string(canonical.VenuePolymarket))
	mk := func(api string, base *url.URL) *client {
		return &client{api: api, base: base, http: hc, maxBodyBytes: opts.MaxBodyBytes,
			retries: retries, backoff: backoff, logger: logger, now: opts.Now}
	}
	return &Adapter{
		gamma:       mk(apiGamma, gammaURL),
		clob:        mk(apiClob, clobURL),
		pageSize:    orDefault(opts.PageSize, DefaultPageSize),
		maxPages:    orDefault(opts.MaxPages, DefaultMaxPages),
		concurrency: orDefault(opts.Concurrency, DefaultConcurrency),
		logger:      logger,
	}, nil
}

// errRedirectRefused marks a redirect to another origin; it is not retried.
var errRedirectRefused = errors.New("redirect to another origin refused")

// maxRedirects caps same-host redirects per request.
const maxRedirects = 3

// sameHostRedirect refuses redirects that leave the original API host, so a
// hostile or compromised response cannot send the adapter to another server
// while RawResponse.Endpoint still names the Polymarket path (AB-02).
func sameHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if req.URL.Host != via[0].URL.Host || req.URL.Scheme != via[0].URL.Scheme {
		return fmt.Errorf("%w: %q", errRedirectRefused, truncate(req.URL.Scheme+"://"+req.URL.Host, 80))
	}
	return nil
}

func orDefault[T int | time.Duration](v, def T) T {
	if v <= 0 {
		return def
	}
	return v
}

func parseBaseURL(name, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("polymarket: %s URL %q must be an absolute http(s) URL", name, raw)
	}
	return u, nil
}

// Venue returns canonical.VenuePolymarket.
func (a *Adapter) Venue() canonical.Venue { return canonical.VenuePolymarket }

// FetchInstruments reads open events for every configured tag slug, paginated,
// and returns one Instrument per open YES/NO binary market. A failed tag does
// not stop the others: their instruments are still returned, together with
// the joined error.
func (a *Adapter) FetchInstruments(ctx context.Context, u venues.Universe) ([]canonical.Instrument, []venues.RawResponse, error) {
	var (
		out  []canonical.Instrument
		raws []venues.RawResponse
		errs []error
		seen = map[string]bool{}
	)
	for _, ct := range universeTags(u) {
		q := url.Values{
			"tag_slug":       {ct.tag},
			"active":         {"true"},
			"closed":         {"false"},
			"exclude_tag_id": {upOrDownTagID},
		}
		events, rs, err := a.fetchEvents(ctx, q, nil)
		raws = append(raws, rs...)
		if err != nil {
			errs = append(errs, fmt.Errorf("fetching open events for tag %q: %w", ct.tag, err))
		}
		for _, ev := range events {
			for _, ins := range a.openInstruments(ev, ct.category) {
				if seen[ins.ID] {
					continue // the same market can sit in events under several tags
				}
				seen[ins.ID] = true
				out = append(out, ins)
			}
		}
	}
	return out, raws, errors.Join(errs...)
}

// FetchSettled reads closed events for every configured tag, newest end date
// first, stopping once a page reaches events that ended before since. It
// returns closed YES/NO binaries whose final outcomePrices are exactly 1/0;
// ambiguous finals are skipped, logged and flagged on their siblings.
func (a *Adapter) FetchSettled(ctx context.Context, u venues.Universe, since time.Time) ([]canonical.Settled, []venues.RawResponse, error) {
	var (
		out  []canonical.Settled
		raws []venues.RawResponse
		errs []error
		seen = map[string]bool{}
	)
	stop := func(page []gammaEvent) bool {
		if since.IsZero() || len(page) == 0 {
			return false
		}
		end, err := parseGammaTime(page[len(page)-1].EndDate)
		return err == nil && !end.IsZero() && end.Before(since)
	}
	for _, ct := range universeTags(u) {
		q := url.Values{
			"tag_slug":       {ct.tag},
			"closed":         {"true"},
			"order":          {"endDate"},
			"ascending":      {"false"},
			"exclude_tag_id": {upOrDownTagID},
		}
		events, rs, err := a.fetchEvents(ctx, q, stop)
		raws = append(raws, rs...)
		if err != nil {
			errs = append(errs, fmt.Errorf("fetching settled events for tag %q: %w", ct.tag, err))
		}
		for _, ev := range events {
			if end, err := parseGammaTime(ev.EndDate); err == nil && !end.IsZero() && end.Before(since) {
				continue
			}
			for _, s := range a.settledInstruments(ev, ct.category, since) {
				if seen[s.Instrument.ID] {
					continue
				}
				seen[s.Instrument.ID] = true
				out = append(out, s)
			}
		}
	}
	return out, raws, errors.Join(errs...)
}

// categoryTag is one tag slug to fetch and the category it feeds.
type categoryTag struct {
	category canonical.Category
	tag      string
}

// universeTags lists the universe's tag slugs in a stable order.
func universeTags(u venues.Universe) []categoryTag {
	cats := make([]canonical.Category, 0, len(u.Categories))
	for c := range u.Categories {
		cats = append(cats, c)
	}
	sort.Slice(cats, func(i, j int) bool { return cats[i] < cats[j] })
	var out []categoryTag
	for _, c := range cats {
		for _, t := range u.Categories[c].TagSlugs {
			out = append(out, categoryTag{category: c, tag: t})
		}
	}
	return out
}

// fetchEvents pages through Gamma /events with base query q until a short
// page, stop(page) returns true, or MaxPages pages were read. It returns the
// events read so far even when a later page fails.
func (a *Adapter) fetchEvents(ctx context.Context, q url.Values, stop func([]gammaEvent) bool) ([]gammaEvent, []venues.RawResponse, error) {
	var (
		events []gammaEvent
		raws   []venues.RawResponse
	)
	for page := 0; page < a.maxPages; page++ {
		pq := url.Values{}
		for k, v := range q {
			pq[k] = v
		}
		pq.Set("limit", strconv.Itoa(a.pageSize))
		pq.Set("offset", strconv.Itoa(page*a.pageSize))
		raw, err := a.gamma.get(ctx, "/events", pq)
		if raw.HTTPStatus != 0 {
			raws = append(raws, raw)
		}
		if err != nil {
			return events, raws, err
		}
		got, skipped, err := parseEvents(raw.Body)
		if err != nil {
			a.logger.Warn("rejecting malformed events page", "endpoint", raw.Endpoint, "flag", canonical.QualityRecordSkipped, "error", err.Error())
			return events, raws, fmt.Errorf("%s: %w", raw.Endpoint, err)
		}
		for _, s := range skipped {
			a.logger.Warn("skipping malformed event", "endpoint", raw.Endpoint, "flag", canonical.QualityRecordSkipped, "detail", truncate(s, 200))
		}
		events = append(events, got...)
		if len(got)+len(skipped) < a.pageSize || (stop != nil && stop(got)) {
			return events, raws, nil
		}
	}
	a.logger.Warn("stopping pagination at the page cap; results are truncated",
		"tag_slug", q.Get("tag_slug"), "max_pages", a.maxPages, "page_size", a.pageSize)
	return events, raws, nil
}

// eventMarkets parses an event's markets. Malformed markets are returned as
// skip reasons; non-binary markets are dropped silently (out of scope).
func (a *Adapter) eventMarkets(ev gammaEvent) (markets []binaryMarket, skipped []string) {
	for i, raw := range ev.Markets {
		m, err := parseMarket(raw)
		if errors.Is(err, errNotBinary) {
			continue
		}
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("market[%d]: %v", i, err))
			continue
		}
		markets = append(markets, m)
	}
	return markets, skipped
}

// openInstruments maps an event's open binary markets to instruments.
func (a *Adapter) openInstruments(ev gammaEvent, cat canonical.Category) []canonical.Instrument {
	markets, skipped := a.eventMarkets(ev)
	var out []canonical.Instrument
	for _, m := range markets {
		if m.Closed || (m.Active != nil && !*m.Active) {
			continue
		}
		ins, err := a.instrument(ev, m, cat, canonical.StatusOpen)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("market %s: %v", truncate(m.ConditionID, 80), err))
			continue
		}
		out = append(out, ins)
	}
	a.flagSkipped(ev, skipped, canonical.QualityRecordSkipped, func(i int) *canonical.Quality { return &out[i].Quality }, len(out))
	return out
}

// settledInstruments maps an event's closed binary markets with a clear final
// outcome. Markets settled before since (by closedTime) are left out.
func (a *Adapter) settledInstruments(ev gammaEvent, cat canonical.Category, since time.Time) []canonical.Settled {
	markets, skipped := a.eventMarkets(ev)
	var (
		out       []canonical.Settled
		ambiguous []string
	)
	for _, m := range markets {
		if !m.Closed {
			continue
		}
		ins, err := a.instrument(ev, m, cat, canonical.StatusSettled)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("market %s: %v", truncate(m.ConditionID, 80), err))
			continue
		}
		settledAt, err := parseGammaTime(m.ClosedTime)
		if err != nil {
			ins.Quality.Add(canonical.QualityInvalidTime, "closedTime", err.Error())
		}
		if !since.IsZero() && !settledAt.IsZero() && settledAt.Before(since) {
			continue
		}
		outcome, err := settledOutcome(m)
		if err != nil {
			ambiguous = append(ambiguous, fmt.Sprintf("market %s: %v", truncate(m.ConditionID, 80), err))
			continue
		}
		out = append(out, canonical.Settled{Instrument: ins, Outcome: outcome, SettledAt: settledAt})
	}
	q := func(i int) *canonical.Quality { return &out[i].Instrument.Quality }
	a.flagSkipped(ev, skipped, canonical.QualityRecordSkipped, q, len(out))
	a.flagSkipped(ev, ambiguous, canonical.QualityAmbiguousOutcome, q, len(out))
	return out
}

// flagSkipped logs each dropped market of ev and flags the event's surviving
// instruments, since a negRisk group with a missing member is incomplete.
func (a *Adapter) flagSkipped(ev gammaEvent, reasons []string, flag canonical.QualityFlag, quality func(int) *canonical.Quality, n int) {
	if len(reasons) == 0 {
		return
	}
	for _, r := range reasons {
		a.logger.Warn("skipping polymarket market", "event_slug", truncate(ev.Slug, 120), "flag", flag, "detail", truncate(r, 200))
	}
	detail := fmt.Sprintf("%d market(s) in event %s dropped: %s", len(reasons), truncate(ev.Slug, 80), reasons[0])
	for i := 0; i < n; i++ {
		quality(i).Add(flag, "markets", detail)
	}
}

// instrument maps one binary market (its YES token) to a canonical Instrument.
func (a *Adapter) instrument(ev gammaEvent, m binaryMarket, cat canonical.Category, status canonical.InstrumentStatus) (canonical.Instrument, error) {
	var q canonical.Quality
	closeTime, err := parseGammaTime(m.EndDate)
	switch {
	case err != nil:
		q.Add(canonical.QualityInvalidTime, "endDate", err.Error())
	case closeTime.IsZero():
		q.Add(canonical.QualityMissingField, "endDate", "market has no endDate")
	}
	rules := strings.TrimSpace(m.Description)
	if rules == "" {
		q.Add(canonical.QualityMissingField, "description", "market has no rules text")
	}
	fees := feeSchedule(m.gammaMarket, &q)
	ins := canonical.Instrument{
		ID:            canonical.InstrumentID(canonical.VenuePolymarket, m.YesToken),
		Venue:         canonical.VenuePolymarket,
		VenueMarketID: m.ConditionID,
		// Every market of an event, including a negRisk group, shares the event slug.
		VenueEventID: ev.Slug,
		Category:     cat,
		Title:        strings.TrimSpace(m.Question),
		RulesText:    rules,
		Terms:        canonical.UnknownTerms(),
		CloseTime:    closeTime,
		Status:       status,
		FeeSchedule:  fees,
		Quality:      q,
	}
	if err := ins.Validate(); err != nil {
		return canonical.Instrument{}, err
	}
	return ins, nil
}

// FetchQuotes fetches the CLOB book of each instrument's YES token, Concurrency
// at a time, and returns one Quote per Polymarket instrument in input order.
// A missing book (HTTP 404), an empty book or a malformed one gives a quote
// flagged no_book. It returns an error only when the context ends or every
// book request failed (the CLOB is down), so one bad book never fails a round.
func (a *Adapter) FetchQuotes(ctx context.Context, ins []canonical.Instrument) ([]canonical.Quote, []venues.RawResponse, error) {
	type result struct {
		quote  canonical.Quote
		raw    venues.RawResponse
		err    error
		active bool
	}
	results := make([]result, len(ins))
	sem := make(chan struct{}, a.concurrency)
	var wg sync.WaitGroup
	for i, in := range ins {
		token, ok := tokenOf(in.ID)
		if !ok {
			a.logger.Warn("skipping instrument that is not a polymarket token", "instrument_id", truncate(in.ID, 80))
			continue
		}
		results[i].active = true
		wg.Add(1)
		go func(i int, id, token string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				results[i].err = ctx.Err()
				return
			}
			defer func() { <-sem }()
			results[i].quote, results[i].raw, results[i].err = a.fetchBook(ctx, id, token)
		}(i, in.ID, token)
	}
	wg.Wait()

	var (
		quotes         []canonical.Quote
		raws           []venues.RawResponse
		failed, active int
		firstErr       error
	)
	for _, r := range results {
		if !r.active {
			continue
		}
		active++
		if r.raw.HTTPStatus != 0 {
			raws = append(raws, r.raw)
		}
		if r.err != nil {
			failed++
			if firstErr == nil {
				firstErr = r.err
			}
		}
		if r.quote.InstrumentID != "" {
			quotes = append(quotes, r.quote)
		}
	}
	if err := ctx.Err(); err != nil {
		return quotes, raws, fmt.Errorf("fetching polymarket books: %w", err)
	}
	if failed > 0 {
		a.logger.Warn("some polymarket book requests failed", "failed", failed, "total", active, "error", firstErr.Error())
	}
	if active > 0 && failed == active {
		return quotes, raws, fmt.Errorf("fetching polymarket books: all %d requests failed: %w", active, firstErr)
	}
	return quotes, raws, nil
}

// tokenOf returns the CLOB token ID of a Polymarket instrument ID.
func tokenOf(id string) (string, bool) {
	token, ok := strings.CutPrefix(id, string(canonical.VenuePolymarket)+":")
	return token, ok && tokenIDPattern.MatchString(token)
}

// fetchBook fetches and maps one book. err is non-nil only when the request
// itself failed (transport, cap, or a non-404 status after retries); the
// returned quote is then flagged no_book.
func (a *Adapter) fetchBook(ctx context.Context, id, token string) (canonical.Quote, venues.RawResponse, error) {
	raw, err := a.clob.get(ctx, "/book", url.Values{"token_id": {token}})
	q := canonical.Quote{InstrumentID: id, FetchedAt: raw.FetchedAt}
	if q.FetchedAt.IsZero() {
		q.FetchedAt = a.clob.now().UTC()
	}
	if err != nil {
		var he *httpError
		if errors.As(err, &he) && he.Status == http.StatusNotFound {
			q.Quality.Add(canonical.QualityNoBook, "book", "the CLOB has no order book for this token")
			return q, raw, nil
		}
		q.Quality.Add(canonical.QualityNoBook, "book", "book request failed: "+err.Error())
		return q, raw, err
	}
	mapBook(&q, token, raw.Body)
	if !q.Quality.OK() {
		a.logger.Warn("polymarket book has quality issues", "instrument_id", truncate(id, 80), "quality", q.Quality)
	}
	return q, raw, nil
}

// mapBook fills q from a CLOB /book body. Malformed levels are dropped and
// flagged; a body that cannot be decoded, belongs to another token or has no
// valid level leaves the quote flagged no_book. Extreme resting orders (e.g.
// 0.001 / 0.999) are kept: the sanity band is applied downstream (domain.md).
func mapBook(q *canonical.Quote, token string, body []byte) {
	var b clobBook
	if err := json.Unmarshal(body, &b); err != nil {
		q.Quality.Add(canonical.QualityNoBook, "book", "malformed book: "+err.Error())
		return
	}
	if b.AssetID != token {
		q.Quality.Add(canonical.QualityNoBook, "asset_id", fmt.Sprintf("book is for token %q, not the requested one", truncate(b.AssetID, 32)))
		return
	}
	if b.Timestamp == "" {
		q.Quality.Add(canonical.QualityMissingField, "timestamp", "book has no timestamp")
	} else if ts, err := parseBookTimestamp(b.Timestamp); err != nil {
		q.Quality.Add(canonical.QualityInvalidTime, "timestamp", err.Error())
	} else {
		q.VenueTS = &ts
	}
	q.YesBids = mapLevels(b.Bids, "bids", true, &q.Quality)
	q.YesAsks = mapLevels(b.Asks, "asks", false, &q.Quality)
	if len(q.YesBids) == 0 && len(q.YesAsks) == 0 {
		q.Quality.Add(canonical.QualityNoBook, "book", "book has no valid levels")
	}
}

// mapLevels parses one side of a book, merges duplicate prices, and sorts it
// best first: bids by descending price, asks by ascending price.
func mapLevels(raw []clobLevel, side string, bids bool, q *canonical.Quality) []canonical.Level {
	byPrice := make(map[string]canonical.Level, len(raw))
	dropped := 0
	for i, l := range raw {
		price, err := parsePrice(l.Price)
		if err != nil {
			q.Add(canonical.QualityInvalidPrice, fmt.Sprintf("%s[%d].price", side, i), err.Error())
			dropped++
			continue
		}
		size, err := parseSize(l.Size)
		if err != nil {
			q.Add(canonical.QualityInvalidSize, fmt.Sprintf("%s[%d].size", side, i), err.Error())
			dropped++
			continue
		}
		key := price.String()
		if prev, ok := byPrice[key]; ok {
			size = size.Add(prev.Size)
		}
		byPrice[key] = canonical.Level{Price: price, Size: size}
	}
	if dropped > 0 {
		q.Add(canonical.QualityRecordSkipped, side, fmt.Sprintf("%d malformed level(s) dropped", dropped))
	}
	out := make([]canonical.Level, 0, len(byPrice))
	for _, l := range byPrice {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool {
		if bids {
			return out[i].Price.GreaterThan(out[j].Price)
		}
		return out[i].Price.LessThan(out[j].Price)
	})
	return out
}
