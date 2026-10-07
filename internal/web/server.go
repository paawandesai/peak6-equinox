// Package web serves the Equinox HTTP interface: public pages, admin routes
// and the health check.
//
// Routes are split across files so tickets can own them separately:
// public.go (public pages), admin.go (/admin/*) and middleware_*.go.
package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// Config holds the settings the web layer needs.
type Config struct {
	AppEnv                string
	AdminUser             string
	AdminPassword         string
	PublicRateLimitPerMin int
	// MinRouteConfidence is passed to the router (MIN_ROUTE_CONFIDENCE).
	MinRouteConfidence decimal.Decimal
	// EvalSummaryFile is the JSON artifact `equinox eval` writes and /eval
	// renders (EVAL_SUMMARY_FILE). Empty or missing means "no eval yet".
	EvalSummaryFile string
}

// Deps holds the collaborators the web layer uses. Store and Ingester may be
// nil (the health check needs neither); admin routes then report that.
type Deps struct {
	Logger   *slog.Logger
	Store    *store.Store
	Ingester Ingester
	// Now overrides the clock (tests). It must return UTC.
	Now func() time.Time
	// DBSize overrides the database-size query behind the AB-15 growth alert
	// (tests inject a size). Nil means ask the Store.
	DBSize func(ctx context.Context) (int64, error)
}

// Server owns the HTTP routes. Create it with New.
type Server struct {
	cfg        Config
	deps       Deps
	log        *slog.Logger
	limiter    *failureLimiter
	trustProxy bool // read the client IP from X-Forwarded-For (behind Cloud Run)
}

// New returns a Server. A nil Logger is replaced with one that discards output.
func New(cfg Config, deps Deps) *Server {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Server{
		cfg: cfg, deps: deps, log: logger,
		limiter:    newFailureLimiter(deps.Now),
		trustProxy: cfg.AppEnv == "staging" || cfg.AppEnv == "prod",
	}
}

func (s *Server) now() time.Time { return s.deps.Now() }

// Handler returns the root handler with all routes and middleware applied.
func (s *Server) Handler() http.Handler {
	return securityHeaders(s.routes())
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	// Cloud Run's front end reserves paths ending in "z" on *.run.app, so
	// /healthz never reaches the app there; /health is the deployed alias (D-052).
	mux.HandleFunc("GET /health", s.handleHealthz)
	s.publicRoutes(mux)
	s.adminRoutes(mux)
	return mux
}

// handleHealthz reports liveness. It needs no database connection.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.log.Error("writing json response", "err", err)
	}
}
