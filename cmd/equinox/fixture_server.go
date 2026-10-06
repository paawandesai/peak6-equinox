package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/fixtureserver"
)

func init() { register("fixture-server", runFixtureServer) }

// defaultFixtureAddr is the port docs/environment.md reserves for the fixture server.
const defaultFixtureAddr = "127.0.0.1:8090"

// runFixtureServer serves the recorded fixtures for offline development
// (`make dev-fixtures`):
//
//	equinox fixture-server [-addr 127.0.0.1:8090] [-fixtures testdata/fixtures]
//
// It prints the venue URLs to put in .env and runs until interrupted.
func runFixtureServer(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fixture-server", flag.ContinueOnError)
	fs.SetOutput(stdout)
	addr := fs.String("addr", defaultFixtureAddr, "listen address")
	dir := fs.String("fixtures", defaultFixtureDir, "directory with <venue>/*.json fixtures")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("fixture-server takes no arguments: %w", errUsage)
	}
	// The log level is the only setting used; the server itself needs no config.
	cfg, err := config.FromEnv()
	if err != nil {
		cfg = config.Config{}
	}
	logger := newLogger(cfg)
	srv, base, err := startFixtureServer(ctx, *addr, *dir, logger)
	if err != nil {
		return err
	}
	k, g, c := fixtureserver.VenueURLs(base)
	_, _ = fmt.Fprintf(stdout, "fixture server listening on %s (serving %s)\nKALSHI_BASE_URL=%s\nPOLYMARKET_GAMMA_URL=%s\nPOLYMARKET_CLOB_URL=%s\n", base, *dir, k, g, c)
	select {
	case err := <-srv.done:
		return fmt.Errorf("fixture server: %w", err)
	case <-ctx.Done():
	}
	srv.stop()
	if err := <-srv.done; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("fixture server: %w", err)
	}
	return nil
}
