// Package web serves the Equinox HTTP interface: public pages, admin routes
// and the health check.
//
// Routes are split across files so tickets can own them separately:
// public.go (public pages), admin.go (/admin/*) and middleware_*.go.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
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
	// StaleAfter is the staleness threshold (STALE_AFTER_SECONDS).
	StaleAfter time.Duration
	// SkewWarn is the skew warning threshold (SKEW_WARN_SECONDS).
	SkewWarn time.Duration
	// SkewMax is the skew exclusion threshold (SKEW_MAX_SECONDS).
	SkewMax time.Duration
	// MinBookDepth is the minimum book depth (MIN_BOOK_DEPTH).
	MinBookDepth decimal.Decimal
	// BasisPenaltyMult is the basis-risk penalty multiplier (BASIS_PENALTY_MULTIPLIER).
	BasisPenaltyMult decimal.Decimal
	// StaleRoundWarnMinutes is the stale-round warning threshold (STALE_ROUND_WARN_MINUTES).
	StaleRoundWarnMinutes int
}

// Deps holds the collaborators the web layer uses. Store and Ingester may be
// nil (the health check needs neither); admin routes then report that.
type Deps struct {
	Logger   *slog.Logger
	Store    *store.Store
	Ingester Ingester
	// AnthropicAPIKey is the key for LLM verification. Empty means disabled.
	AnthropicAPIKey string
	// LLMModel is the Anthropic model ID for verification (default claude-haiku-4-5-20251001).
	LLMModel string
	// LLMDailyCallCap is the maximum LLM calls per UTC day (default 200).
	LLMDailyCallCap int
	// LLMMaxOutputTokens caps each LLM response (default 400).
	LLMMaxOutputTokens int
	// Now overrides the clock (tests). It must return UTC.
	Now func() time.Time
	// DBSize overrides the database-size query behind the AB-15 growth alert
	// (tests inject a size). Nil means ask the Store.
	DBSize func(ctx context.Context) (int64, error)
}

// Server owns the HTTP routes. Create it with New.
type Server struct {
	cfg               Config
	deps              Deps
	log               *slog.Logger
	limiter           *failureLimiter
	publicRateLimiter *publicRateLimiter
	trustProxy        bool       // read the client IP from X-Forwarded-For (behind Cloud Run)
	llmVerifyMutex    sync.Mutex // single-flight lock for /admin/llm-verify (AO review issue #4)
}

// New returns a Server. A nil Logger is replaced with one that discards output.
// Zero-valued eligibility thresholds are filled from internal/config defaults
// (production always loads them via config.Load).
func New(cfg Config, deps Deps) *Server {
	logger := deps.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	// Fill zero eligibility thresholds with defaults (test/e2e code may not populate).
	// Hardcode defaults here to avoid internal/web → internal/config dependency.
	if cfg.StaleAfter == 0 {
		cfg.StaleAfter = 120 * time.Second
	}
	if cfg.SkewWarn == 0 {
		cfg.SkewWarn = 5 * time.Second
	}
	if cfg.SkewMax == 0 {
		cfg.SkewMax = 30 * time.Second
	}
	if cfg.MinBookDepth.IsZero() {
		cfg.MinBookDepth = decimal.RequireFromString("1")
	}
	// BASIS_PENALTY_MULTIPLIER: config.Load rejects zero/negative, so this default
	// is only for direct test construction (not production).
	if cfg.BasisPenaltyMult.IsZero() {
		cfg.BasisPenaltyMult = decimal.RequireFromString("0.01")
	}
	return &Server{
		cfg: cfg, deps: deps, log: logger,
		limiter:           newFailureLimiter(deps.Now),
		publicRateLimiter: newPublicRateLimiter(cfg.PublicRateLimitPerMin, deps.Now, logger),
		trustProxy:        cfg.AppEnv == "staging" || cfg.AppEnv == "prod",
	}
}

func (s *Server) now() time.Time { return s.deps.Now() }

// staleRoundWarning returns a warning message if the round snapshot is older
// than StaleRoundWarnMinutes, or an empty string if the round is fresh (NFR-09, D-077).
func (s *Server) staleRoundWarning(r store.Round) string {
	if s.cfg.StaleRoundWarnMinutes <= 0 {
		return ""
	}
	snapshot := r.SnapshotAt()
	if snapshot.IsZero() {
		return ""
	}
	age := s.now().Sub(snapshot)
	ageMinutes := int(age.Minutes())
	threshold := s.cfg.StaleRoundWarnMinutes
	if ageMinutes >= threshold {
		return fmt.Sprintf("Data is %d minutes old.", ageMinutes)
	}
	return ""
}

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
