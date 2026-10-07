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
	"strconv"
	"strings"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/eval"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/fixtureserver"
)

func init() { register("backtest", runBacktest) }

// fixturesRecordedAt is when testdata/fixtures/** was recorded (D-056,
// D-057); the fixture back-test looks back from it so the recorded settled
// windows are what gets fetched, whatever today's date is.
var fixturesRecordedAt = time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)

// backtestFactsFile is the facts snapshot shipped with the fixtures: the
// values in force when the recorded markets settled (not today's
// config/facts.yaml, whose Fed level postdates the recorded meeting).
const backtestFactsFile = "backtest/facts.yaml"

// backtestOptions is one parsed invocation.
type backtestOptions struct {
	fixtures   bool
	fixtureDir string
	live       bool
	asOf       time.Time
	lookback   eval.Lookback
	factsFile  string
	allowlist  string
}

// runBacktest is the settlement back-test (FR-19, NFR-07):
//
//	equinox backtest --fixtures [-fixture-dir testdata/fixtures] | --live
//	                 [-as-of YYYY-MM-DD] [-lookback crypto=1,fed=90,politics=90]
//	                 [-facts FILE] [-allowlist FILE]
//
// It fetches settled markets through every registered adapter's
// FetchSettled, runs the matcher on the cross-venue pairs and reports every
// `equivalent` / `equivalent_given_facts` pair whose settlements disagree
// (and every complement or implies verdict the settlements contradict).
// Exactly one source is required: --fixtures serves the recorded files
// in-process and never touches the network; --live calls the public venue
// APIs from the environment's URLs, which tests never do. The exit code is
// non-zero when the NFR-07 gate fails, so CI can run it as a check.
func runBacktest(ctx context.Context, args []string, stdout io.Writer) error {
	opts, help, err := parseBacktestArgs(args, stdout)
	if err != nil || help {
		return err
	}
	cfg, err := config.FromEnv()
	if err != nil {
		if opts.live {
			return err
		}
		// Offline runs need no database or admin credentials.
		cfg = fixtureConfig()
	}
	logger := newLogger(cfg)
	if opts.fixtures {
		srv, base, err := startFixtureServer(ctx, "127.0.0.1:0", opts.fixtureDir, logger)
		if err != nil {
			return err
		}
		defer srv.stop()
		cfg.KalshiBaseURL, cfg.PolymarketGammaURL, cfg.PolymarketClobURL = fixtureserver.VenueURLs(base)
		if opts.factsFile == "" {
			opts.factsFile = filepath.Join(opts.fixtureDir, backtestFactsFile)
		}
		_, _ = fmt.Fprintf(stdout, "source: fixtures %s via %s (as of %s)\n", opts.fixtureDir, base, opts.asOf.Format(time.RFC3339))
	} else {
		if opts.factsFile == "" {
			opts.factsFile = cfg.FactsFile
		}
		_, _ = fmt.Fprintf(stdout, "source: LIVE public venue APIs (as of %s)\n", opts.asOf.Format(time.RFC3339))
	}
	report, warnings, err := backtest(ctx, cfg, opts, logger)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprint(stdout, report.String())
	if len(warnings) > 0 {
		_, _ = fmt.Fprintf(stdout, "\nFetch warnings (%d; the sample above is what was read before each failure):\n", len(warnings))
		for _, w := range warnings {
			_, _ = fmt.Fprintf(stdout, "- %s\n", strings.ReplaceAll(w.Error(), "\n", "\n  "))
		}
	}
	if !report.Passed() {
		return fmt.Errorf("NFR-07 gate failed: %d equivalent-tier pair(s) settled differently; investigate each and allowlist only with a logged decision", len(report.Mismatches(true)))
	}
	return nil
}

// parseBacktestArgs parses and validates the flags; help is true when -h
// printed the usage and nothing should run.
func parseBacktestArgs(args []string, stdout io.Writer) (opts backtestOptions, help bool, err error) {
	fs := flag.NewFlagSet("backtest", flag.ContinueOnError)
	fs.SetOutput(stdout)
	var asOf, lookback string
	fs.BoolVar(&opts.fixtures, "fixtures", false, "read settled markets from the recorded fixtures (offline)")
	fs.StringVar(&opts.fixtureDir, "fixture-dir", defaultFixtureDir, "directory with <venue>/*.json fixtures and backtest/facts.yaml")
	fs.BoolVar(&opts.live, "live", false, "read settled markets from the LIVE public venue APIs (never in tests)")
	fs.StringVar(&asOf, "as-of", "", "date (UTC) the lookback counts back from; default: now, or the fixture recording date with --fixtures")
	fs.StringVar(&lookback, "lookback", "crypto=1,fed=90,politics=90", "days of settled markets to fetch, per category or one number for all")
	fs.StringVar(&opts.factsFile, "facts", "", "facts file for the matcher; default: FACTS_FILE, or <fixture-dir>/backtest/facts.yaml with --fixtures")
	fs.StringVar(&opts.allowlist, "allowlist", "", "YAML file of investigated exceptions (a_id, b_id, reason, decision)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return opts, true, nil
		}
		return opts, false, fmt.Errorf("%w: %w", err, errUsage)
	}
	if fs.NArg() > 0 {
		return opts, false, fmt.Errorf("backtest takes no positional arguments: %w", errUsage)
	}
	if opts.fixtures == opts.live {
		return opts, false, fmt.Errorf("pass exactly one of --fixtures or --live: %w", errUsage)
	}
	switch {
	case asOf != "":
		t, err := time.ParseInLocation(facts.DateLayout, asOf, time.UTC)
		if err != nil {
			return opts, false, fmt.Errorf("-as-of must be YYYY-MM-DD: %w", errUsage)
		}
		opts.asOf = t
	case opts.fixtures:
		opts.asOf = fixturesRecordedAt
	default:
		opts.asOf = time.Now().UTC()
	}
	lb, err := parseLookback(lookback)
	if err != nil {
		return opts, false, fmt.Errorf("%w: %w", err, errUsage)
	}
	opts.lookback = lb
	return opts, false, nil
}

// parseLookback reads "30" or "crypto=1,fed=90,politics=90" (days).
func parseLookback(s string) (eval.Lookback, error) {
	lb := eval.Lookback{}
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		if n < 1 {
			return nil, errors.New("-lookback days must be at least 1")
		}
		for _, c := range canonical.Categories() {
			lb[c] = time.Duration(n) * 24 * time.Hour
		}
		return lb, nil
	}
	for _, part := range strings.Split(s, ",") {
		name, days, ok := strings.Cut(strings.TrimSpace(part), "=")
		cat := canonical.Category(strings.TrimSpace(name))
		n, err := strconv.Atoi(strings.TrimSpace(days))
		if !ok || !cat.Valid() || err != nil || n < 1 {
			return nil, fmt.Errorf("-lookback: %q is not <category>=<days> with a known category and days >= 1", part)
		}
		lb[cat] = time.Duration(n) * 24 * time.Hour
	}
	return lb, nil
}

// fixtureConfig is the configuration an offline back-test runs with when
// the full environment (database, admin credentials) is not set: adapter
// defaults, the repo-relative paths unless UNIVERSE_DIR / FACTS_FILE
// override them, and only errors logged unless LOG_LEVEL says otherwise.
func fixtureConfig() config.Config {
	level := slog.LevelError
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		_ = level.UnmarshalText([]byte(v)) // an unknown level keeps the quiet default
	}
	return config.Config{
		HTTPTimeout:  config.DefaultHTTPTimeoutSeconds * time.Second,
		MaxBodyBytes: config.DefaultMaxBodyBytes,
		UniverseDir:  envOr("UNIVERSE_DIR", config.DefaultUniverseDir),
		FactsFile:    envOr("FACTS_FILE", config.DefaultFactsFile),
		LogLevel:     level,
	}
}

// envOr returns the environment variable, or def when unset or blank.
func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// backtest fetches, matches and checks; warnings are per-(venue, category)
// fetch failures that did not stop the run.
func backtest(ctx context.Context, cfg config.Config, opts backtestOptions, logger *slog.Logger) (eval.BacktestReport, []error, error) {
	reg, err := buildRegistry(cfg, logger)
	if err != nil {
		return eval.BacktestReport{}, nil, err
	}
	if reg.Len() == 0 {
		return eval.BacktestReport{}, nil, errors.New("no venue adapters are registered")
	}
	universes, err := venues.LoadUniverse(cfg.UniverseDir)
	if err != nil {
		return eval.BacktestReport{}, nil, err
	}
	factSet, err := facts.Load(opts.factsFile)
	if err != nil {
		return eval.BacktestReport{}, nil, fmt.Errorf("backtest needs a facts file: %w", err)
	}
	var allow eval.Allowlist
	if opts.allowlist != "" {
		if allow, err = eval.LoadAllowlist(opts.allowlist); err != nil {
			return eval.BacktestReport{}, nil, err
		}
	}
	settled, warnings := eval.FetchSettled(ctx, reg.Adapters(), universes, opts.lookback, opts.asOf, logger)
	if len(settled) == 0 {
		return eval.BacktestReport{}, warnings, fmt.Errorf("%w: %w", eval.ErrNoSettled, errors.Join(warnings...))
	}
	report := eval.Backtest(eval.NormalizeSettled(settled), factSet, allow)
	logger.Info("backtest finished", "settled", len(settled), "pairs", report.Pairs,
		"equivalent", report.ByType[canonical.RelEquivalent], "equivalent_given_facts", report.ByType[canonical.RelEquivalentGivenFacts],
		"complement", report.ByType[canonical.RelComplement], "implies", report.ByType[canonical.RelImplies],
		"mismatches", len(report.Mismatches(true)), "exceptions", len(report.Exceptions()), "fetch_warnings", len(warnings), "passed", report.Passed())
	return report, warnings, nil
}
