// Package venues defines the venue Adapter interface, the adapter registry and
// the per-venue universe configuration. Venue-specific code lives only in
// internal/venues/<venue>/.
package venues

import (
	"context"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Adapter reads one venue's public API and maps it into canonical types.
// Implementations must never call endpoints that need auth.
type Adapter interface {
	Venue() canonical.Venue // "kalshi" | "polymarket"
	// FetchInstruments returns open instruments for the configured universe plus the raw bodies it read.
	FetchInstruments(ctx context.Context, u Universe) ([]canonical.Instrument, []RawResponse, error)
	// FetchQuotes returns one Quote per instrument; YesAsks/YesBids are normalised to YES-side probabilities.
	FetchQuotes(ctx context.Context, ins []canonical.Instrument) ([]canonical.Quote, []RawResponse, error)
	// FetchSettled returns settled instruments with their outcome, for the back-test.
	FetchSettled(ctx context.Context, u Universe, since time.Time) ([]canonical.Settled, []RawResponse, error)
}

// RawResponse is one HTTP response body an adapter read, kept verbatim for
// raw_responses and for recorded fixtures.
type RawResponse struct {
	Venue canonical.Venue
	// Endpoint is the request path and query relative to the API base URL,
	// e.g. "/events?series_ticker=KXBTCD&status=open". A venue with several
	// APIs prefixes the API name, e.g. "clob/book?token_id=123".
	Endpoint   string
	HTTPStatus int
	Body       []byte
	FetchedAt  time.Time // UTC
}
