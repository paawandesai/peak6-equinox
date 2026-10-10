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
	"cents": func(d decimal.Decimal) string { return d.Mul(decimal.NewFromInt(100)).StringFixed(2) },
	"conf":  func(d decimal.Decimal) string { return d.StringFixed(2) },
	"price": nullPrice,
	"utc":   formatUTC,
	"upper": strings.ToUpper,
	"pct":   formatPercent,
	// httpURL returns s only when it is an absolute http(s) URL, else "", so
	// a template can decide whether a source is safe to turn into a link.
	"httpURL": httpURL,
	// formatTime formats a time.Time in the user's chosen timezone (M3-08).
	"formatTime": formatTime,
	// Age formats a duration as a human-readable age (UI-13).
	"Age": Age,
	// UI-12: partOrder returns the ordered list of rule parts for diff grouping.
	"partOrder": func() []string {
		return []string{
			"Resolution criteria",
			"Strike / threshold",
			"Direction",
			"Timeframe / cutoff",
			"Sources",
			"Qualifying / edge cases",
			"Other terms",
		}
	},
	// UI-12: filterByPart filters diff rows by rule part.
	"filterByPart": func(rows []diffRowView, part string) []diffRowView {
		var filtered []diffRowView
		for _, r := range rows {
			if r.Part == part {
				filtered = append(filtered, r)
			}
		}
		return filtered
	},
	// RoundStatusBadge returns a Badge for a round status (UI-17).
	"RoundStatusBadge": RoundStatusBadge,
	// Template arithmetic helpers (UI-17).
	"mul": func(a, b int) int { return a * b },
	"add": func(a, b int64) int64 { return a + b },
	// UI-23: confusion matrix bucket classes (HANDOFF ◆9: d1-d3 diagonal, e1-e3 off-diagonal)
	"ConfusionClass": func(label, predicted string, count int) string {
		// Bucket by count: 1-5 light, 6-15 medium, 16+ dark
		var bucket string
		switch {
		case count <= 5:
			bucket = "1"
		case count <= 15:
			bucket = "2"
		default:
			bucket = "3"
		}
		if label == predicted {
			return "d" + bucket // diagonal: d1/d2/d3 (green intensity)
		}
		return "e" + bucket // off-diagonal: e1/e2/e3 (red intensity)
	},
}

// pages are the parsed template sets, one per page, each with the layout.
var pages = map[string]*template.Template{
	"matches":     parsePage("matches.html"),
	"near_misses": parsePage("near_misses.html"),
	"pair":        parsePage("pair.html", "route_result.html", "history.html"),
	"route":       parsePage("route.html", "route_result.html"),
	"decision":    parsePage("decision.html", "route_result.html"),
	"log":         parsePage("log.html"),
	"eval":        parsePage("eval.html", "backtest_results.html"), // M4-76: backtest results partial
	"eval_cell":   parsePage("eval_cell.html"),                     // UI-23
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
// All routes except /static are rate-limited per client IP (AB-03) and wrapped
// with round middleware (M3-07: load pinned or latest round once per request).
func (s *Server) publicRoutes(mux *http.ServeMux) {
	// Apply both round middleware and rate limiting to public pages
	wrap := func(h http.HandlerFunc) http.Handler {
		return s.roundMiddleware(s.rateLimitMiddleware(http.HandlerFunc(h)))
	}
	mux.Handle("GET /{$}", wrap(s.handleMatches))
	mux.Handle("GET /near-misses", wrap(s.handleNearMisses))
	mux.Handle("GET /pairs/{id}", wrap(s.handlePair))
	mux.Handle("POST /route", wrap(s.handleRoute))
	mux.Handle("GET /decisions/{id}", wrap(s.handleDecision))
	mux.Handle("GET /log", wrap(s.handleLog))
	mux.Handle("GET /eval", wrap(s.handleEval))
	mux.Handle("GET /eval/cell", wrap(s.handleEvalCell)) // UI-23
	// POST /timezone sets the user's timezone preference cookie (M3-08, no round middleware needed)
	mux.Handle("POST /timezone", s.rateLimitMiddleware(http.HandlerFunc(s.handleTimezone)))
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
	Stamp        *stampInfo // Freshness stamp data (UI-10)
	StaleWarning string     // "Data is N minutes old" when round is stale (NFR-09, D-077)
	Banner       banner     // Demo mode or no-round banner (NFR-04, M3-07)
	Timezone     string     // User's timezone preference (ET/CT/UTC, M3-08)
	Page         string     // Current page name for aria-current (UI-10)
}

// stampInfo is the data for the freshness stamp template (UI-10).
type stampInfo struct {
	Badge    Badge  // Freshness badge (Fresh/Stale/Unknown/Pinned)
	AsOf     string // Formatted timestamp
	DateTime string // ISO 8601 for <time datetime>
	Age      string // Human-readable age ("14 s ago")
}

// banner is the structured data for the demo/no-round banner (M3-07).
// The HTML markup is in layout.html; these fields are auto-escaped.
type banner struct {
	Show    bool   // true = show demo banner or no-round fallback
	Pinned  bool   // true = demo mode (pinned round), false = no-round fallback
	AsOf    string // formatted timestamp in ET (demo mode only)
	RoundID int64  // round ID (demo mode only)
}

// roundInfo is the round a page reads from, as shown in headers.
type roundInfo struct {
	ID     int64
	Status string
	Pinned bool
	AsOf   string // Formatted in user's timezone (M3-08 Cursor round 2: UI-02 contract)
}

func newRoundInfo(r store.Round, tz string) *roundInfo {
	asOf := r.FinishedAt
	if asOf.IsZero() {
		asOf = r.StartedAt
	}
	return &roundInfo{ID: r.ID, Status: string(r.Status), Pinned: r.Pinned, AsOf: formatTime(asOf, tz)}
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
	// renderError is called without request context; use an empty round and default timezone
	s.render(w, status, "error", errorPage{
		page:    s.pageBase(store.Round{}, http.StatusText(status), "Something went wrong.", TzET, "error"),
		Status:  status,
		Message: message,
	})
}

// renderErrorWithContext writes a full error page with banner from request context.
func (s *Server) renderErrorWithContext(w http.ResponseWriter, r *http.Request, status int, message string) {
	round := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	s.render(w, status, "error", errorPage{
		page:    s.pageBase(round, http.StatusText(status), "Something went wrong.", tz, "error"),
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
	ID                 string
	Venue              string
	MarketID           string
	EventID            string
	Category           string
	Title              string
	Summary            string // UI-21: rules summary (empty if venue doesn't publish)
	Rules              string // Full rules text
	Clarifications     string // UI-21: additional clarifications (empty if venue doesn't publish)
	ClarificationsDate string // UI-21: "clarified YYYY-MM-DD" if available, else empty
	Status             string
	Close              time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
	Quality            []canonical.QualityIssue
	Missing            bool
	Tz                 string // Timezone for formatTime in sub-templates (M3-08)
}

func newInstrumentView(in canonical.Instrument) instrumentView {
	clarDate := ""
	if !in.ClarificationsUpdatedAt.IsZero() {
		clarDate = in.ClarificationsUpdatedAt.Format("2006-01-02")
	}
	return instrumentView{
		ID: in.ID, Venue: string(in.Venue), MarketID: in.VenueMarketID, EventID: in.VenueEventID,
		Category: string(in.Category), Title: in.Title, Summary: in.RulesSummary,
		Rules: in.RulesText, Clarifications: in.Clarifications, ClarificationsDate: clarDate,
		Status: string(in.Status), Close: in.CloseTime, Quality: in.Quality,
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
