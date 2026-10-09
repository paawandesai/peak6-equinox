//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/collector"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/normalize"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/fixtureserver"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/kalshi"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/polymarket"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

const (
	roundTimeout = 5 * time.Minute
	httpTimeout  = 30 * time.Second
	// matchPassword only satisfies config.FromEnv in the `equinox match`
	// subprocess; nothing is served with it.
	matchPassword = "e2e-only-not-a-secret-0123456789" //nolint:gosec // test-only placeholder, never a real credential
)

// TestSmoke_DemoPath runs the M1 demo path offline, in the order the ticket
// lists (M1-10): fixture server, venue URLs, fresh schema, one collector
// round, match, the web server, GET /, GET /pairs/{id}, POST /route.
func TestSmoke_DemoPath(t *testing.T) {
	pool := tdb.Pool(t) // skips locally, fails in CI, when DATABASE_URL_TEST is unset
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("locating the repo root: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	ctx, cancel := context.WithTimeout(context.Background(), roundTimeout)
	defer cancel()

	// 1. Start the fixture venue server over the recorded fixtures plus the
	//    synthetic equivalent pair (fixtures_test.go).
	fx, err := fixtureserver.New(fixtureRoot(t, repoRoot), logger)
	if err != nil {
		t.Fatalf("fixture server: %v", err)
	}
	fixtures := httptest.NewServer(fx.Handler())
	t.Cleanup(fixtures.Close)

	// 2. Point the venue URLs at it. Every adapter request goes through the
	//    counting transport, which refuses any other host.
	transport := newFixtureOnlyTransport(t, fixtures.URL)
	col := newCollector(t, repoRoot, fixtures.URL, transport, store.New(pool), logger)

	// 3. The schema was created and migrated by testdb in TestMain; it must be empty.
	st := store.New(pool)
	if _, err := st.CurrentRound(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("fresh schema should have no round, got err=%v", err)
	}

	// 4. Run one collector round.
	res, err := col.Run(ctx)
	if err != nil {
		t.Fatalf("collector round: %v", err)
	}
	t.Logf("round %d: %s; venue requests: %s; fixture hits: %d", res.RoundID, res.Status, transport.summary(), fx.Hits())
	if res.Status != store.RoundOK {
		t.Fatalf("round %d status = %s, want %s (%s)", res.RoundID, res.Status, store.RoundOK, res.Summary())
	}
	for _, v := range res.Venues {
		if !v.OK || v.Instruments == 0 || v.Quotes == 0 {
			t.Errorf("venue %s: ok=%v instruments=%d quotes=%d error=%q", v.Venue, v.OK, v.Instruments, v.Quotes, v.Error)
		}
	}
	transport.check(t)
	if got, want := int64(transport.total()), fx.Hits(); got != want {
		t.Errorf("transport saw %d requests, fixture server served %d", got, want)
	}

	// 5. Run match with the real CLI against the same schema.
	runMatch(t, repoRoot, res.RoundID)

	// 6. Start web.Server via httptest.
	srv := web.New(web.Config{
		AppEnv:                config.EnvCI,
		MinRouteConfidence:    decimal.RequireFromString(config.DefaultMinRouteConfidence),
		PublicRateLimitPerMin: config.DefaultPublicRateLimitPerMin,
	}, web.Deps{Logger: logger, Store: st, Ingester: col})
	site := httptest.NewServer(srv.Handler())
	t.Cleanup(site.Close)
	client := &http.Client{Timeout: httpTimeout}

	// 7. GET / lists at least one equivalent pair.
	body := get(t, client, site.URL+"/")
	pairID := firstEquivalentPair(t, body)
	t.Logf("GET /: first equivalent pair #%d", pairID)

	// 8. GET /pairs/{id} shows the diff table (UI-12: table.terms with t-match/t-differ/t-closed/t-unknown).
	body = get(t, client, site.URL+"/pairs/"+strconv.FormatInt(pairID, 10))
	if !strings.Contains(body, `<table class="data terms">`) {
		t.Fatal("GET /pairs/{id}: no diff table")
	}
	if n := strings.Count(body, `<tr class="t-`); n == 0 {
		t.Fatal("GET /pairs/{id}: the diff table has no rows")
	}
	if !strings.Contains(body, ">equivalent<") {
		t.Error("GET /pairs/{id}: the page does not show the equivalent type")
	}
	if explanation(t, body) == "" {
		t.Error("GET /pairs/{id}: the relationship explanation is empty")
	}
	rel, err := st.GetRelationship(ctx, pairID)
	if err != nil {
		t.Fatalf("loading pair %d: %v", pairID, err)
	}
	if rel.Type != canonical.RelEquivalent || canonical.VenueOf(rel.AID) == canonical.VenueOf(rel.BID) {
		t.Fatalf("pair %d is %s between %s and %s, want a cross-venue equivalent pair", pairID, rel.Type, rel.AID, rel.BID)
	}
	if rel.AID != syntheticKalshiID || rel.BID != syntheticPolymarketID {
		t.Logf("note: pair %d is %s / %s, not the synthetic pair", pairID, rel.AID, rel.BID)
	}

	// 9. POST /route returns a chosen venue and a non-empty explanation.
	form := url.Values{
		"pair": {strconv.FormatInt(pairID, 10)}, "target": {rel.AID},
		"side": {"buy"}, "outcome": {"yes"}, "quantity": {"100"}, "type": {"market"}, "tif": {"IOC"},
	}
	body = post(t, client, site.URL+"/route", form)
	if strings.Contains(body, "The order was not routed") {
		t.Fatalf("POST /route: the order was not routed:\n%s", excerpt(body))
	}
	venue := chosenVenue(t, body)
	expl := explanation(t, body)
	if expl == "" {
		t.Fatal("POST /route: the routing explanation is empty")
	}
	t.Logf("POST /route: chosen venue %s; explanation: %s", venue, expl)
}

// newCollector builds the real adapters (pacing and retries off, since the
// fixture server never answers 429) over the counting transport, the
// recorded universe plus the synthetic tag, and the normaliser, as
// cmd/equinox wires them.
func newCollector(t *testing.T, repoRoot, fixtureURL string, rt http.RoundTripper, st *store.Store, logger *slog.Logger) *collector.Collector {
	t.Helper()
	hc := &http.Client{Transport: rt, Timeout: httpTimeout}
	kURL, gURL, cURL := fixtureserver.VenueURLs(fixtureURL)
	ka, err := kalshi.New(kalshi.Options{BaseURL: kURL, HTTPClient: hc, MaxBodyBytes: config.DefaultMaxBodyBytes,
		MinInterval: -1, Retries: -1, Logger: logger})
	if err != nil {
		t.Fatalf("kalshi adapter: %v", err)
	}
	pa, err := polymarket.New(polymarket.Options{GammaURL: gURL, ClobURL: cURL, HTTPClient: hc,
		MaxBodyBytes: config.DefaultMaxBodyBytes, Retries: -1, Logger: logger})
	if err != nil {
		t.Fatalf("polymarket adapter: %v", err)
	}
	reg, err := venues.NewRegistry(ka, pa)
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	universes, err := venues.LoadUniverse(filepath.Join(repoRoot, config.DefaultUniverseDir))
	if err != nil {
		t.Fatalf("universe: %v", err)
	}
	pm := universes[canonical.VenuePolymarket]
	sel := pm.Categories[canonical.CategoryCrypto]
	sel.TagSlugs = append(append([]string(nil), sel.TagSlugs...), syntheticTag)
	pm.Categories[canonical.CategoryCrypto] = sel
	// Quote every instrument (-1): the recorded books are local, and the
	// round-robin cap could otherwise leave the pair without a book.
	col, err := collector.New(st, reg, universes, collector.Options{MaxQuotesPerVenue: -1, Normalize: normalize.Normalize, Logger: logger})
	if err != nil {
		t.Fatalf("collector: %v", err)
	}
	return col
}

// runMatch builds cmd/equinox and runs `equinox match --round N` against the
// test schema, the way the demo does after `make seed`.
func runMatch(t *testing.T, repoRoot string, roundID int64) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "equinox")
	build := exec.Command("go", "build", "-o", bin, "./cmd/equinox") //nolint:gosec // fixed command; bin is under t.TempDir()
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building cmd/equinox: %v\n%s", err, out)
	}
	match := exec.Command(bin, "match", "--round", strconv.FormatInt(roundID, 10)) //nolint:gosec // the binary built above
	match.Dir = repoRoot
	match.Env = append(os.Environ(),
		"APP_ENV="+config.EnvCI, "DATABASE_URL="+tdb.DSN(t), "ADMIN_USER=admin", "ADMIN_PASSWORD="+matchPassword, "LOG_LEVEL=warn")
	var stdout, stderr bytes.Buffer
	match.Stdout, match.Stderr = &stdout, &stderr
	if err := match.Run(); err != nil {
		t.Fatalf("equinox match: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	t.Logf("equinox match:\n%s", strings.TrimSpace(stdout.String()))
	if strings.Contains(stdout.String(), "equivalent 0,") {
		t.Fatalf("equinox match found no equivalent pair:\n%s", stdout.String())
	}
}

func get(t *testing.T, c *http.Client, u string) string {
	t.Helper()
	resp, err := c.Get(u)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return readPage(t, "GET "+u, resp)
}

func post(t *testing.T, c *http.Client, u string, form url.Values) string {
	t.Helper()
	resp, err := c.PostForm(u, form)
	if err != nil {
		t.Fatalf("POST %s: %v", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return readPage(t, "POST "+u, resp)
}

// readPage requires a 200 HTML response and returns its body.
func readPage(t *testing.T, what string, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		t.Fatalf("%s: reading body: %v", what, err)
	}
	body := string(b)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s: status %d\n%s", what, resp.StatusCode, excerpt(body))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("%s: content type %q, want text/html", what, ct)
	}
	return body
}

var (
	pairLinkRe    = regexp.MustCompile(`href="/pairs/([0-9]+)"`)
	explanationRe = regexp.MustCompile(`(?s)<p class="explanation">\s*(.*?)\s*</p>`)
	chosenRe      = regexp.MustCompile(`(?s)<h3>Chosen legs</h3>\s*<table[^>]*>.*?<span class="venue">([a-z]+)</span>`)
)

// firstEquivalentPair finds the routable section (UI-03 markup change) and
// returns the ID of the first equivalent pair within it.
func firstEquivalentPair(t *testing.T, body string) int64 {
	t.Helper()
	// UI-03: routable pairs (equivalent, equivalent_given_facts, complement) are now
	// grouped under <h2 id="routable">, not separate type sections.
	start := strings.Index(body, `<h2 id="routable">`)
	if start < 0 {
		t.Fatalf("GET /: no routable section found\n%s", excerpt(body))
	}
	section := body[start:]
	// Find the end of the routable section (next <section> or <h2>).
	if end := strings.Index(section[1:], `</section>`); end >= 0 {
		section = section[:end+1]
	}
	// Look for the first row with type chip "equivalent" (not equivalent_given_facts).
	// The chip is: <span class="chip chip-ok">equivalent</span>
	chipStart := strings.Index(section, `<span class="chip chip-ok">equivalent</span>`)
	if chipStart < 0 {
		t.Fatalf("GET /: no equivalent pairs listed in routable section\n%s", excerpt(body))
	}
	// Find the <tr> containing this chip and extract its pair link.
	// Walk backwards from chipStart to find the opening <tr>.
	rowStart := strings.LastIndex(section[:chipStart], `<tr>`)
	if rowStart < 0 {
		t.Fatal("GET /: found equivalent chip but no <tr> before it")
	}
	rowSection := section[rowStart:]
	m := pairLinkRe.FindStringSubmatch(rowSection)
	if m == nil {
		t.Fatal("GET /: the equivalent row has no pair link")
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		t.Fatalf("GET /: pair link %q: %v", m[1], err)
	}
	return id
}

// explanation returns the first explanation paragraph's text.
func explanation(t *testing.T, body string) string {
	t.Helper()
	m := explanationRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// chosenVenue returns the venue of the first chosen leg and requires it to be
// a known venue.
func chosenVenue(t *testing.T, body string) canonical.Venue {
	t.Helper()
	m := chosenRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("POST /route: no chosen route\n%s", excerpt(body))
	}
	v := canonical.Venue(m[1])
	if !v.Valid() {
		t.Fatalf("POST /route: chosen venue %q is not a known venue", m[1])
	}
	return v
}

// excerpt keeps a failure message readable.
func excerpt(body string) string {
	const n = 2000
	if len(body) <= n {
		return body
	}
	return body[:n] + "\n…"
}
