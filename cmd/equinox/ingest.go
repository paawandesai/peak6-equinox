package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/collector"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

func init() { register("ingest", runIngest) }

// runIngest runs one fetch round against the configured venue URLs and prints
// a summary:
//
//	equinox ingest [-max-quotes N]
//
// It exits 1 when the round failed (no venue delivered data); a partial round
// exits 0 with the failure shown in the summary.
func runIngest(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(stdout)
	maxQuotes := fs.Int("max-quotes", collector.DefaultMaxQuotesPerVenue, "order books to fetch per venue per round (-1 for all)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("ingest takes no arguments: %w", errUsage)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := newLogger(cfg)
	return runRound(ctx, cfg, logger, stdout, *maxQuotes)
}

// newLogger is the JSON logger every data command uses.
func newLogger(cfg config.Config) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel, ReplaceAttr: utcTime}))
}

// buildCollector wires the registered adapters, the universe files and the
// store into a collector. M1-07's normalize.Extract is wired here once it
// lands (collector.Options.Normalize); until then terms stay unknown.
func buildCollector(cfg config.Config, logger *slog.Logger, st *store.Store, maxQuotes int) (*collector.Collector, error) {
	reg, err := buildRegistry(cfg, logger)
	if err != nil {
		return nil, err
	}
	if reg.Len() == 0 {
		return nil, errors.New("no venue adapters are registered")
	}
	universes, err := venues.LoadUniverse(cfg.UniverseDir)
	if err != nil {
		return nil, err
	}
	if maxQuotes == 0 {
		maxQuotes = collector.DefaultMaxQuotesPerVenue
	}
	return collector.New(st, reg, universes, collector.Options{MaxQuotesPerVenue: maxQuotes, Logger: logger})
}

// runRound opens the database, runs one round and prints its summary.
func runRound(ctx context.Context, cfg config.Config, logger *slog.Logger, stdout io.Writer, maxQuotes int) error {
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	col, err := buildCollector(cfg, logger, st, maxQuotes)
	if err != nil {
		return err
	}
	res, err := col.Run(ctx)
	if res.RoundID != 0 {
		printSummary(stdout, res)
	}
	if err != nil {
		return err
	}
	if res.Status == store.RoundFailed {
		return fmt.Errorf("round %d failed: no venue delivered data", res.RoundID)
	}
	return nil
}

// printSummary writes the per-venue counts, duration and status of a round.
func printSummary(w io.Writer, res collector.Result) {
	_, _ = fmt.Fprintf(w, "round %d: %s in %s (started %s)\n", res.RoundID, res.Status,
		res.Duration().Round(100*time.Millisecond), res.StartedAt.Format(time.RFC3339))
	var table bytes.Buffer
	tw := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "  venue\tstatus\tinstruments\tquotes\tunquoted\traw\tduration\terror")
	for _, v := range res.Venues {
		status := "ok"
		if !v.OK {
			status = "failed"
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\t%d\t%d\t%d\t%d\t%s\t%s\n", v.Venue, status, v.Instruments, v.Quotes, v.QuotesNotRequested,
			v.RawResponses, (time.Duration(v.DurationMS) * time.Millisecond).Round(100*time.Millisecond), v.Error)
	}
	_ = tw.Flush()
	for line := range strings.SplitSeq(strings.TrimRight(table.String(), "\n"), "\n") {
		_, _ = fmt.Fprintln(w, strings.TrimRight(line, " "))
	}
}
