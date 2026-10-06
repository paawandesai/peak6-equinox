package main

import (
	"log/slog"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/polymarket"
)

func init() { registerAdapter(canonical.VenuePolymarket, buildPolymarket) }

// buildPolymarket builds the Polymarket adapter from POLYMARKET_GAMMA_URL,
// POLYMARKET_CLOB_URL, HTTP_TIMEOUT_SECONDS and MAX_BODY_BYTES.
func buildPolymarket(cfg config.Config, logger *slog.Logger) (venues.Adapter, error) {
	return polymarket.New(polymarket.Options{
		GammaURL:     cfg.PolymarketGammaURL,
		ClobURL:      cfg.PolymarketClobURL,
		Timeout:      cfg.HTTPTimeout,
		MaxBodyBytes: cfg.MaxBodyBytes,
		Logger:       logger,
	})
}
