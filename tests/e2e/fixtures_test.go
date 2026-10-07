//go:build e2e

package e2e

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// The synthetic Polymarket market the overlay adds (see fixtureRoot). It is
// the EQ-01 case of testdata/match/crypto_pairs.yaml: Polymarket phrasing,
// but resolving on CF Benchmarks' BRTI at 6 PM ET on Oct 6, 2026, which is
// exactly what the recorded Kalshi market KXBTCD-26OCT0618-T76599.99 does.
// In the recorded data Kalshi always resolves on CF Benchmarks and
// Polymarket on Binance, so no recorded cross-venue pair is equivalent under
// D-017 (status.md Q-4); this one pair is what proves the equivalent path.
const (
	syntheticTag      = "e2e-cf-btc"
	syntheticYesToken = "900000000000000000000000000000000000000000000000000000000000000000000000000001"
	// The Gamma page size and the up-or-down exclusion the adapter sends
	// (polymarket.DefaultPageSize, upOrDownTagID); the file name is derived
	// from the exact query, so these must match what the adapter requests.
	gammaPageSize = "50"
	upOrDownTagID = "102127"
)

// syntheticFS holds the synthetic market's Gamma events page and CLOB book.
//
//go:embed testdata/polymarket/*.json
var syntheticFS embed.FS

// syntheticKalshiID is the recorded Kalshi instrument the synthetic market is
// equivalent to.
var syntheticKalshiID = canonical.InstrumentID(canonical.VenueKalshi, "KXBTCD-26OCT0618-T76599.99")

// syntheticPolymarketID is the synthetic Polymarket instrument's ID.
var syntheticPolymarketID = canonical.InstrumentID(canonical.VenuePolymarket, syntheticYesToken)

// fixtureRoot builds the directory the fixture server serves: a copy of the
// recorded testdata/fixtures plus the synthetic Polymarket market under the
// file names the fixture server resolves for the adapter's requests.
func fixtureRoot(t *testing.T, repoRoot string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS(filepath.Join(repoRoot, "testdata", "fixtures"))); err != nil {
		t.Fatalf("copying recorded fixtures: %v", err)
	}
	overlay := map[string]string{ // adapter endpoint -> synthetic body file
		"gamma/events?" + url.Values{
			"active": {"true"}, "closed": {"false"}, "exclude_tag_id": {upOrDownTagID},
			"limit": {gammaPageSize}, "offset": {"0"}, "tag_slug": {syntheticTag},
		}.Encode(): "e2e_gamma_events.json",
		"clob/book?" + url.Values{"token_id": {syntheticYesToken}}.Encode(): "e2e_clob_book.json",
	}
	for endpoint, src := range overlay {
		body, err := fs.ReadFile(syntheticFS, "testdata/polymarket/"+src)
		if err != nil {
			t.Fatalf("reading synthetic fixture: %v", err)
		}
		// FixtureFileName yields a flat name from a fixed character set, so
		// the destination always sits in the temp root's polymarket directory.
		dst := filepath.Join(root, string(canonical.VenuePolymarket), venues.FixtureFileName(endpoint))
		if _, err := os.Stat(dst); err == nil {
			t.Fatalf("synthetic fixture %s would overwrite a recorded fixture", dst)
		}
		if err := os.WriteFile(dst, body, 0o600); err != nil { //nolint:gosec // see above: generated flat name under t.TempDir()
			t.Fatalf("writing synthetic fixture: %v", err)
		}
		t.Logf("synthetic fixture %s -> %s", endpoint, filepath.Base(dst))
	}
	return root
}

// fixtureOnlyTransport is the counting http.RoundTripper every venue adapter
// uses. It counts requests per host and fails the test on any request to a
// host other than the fixture server, which it also refuses to send, so a
// misconfigured adapter can never reach a real venue from CI.
type fixtureOnlyTransport struct {
	t       *testing.T
	allowed string // host:port of the fixture server
	next    http.RoundTripper

	mu         sync.Mutex
	byHost     map[string]int
	violations []string
}

var errNotFixtureHost = errors.New("e2e: request to a non-fixture host refused")

func newFixtureOnlyTransport(t *testing.T, fixtureURL string) *fixtureOnlyTransport {
	t.Helper()
	u, err := url.Parse(fixtureURL)
	if err != nil {
		t.Fatalf("parsing fixture server URL: %v", err)
	}
	return &fixtureOnlyTransport{t: t, allowed: u.Host, next: http.DefaultTransport.(*http.Transport).Clone(), byHost: map[string]int{}}
}

func (rt *fixtureOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	rt.mu.Lock()
	rt.byHost[host]++
	ok := host == rt.allowed
	if !ok {
		rt.violations = append(rt.violations, req.Method+" "+req.URL.Redacted())
	}
	rt.mu.Unlock()
	if !ok {
		// Error (not Fatal): RoundTrip runs on the adapters' goroutines.
		rt.t.Errorf("venue request left the fixture server: %s %s", req.Method, req.URL.Redacted())
		return nil, fmt.Errorf("%w: %s", errNotFixtureHost, host)
	}
	return rt.next.RoundTrip(req)
}

// total is the number of requests seen, on any host.
func (rt *fixtureOnlyTransport) total() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	n := 0
	for _, c := range rt.byHost {
		n += c
	}
	return n
}

// summary lists the per-host counts, for the log.
func (rt *fixtureOnlyTransport) summary() string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	hosts := make([]string, 0, len(rt.byHost))
	for h := range rt.byHost {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	parts := make([]string, 0, len(hosts))
	for _, h := range hosts {
		parts = append(parts, fmt.Sprintf("%s=%d", h, rt.byHost[h]))
	}
	return strings.Join(parts, ", ")
}

// check fails the test unless every request went to the fixture server and
// at least one was made.
func (rt *fixtureOnlyTransport) check(t *testing.T) {
	t.Helper()
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.violations) > 0 {
		t.Fatalf("%d venue request(s) left the fixture server: %s", len(rt.violations), strings.Join(rt.violations, "; "))
	}
	if n := rt.byHost[rt.allowed]; n == 0 {
		t.Fatal("the collector made no request to the fixture server")
	}
	if len(rt.byHost) != 1 {
		t.Fatalf("requests went to %d hosts, want only the fixture server", len(rt.byHost))
	}
}
