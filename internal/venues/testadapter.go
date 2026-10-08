package venues

import (
	"context"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// PanicOnCallAdapter wraps an adapter and panics if any fetch method is called.
// NFR-04: when a round is pinned, all pages and the router must use stored data
// only and make 0 outbound venue calls. This adapter verifies that by panicking
// if any method is called; the test wraps both adapters and expects no panics.
type PanicOnCallAdapter struct {
	wrapped Adapter
}

// NewPanicOnCallAdapter returns an adapter that panics on any fetch call.
func NewPanicOnCallAdapter(a Adapter) Adapter {
	return &PanicOnCallAdapter{wrapped: a}
}

// Venue returns the wrapped adapter's venue.
func (p *PanicOnCallAdapter) Venue() canonical.Venue {
	return p.wrapped.Venue()
}

// FetchInstruments panics if called (NFR-04 violation).
func (p *PanicOnCallAdapter) FetchInstruments(_ context.Context, _ Universe) ([]canonical.Instrument, []RawResponse, error) {
	panic("venue call (FetchInstruments) while pinned round is active (NFR-04 violation)")
}

// FetchQuotes panics if called (NFR-04 violation).
func (p *PanicOnCallAdapter) FetchQuotes(_ context.Context, _ []canonical.Instrument) ([]canonical.Quote, []RawResponse, error) {
	panic("venue call (FetchQuotes) while pinned round is active (NFR-04 violation)")
}

// FetchSettled panics if called (NFR-04 violation).
func (p *PanicOnCallAdapter) FetchSettled(_ context.Context, _ Universe, _ time.Time) ([]canonical.Settled, []RawResponse, error) {
	panic("venue call (FetchSettled) while pinned round is active (NFR-04 violation)")
}
