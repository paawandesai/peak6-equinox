package main

import (
	"log/slog"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/kalshi"
)

func init() { registerAdapter(canonical.VenueKalshi, newKalshiAdapter) }

// newKalshiAdapter builds the Kalshi adapter from KALSHI_BASE_URL,
// HTTP_TIMEOUT_SECONDS and MAX_BODY_BYTES.
func newKalshiAdapter(cfg config.Config, logger *slog.Logger) (venues.Adapter, error) {
	return kalshi.New(kalshi.Options{
		BaseURL:      cfg.KalshiBaseURL,
		Timeout:      cfg.HTTPTimeout,
		MaxBodyBytes: cfg.MaxBodyBytes,
		Logger:       logger,
	})
}
