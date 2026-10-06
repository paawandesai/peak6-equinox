// Package kalshi is the read-only adapter for Kalshi's public trade API
// (https://api.elections.kalshi.com/trade-api/v2). It never sends credentials
// and only calls endpoints that need no auth: events with nested markets, a
// market's order book, and settled markets.
package kalshi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// Defaults for Options fields left zero.
const (
	DefaultBaseURL          = "https://api.elections.kalshi.com/trade-api/v2"
	DefaultTimeout          = 10 * time.Second
	DefaultMaxBodyBytes     = 10 << 20
	DefaultRetries          = 2
	DefaultBackoff          = time.Second
	DefaultMinInterval      = 100 * time.Millisecond
	DefaultEventsPageLimit  = 200 // the API maximum for /events
	DefaultSettledPageLimit = 200
	DefaultMaxEventPages    = 20
	DefaultMaxSettledPages  = 3
	DefaultQuoteConcurrency = 4
	userAgent               = "equinox/1 (read-only; +https://labs.gauntletai.com/paawandesai/peak6-equinox)"
)

// Options configures an Adapter. Zero fields take the defaults above.
type Options struct {
	BaseURL      string        // KALSHI_BASE_URL, without a trailing slash
	Timeout      time.Duration // HTTP_TIMEOUT_SECONDS, per request attempt
	MaxBodyBytes int64         // MAX_BODY_BYTES
	// Retries is how many times a 429 or 5xx response (or a transport error)
	// is retried; a negative value disables retries.
	Retries int
	// Backoff is the wait before the first retry; it doubles on each retry.
	Backoff time.Duration
	// EventsPageLimit and SettledPageLimit are the page sizes requested.
	EventsPageLimit  int
	SettledPageLimit int
	// MaxEventPages and MaxSettledPages cap cursor pagination per series.
	MaxEventPages   int
	MaxSettledPages int
	// MinInterval is the least time between the starts of two requests, shared
	// by all of the adapter's requests; Kalshi answers bursts with 429. A
	// negative value disables pacing.
	MinInterval time.Duration
	// QuoteConcurrency is how many order books are fetched at once.
	QuoteConcurrency int
	Logger           *slog.Logger
	// HTTPClient overrides the client built from Timeout (tests only).
	HTTPClient *http.Client
	// Now overrides the clock (tests only). It must return UTC.
	Now func() time.Time
}

// Adapter implements venues.Adapter for Kalshi.
type Adapter struct {
	opts Options
	base *url.URL
	hc   *http.Client
	log  *slog.Logger

	paceMu   sync.Mutex
	nextSlot time.Time
}

var _ venues.Adapter = (*Adapter)(nil)

// New returns a Kalshi adapter. The base URL must be an absolute http(s) URL.
func New(o Options) (*Adapter, error) {
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	o.BaseURL = strings.TrimRight(o.BaseURL, "/")
	base, err := url.Parse(o.BaseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("kalshi: base URL %q must be an absolute http(s) URL", o.BaseURL)
	}
	setDefault(&o.Timeout, DefaultTimeout)
	setDefault(&o.MaxBodyBytes, DefaultMaxBodyBytes)
	setDefault(&o.Backoff, DefaultBackoff)
	setDefault(&o.EventsPageLimit, DefaultEventsPageLimit)
	setDefault(&o.SettledPageLimit, DefaultSettledPageLimit)
	setDefault(&o.MaxEventPages, DefaultMaxEventPages)
	setDefault(&o.MaxSettledPages, DefaultMaxSettledPages)
	setDefault(&o.QuoteConcurrency, DefaultQuoteConcurrency)
	switch {
	case o.MinInterval == 0:
		o.MinInterval = DefaultMinInterval
	case o.MinInterval < 0:
		o.MinInterval = 0
	}
	switch {
	case o.Retries == 0:
		o.Retries = DefaultRetries
	case o.Retries < 0:
		o.Retries = 0
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Now == nil {
		o.Now = func() time.Time { return time.Now().UTC() }
	}
	hc := o.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: o.Timeout, CheckRedirect: sameHostRedirect}
	}
	return &Adapter{opts: o, base: base, hc: hc, log: o.Logger.With("venue", string(canonical.VenueKalshi))}, nil
}

func setDefault[T int | int64 | time.Duration](p *T, def T) {
	if *p <= 0 {
		*p = def
	}
}

// sameHostRedirect refuses redirects that leave the API host, so a hostile
// response cannot point the adapter at another server.
func sameHostRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("too many redirects")
	}
	if req.URL.Host != via[0].URL.Host {
		return fmt.Errorf("redirect to another host %q refused", req.URL.Host)
	}
	return nil
}

// Venue returns canonical.VenueKalshi.
func (a *Adapter) Venue() canonical.Venue { return canonical.VenueKalshi }

// errBodyTooLarge is returned when a response body exceeds MaxBodyBytes.
var errBodyTooLarge = errors.New("response body exceeds the size cap")

// statusError is a non-2xx response after any retries.
type statusError struct{ status int }

func (e statusError) Error() string { return fmt.Sprintf("HTTP status %d", e.status) }

// get fetches endpoint (path and query relative to the base URL), retrying
// 429/5xx responses and transport errors with exponential backoff. It returns
// the raw response of the last attempt whose body was read, even on a non-2xx
// status, so callers can store it.
func (a *Adapter) get(ctx context.Context, endpoint string) (venues.RawResponse, error) {
	var (
		lastRaw venues.RawResponse
		lastErr error
	)
	wait := a.opts.Backoff
	for attempt := 0; attempt <= a.opts.Retries; attempt++ {
		if attempt > 0 {
			a.log.Warn("retrying request", "endpoint", endpoint, "attempt", attempt, "wait", wait, "error", lastErr)
			if err := sleep(ctx, wait); err != nil {
				return lastRaw, fmt.Errorf("GET %s: %w", endpoint, err)
			}
			wait *= 2
		}
		raw, retry, err := a.getOnce(ctx, endpoint)
		if err == nil {
			return raw, nil
		}
		lastRaw, lastErr = raw, err
		if !retry || ctx.Err() != nil {
			return raw, err
		}
	}
	return lastRaw, lastErr
}

// pace waits until the adapter may start another request.
func (a *Adapter) pace(ctx context.Context) error {
	if a.opts.MinInterval <= 0 {
		return nil
	}
	a.paceMu.Lock()
	now := time.Now()
	slot := a.nextSlot
	if slot.Before(now) {
		slot = now
	}
	a.nextSlot = slot.Add(a.opts.MinInterval)
	a.paceMu.Unlock()
	return sleep(ctx, slot.Sub(now))
}

// sleep waits for d or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// getOnce makes one request. retry reports whether the failure is worth retrying.
func (a *Adapter) getOnce(ctx context.Context, endpoint string) (raw venues.RawResponse, retry bool, err error) {
	if err := a.pace(ctx); err != nil {
		return venues.RawResponse{}, false, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.opts.BaseURL+endpoint, nil)
	if err != nil {
		return venues.RawResponse{}, false, fmt.Errorf("GET %s: building request: %w", endpoint, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := a.hc.Do(req)
	if err != nil {
		return venues.RawResponse{}, true, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, a.opts.MaxBodyBytes+1))
	if err != nil {
		return venues.RawResponse{}, true, fmt.Errorf("GET %s: reading body: %w", endpoint, err)
	}
	if int64(len(body)) > a.opts.MaxBodyBytes {
		return venues.RawResponse{}, false, fmt.Errorf("GET %s: %w (%d bytes)", endpoint, errBodyTooLarge, a.opts.MaxBodyBytes)
	}
	raw = venues.RawResponse{
		Venue:      canonical.VenueKalshi,
		Endpoint:   endpoint,
		HTTPStatus: resp.StatusCode,
		Body:       body,
		FetchedAt:  a.opts.Now(),
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		retry = resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		return raw, retry, fmt.Errorf("GET %s: %w", endpoint, statusError{resp.StatusCode})
	}
	return raw, false, nil
}
