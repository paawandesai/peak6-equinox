package fixtureserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/kalshi"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues/polymarket"
)

var repoFixtures = filepath.Join("..", "..", "..", "testdata", "fixtures")

func newRepoServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	s, err := New(repoFixtures, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, srv
}

func get(t *testing.T, url string, headers ...string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func TestServesRecordedKalshiAndPolymarketFiles(t *testing.T) {
	s, srv := newRepoServer(t)
	cases := []struct {
		path string
		want string // a fragment the recorded body must contain
	}{
		{"/kalshi/events?series_ticker=KXFED&status=open&with_nested_markets=true&limit=200", `"KXFED-26OCT"`},
		{"/kalshi/markets/KXFED-26OCT-T3.25/orderbook", `"orderbook_fp"`},
		{"/polymarket/gamma/events?active=true&closed=false&exclude_tag_id=102127&limit=50&offset=0&tag_slug=fed-rates", `"slug"`},
		{"/polymarket/clob/book?token_id=35539979239584554883356157015948089057863104809265335146918370316987175741849", `"asset_id"`},
	}
	for _, tc := range cases {
		status, body := get(t, srv.URL+tc.path)
		if status != http.StatusOK {
			t.Errorf("%s: status %d, body %s", tc.path, status, body)
			continue
		}
		if !json.Valid(body) || !strings.Contains(string(body), tc.want) {
			t.Errorf("%s: body does not look like the recorded fixture (want %s)", tc.path, tc.want)
		}
	}
	if s.Hits() != int64(len(cases)) {
		t.Errorf("hits = %d, want %d", s.Hits(), len(cases))
	}
}

func TestKalshiSettledIgnoresMinCloseTS(t *testing.T) {
	_, srv := newRepoServer(t)
	// Recorded with min_close_ts=1791235925; a live adapter sends the current clock.
	status, body := get(t, srv.URL+"/kalshi/markets?series_ticker=KXBTCD&status=settled&min_close_ts=1799999999&limit=200")
	if status != http.StatusOK || !strings.Contains(string(body), `"markets"`) {
		t.Errorf("loose settled match: status %d, body %.80s", status, body)
	}
	status, body = get(t, srv.URL+"/kalshi/markets?series_ticker=KXBTCD&status=settled&min_close_ts=1&limit=200&cursor=CgYIwKaV1gY")
	if status != http.StatusOK || !strings.Contains(string(body), `"markets"`) {
		t.Errorf("loose settled match with cursor: status %d, body %.80s", status, body)
	}
}

func TestUnknownRequestsAre404(t *testing.T) {
	_, srv := newRepoServer(t)
	for _, p := range []string{
		"/kalshi/markets/NOPE/orderbook",
		"/polymarket/clob/book?token_id=123",
		"/polymarket/events?x=1", // no API prefix
		"/nope/events",
		"/",
		"/kalshi/../polymarket/clob/book?token_id=1",
	} {
		status, body := get(t, srv.URL+p)
		if status != http.StatusNotFound || !json.Valid(body) {
			t.Errorf("%s: status %d body %s, want a JSON 404", p, status, body)
		}
	}
}

func TestRejectsAuthAndNonGET(t *testing.T) {
	_, srv := newRepoServer(t)
	if status, _ := get(t, srv.URL+"/kalshi/markets/KXFED-26OCT-T3.25/orderbook", "Authorization", "Bearer x"); status != http.StatusBadRequest {
		t.Errorf("request with Authorization: status %d, want 400", status)
	}
	resp, err := http.Post(srv.URL+"/kalshi/markets/KXFED-26OCT-T3.25/orderbook", "application/json", strings.NewReader("{}")) //nolint:gosec,noctx // test server URL
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") == "" {
		t.Errorf("POST: status %d Allow %q, want 405 with Allow", resp.StatusCode, resp.Header.Get("Allow"))
	}
	resp, err = http.Head(srv.URL + "/kalshi/markets/KXFED-26OCT-T3.25/orderbook") //nolint:gosec,noctx // test server URL
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HEAD: status %d, want 200", resp.StatusCode)
	}
}

func TestNewRejectsBadRoot(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "missing"), nil); err == nil {
		t.Error("missing root accepted")
	}
	f := filepath.Join(t.TempDir(), "file")
	if err := writeFile(f, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := New(f, nil); err == nil {
		t.Error("file root accepted")
	}
	// An empty root (no venue directories) is fine: everything is a 404.
	s, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("empty root: %v", err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/kalshi/events", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("empty root: status %d", rec.Code)
	}
}

func TestLooseIndexSkipsAmbiguousKeys(t *testing.T) {
	a := venues.FixtureFileName("/markets?series_ticker=KXFED&status=settled&min_close_ts=1&limit=200")
	b := venues.FixtureFileName("/markets?series_ticker=KXFED&status=settled&min_close_ts=2&limit=200")
	fsys := fstest.MapFS{
		"kalshi/" + a:           {Data: []byte(`{"a":1}`)},
		"kalshi/" + b:           {Data: []byte(`{"b":1}`)},
		"kalshi/notes.txt":      {Data: []byte("ignored")},
		"polymarket/sub/x.json": {Data: []byte("{}")},
		"polymarket/" + venues.FixtureFileName("clob/book?token_id=9"): {Data: []byte(`{"c":1}`)},
	}
	s, err := NewFS(fsys, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.loose[canonical.VenueKalshi]) != 0 {
		t.Errorf("ambiguous min_close_ts pages were indexed: %v", s.loose[canonical.VenueKalshi])
	}
	// Exact names still serve.
	for _, p := range []string{
		"/kalshi/markets?series_ticker=KXFED&status=settled&min_close_ts=2&limit=200",
		"/polymarket/clob/book?token_id=9",
	} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d", p, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/kalshi/markets?series_ticker=KXFED&status=settled&min_close_ts=3&limit=200", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("ambiguous loose match served: status %d", rec.Code)
	}
}

func TestSplitRequest(t *testing.T) {
	cases := []struct {
		path, query string
		venue       canonical.Venue
		endpoint    string
		ok          bool
	}{
		{"/kalshi/events", "a=1", canonical.VenueKalshi, "/events?a=1", true},
		{"/kalshi/markets/X/orderbook", "", canonical.VenueKalshi, "/markets/X/orderbook", true},
		{"/polymarket/gamma/events", "tag_slug=bitcoin", canonical.VenuePolymarket, "gamma/events?tag_slug=bitcoin", true},
		{"/polymarket/clob/book", "token_id=1", canonical.VenuePolymarket, "clob/book?token_id=1", true},
		{"/polymarket/book", "token_id=1", "", "", false},
		{"/polymarket", "", "", "", false},
		{"/other/x", "", "", "", false},
	}
	for _, tc := range cases {
		v, ep, ok := splitRequest(tc.path, tc.query)
		if ok != tc.ok || v != tc.venue || ep != tc.endpoint {
			t.Errorf("splitRequest(%q, %q) = (%q, %q, %v), want (%q, %q, %v)", tc.path, tc.query, v, ep, ok, tc.venue, tc.endpoint, tc.ok)
		}
	}
}

func TestVenueURLs(t *testing.T) {
	k, g, c := VenueURLs("http://127.0.0.1:8090/")
	if k != "http://127.0.0.1:8090/kalshi" || g != "http://127.0.0.1:8090/polymarket/gamma" || c != "http://127.0.0.1:8090/polymarket/clob" {
		t.Errorf("VenueURLs = %q %q %q", k, g, c)
	}
}

// TestAdaptersRunAgainstFixtureServer is the integration check: both real
// adapters fetch instruments and quotes through the server with no network.
func TestAdaptersRunAgainstFixtureServer(t *testing.T) {
	_, srv := newRepoServer(t)
	kURL, gURL, cURL := VenueURLs(srv.URL)
	us, err := venues.LoadUniverse(filepath.Join("..", "..", "..", "config", "universe"))
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC) }
	ka, err := kalshi.New(kalshi.Options{BaseURL: kURL, MinInterval: -1, Retries: -1, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	pa, err := polymarket.New(polymarket.Options{GammaURL: gURL, ClobURL: cURL, Timeout: 5 * time.Second, MaxBodyBytes: 10 << 20, Retries: -1, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range []venues.Adapter{ka, pa} {
		ins, raws, err := a.FetchInstruments(context.Background(), us.For(a.Venue()))
		if err != nil || len(ins) == 0 || len(raws) == 0 {
			t.Fatalf("%s FetchInstruments: %d instruments, %d raws, err %v", a.Venue(), len(ins), len(raws), err)
		}
		quotes, _, err := a.FetchQuotes(context.Background(), ins[:3])
		if err != nil || len(quotes) != 3 {
			t.Errorf("%s FetchQuotes: %d quotes, err %v", a.Venue(), len(quotes), err)
		}
	}
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) }
