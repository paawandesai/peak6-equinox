package web

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/collector"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/llm"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// Ingester runs fetch rounds; *collector.Collector implements it.
type Ingester interface {
	Run(ctx context.Context) (collector.Result, error)
	Running() (int64, bool)
}

const (
	// ingestTimeout bounds one round triggered over HTTP. The response
	// deadline is extended to match, since the server's default write timeout
	// is shorter than a round.
	ingestTimeout = 5 * time.Minute
	adminRounds   = 20
)

//go:embed templates/admin.html
var adminHTML string

var adminTmpl = template.Must(template.New("admin").Parse(adminHTML))

// adminRoutes registers /admin/*. Every path, including unknown ones, goes
// through adminAuth before anything else, and method checks run after it so an
// unauthenticated probe learns nothing.
func (s *Server) adminRoutes(mux *http.ServeMux) {
	index := s.adminAuth(http.HandlerFunc(s.handleAdminIndex))
	mux.Handle("/admin", index)
	mux.Handle("/admin/{$}", index)
	mux.Handle("/admin/ingest", s.adminAuth(http.HandlerFunc(s.handleAdminIngest)))
	mux.Handle("/admin/llm-verify", s.adminAuth(http.HandlerFunc(s.handleAdminLLMVerify)))
	mux.Handle("/admin/", s.adminAuth(http.NotFoundHandler()))
}

// roundView is one fetch_rounds row prepared for the template.
type roundView struct {
	ID       int64
	Started  string
	Duration string
	Status   string
	Pinned   bool
	Venues   []venueView
}

type venueView struct {
	Venue       string
	OK          bool
	Instruments int
	Quotes      int
	Duration    string
	Error       string
}

type adminPage struct {
	Now         string
	AppEnv      string
	DBSize      string
	DBThreshold string
	DBAlert     bool // AB-15: size above dbSizeAlertBytes
	DBError     string
	Collector   collectorStats // NFR-05
	RunningID   int64
	Rounds      []roundView
	Message     string
	IngestReady bool
}

func (s *Server) handleAdminIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	page := adminPage{Now: s.now().Format(time.RFC3339), AppEnv: s.cfg.AppEnv, IngestReady: s.deps.Ingester != nil}
	if s.deps.Ingester != nil {
		if id, running := s.deps.Ingester.Running(); running {
			page.RunningID = id
		}
	}
	if rid := r.URL.Query().Get("round"); rid != "" {
		page.Message = "ingest round " + sanitizeID(rid) + " finished; see the table"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	switch growth, err := s.checkDBGrowth(ctx); {
	case errors.Is(err, errNoDBSize):
		page.DBError = "database not configured"
	case err != nil:
		s.log.Error("reading database size", "error", err)
		page.DBError = "database unavailable"
	default:
		page.DBSize, page.DBThreshold, page.DBAlert = growth.Size, growth.Threshold, growth.Breached
	}
	page.Collector = collectorSuccess(nil, s.now())
	if s.deps.Store != nil {
		rounds, err := s.deps.Store.ListRounds(ctx, adminRounds)
		if err != nil {
			s.log.Error("listing rounds", "error", err)
			page.DBError = "database unavailable"
		}
		page.Rounds = roundViews(rounds)
		recent, err := s.deps.Store.ListRoundsSince(ctx, s.now().Add(-collectorWindow), 0)
		if err != nil {
			s.log.Error("listing recent rounds", "error", err)
			page.DBError = "database unavailable"
		}
		page.Collector = collectorSuccess(recent, s.now())
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminTmpl.Execute(w, page); err != nil {
		s.log.Error("rendering admin page", "error", err)
	}
}

// ingestResponse is the JSON body of POST /admin/ingest.
type ingestResponse struct {
	RoundID    int64                            `json:"round_id,omitempty"`
	Status     string                           `json:"status,omitempty"`
	StartedAt  *time.Time                       `json:"started_at,omitempty"`
	FinishedAt *time.Time                       `json:"finished_at,omitempty"`
	DurationMS int64                            `json:"duration_ms,omitempty"`
	Venues     map[canonical.Venue]venueSummary `json:"venues,omitempty"`
	Error      string                           `json:"error,omitempty"`
}

type venueSummary struct {
	OK          bool   `json:"ok"`
	Instruments int    `json:"instruments"`
	Quotes      int    `json:"quotes"`
	DurationMS  int64  `json:"duration_ms"`
	Error       string `json:"error,omitempty"`
}

// handleAdminIngest runs one fetch round (FR-04: Cloud Scheduler calls this
// every 5 minutes). 200 for an ok or partial round, 502 when every venue
// failed, 409 while another round is running. A browser form (Accept:
// text/html) is sent back to the admin page instead of JSON.
func (s *Server) handleAdminIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeJSON(w, http.StatusMethodNotAllowed, ingestResponse{Error: "use POST"})
		return
	}
	if s.deps.Ingester == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, ingestResponse{Error: "ingest is not configured"})
		return
	}
	// The round outlives a client that hangs up, and the response deadline
	// must cover a full round.
	_ = http.NewResponseController(w).SetWriteDeadline(s.now().Add(ingestTimeout + 10*time.Second))
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), ingestTimeout)
	defer cancel()
	res, err := s.deps.Ingester.Run(ctx)
	switch {
	case errors.Is(err, collector.ErrRoundInProgress):
		id, _ := s.deps.Ingester.Running()
		s.writeJSON(w, http.StatusConflict, ingestResponse{RoundID: id, Status: string(store.RoundRunning), Error: err.Error()})
		return
	case err != nil:
		s.log.Error("ingest failed", "error", err, "round_id", res.RoundID)
		s.writeJSON(w, http.StatusInternalServerError, ingestResponse{RoundID: res.RoundID, Status: string(res.Status), Error: "ingest failed; see the server log"})
		return
	}
	// AB-15: every round adds data; measure after each one so the growth
	// alert fires on the Scheduler's cadence, not only when someone looks.
	if _, err := s.checkDBGrowth(ctx); err != nil && !errors.Is(err, errNoDBSize) {
		s.log.Error("checking database growth after ingest", "error", err, "round_id", res.RoundID)
	}
	if wantsHTML(r) {
		http.Redirect(w, r, fmt.Sprintf("/admin?round=%d", res.RoundID), http.StatusSeeOther)
		return
	}
	status := http.StatusOK
	if res.Status == store.RoundFailed {
		status = http.StatusBadGateway
	}
	s.writeJSON(w, status, ingestJSON(res))
}

func ingestJSON(res collector.Result) ingestResponse {
	started, finished := res.StartedAt, res.FinishedAt
	out := ingestResponse{RoundID: res.RoundID, Status: string(res.Status), StartedAt: &started, FinishedAt: &finished,
		DurationMS: res.Duration().Milliseconds(), Venues: make(map[canonical.Venue]venueSummary, len(res.Venues))}
	for _, v := range res.Venues {
		out.Venues[v.Venue] = venueSummary{OK: v.OK, Instruments: v.Instruments, Quotes: v.Quotes, DurationMS: v.DurationMS, Error: v.Error}
	}
	return out
}

// wantsHTML is true for a browser form post, which prefers an HTML page over JSON.
func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html") && !strings.Contains(accept, "application/json")
}

func roundViews(rounds []store.Round) []roundView {
	out := make([]roundView, 0, len(rounds))
	for _, rd := range rounds {
		rv := roundView{ID: rd.ID, Started: rd.StartedAt.Format("2006-01-02 15:04:05"), Status: string(rd.Status), Pinned: rd.Pinned}
		if !rd.FinishedAt.IsZero() {
			rv.Duration = rd.FinishedAt.Sub(rd.StartedAt).Round(100 * time.Millisecond).String()
		}
		for _, v := range canonical.Venues() {
			vs, ok := rd.VenueStatus[v]
			if !ok {
				continue
			}
			rv.Venues = append(rv.Venues, venueView{Venue: string(v), OK: vs.OK, Instruments: vs.Instruments, Quotes: vs.Quotes,
				Duration: (time.Duration(vs.DurationMS) * time.Millisecond).Round(100 * time.Millisecond).String(), Error: vs.Error})
		}
		out = append(out, rv)
	}
	return out
}

// humanBytes renders a byte count as KB/MB/GB for the admin page.
func humanBytes(n int64) string {
	const unit = 1024
	switch {
	case n >= unit*unit*unit:
		return fmt.Sprintf("%.1f GB", float64(n)/(unit*unit*unit))
	case n >= unit*unit:
		return fmt.Sprintf("%.1f MB", float64(n)/(unit*unit))
	case n >= unit:
		return fmt.Sprintf("%.1f KB", float64(n)/unit)
	}
	return fmt.Sprintf("%d B", n)
}

// sanitizeID keeps only digits of a user-supplied round ID for display.
func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' && b.Len() < 19 {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "?"
	}
	return b.String()
}

// llmVerifyResponse is the JSON body of POST /admin/llm-verify.
type llmVerifyResponse struct {
	RoundID         int64  `json:"round_id,omitempty"`
	Verified        int    `json:"verified"`
	Downgraded      int    `json:"downgraded"`
	BudgetExhausted bool   `json:"budget_exhausted"`
	Error           string `json:"error,omitempty"`
}

// handleAdminLLMVerify runs the LLM verifier on all rules-equivalent pairs from
// the current or pinned round. It updates relationships.method='rules+llm' and
// downgrades relationships.type when the LLM says so, logs each verdict to
// decision_log, and returns a summary. If ANTHROPIC_API_KEY is missing, returns
// HTTP 503.
func (s *Server) handleAdminLLMVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		s.writeJSON(w, http.StatusMethodNotAllowed, llmVerifyResponse{Error: "use POST"})
		return
	}
	if s.deps.Store == nil {
		s.writeJSON(w, http.StatusServiceUnavailable, llmVerifyResponse{Error: "database not configured"})
		return
	}
	if s.deps.AnthropicAPIKey == "" {
		s.writeJSON(w, http.StatusServiceUnavailable, llmVerifyResponse{Error: "LLM verifier unavailable: ANTHROPIC_API_KEY not set"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()

	// Get the current or pinned round
	rd, err := s.deps.Store.CurrentRound(ctx)
	if err != nil {
		s.log.Error("getting current round for llm verification", "error", err)
		s.writeJSON(w, http.StatusInternalServerError, llmVerifyResponse{Error: "database unavailable"})
		return
	}

	// Get all rules-equivalent pairs
	candidates, err := s.deps.Store.ListRelationships(ctx, store.RelationshipFilter{
		RoundID: rd.ID,
		Limit:   10000,
	})
	if err != nil {
		s.log.Error("listing relationships for llm verification", "error", err, "round_id", rd.ID)
		s.writeJSON(w, http.StatusInternalServerError, llmVerifyResponse{Error: "database unavailable"})
		return
	}

	// Filter to rules-only equivalent or equivalent_given_facts
	var pairs []canonical.Relationship
	for _, rel := range candidates {
		if rel.Method == canonical.MethodRules && (rel.Type == canonical.RelEquivalent || rel.Type == canonical.RelEquivalentGivenFacts) {
			pairs = append(pairs, rel)
		}
	}

	if len(pairs) == 0 {
		s.writeJSON(w, http.StatusOK, llmVerifyResponse{RoundID: rd.ID, Verified: 0, Downgraded: 0})
		return
	}

	// Build the verifier stack: Anthropic client -> cache -> budget
	model := s.deps.LLMModel
	if model == "" {
		model = "claude-haiku-4-5-20251001"
	}
	maxTokens := s.deps.LLMMaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = 400
	}
	dailyCap := s.deps.LLMDailyCallCap
	if dailyCap <= 0 {
		dailyCap = 200
	}

	// Build verifier stack: Anthropic client -> Budget -> Cache
	// Cache wraps budget so cache hits don't burn the daily cap
	client, err := llm.NewAnthropicClient(s.deps.AnthropicAPIKey, model, maxTokens)
	if err != nil {
		s.log.Error("creating anthropic client", "error", err)
		s.writeJSON(w, http.StatusInternalServerError, llmVerifyResponse{Error: "failed to initialize LLM client"})
		return
	}

	verifier := llm.NewVerifier(client)
	budgetedVerifier := llm.NewBudgetedVerifier(verifier, s.deps.Store, dailyCap)
	cache := llm.NewPGCache(s.deps.Store, model, llm.PromptVersion)
	cachedVerifier := llm.NewCachedVerifier(budgetedVerifier, cache, model, llm.PromptVersion)

	var verified, downgraded int
	var budgetExhausted bool

	// Verify each pair and update the relationship
	for _, rel := range pairs {
		// Get instruments
		aRec, err := s.deps.Store.GetInstrument(ctx, rel.AID)
		if err != nil {
			s.log.Error("getting instrument for llm verification", "error", err, "id", rel.AID)
			continue
		}
		bRec, err := s.deps.Store.GetInstrument(ctx, rel.BID)
		if err != nil {
			s.log.Error("getting instrument for llm verification", "error", err, "id", rel.BID)
			continue
		}

		// Verify with the cached+budgeted stack
		verdict, err := cachedVerifier.Verify(ctx, aRec.Instrument, bRec.Instrument, rel)
		if err != nil {
			if errors.Is(err, llm.ErrBudgetExhausted) {
				budgetExhausted = true
				s.log.Warn("llm daily budget exhausted", "round_id", rd.ID, "verified_so_far", verified)
				break
			}
			// Record the error and keep the rules verdict
			result := llm.GuardError(ctx, s.log, rel, err)
			if _, err := s.deps.Store.UpsertRelationship(ctx, result.Relationship); err != nil {
				s.log.Error("updating relationship after llm error", "error", err, "a_id", rel.AID, "b_id", rel.BID)
			}
			continue
		}

		// Apply the guard (downgrade-only)
		result := llm.Guard(ctx, s.log, rel, verdict)
		verified++
		if result.Outcome == llm.OutcomeDowngraded {
			downgraded++
		}

		// Update the relationship in the database
		if _, err := s.deps.Store.UpsertRelationship(ctx, result.Relationship); err != nil {
			s.log.Error("updating relationship after llm verification", "error", err, "a_id", rel.AID, "b_id", rel.BID)
			continue
		}
	}

	s.writeJSON(w, http.StatusOK, llmVerifyResponse{
		RoundID:         rd.ID,
		Verified:        verified,
		Downgraded:      downgraded,
		BudgetExhausted: budgetExhausted,
	})
}
