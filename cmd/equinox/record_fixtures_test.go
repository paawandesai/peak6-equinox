package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// fakeAdapter returns canned raw responses; no network is involved.
type fakeAdapter struct {
	venue      canonical.Venue
	instrument []canonical.Instrument
	settledErr error
	gotUniv    venues.Universe
	gotQuotes  int
	gotSince   time.Time
}

func (f *fakeAdapter) Venue() canonical.Venue { return f.venue }

func (f *fakeAdapter) raw(endpoint string, status int) venues.RawResponse {
	return venues.RawResponse{Venue: f.venue, Endpoint: endpoint, HTTPStatus: status, Body: []byte(`{"endpoint":"` + endpoint + `"}`)}
}

func (f *fakeAdapter) FetchInstruments(_ context.Context, u venues.Universe) ([]canonical.Instrument, []venues.RawResponse, error) {
	f.gotUniv = u
	return f.instrument, []venues.RawResponse{f.raw("/events?series_ticker=KXBTCD", 200), f.raw("/events?series_ticker=NOPE", 404)}, nil
}

func (f *fakeAdapter) FetchQuotes(_ context.Context, ins []canonical.Instrument) ([]canonical.Quote, []venues.RawResponse, error) {
	f.gotQuotes = len(ins)
	raws := make([]venues.RawResponse, 0, len(ins))
	for _, in := range ins {
		raws = append(raws, f.raw("/markets/"+in.VenueMarketID+"/orderbook", 200))
	}
	return nil, raws, nil
}

func (f *fakeAdapter) FetchSettled(_ context.Context, _ venues.Universe, since time.Time) ([]canonical.Settled, []venues.RawResponse, error) {
	f.gotSince = since
	return nil, []venues.RawResponse{f.raw("/markets?status=settled", 200)}, f.settledErr
}

// withAdapters swaps the adapter builders for the test.
func withAdapters(t *testing.T, adapters ...venues.Adapter) {
	t.Helper()
	saved := adapterBuilders
	t.Cleanup(func() { adapterBuilders = saved })
	adapterBuilders = map[canonical.Venue]adapterBuilder{}
	for _, a := range adapters {
		registerAdapter(a.Venue(), func(config.Config, *slog.Logger) (venues.Adapter, error) { return a, nil })
	}
}

func TestRecordFixturesWritesRawResponses(t *testing.T) {
	setAppEnv(t, "postgres://unused@127.0.0.1:1/none")
	t.Setenv("UNIVERSE_DIR", filepath.Join("..", "..", "config", "universe"))
	ins := make([]canonical.Instrument, 4)
	for i := range ins {
		ins[i].VenueMarketID = "KXBTCD-" + string(rune('A'+i))
	}
	k := &fakeAdapter{venue: canonical.VenueKalshi, instrument: ins}
	p := &fakeAdapter{venue: canonical.VenuePolymarket, settledErr: errors.New("gamma 500")}
	withAdapters(t, p, k)

	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"record-fixtures", "-out", out, "-books", "2", "-settled-days", "7"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "polymarket: settled: gamma 500") {
		t.Fatalf("exit %d, stderr %q (want polymarket settled error reported)", code, stderr.String())
	}
	if k.gotQuotes != 2 {
		t.Errorf("books fetched = %d, want 2 (-books)", k.gotQuotes)
	}
	if got := k.gotUniv.Categories[canonical.CategoryCrypto].Series; len(got) != 2 || got[0] != "KXBTCD" {
		t.Errorf("kalshi universe = %+v", k.gotUniv)
	}
	if age := time.Since(k.gotSince); age < 7*24*time.Hour-time.Minute || age > 7*24*time.Hour+time.Minute {
		t.Errorf("settled since = %v ago, want 7 days", age)
	}
	// kalshi: 1 event (404 skipped) + 2 books + 1 settled; polymarket: 1 event + settled body still written.
	for venue, want := range map[string]int{"kalshi": 4, "polymarket": 2} {
		files, err := os.ReadDir(filepath.Join(out, venue))
		if err != nil || len(files) != want {
			t.Errorf("%s: %d files (%v), want %d", venue, len(files), err, want)
		}
	}
	name := venues.FixtureFileName("/markets/KXBTCD-A/orderbook")
	body, err := os.ReadFile(filepath.Join(out, "kalshi", name)) //nolint:gosec // test temp dir
	if err != nil || string(body) != `{"endpoint":"/markets/KXBTCD-A/orderbook"}` {
		t.Errorf("fixture body = %q %v", body, err)
	}
	if _, err := os.Stat(filepath.Join(out, "kalshi", venues.FixtureFileName("/events?series_ticker=NOPE"))); !os.IsNotExist(err) {
		t.Error("non-2xx response was written")
	}
	for _, want := range []string{"kalshi: wrote 4 fixture files", "polymarket: wrote 2 fixture files"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout %q lacks %q", stdout.String(), want)
		}
	}
}

func TestRecordFixturesErrors(t *testing.T) {
	setAppEnv(t, "postgres://unused@127.0.0.1:1/none")
	t.Setenv("UNIVERSE_DIR", filepath.Join("..", "..", "config", "universe"))
	withAdapters(t)
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"record-fixtures", "-out", t.TempDir()}, io.Discard, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "no venue adapters are registered") {
		t.Errorf("no adapters: exit %d, stderr %q", code, stderr.String())
	}
	for _, args := range [][]string{{"-books", "-1"}, {"-settled-days", "0"}, {"stray"}, {"-nope"}} {
		stderr.Reset()
		if code := run(context.Background(), append([]string{"record-fixtures"}, args...), io.Discard, &stderr); code != 2 {
			t.Errorf("%v: exit %d, want 2 (stderr %q)", args, code, stderr.String())
		}
	}
	if code := run(context.Background(), []string{"record-fixtures", "-h"}, io.Discard, io.Discard); code != 0 {
		t.Errorf("-h exit %d", code)
	}

	withAdapters(t, &fakeAdapter{venue: canonical.VenueKalshi})
	t.Setenv("UNIVERSE_DIR", t.TempDir())
	stderr.Reset()
	if code := run(context.Background(), []string{"record-fixtures"}, io.Discard, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "loading universe") {
		t.Errorf("empty universe dir: exit %d, stderr %q", code, stderr.String())
	}

	saved := adapterBuilders
	t.Cleanup(func() { adapterBuilders = saved })
	adapterBuilders = map[canonical.Venue]adapterBuilder{
		canonical.VenueKalshi: func(config.Config, *slog.Logger) (venues.Adapter, error) { return nil, errors.New("bad url") },
	}
	stderr.Reset()
	if code := run(context.Background(), []string{"record-fixtures"}, io.Discard, &stderr); code != 1 ||
		!strings.Contains(stderr.String(), "building kalshi adapter: bad url") {
		t.Errorf("builder error: exit %d, stderr %q", code, stderr.String())
	}
}

func TestRegisterAdapterDuplicatePanics(t *testing.T) {
	withAdapters(t, &fakeAdapter{venue: canonical.VenueKalshi})
	defer func() {
		if recover() == nil {
			t.Error("duplicate registerAdapter did not panic")
		}
	}()
	registerAdapter(canonical.VenueKalshi, nil)
}

func TestWriteFixturesUnwritableDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	raws := []venues.RawResponse{{Endpoint: "/x", HTTPStatus: 200}}
	if _, err := writeFixtures(filepath.Join(file, "sub"), raws, slog.New(slog.DiscardHandler)); err == nil {
		t.Error("writing under a regular file succeeded")
	}
}
