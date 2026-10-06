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
	DBError     string
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
	if s.deps.Store == nil {
		page.DBError = "database not configured"
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if size, err := s.deps.Store.DatabaseSize(ctx); err != nil {
			s.log.Error("reading database size", "error", err)
			page.DBError = "database unavailable"
		} else {
			page.DBSize = humanBytes(size)
		}
		rounds, err := s.deps.Store.ListRounds(ctx, adminRounds)
		if err != nil {
			s.log.Error("listing rounds", "error", err)
			page.DBError = "database unavailable"
		}
		page.Rounds = roundViews(rounds)
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
