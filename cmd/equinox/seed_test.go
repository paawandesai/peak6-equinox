package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// dbEnv points the CLI at this package's test schema.
func dbEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "ci")
	t.Setenv("DATABASE_URL", tdb.DSN(t))
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "test-only-password-0123456789")
	t.Setenv("LOG_LEVEL", "error")
	// Tests run inside cmd/equinox; the repo-relative defaults need the root.
	t.Setenv("UNIVERSE_DIR", filepath.Join("..", "..", "config", "universe"))
}

var repoFixtures = filepath.Join("..", "..", "testdata", "fixtures")

func TestSeedLoadsFixtureRound(t *testing.T) {
	dbEnv(t)
	tdb.Reset(t)
	var out, errOut bytes.Buffer
	// -max-quotes keeps the Kalshi adapter's 100 ms pacing to a few seconds.
	code := run(context.Background(), []string{"seed", "-max-quotes", "20", "-fixtures", repoFixtures}, &out, &errOut)
	if code != 0 {
		t.Fatalf("seed exit %d\nstdout: %s\nstderr: %s", code, out.String(), errOut.String())
	}
	stdout := out.String()
	for _, want := range []string{"seeding from " + repoFixtures + " via http://127.0.0.1:", "round 1: ok in", "kalshi", "polymarket", "instruments  quotes  unquoted  raw  duration  error\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	st := store.New(tdb.Pool(t))
	ctx := context.Background()
	round, err := st.LatestRound(ctx)
	if err != nil || round.Status != store.RoundOK {
		t.Fatalf("latest round = %+v, %v", round, err)
	}
	for _, v := range canonical.Venues() {
		vs := round.VenueStatus[v]
		if !vs.OK || vs.Instruments < 100 || vs.Quotes != 20 {
			t.Errorf("%s status = %+v", v, vs)
		}
	}
	if quotes, _ := st.ListQuotes(ctx, round.ID); len(quotes) != 40 {
		t.Errorf("quotes stored = %d, want 40", len(quotes))
	}
	if logs, _ := st.ListLog(ctx, store.LogFilter{Kind: store.LogIngest}); len(logs) != 1 {
		t.Errorf("ingest log entries = %d", len(logs))
	}
	// The normaliser (M1-07) ran on every stored instrument.
	if in, err := st.GetInstrument(ctx, "kalshi:KXBTCD-26OCT0618-T76599.99"); err != nil || in.Terms.Subject == canonical.Unknown || in.Terms.Threshold == nil {
		t.Errorf("normalised terms = %+v (%v)", in.Terms, err)
	}
	if raws, _ := st.ListRawResponses(ctx, round.ID); len(raws) < 40 {
		t.Errorf("raw responses = %d", len(raws))
	}
}

func TestSeedBadFlagsAndMissingFixtures(t *testing.T) {
	dbEnv(t)
	for _, args := range [][]string{{"seed", "stray"}, {"seed", "-nope"}} {
		var errOut bytes.Buffer
		if code := run(context.Background(), args, io.Discard, &errOut); code != 2 {
			t.Errorf("%v: exit %d, stderr %s", args, code, errOut.String())
		}
	}
	var errOut bytes.Buffer
	if code := run(context.Background(), []string{"seed", "-fixtures", filepath.Join(t.TempDir(), "missing")}, io.Discard, &errOut); code != 1 || !strings.Contains(errOut.String(), "fixture root") {
		t.Errorf("missing fixtures: exit %d, stderr %s", code, errOut.String())
	}
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	errOut.Reset()
	if code := run(context.Background(), []string{"seed", "-fixtures", repoFixtures}, io.Discard, &errOut); code != 1 || !strings.Contains(errOut.String(), "database") {
		t.Errorf("unreachable database: exit %d, stderr %s", code, errOut.String())
	}
}

func TestIngestRunsRoundWithRegisteredAdapters(t *testing.T) {
	dbEnv(t)
	tdb.Reset(t)
	k := &fakeAdapter{venue: canonical.VenueKalshi, instrument: []canonical.Instrument{seedInstrument("KXBTCD-A")}}
	p := &fakeAdapter{venue: canonical.VenuePolymarket, instrument: []canonical.Instrument{seedInstrument("1234")}}
	p.instrument[0].ID, p.instrument[0].Venue, p.instrument[0].FeeSchedule.ID = "polymarket:1234", canonical.VenuePolymarket, canonical.FeeSchedulePolymarketV1
	withAdapters(t, k, p)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"ingest"}, &out, &errOut); code != 0 {
		t.Fatalf("ingest exit %d\nstdout: %s\nstderr: %s", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "round 1: ok in") || k.gotQuotes != 1 || p.gotQuotes != 1 {
		t.Errorf("stdout %q, kalshi quotes %d, polymarket quotes %d", out.String(), k.gotQuotes, p.gotQuotes)
	}
	st := store.New(tdb.Pool(t))
	if ins, _ := st.ListInstruments(context.Background(), store.InstrumentFilter{}); len(ins) != 2 {
		t.Errorf("instruments stored = %d", len(ins))
	}

	// Every venue failing: the round is recorded and the command exits 1.
	withAdapters(t, &downAdapter{k}, &downAdapter{p})
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"ingest"}, &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "round 2 failed") || !strings.Contains(out.String(), "failed") {
		t.Errorf("failed round: exit %d\nstdout: %s\nstderr: %s", code, out.String(), errOut.String())
	}
	if code := run(context.Background(), []string{"ingest", "extra"}, io.Discard, io.Discard); code != 2 {
		t.Errorf("stray argument: exit %d", code)
	}
}

func TestIngestNeedsAdapters(t *testing.T) {
	dbEnv(t)
	withAdapters(t)
	var errOut bytes.Buffer
	if code := run(context.Background(), []string{"ingest"}, io.Discard, &errOut); code != 1 || !strings.Contains(errOut.String(), "no venue adapters") {
		t.Errorf("exit %d, stderr %s", code, errOut.String())
	}
}

// syncBuffer is a bytes.Buffer safe to read while another goroutine writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// downAdapter is a fakeAdapter whose venue is unreachable.
type downAdapter struct{ *fakeAdapter }

func (d *downAdapter) FetchInstruments(context.Context, venues.Universe) ([]canonical.Instrument, []venues.RawResponse, error) {
	return nil, nil, errors.New("HTTP status 503")
}

func seedInstrument(key string) canonical.Instrument {
	return canonical.Instrument{
		ID: "kalshi:" + key, Venue: canonical.VenueKalshi, VenueMarketID: key, VenueEventID: "EVT", Category: canonical.CategoryCrypto,
		Title: "Will " + key + "?", Terms: canonical.UnknownTerms(), CloseTime: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC),
		Status: canonical.StatusOpen, FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
	}
}

func TestFixtureServerCommandServesFixtures(t *testing.T) {
	dbEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	out := &syncBuffer{}
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"fixture-server", "-addr", "127.0.0.1:0", "-fixtures", repoFixtures}, out, io.Discard)
	}()
	urlRe := regexp.MustCompile(`listening on (http://127\.0\.0\.1:\d+)`)
	var base string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if m := urlRe.FindStringSubmatch(out.String()); m != nil {
			base = m[1]
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if base == "" {
		cancel()
		t.Fatalf("fixture server did not announce its URL: %q", out.String())
	}
	if !strings.Contains(out.String(), "KALSHI_BASE_URL="+base+"/kalshi") || !strings.Contains(out.String(), "POLYMARKET_CLOB_URL="+base+"/polymarket/clob") {
		t.Errorf("stdout %q lacks the venue URLs", out.String())
	}
	resp, err := http.Get(base + "/kalshi/markets/KXFED-26OCT-T3.25/orderbook") //nolint:gosec,noctx // loopback test server
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET fixture: status %d", resp.StatusCode)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("fixture-server exit %d", code)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("fixture-server did not stop")
	}
	if code := run(context.Background(), []string{"fixture-server", "-addr", "127.0.0.1:0", "-fixtures", filepath.Join(t.TempDir(), "nope")}, io.Discard, io.Discard); code != 1 {
		t.Errorf("missing fixtures: exit %d", code)
	}
	if code := run(context.Background(), []string{"fixture-server", "stray"}, io.Discard, io.Discard); code != 2 {
		t.Errorf("stray argument: exit %d", code)
	}
}

func TestServeBuildsDepsWithLazyPool(t *testing.T) {
	dbEnv(t)
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none")
	cfg, err := configFromTestEnv()
	if err != nil {
		t.Fatal(err)
	}
	deps, closeDeps, err := buildDeps(context.Background(), cfg, newLogger(cfg))
	if err != nil {
		t.Fatalf("buildDeps: %v (the pool must not connect eagerly)", err)
	}
	defer closeDeps()
	if deps.Store == nil || deps.Ingester == nil {
		t.Errorf("deps = %+v", deps)
	}
	t.Setenv("DATABASE_URL", "not a dsn ://")
	cfg, _ = configFromTestEnv()
	if _, _, err := buildDeps(context.Background(), cfg, newLogger(cfg)); err == nil {
		t.Error("malformed DSN accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none")
	t.Setenv("UNIVERSE_DIR", t.TempDir())
	cfg, _ = configFromTestEnv()
	if _, _, err := buildDeps(context.Background(), cfg, newLogger(cfg)); err == nil {
		t.Error("missing universe accepted")
	}
}

func configFromTestEnv() (config.Config, error) { return config.FromEnv() }
