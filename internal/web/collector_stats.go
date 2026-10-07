package web

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// NFR-05: the collector must complete at least 95% of its 5-minute rounds
// over any trailing 24 h window, measured from fetch_rounds.
const (
	collectorWindow  = 24 * time.Hour
	collectorCadence = 5 * time.Minute
	// collectorTargetBP is the target success rate in basis points (95.00%).
	collectorTargetBP = 9500
)

// collectorStats is the trailing-window success rate shown on /admin.
type collectorStats struct {
	Since     string
	Window    string
	Cadence   string
	Expected  int // ticks the schedule owes the window (window / cadence)
	Observed  int // rounds started in the window
	OK        int
	Partial   int
	Failed    int
	Running   int
	Missing   int // expected ticks with no round at all
	Succeeded int // ok + partial
	Attempts  int // the denominator: max(expected, finished rounds)
	RateBP    int // success rate in basis points
	Rate      string
	Target    string
	Meets     bool
}

// collectorSuccess scores rounds (any order; rounds outside the window are
// ignored) against the schedule. A tick is a success when its round is ok
// or partial; failed rounds and missing ticks both count against the rate,
// so a Scheduler outage shows up as missing ticks (NFR-05). A round still
// running is neither a success nor a failure and is left out of the
// denominator. Manual rounds beyond the schedule raise the denominator so
// the rate never exceeds 100%.
func collectorSuccess(rounds []store.Round, now time.Time) collectorStats {
	since := now.Add(-collectorWindow)
	st := collectorStats{
		Since: formatUTC(since), Window: collectorWindow.String(), Cadence: collectorCadence.String(),
		Expected: int(collectorWindow / collectorCadence),
		Target:   formatBP(collectorTargetBP),
	}
	for _, r := range rounds {
		if r.StartedAt.Before(since) || r.StartedAt.After(now) {
			continue
		}
		st.Observed++
		switch r.Status {
		case store.RoundOK:
			st.OK++
		case store.RoundPartial:
			st.Partial++
		case store.RoundFailed:
			st.Failed++
		default:
			st.Running++
		}
	}
	st.Succeeded = st.OK + st.Partial
	finished := st.Observed - st.Running
	st.Attempts = st.Expected
	if finished > st.Attempts {
		st.Attempts = finished
	}
	if st.Observed < st.Expected {
		st.Missing = st.Expected - st.Observed
	}
	if st.Attempts > 0 {
		st.RateBP = st.Succeeded * 10000 / st.Attempts
	}
	st.Rate = formatBP(st.RateBP)
	st.Meets = st.RateBP >= collectorTargetBP
	return st
}

// formatBP renders basis points as a percentage with two decimals.
func formatBP(bp int) string {
	return strconv.Itoa(bp/100) + "." + fmt.Sprintf("%02d", bp%100) + "%"
}

// AB-15: unbounded collector growth. Above this estimated database size an
// alert is logged and shown on /admin next to the size. 5 GiB, read as the
// threat model's "5 GB"; the Cloud SQL disk is 10 GB (D-053).
const dbSizeAlertBytes int64 = 5 << 30

// dbGrowth is the size check's result for the admin page.
type dbGrowth struct {
	Bytes     int64
	Size      string
	Threshold string
	Breached  bool
}

// errNoDBSize is returned when neither a Store nor a DBSize override exists.
var errNoDBSize = errors.New("database size is not available")

// checkDBGrowth measures the database and, above the threshold, emits one
// structured error-level log entry (the alert) and reports the breach. It is
// called on every admin page view and after every ingest round, so the alert
// repeats every 5 minutes while the breach lasts.
func (s *Server) checkDBGrowth(ctx context.Context) (dbGrowth, error) {
	size := s.deps.DBSize
	if size == nil {
		if s.deps.Store == nil {
			return dbGrowth{}, errNoDBSize
		}
		size = s.deps.Store.DatabaseSize
	}
	n, err := size(ctx)
	if err != nil {
		return dbGrowth{}, fmt.Errorf("checking database growth: %w", err)
	}
	g := dbGrowth{Bytes: n, Size: humanBytes(n), Threshold: humanBytes(dbSizeAlertBytes), Breached: n > dbSizeAlertBytes}
	if g.Breached {
		s.log.Error("database size above growth threshold",
			"alert", "db_growth", "abuse_case", "AB-15",
			"size_bytes", n, "threshold_bytes", dbSizeAlertBytes, "size", g.Size, "threshold", g.Threshold)
	}
	return g, nil
}
