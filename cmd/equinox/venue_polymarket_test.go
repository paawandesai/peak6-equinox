package main

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
)

func TestPolymarketAdapterRegistered(t *testing.T) {
	b, ok := adapterBuilders[canonical.VenuePolymarket]
	if !ok {
		t.Fatal("polymarket adapter builder not registered")
	}
	cfg := config.Config{
		PolymarketGammaURL: config.DefaultPolymarketGammaURL,
		PolymarketClobURL:  config.DefaultPolymarketClobURL,
		HTTPTimeout:        10 * time.Second,
		MaxBodyBytes:       config.DefaultMaxBodyBytes,
	}
	a, err := b(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil || a.Venue() != canonical.VenuePolymarket {
		t.Fatalf("buildPolymarket = %v, %v", a, err)
	}
	cfg.MaxBodyBytes = 0
	if _, err := b(cfg, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Error("buildPolymarket accepted a zero body cap")
	}
}
