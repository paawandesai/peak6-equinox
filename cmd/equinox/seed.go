package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/collector"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/fixtureserver"
)

func init() { register("seed", runSeed) }

// defaultFixtureDir is where `equinox record-fixtures` writes and the fixture
// server reads, relative to the repo root.
var defaultFixtureDir = filepath.Join("testdata", "fixtures")

// runSeed loads one offline fixture round (`make seed`):
//
//	equinox seed [-fixtures testdata/fixtures] [-max-quotes N]
//
// It starts the fixture venue server in-process on a random loopback port,
// points the venue URLs at it and runs the collector exactly as `ingest`
// does, so pages work with no network.
func runSeed(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(stdout)
	dir := fs.String("fixtures", defaultFixtureDir, "directory with <venue>/*.json fixtures")
	maxQuotes := fs.Int("max-quotes", collector.DefaultMaxQuotesPerVenue, "order books to fetch per venue (-1 for all)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("seed takes no arguments: %w", errUsage)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := newLogger(cfg)
	srv, base, err := startFixtureServer(ctx, "127.0.0.1:0", *dir, logger)
	if err != nil {
		return err
	}
	defer srv.stop()
	cfg.KalshiBaseURL, cfg.PolymarketGammaURL, cfg.PolymarketClobURL = fixtureserver.VenueURLs(base)
	_, _ = fmt.Fprintf(stdout, "seeding from %s via %s\n", *dir, base)
	return runRound(ctx, cfg, logger, stdout, *maxQuotes)
}

// fixtureHTTP is a running fixture server.
type fixtureHTTP struct {
	srv  *http.Server
	fx   *fixtureserver.Server
	addr net.Addr
	done chan error
}

// startFixtureServer serves dir on addr ("host:0" picks a free port) and
// returns the server with its base URL.
func startFixtureServer(ctx context.Context, addr, dir string, logger *slog.Logger) (*fixtureHTTP, string, error) {
	fx, err := fixtureserver.New(dir, logger)
	if err != nil {
		return nil, "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", fmt.Errorf("listening on %s: %w", addr, err)
	}
	srv := &http.Server{
		Handler:           fx.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}
	f := &fixtureHTTP{srv: srv, fx: fx, addr: ln.Addr(), done: make(chan error, 1)}
	go func() { f.done <- srv.Serve(ln) }()
	return f, "http://" + ln.Addr().String(), nil
}

// stop shuts the server down; Serve's result is left on done for the caller.
func (f *fixtureHTTP) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = f.srv.Shutdown(ctx)
}
