package web

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/collector"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/decisionlog"
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

// adminTmpl is parsed with the layout like other pages (UI-08).
var adminTmpl *template.Template

func init() {
	// Parse admin.html with layout.html
	adminTmpl = template.Must(template.New("").Funcs(templateFuncs).ParseFS(templateFS, "templates/layout.html", "templates/admin.html"))
}

// adminRoutes registers /admin/*. Every path, including unknown ones, goes
// through adminAuth before anything else, and method checks run after it so an
// unauthenticated probe learns nothing.
func (s *Server) adminRoutes(mux *http.ServeMux) {
	index := s.adminAuth(http.HandlerFunc(s.handleAdminIndex))
	mux.Handle("/admin", index)
	mux.Handle("/admin/{$}", index)
	mux.Handle("/admin/ingest", s.adminAuth(http.HandlerFunc(s.handleAdminIngest)))
	mux.Handle("/admin/llm-verify", s.adminAuth(http.HandlerFunc(s.handleAdminLLMVerify)))
	mux.Handle("/admin/pin", s.adminAuth(http.HandlerFunc(s.handleAdminPin)))
	mux.Handle("/admin/unpin", s.adminAuth(http.HandlerFunc(s.handleAdminUnpin)))
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

// Signal represents one health signal (UI-17, HANDOFF.md).
type Signal struct {
	Glyph string // ✓/!/✕/?
	Class string // signal-healthy, signal-warning, signal-degraded, signal-unknown
	Text  string // screen-reader text
}

// llmVerifyRunView represents one LLM verify run for the admin runs table (UI-22).
type llmVerifyRunView struct {
	ID           int64
	StartedAt    string
	Duration     string
	Model        string
	Verified     int
	Downgraded   int
	CacheHits    int
	CallsMade    int
	InputTokens  int64
	OutputTokens int64
	Outcome      string     // "ok", "cap_reached", "error"
	OutcomeBadge Badge      // badge for the outcome
	FinishedAt   *time.Time // nil if still running
}

// HealthRow represents one row in the admin health table (UI-17, HANDOFF.md).
type HealthRow struct {
	Name      string
	Status    Badge    // worst signal as a badge
	Signals   []Signal // individual signals for the row
	Bars      []Bar    // only for Collectors row: recent-round bars
	AriaLabel string   // aria-label for the bars SVG (UI-26)
}

// Bar represents one SVG bar for recent-round visualization (UI-17, HANDOFF.md).
type Bar struct {
	Title  string // tooltip text (e.g. "Round 123: ok")
	Class  string // bar-ok, bar-partial, bar-failed, bar-missing
	Height string // SVG height attribute (100%, 75%, 50%, or just stroke for missing)
}

type adminPage struct {
	page        // UI-08: embed base page (Title, Assets, Round, etc.)
	Now         string
	AppEnv      string
	DBSize      string
	DBThreshold string
	DBAlert     bool    // AB-15: size above dbSizeAlertBytes
	DBSizeRatio float64 // UI-17: DB size / threshold for meter (0.0-1.0+)
	DBError     string
	Collector   collectorStats // NFR-05
	RunningID   int64
	Rounds      []roundView
	Message     string
	IngestReady bool
	PinnedRound *roundView // M3-07: pinned round info for demo mode section
	HasPinned   bool       // M3-07: whether a round is currently pinned
	// UI-08: LLM verifier state (design.md § 4)
	LLMVerifierState       string // "disabled", "ready", "cap_reached"
	LLMVerifierStateChip   string // chip class for the state
	LLMVerifierStateText   string // human-readable state text
	LLMVerifierButtonLabel string // button label
	// UI-17: health rollup
	Health           []HealthRow // health table rows (Collectors, Database, Matcher, Pinned, LLM)
	FreshnessWindows string      // freshness windows line under health table
	// UI-22: LLM verify runs table
	LLMVerifyRuns []llmVerifyRunView // latest N runs, newest first
}

// worstHealth returns the worst health state from a set of signals (UI-17, HANDOFF.md).
// Precedence: degraded > warning > healthy. Unknown with nothing worse stays unknown.
func worstHealth(signals []Signal) HealthState {
	var hasHealthy, hasWarning, hasDegraded, hasUnknown bool
	for _, sig := range signals {
		switch sig.Class {
		case "signal-healthy":
			hasHealthy = true
		case "signal-warning":
			hasWarning = true
		case "signal-degraded":
			hasDegraded = true
		case "signal-unknown":
			hasUnknown = true
		}
	}
	if hasDegraded {
		return HealthDegraded
	}
	if hasWarning {
		return HealthWarning
	}
	if hasHealthy {
		return HealthHealthy
	}
	if hasUnknown {
		return HealthUnknown
	}
	return HealthUnknown
}

// buildHealthRows constructs the health table rows for the admin page (UI-17, HANDOFF.md).
// UI-26: accepts match run data and daily history for Matcher row and 90-day bars.
func buildHealthRows(collector collectorStats, dbGrowth dbGrowth, hasDB bool, dbError string, pinnedRound *roundView, llmState string, _ []store.Round, latestMatchRun *store.MatchRun, dailyHistory []store.DailyRoundSummary, now time.Time) []HealthRow {
	rows := make([]HealthRow, 0, 5)

	// Collectors row: success rate signal + 90-day bars (UI-26)
	var collectorSignals []Signal
	if collector.Meets {
		collectorSignals = append(collectorSignals, Signal{Glyph: "✓", Class: "signal-healthy", Text: "Meets target"})
	} else {
		collectorSignals = append(collectorSignals, Signal{Glyph: "✕", Class: "signal-degraded", Text: "Below target"})
	}
	bars := buildDailyHistoryBars(dailyHistory)

	// Compute aria-label for 90-day bars
	okCount, partialCount, failedCount, missingCount := 0, 0, 0, 0
	for _, bar := range bars {
		switch bar.Class {
		case "ok":
			okCount++
		case "partial":
			partialCount++
		case "failed":
			failedCount++
		case "missing":
			missingCount++
		}
	}
	ariaLabel := fmt.Sprintf("%d days: %d ok, %d partial, %d failed, %d no data", len(bars), okCount, partialCount, failedCount, missingCount)

	rows = append(rows, HealthRow{
		Name:      "Collectors",
		Status:    HealthBadge(worstHealth(collectorSignals)),
		Signals:   collectorSignals,
		Bars:      bars,
		AriaLabel: ariaLabel,
	})

	// Database row: size against threshold (UI-17: Unknown when unavailable)
	var dbSignals []Signal
	if !hasDB || dbError != "" {
		dbSignals = append(dbSignals, Signal{Glyph: "?", Class: "signal-unknown", Text: dbError})
		if dbError == "" {
			dbSignals[0].Text = "Database not configured"
		}
	} else if dbGrowth.Breached {
		dbSignals = append(dbSignals, Signal{Glyph: "✕", Class: "signal-degraded", Text: "Above threshold"})
	} else {
		dbSignals = append(dbSignals, Signal{Glyph: "✓", Class: "signal-healthy", Text: "Below threshold"})
	}
	rows = append(rows, HealthRow{
		Name:    "Database",
		Status:  HealthBadge(worstHealth(dbSignals)),
		Signals: dbSignals,
	})

	// Matcher row: derives signals from match runs (UI-26, HANDOFF.md ◆ 14)
	var matcherSignals []Signal
	if latestMatchRun == nil {
		matcherSignals = append(matcherSignals, Signal{Glyph: "?", Class: "signal-unknown", Text: "No match runs recorded"})
	} else {
		// Signal 1: last run age against a 6-hour window
		age := now.Sub(latestMatchRun.FinishedAt)
		ageWindow := 6 * time.Hour
		if age < ageWindow {
			matcherSignals = append(matcherSignals, Signal{Glyph: "✓", Class: "signal-healthy", Text: fmt.Sprintf("Last run %s ago", formatDurationShort(age))})
		} else if age < 2*ageWindow {
			matcherSignals = append(matcherSignals, Signal{Glyph: "!", Class: "signal-warning", Text: fmt.Sprintf("Last run %s ago", formatDurationShort(age))})
		} else {
			matcherSignals = append(matcherSignals, Signal{Glyph: "✕", Class: "signal-degraded", Text: fmt.Sprintf("Last run %s ago", formatDurationShort(age))})
		}
		// Signal 2: last run outcome (failure is degraded)
		if latestMatchRun.Outcome == store.MatchRunFailed {
			matcherSignals = append(matcherSignals, Signal{Glyph: "✕", Class: "signal-degraded", Text: "Last run failed"})
		}
		// Signal 3: last run duration (warn if > 60s)
		durationS := latestMatchRun.DurationMS / 1000
		if durationS > 60 {
			matcherSignals = append(matcherSignals, Signal{Glyph: "!", Class: "signal-warning", Text: fmt.Sprintf("Last run took %ds", durationS)})
		}
	}
	rows = append(rows, HealthRow{
		Name:    "Matcher",
		Status:  HealthBadge(worstHealth(matcherSignals)),
		Signals: matcherSignals,
	})

	// Pinned round row: pinned vs unpinned
	var pinnedSignals []Signal
	if pinnedRound != nil {
		pinnedSignals = append(pinnedSignals, Signal{Glyph: "✓", Class: "signal-healthy", Text: fmt.Sprintf("Round %d pinned", pinnedRound.ID)})
	} else {
		pinnedSignals = append(pinnedSignals, Signal{Glyph: "?", Class: "signal-unknown", Text: "No round pinned"})
	}
	rows = append(rows, HealthRow{
		Name:    "Pinned round",
		Status:  HealthBadge(worstHealth(pinnedSignals)),
		Signals: pinnedSignals,
	})

	// LLM verifier row: based on existing state logic
	var llmSignals []Signal
	switch llmState {
	case "disabled":
		llmSignals = append(llmSignals, Signal{Glyph: "?", Class: "signal-unknown", Text: "Disabled"})
	case "cap_reached":
		llmSignals = append(llmSignals, Signal{Glyph: "✕", Class: "signal-degraded", Text: "Cap reached"})
	case "ready", "last_run":
		llmSignals = append(llmSignals, Signal{Glyph: "✓", Class: "signal-healthy", Text: "Ready"})
	default:
		llmSignals = append(llmSignals, Signal{Glyph: "?", Class: "signal-unknown", Text: "Unknown"})
	}
	rows = append(rows, HealthRow{
		Name:    "LLM verifier",
		Status:  HealthBadge(worstHealth(llmSignals)),
		Signals: llmSignals,
	})

	return rows
}

// buildBars generates SVG bars for the last 24 rounds (UI-17, HANDOFF.md).
// Full height = ok, 75% = partial, 50% = failed, outline only = missing.
func buildBars(rounds []store.Round) []Bar {
	// Take last 24 rounds
	count := 24
	if len(rounds) > count {
		rounds = rounds[len(rounds)-count:]
	}

	bars := make([]Bar, 0, count)
	for _, r := range rounds {
		var class, height, title string
		switch r.Status {
		case store.RoundOK:
			class = "ok"
			height = "100%"
			title = fmt.Sprintf("Round %d: ok", r.ID)
		case store.RoundPartial:
			class = "partial"
			height = "75%"
			title = fmt.Sprintf("Round %d: partial", r.ID)
		case store.RoundFailed:
			class = "failed"
			height = "50%"
			title = fmt.Sprintf("Round %d: failed", r.ID)
		default:
			class = "missing"
			height = "0"
			title = fmt.Sprintf("Round %d: missing", r.ID)
		}
		bars = append(bars, Bar{Title: title, Class: class, Height: height})
	}

	return bars
}

// buildDailyHistoryBars generates 90-day SVG bars for daily round outcomes (UI-26, HANDOFF.md ◆ 15).
// Statuspage-style bars: gray outline for no-data days, full height = ok, 75% = partial, 50% = failed.
func buildDailyHistoryBars(history []store.DailyRoundSummary) []Bar {
	bars := make([]Bar, 0, 90)
	for _, day := range history {
		if day.Missing {
			// No-data day: gray outline only (height "0")
			bars = append(bars, Bar{
				Title:  fmt.Sprintf("%s: no rounds", day.Date.Format("2006-01-02")),
				Class:  "missing",
				Height: "0",
			})
		} else {
			// Determine worst outcome for the day
			var class, height string
			if day.Failed > 0 {
				class = "failed"
				height = "50%"
			} else if day.Partial > 0 {
				class = "partial"
				height = "75%"
			} else {
				class = "ok"
				height = "100%"
			}
			title := fmt.Sprintf("%s: %d ok, %d partial, %d failed", day.Date.Format("2006-01-02"), day.OK, day.Partial, day.Failed)
			bars = append(bars, Bar{Title: title, Class: class, Height: height})
		}
	}
	return bars
}

// formatDurationShort formats a duration for display in health signals (UI-26).
// Returns compact format: "5s", "10m", "2h", "3d".
func formatDurationShort(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func (s *Server) handleAdminIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// UI-08: admin page now uses layout template
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Get current round for page base
	var currentRound store.Round
	if s.deps.Store != nil {
		if rd, err := s.deps.Store.CurrentRound(ctx); err == nil {
			currentRound = rd
		}
	}

	tz := timezoneFromRequest(r)
	p := adminPage{
		page:        s.pageBase(currentRound, "Admin", "Is data flowing, which round is pinned, and what can the LLM verifier spend?", tz, ""),
		Now:         s.now().Format(time.RFC3339),
		AppEnv:      s.cfg.AppEnv,
		IngestReady: s.deps.Ingester != nil,
	}
	if s.deps.Ingester != nil {
		if id, running := s.deps.Ingester.Running(); running {
			p.RunningID = id
		}
	}
	if rid := r.URL.Query().Get("round"); rid != "" {
		p.Message = "ingest round " + sanitizeID(rid) + " finished; see the table"
	}

	switch growth, err := s.checkDBGrowth(ctx); {
	case errors.Is(err, errNoDBSize):
		p.DBError = "database not configured"
	case err != nil:
		s.log.Error("reading database size", "error", err)
		p.DBError = "database unavailable"
	default:
		p.DBSize, p.DBThreshold, p.DBAlert = growth.Size, growth.Threshold, growth.Breached
		// UI-17: compute DB size ratio for meter
		if growth.Bytes > 0 {
			p.DBSizeRatio = float64(growth.Bytes) / float64(dbSizeAlertBytes)
		}
	}
	p.Collector = collectorSuccess(nil, s.now())
	if s.deps.Store != nil {
		// M3-07: query pinned round directly
		if pinned, err := s.deps.Store.PinnedRound(ctx); err == nil {
			views := roundViews([]store.Round{pinned})
			if len(views) > 0 {
				p.PinnedRound = &views[0]
				p.HasPinned = true
			}
		} else if !isNotFound(err) {
			s.log.Error("querying pinned round", "error", err)
		}

		rounds, err := s.deps.Store.ListRounds(ctx, adminRounds)
		if err != nil {
			s.log.Error("listing rounds", "error", err)
			p.DBError = "database unavailable"
		}
		p.Rounds = roundViews(rounds)
		recent, err := s.deps.Store.ListRoundsSince(ctx, s.now().Add(-collectorWindow), 0)
		if err != nil {
			s.log.Error("listing recent rounds", "error", err)
			p.DBError = "database unavailable"
		}
		p.Collector = collectorSuccess(recent, s.now())

	}

	// UI-17 + UI-26: Build health rows and freshness windows
	var growth dbGrowth
	hasDB := s.deps.Store != nil || s.deps.DBSize != nil
	if p.DBError == "" {
		growth = dbGrowth{Size: p.DBSize, Threshold: p.DBThreshold, Breached: p.DBAlert}
	}

	// Get recent rounds for bars (last 24) - kept for compatibility
	var recentForBars []store.Round
	if s.deps.Store != nil {
		recent, err := s.deps.Store.ListRounds(ctx, 24)
		if err == nil {
			recentForBars = recent
		}
	}

	// UI-26: Get latest match run for Matcher row
	var latestMatchRun *store.MatchRun
	if s.deps.Store != nil {
		if mr, err := s.deps.Store.GetLatestMatchRun(ctx); err == nil {
			latestMatchRun = &mr
		} else if !isNotFound(err) {
			s.log.Error("getting latest match run", "error", err)
		}
	}

	// UI-26: Get 90-day daily history for Collectors bars
	var dailyHistory []store.DailyRoundSummary
	if s.deps.Store != nil {
		if history, err := s.deps.Store.DailyRoundHistory(ctx, 90); err == nil {
			dailyHistory = history
		} else {
			s.log.Error("getting daily round history", "error", err)
		}
	}

	// UI-08: LLM verifier state (design.md § 4)
	// Precedence: disabled > cap_reached > last_run > ready (operator review !59).
	p.LLMVerifierButtonLabel = "Verify equivalent pairs with LLM"
	if s.deps.AnthropicAPIKey == "" {
		p.LLMVerifierState = "disabled"
		p.LLMVerifierStateChip = "chip-muted"
		p.LLMVerifierStateText = "disabled: ANTHROPIC_API_KEY not set"
	} else if s.deps.Store == nil {
		// Default state when Store is nil
		p.LLMVerifierState = "ready"
		p.LLMVerifierStateChip = "chip-ok"
		p.LLMVerifierStateText = "ready: 0 of 200 calls used today"
	} else {
		// Check daily usage first to determine cap_reached (higher precedence than last_run)
		dailyCap := s.deps.LLMDailyCallCap
		if dailyCap <= 0 {
			dailyCap = 200
		}
		calls, _, _, err := s.deps.Store.GetLLMUsage(ctx, s.now())
		if err != nil {
			s.log.Error("getting LLM usage for admin page", "error", err)
			// Fall through to check last run or show ready with 0
		}

		if err == nil && calls >= int64(dailyCap) {
			// Cap reached takes precedence over last run
			p.LLMVerifierState = "cap_reached"
			p.LLMVerifierStateChip = "chip-danger"
			p.LLMVerifierStateText = fmt.Sprintf("cap reached: %d of %d", calls, dailyCap)
		} else if lastRun, ok, err := s.deps.Store.GetLLMVerifyLastRun(ctx); err != nil {
			s.log.Error("getting LLM verify last run for admin page", "error", err)
			// Fall through to show ready state
		} else if ok {
			// Show "last run" state (design.md § 4, issue #40)
			p.LLMVerifierState = "last_run"
			p.LLMVerifierStateChip = "chip-info"
			p.LLMVerifierStateText = fmt.Sprintf("last run: %d verified, %d downgraded, %d cache hits", lastRun.Verified, lastRun.Downgraded, lastRun.CacheHits)
		} else if err == nil {
			// Ready state: show call count if we got usage
			p.LLMVerifierState = "ready"
			p.LLMVerifierStateChip = "chip-ok"
			p.LLMVerifierStateText = fmt.Sprintf("ready: %d of %d calls used today", calls, dailyCap)
		} else {
			// Default ready state if both queries failed
			p.LLMVerifierState = "ready"
			p.LLMVerifierStateChip = "chip-ok"
			p.LLMVerifierStateText = fmt.Sprintf("ready: 0 of %d calls used today", dailyCap)
		}
	}

	// UI-17 + UI-26: build health rows with worst-signal rollup, match runs, and 90-day bars
	p.Health = buildHealthRows(p.Collector, growth, hasDB, p.DBError, p.PinnedRound, p.LLMVerifierState, recentForBars, latestMatchRun, dailyHistory, s.now())
	// UI-17: freshness windows (D-077 threshold 120s, UI-19 will add warn windows)
	// UI-19: show both warn and stale windows per source
	p.FreshnessWindows = fmt.Sprintf("Freshness windows (warn→stale): Kalshi quotes %d→%d s, Polymarket quotes %d→%d s, rounds %d→%d min.",
		int(s.cfg.KalshiWarnAfter.Seconds()),
		int(s.cfg.StaleAfter.Seconds()),
		int(s.cfg.PolymarketWarnAfter.Seconds()),
		int(s.cfg.StaleAfter.Seconds()),
		s.cfg.RoundWarnMinutes,
		s.cfg.StaleRoundWarnMinutes,
	)

	// UI-22: fetch LLM verify runs (latest 20, newest first)
	tz = timezoneFromRequest(r)
	if s.deps.Store != nil {
		if runs, err := s.deps.Store.GetLLMVerifyRuns(ctx, 20); err != nil {
			s.log.Error("getting LLM verify runs for admin page", "error", err)
		} else {
			p.LLMVerifyRuns = make([]llmVerifyRunView, 0, len(runs))
			for _, r := range runs {
				v := llmVerifyRunView{
					ID:           r.ID,
					StartedAt:    formatTime(r.StartedAt, tz),
					Model:        r.Model,
					Verified:     r.Verified,
					Downgraded:   r.Downgraded,
					CacheHits:    r.CacheHits,
					CallsMade:    r.CallsMade,
					InputTokens:  r.InputTokens,
					OutputTokens: r.OutputTokens,
					Outcome:      r.Outcome,
					FinishedAt:   r.FinishedAt,
				}
				if r.FinishedAt != nil {
					v.Duration = r.FinishedAt.Sub(r.StartedAt).Round(time.Second).String()
				}
				// Outcome badge
				switch r.Outcome {
				case "ok":
					v.OutcomeBadge = Badge{Class: "b-ok", Glyph: "✓", Text: "ok"}
				case "cap_reached":
					v.OutcomeBadge = Badge{Class: "b-warn", Glyph: "!", Text: "cap reached"}
				case "error":
					v.OutcomeBadge = Badge{Class: "b-danger", Glyph: "✕", Text: "error"}
				default:
					v.OutcomeBadge = Badge{Class: "b-unknown", Glyph: "?", Text: r.Outcome}
				}
				p.LLMVerifyRuns = append(p.LLMVerifyRuns, v)
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Robots-Tag", "noindex") // UI-08: noindex via header, not meta tag
	// UI-08: render through layout template
	if err := adminTmpl.ExecuteTemplate(w, "layout", p); err != nil {
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

	// AO review issue #4: single-flight lock to prevent concurrent runs
	if !s.llmVerifyMutex.TryLock() {
		s.writeJSON(w, http.StatusConflict, llmVerifyResponse{Error: "LLM verification already in progress"})
		return
	}
	defer s.llmVerifyMutex.Unlock()

	// AO review: extend write deadline to prevent client disconnect mid-run (like ingest handler)
	const llmTimeout = 5 * time.Minute
	_ = http.NewResponseController(w).SetWriteDeadline(s.now().Add(llmTimeout + 10*time.Second))
	// Use WithoutCancel so client disconnect doesn't leave round partly verified
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), llmTimeout)
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
		// UI-08 follow-up (issue #40): redirect to /admin for browser posts
		if wantsHTML(r) {
			http.Redirect(w, r, "/admin", http.StatusSeeOther)
			return
		}
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
	budgetedVerifier := llm.NewBudgetedVerifier(verifier, s.deps.Store, dailyCap, s.log)
	cache := llm.NewPGCache(s.deps.Store, model, llm.PromptVersion)
	cachedVerifier := llm.NewCachedVerifier(budgetedVerifier, cache, model, llm.PromptVersion, s.log)

	// FR-17: log every relationship verdict to decision_log
	dlog := decisionlog.New(s.deps.Store)

	var verified, downgraded, cacheHits int
	var budgetExhausted bool
	var consecutiveErrors int
	const maxConsecutiveErrors = 10 // AO review issue #4: stop after N transport errors

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
		verifyResult, err := cachedVerifier.Verify(ctx, aRec.Instrument, bRec.Instrument, rel)
		// Track cache hits: zero usage means it was served from cache
		if err == nil && verifyResult.Usage.InputTokens == 0 && verifyResult.Usage.OutputTokens == 0 {
			cacheHits++
		}
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

			// AO review issue #4: stop after N consecutive transport errors
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				s.log.Error("stopping llm verification after consecutive errors", "count", consecutiveErrors, "round_id", rd.ID)
				break
			}
			continue
		}

		// Reset error counter on success
		consecutiveErrors = 0

		// Apply the guard (downgrade-only)
		result := llm.Guard(ctx, s.log, rel, verifyResult.Verdict)
		verified++
		if result.Outcome == llm.OutcomeDowngraded {
			downgraded++
		}

		// Update the relationship in the database
		relID, err := s.deps.Store.UpsertRelationship(ctx, result.Relationship)
		if err != nil {
			s.log.Error("updating relationship after llm verification", "error", err, "a_id", rel.AID, "b_id", rel.BID)
			continue
		}

		// FR-17: log the verdict to decision_log (Cursor: actually write each verdict)
		result.Relationship.ID = relID // Need ID for logging
		if _, err := dlog.Relationship(ctx, result.Relationship); err != nil {
			s.log.Warn("logging relationship verdict", "error", err, "relationship_id", relID)
			// Don't fail the verification on log error - verdict is already saved
		}
	}

	// UI-08 follow-up (issue #40): persist the last run summary (operator review: always persist)
	summary := store.LLMVerifyLastRun{
		Verified:   verified,
		Downgraded: downgraded,
		CacheHits:  cacheHits,
		RanAt:      s.now(),
	}
	if err := s.deps.Store.PutLLMVerifyLastRun(ctx, summary); err != nil {
		s.log.Error("storing llm verify last run summary", "error", err)
		// Don't fail the request on summary storage error
	}

	// UI-08 follow-up (issue #40): redirect to /admin for browser form posts (like ingest and unpin)
	if wantsHTML(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	s.writeJSON(w, http.StatusOK, llmVerifyResponse{
		RoundID:         rd.ID,
		Verified:        verified,
		Downgraded:      downgraded,
		BudgetExhausted: budgetExhausted,
	})
}

// handleAdminPin pins a specified round for demo mode (M4-78, FR-22, US-09).
// After pin, all pages use that round's stored data with no live venue calls.
func (s *Server) handleAdminPin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.deps.Store == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Parse round_id from form
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form data", http.StatusBadRequest)
		return
	}

	roundIDStr := r.FormValue("round_id")
	if roundIDStr == "" {
		http.Error(w, "round_id is required", http.StatusBadRequest)
		return
	}

	roundID, err := strconv.ParseInt(roundIDStr, 10, 64)
	if err != nil || roundID <= 0 {
		http.Error(w, "round_id must be a positive integer", http.StatusBadRequest)
		return
	}

	if err := s.deps.Store.PinRound(ctx, roundID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "round not found or not finished ok/partial", http.StatusNotFound)
			return
		}
		s.log.Error("pinning round", "error", err, "round_id", roundID)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}

	s.log.Info("pinned round", "round_id", roundID)

	// Redirect back to the admin page
	if wantsHTML(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleAdminUnpin unpins any pinned round (M3-07, NFR-04). After unpin, the
// next page request uses the latest ok or partial round.
func (s *Server) handleAdminUnpin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.deps.Store == nil {
		http.Error(w, "database not configured", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := s.deps.Store.UnpinRounds(ctx); err != nil {
		s.log.Error("unpinning rounds", "error", err)
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}

	s.log.Info("unpinned all rounds")

	// Redirect back to the admin page
	if wantsHTML(r) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusOK)
}
