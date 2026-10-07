package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// CDN assets, pinned by version with Subresource Integrity hashes computed
// from the files at these exact URLs (sha384, base64). Bump the version and
// the hash together.
const (
	picoURL = cdnHost + "/npm/@picocss/pico@2.1.1/css/pico.classless.min.css"
	picoSRI = "sha384-NZhm4G1I7BpEGdjDKnzEfy3d78xvy7ECKUwwnKTYi036z42IyF056PbHfpQLIYgL"
	htmxURL = cdnHost + "/npm/htmx.org@2.0.11/dist/htmx.min.js"
	htmxSRI = "sha384-2OatzQy1H+Zd/IIrjr1TcuDGqLXeHhbooAyJY1KdQMKnr4LZ22k31GBLdYKHmVjg"
)

// dbTimeout bounds the database work behind one page render (NFR-01).
const dbTimeout = 10 * time.Second

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/app.css
var appCSS []byte

// assets is what the layout needs to reference the pinned CDN files.
type assets struct {
	PicoURL, PicoSRI, HtmxURL, HtmxSRI string
}

var pinnedAssets = assets{PicoURL: picoURL, PicoSRI: picoSRI, HtmxURL: htmxURL, HtmxSRI: htmxSRI}

// templateFuncs are the formatting helpers templates may call. They only
// format Go values into plain strings, so everything they return is still
// auto-escaped (AB-07).
var templateFuncs = template.FuncMap{
	"money": func(d decimal.Decimal) string { return d.StringFixed(4) },
	"conf":  func(d decimal.Decimal) string { return d.StringFixed(2) },
	"price": nullPrice,
	"utc":   formatUTC,
	"upper": strings.ToUpper,
	"pct":   formatPercent,
	// httpURL returns s only when it is an absolute http(s) URL, else "", so
	// a template can decide whether a source is safe to turn into a link.
	"httpURL": httpURL,
}

// pages are the parsed template sets, one per page, each with the layout.
var pages = map[string]*template.Template{
	"matches":     parsePage("matches.html"),
	"near_misses": parsePage("near_misses.html"),
	"pair":        parsePage("pair.html", "route_result.html", "history.html"),
	"route":       parsePage("route.html", "route_result.html"),
	"decision":    parsePage("decision.html", "route_result.html"),
	"log":         parsePage("log.html"),
	"eval":        parsePage("eval.html"),
	"error":       parsePage("error.html"),
}

func parsePage(files ...string) *template.Template {
	paths := []string{"templates/layout.html"}
	for _, f := range files {
		paths = append(paths, "templates/"+f)
	}
	return template.Must(template.New("").Funcs(templateFuncs).ParseFS(templateFS, paths...))
}

// publicRoutes registers the open, read-only pages (D-035). The only public
// POST is /route, a simulation whose only write is its own decision (AB-14).
func (s *Server) publicRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.handleMatches)
	mux.HandleFunc("GET /near-misses", s.handleNearMisses)
	mux.HandleFunc("GET /pairs/{id}", s.handlePair)
	mux.HandleFunc("POST /route", s.handleRoute)
	mux.HandleFunc("GET /decisions/{id}", s.handleDecision)
	mux.HandleFunc("GET /log", s.handleLog)
	mux.HandleFunc("GET /eval", s.handleEval)
	mux.HandleFunc("GET /static/app.css", s.handleAppCSS)
}

func (s *Server) handleAppCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if _, err := w.Write(appCSS); err != nil {
		s.log.Error("writing app.css", "err", err)
	}
}

// page is the data every full page shares. Question is the one line that
// states what the page answers (NFR-16).
type page struct {
	Title        string
	Question     string
	Assets       assets
	Round        *roundInfo
	StaleWarning string // "Data is N minutes old" when round is stale (NFR-09, D-077)
}

// roundInfo is the round a page reads from, as shown in headers.
type roundInfo struct {
	ID     int64
	Status string
	Pinned bool
	AsOf   string
}

func newRoundInfo(r store.Round) *roundInfo {
	asOf := r.FinishedAt
	if asOf.IsZero() {
		asOf = r.StartedAt
	}
	return &roundInfo{ID: r.ID, Status: string(r.Status), Pinned: r.Pinned, AsOf: formatUTC(asOf)}
}

// nullPrice renders an optional price for display.
func nullPrice(d decimal.NullDecimal) string {
	if !d.Valid {
		return "n/a"
	}
	return d.Decimal.StringFixed(4)
}

func formatUTC(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

// formatPercent renders a ratio in [0,1] as a percentage with two decimals,
// or "n/a" for a nil ratio (an undefined metric such as 0/0).
func formatPercent(d *decimal.Decimal) string {
	if d == nil {
		return "n/a"
	}
	return d.Mul(decimal.NewFromInt(100)).StringFixed(2) + "%"
}

// httpURL returns raw when it parses as an absolute http or https URL with a
// host, else "". Templates use it to link a fact's source only when the
// source is a web address; anything else renders as text (AB-07).
func httpURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.String()
}

// render executes the named page's layout into a buffer, then writes it, so
// a template error becomes a 500 instead of a half-written page.
func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	s.renderTemplate(w, status, name, "layout", data)
}

func (s *Server) renderTemplate(w http.ResponseWriter, status int, page, tmpl string, data any) {
	var buf bytes.Buffer
	if err := pages[page].ExecuteTemplate(&buf, tmpl, data); err != nil {
		s.log.Error("rendering template", "page", page, "template", tmpl, "err", err)
		http.Error(w, "internal error rendering the page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		s.log.Error("writing page", "page", page, "err", err)
	}
}

// errorPage is the data for error.html.
type errorPage struct {
	page
	Status  int
	Message string
}

// renderError writes a full error page with a plain message. The message is
// written by this package, never copied from the request or the database.
func (s *Server) renderError(w http.ResponseWriter, status int, message string) {
	s.render(w, status, "error", errorPage{
		page:    page{Title: http.StatusText(status), Question: "Something went wrong.", Assets: pinnedAssets},
		Status:  status,
		Message: message,
	})
}

// storeOrFail returns the store, or answers 503 when none is configured.
func (s *Server) storeOrFail(w http.ResponseWriter) (*store.Store, bool) {
	if s.deps.Store == nil {
		s.renderError(w, http.StatusServiceUnavailable, "The database is not configured on this server.")
		return nil, false
	}
	return s.deps.Store, true
}

// dbError answers 503 for a database failure and logs the cause. Callers
// handle not-found themselves before calling it.
func (s *Server) dbError(w http.ResponseWriter, what string, err error) {
	s.log.Error(what, "err", err)
	s.renderError(w, http.StatusServiceUnavailable, "The database is unavailable; try again shortly.")
}

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }

// maxPathIDLen bounds a numeric path ID: 18 digits always fit an int64.
const maxPathIDLen = 18

// parseID accepts a positive decimal integer of up to 18 digits with no
// sign, spaces or leading zeros, and nothing else (AB-08: IDs are validated
// against their expected format before any lookup).
func parseID(raw string) (int64, bool) {
	if raw == "" || len(raw) > maxPathIDLen || raw[0] == '0' {
		return 0, false
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// instrumentView is an instrument as the pages show it. Missing is set when
// the relationship refers to an instrument the store no longer has.
type instrumentView struct {
	ID       string
	Venue    string
	MarketID string
	EventID  string
	Category string
	Title    string
	Rules    string
	Status   string
	Close    string
	Quality  []canonical.QualityIssue
	Missing  bool
}

func newInstrumentView(in canonical.Instrument) instrumentView {
	return instrumentView{
		ID: in.ID, Venue: string(in.Venue), MarketID: in.VenueMarketID, EventID: in.VenueEventID,
		Category: string(in.Category), Title: in.Title, Rules: in.RulesText, Status: string(in.Status),
		Close: formatUTC(in.CloseTime), Quality: in.Quality,
	}
}

func missingInstrumentView(id string) instrumentView {
	return instrumentView{ID: id, Venue: string(canonical.VenueOf(id)), Title: "(instrument not in store)", Missing: true}
}

// loadInstrumentViews fetches every distinct ID in one query and returns a
// view per ID, with a Missing placeholder for IDs the store does not have.
func loadInstrumentViews(ctx context.Context, st *store.Store, ids []string) (map[string]instrumentView, error) {
	recs, err := st.GetInstruments(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("loading instruments: %w", err)
	}
	out := make(map[string]instrumentView, len(ids))
	for _, id := range ids {
		if rec, ok := recs[id]; ok {
			out[id] = newInstrumentView(rec.Instrument)
		} else {
			out[id] = missingInstrumentView(id)
		}
	}
	return out, nil
}
