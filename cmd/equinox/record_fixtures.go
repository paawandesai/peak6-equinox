package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/collector"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

func init() { register("record-fixtures", runRecordFixtures) }

// recordOptions controls one record-fixtures run.
type recordOptions struct {
	outDir      string
	maxBooks    int
	settledDays int
}

// runRecordFixtures is a DEV-ONLY command:
//
//	equinox record-fixtures [-out testdata/fixtures] [-books 10] [-settled-days 30]
//
// It calls the LIVE public venue APIs through every registered adapter and
// writes each raw 2xx response body to <out>/<venue>/<venues.FixtureFileName>.
// It is never run by tests or CI: tests serve the committed files through
// httptest instead (CLAUDE.md § Testing, docs/environment.md).
func runRecordFixtures(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("record-fixtures", flag.ContinueOnError)
	fs.SetOutput(stdout)
	opts := recordOptions{}
	fs.StringVar(&opts.outDir, "out", filepath.Join("testdata", "fixtures"), "output directory")
	fs.IntVar(&opts.maxBooks, "books", 10, "order books to record per venue")
	fs.IntVar(&opts.settledDays, "settled-days", 30, "record markets settled within this many days")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	if fs.NArg() > 0 || opts.maxBooks < 0 || opts.settledDays < 1 {
		return fmt.Errorf("record-fixtures: unexpected arguments or out-of-range flags: %w", errUsage)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel, ReplaceAttr: utcTime}))
	reg, err := buildRegistry(cfg, logger)
	if err != nil {
		return err
	}
	if reg.Len() == 0 {
		return errors.New("no venue adapters are registered yet (tickets M1-04 and M1-05 add them)")
	}
	universes, err := venues.LoadUniverse(cfg.UniverseDir)
	if err != nil {
		return err
	}
	var errs []error
	for _, a := range reg.Adapters() {
		n, err := recordVenue(ctx, a, universes.For(a.Venue()), opts, time.Now().UTC(), logger)
		_, _ = fmt.Fprintf(stdout, "%s: wrote %d fixture files to %s\n", a.Venue(), n, filepath.Join(opts.outDir, string(a.Venue())))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", a.Venue(), err))
		}
	}
	return errors.Join(errs...)
}

// recordVenue fetches instruments, up to opts.maxBooks books and recently
// settled markets from one adapter, writing every raw response it gets, even
// when a later step fails.
func recordVenue(ctx context.Context, a venues.Adapter, u venues.Universe, opts recordOptions, now time.Time, logger *slog.Logger) (int, error) {
	dir := filepath.Join(opts.outDir, string(a.Venue()))
	written := 0
	var errs []error
	save := func(step string, raws []venues.RawResponse, err error) {
		n, werr := writeFixtures(dir, raws, logger)
		written += n
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", step, err))
		}
		if werr != nil {
			errs = append(errs, werr)
		}
	}
	ins, raws, err := a.FetchInstruments(ctx, u)
	save("instruments", raws, err)
	// Record the books a collector round would quote first (M1-06), so the
	// fixture round has books for the instruments it fetches.
	ins = collector.SelectForQuotes(ins, opts.maxBooks, now)
	if len(ins) > 0 {
		_, raws, err = a.FetchQuotes(ctx, ins)
		save("quotes", raws, err)
	}
	_, raws, err = a.FetchSettled(ctx, u, now.AddDate(0, 0, -opts.settledDays))
	save("settled", raws, err)
	return written, errors.Join(errs...)
}

// writeFixtures writes each 2xx body verbatim; other statuses are logged and skipped.
func writeFixtures(dir string, raws []venues.RawResponse, logger *slog.Logger) (int, error) {
	if len(raws) == 0 {
		return 0, nil
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return 0, fmt.Errorf("creating %s: %w", dir, err)
	}
	n := 0
	for _, r := range raws {
		if r.HTTPStatus < 200 || r.HTTPStatus > 299 {
			logger.Warn("skipping non-2xx response", "venue", r.Venue, "endpoint", r.Endpoint, "status", r.HTTPStatus)
			continue
		}
		path := filepath.Join(dir, venues.FixtureFileName(r.Endpoint))
		if err := os.WriteFile(path, r.Body, 0o600); err != nil {
			return n, fmt.Errorf("writing %s: %w", path, err)
		}
		n++
	}
	return n, nil
}
