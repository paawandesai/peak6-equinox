// Package fixtureserver serves the recorded venue responses under
// testdata/fixtures/** over HTTP, so the collector, the seed command and the
// end-to-end test run offline against the same bodies the adapter tests use.
//
// Point the venue base URLs at it (see VenueURLs):
//
//	KALSHI_BASE_URL=http://127.0.0.1:8090/kalshi
//	POLYMARKET_GAMMA_URL=http://127.0.0.1:8090/polymarket/gamma
//	POLYMARKET_CLOB_URL=http://127.0.0.1:8090/polymarket/clob
//
// A request is mapped to the file name `equinox record-fixtures` wrote for it
// (venues.FixtureFileName of the API-relative endpoint). Anything not recorded
// gets a 404, which the adapters already handle (a missing book is `no_book`).
package fixtureserver

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync/atomic"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// Server serves recorded fixtures for every venue directory under its root.
type Server struct {
	fsys  fs.FS
	log   *slog.Logger
	loose map[canonical.Venue]map[string]string // venue -> loose key -> file name
	hits  atomic.Int64
}

// New serves <root>/<venue>/*.json. root is usually "testdata/fixtures".
func New(root string, logger *slog.Logger) (*Server, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("fixture root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("fixture root %q is not a directory", root)
	}
	return NewFS(os.DirFS(root), logger)
}

// NewFS is New over any fs.FS whose top-level directories are venue names.
func NewFS(fsys fs.FS, logger *slog.Logger) (*Server, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	s := &Server{fsys: fsys, log: logger, loose: map[canonical.Venue]map[string]string{}}
	for _, v := range canonical.Venues() {
		idx, err := looseIndex(fsys, string(v))
		if err != nil {
			return nil, err
		}
		s.loose[v] = idx
	}
	return s, nil
}

// VenueURLs returns the venue base URLs to configure for a server reachable
// at base (e.g. "http://127.0.0.1:8090"), in the order Kalshi, Polymarket
// Gamma, Polymarket CLOB.
func VenueURLs(base string) (kalshi, gamma, clob string) {
	base = strings.TrimRight(base, "/")
	return base + "/kalshi", base + "/polymarket/gamma", base + "/polymarket/clob"
}

// Hits returns the number of requests served so far (any status).
func (s *Server) Hits() int64 { return s.hits.Load() }

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.hits.Add(1)
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "fixture server only answers GET")
		return
	}
	// Adapters must never send credentials to a venue; a fixture request that
	// carries any is a bug worth failing loudly.
	if r.Header.Get("Authorization") != "" {
		writeError(w, http.StatusBadRequest, "unexpected Authorization header")
		return
	}
	venue, endpoint, ok := splitRequest(r.URL.Path, r.URL.RawQuery)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown venue prefix; use /kalshi/... or /polymarket/{gamma,clob}/...")
		return
	}
	name, body, err := s.lookup(venue, endpoint)
	if err != nil {
		s.log.Debug("no fixture", "venue", venue, "endpoint", endpoint, "error", err)
		writeError(w, http.StatusNotFound, "no fixture recorded for this endpoint")
		return
	}
	s.log.Debug("serving fixture", "venue", venue, "endpoint", endpoint, "file", name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(body) //nolint:gosec // recorded JSON fixture served as application/json, never HTML
	}
}

// splitRequest maps a request path to the venue and the API-relative endpoint
// the adapter asked for: "/kalshi/events?x" -> ("/events?x") and
// "/polymarket/clob/book?x" -> ("clob/book?x").
func splitRequest(reqPath, rawQuery string) (canonical.Venue, string, bool) {
	trimmed := strings.TrimPrefix(reqPath, "/")
	venueName, rest, _ := strings.Cut(trimmed, "/")
	v := canonical.Venue(venueName)
	if !v.Valid() {
		return "", "", false
	}
	var endpoint string
	switch v {
	case canonical.VenueKalshi:
		endpoint = "/" + rest
	case canonical.VenuePolymarket:
		api, apiPath, ok := strings.Cut(rest, "/")
		if !ok || api == "" {
			return "", "", false
		}
		endpoint = api + "/" + apiPath
	}
	if rawQuery != "" {
		endpoint += "?" + rawQuery
	}
	return v, endpoint, true
}

// lookup finds the body for an endpoint: the exact recorded name first, then
// the loose match that ignores a `min_close_ts` value (Kalshi settled pages
// embed the recording clock in the query, D-056).
func (s *Server) lookup(v canonical.Venue, endpoint string) (string, []byte, error) {
	name := venues.FixtureFileName(endpoint)
	body, err := s.read(v, name)
	if err == nil {
		return name, body, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", nil, err
	}
	alt, ok := s.loose[v][looseKey(name)]
	if !ok {
		return "", nil, err
	}
	body, err = s.read(v, alt)
	if err != nil {
		return "", nil, err
	}
	return alt, body, nil
}

// read opens <venue>/<name>; name comes from FixtureFileName or the index, so
// it is a flat file name with no separators.
func (s *Server) read(v canonical.Venue, name string) ([]byte, error) {
	if strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return nil, fmt.Errorf("fixture name %q: %w", name, fs.ErrNotExist)
	}
	f, err := s.fsys.Open(path.Join(string(v), name))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(f)
}

// clockParam matches the query values that depend on the recording time.
var clockParam = regexp.MustCompile(`_min_close_ts-[0-9]+`)

// looseKey strips the hash suffix and any clock-dependent value from a fixture
// file name, so a request made now can still find a page recorded earlier.
func looseKey(name string) string {
	stem := strings.TrimSuffix(name, ".json")
	if i := strings.LastIndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	return clockParam.ReplaceAllString(stem, "")
}

// looseIndex maps looseKey(file) -> file for one venue directory. A key that
// several files share is left out (an exact match still serves them).
func looseIndex(fsys fs.FS, venueDir string) (map[string]string, error) {
	entries, err := fs.ReadDir(fsys, venueDir)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading fixtures %s: %w", venueDir, err)
	}
	idx := map[string]string{}
	ambiguous := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		k := looseKey(e.Name())
		if _, dup := idx[k]; dup {
			ambiguous[k] = true
			continue
		}
		idx[k] = e.Name()
	}
	for k := range ambiguous {
		delete(idx, k)
	}
	return idx, nil
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":%q}`+"\n", msg)
}
