package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// fixtureRoot is testdata/fixtures from the package directory tests run in.
var fixtureRoot = filepath.Join("..", "..", "testdata", "fixtures")

func TestBacktestUsageErrors(t *testing.T) {
	cases := map[string][]string{
		"no source":     {},
		"both sources":  {"--fixtures", "--live"},
		"positional":    {"--fixtures", "extra"},
		"bad as-of":     {"--fixtures", "-as-of", "yesterday"},
		"bad lookback":  {"--fixtures", "-lookback", "crypto=0"},
		"bad category":  {"--fixtures", "-lookback", "weather=3"},
		"zero lookback": {"--fixtures", "-lookback", "0"},
		"unknown flag":  {"--fixtures", "-nope"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(context.Background(), append([]string{"backtest"}, args...), &stdout, &stderr); code != 2 {
				t.Errorf("exit %d, want 2; stderr %q", code, stderr.String())
			}
		})
	}
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"backtest", "-h"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "-fixtures") {
		t.Errorf("help: exit %d, stdout %q", code, stdout.String())
	}
}

func TestParseLookback(t *testing.T) {
	lb, err := parseLookback("7")
	if err != nil || len(lb) != len(canonical.Categories()) || lb[canonical.CategoryFed] != 7*24*time.Hour {
		t.Errorf("all categories: %v %v", lb, err)
	}
	lb, err = parseLookback(" crypto=2 , fed=45 ")
	if err != nil || len(lb) != 2 || lb[canonical.CategoryCrypto] != 48*time.Hour || lb[canonical.CategoryFed] != 45*24*time.Hour {
		t.Errorf("per category: %v %v", lb, err)
	}
	for _, bad := range []string{"", "crypto", "crypto=x", "crypto=1,,", "-3"} {
		if _, err := parseLookback(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestParseBacktestDefaults(t *testing.T) {
	opts, _, err := parseBacktestArgs([]string{"--fixtures"}, &bytes.Buffer{})
	if err != nil || !opts.asOf.Equal(fixturesRecordedAt) || opts.factsFile != "" || opts.lookback[canonical.CategoryCrypto] != 24*time.Hour {
		t.Errorf("fixture defaults: %+v, %v", opts, err)
	}
	opts, _, err = parseBacktestArgs([]string{"--live", "-as-of", "2026-09-30", "-lookback", "10"}, &bytes.Buffer{})
	if err != nil || opts.asOf != time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC) || opts.lookback[canonical.CategoryFed] != 10*24*time.Hour {
		t.Errorf("live with as-of: %+v, %v", opts, err)
	}
	opts, _, err = parseBacktestArgs([]string{"--live"}, &bytes.Buffer{})
	if err != nil || time.Since(opts.asOf) > time.Minute {
		t.Errorf("live default as-of: %+v, %v", opts, err)
	}
	if _, help, err := parseBacktestArgs([]string{"-h"}, &bytes.Buffer{}); err != nil || !help {
		t.Errorf("-h: help=%v err=%v", help, err)
	}
}

// TestBacktestCommandFixtures is the ticket's verification command
// (`equinox backtest --fixtures`) end to end: fixture server in-process, real
// adapters, real matcher, no network, no database, no environment.
func TestBacktestCommandFixtures(t *testing.T) {
	t.Setenv("UNIVERSE_DIR", filepath.Join("..", "..", "config", "universe"))
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"backtest", "--fixtures", "-fixture-dir", fixtureRoot}, &stdout, &stderr)
	out := stdout.String()
	if code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", code, out, stderr.String())
	}
	for _, want := range []string{
		"source: fixtures " + fixtureRoot,
		"Gate (NFR-07): 0 equivalent-tier pairs settled differently — PASS",
		"| equivalent_given_facts | 1 | 1 | 1 | 0 | 0 | same outcome |",
		"| complement | 1 | 1 | 1 | 0 | 0 | opposite outcomes |",
		"| implies | 22 | 22 | 22 | 0 | 0 |",
		"kalshi:KXFED-26SEP-T4.00 settled no",
		// The ethereum settled pages are not recorded (D-057): reported, not fatal.
		"Fetch warnings",
		"KXETHD",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "DISAGREED") {
		t.Errorf("unexpected disagreement:\n%s", out)
	}
}

func TestBacktestCommandAllowlistAndFacts(t *testing.T) {
	t.Setenv("UNIVERSE_DIR", filepath.Join("..", "..", "config", "universe"))
	dir := t.TempDir()
	allow := filepath.Join(dir, "allow.yaml")
	if err := os.WriteFile(allow, []byte("exceptions: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	// Today's facts (config/facts.yaml) name the October meeting, so the
	// September pairs fall back to related and the gate passes vacuously.
	code := run(context.Background(), []string{"backtest", "--fixtures", "-fixture-dir", fixtureRoot, "-allowlist", allow,
		"-facts", filepath.Join("..", "..", "config", "facts.yaml"), "-lookback", "crypto=1,fed=30"}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "| equivalent_given_facts | 0 | 0 |") {
		t.Errorf("exit %d\n%s\n%s", code, stdout.String(), stderr.String())
	}

	for name, args := range map[string][]string{
		"missing facts":    {"-facts", filepath.Join(dir, "none.yaml")},
		"bad allowlist":    {"-allowlist", filepath.Join(dir, "none.yaml")},
		"missing fixtures": {"-fixture-dir", filepath.Join(dir, "nofixtures")},
		"nothing recorded": {"-fixture-dir", dir, "-facts", filepath.Join("..", "..", "config", "facts.yaml")},
	} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(context.Background(), append([]string{"backtest", "--fixtures", "-fixture-dir", fixtureRoot}, args...), &stdout, &stderr); code != 1 {
				t.Errorf("exit %d, want 1; stderr %q", code, stderr.String())
			}
		})
	}
}
