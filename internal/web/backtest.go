package web

import (
	"errors"
	"io/fs"
	"log/slog"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/eval"
)

// BacktestViewModel holds the view data for backtest results on the eval page.
type BacktestViewModel struct {
	GeneratedAt string
	Command     string
	AsOf        string
	Source      string
	Pairs       int
	Checked     int
	Agreed      int
	Disagreed   int
	Mismatches  []BacktestMismatchRow
	Exceptions  []BacktestExceptionRow
	FactIDs     []string
	Passed      bool
}

// BacktestMismatchRow is one mismatch row.
type BacktestMismatchRow struct {
	AID         string
	BID         string
	ATitle      string
	BTitle      string
	AOutcome    string
	BOutcome    string
	TypeBadge   Badge
	Confidence  string
	Expectation string
	Forcing     string
	Forced      string
}

// BacktestExceptionRow is one exception row.
type BacktestExceptionRow struct {
	BacktestMismatchRow
	Reason   string
	Decision string
}

// LoadBacktestResults loads the backtest summary for the eval page.
// Returns nil if the file doesn't exist (shows "no results yet" state).
// Logs and returns an error for malformed files.
func LoadBacktestResults(path string, logger *slog.Logger) (*BacktestViewModel, error) {
	summary, err := eval.LoadBacktestSummary(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		logger.Error("loading backtest summary failed", "path", path, "error", err)
		return nil, err
	}

	vm := &BacktestViewModel{
		GeneratedAt: summary.GeneratedAt.UTC().Format("2006-01-02 15:04 UTC"),
		Command:     summary.Command,
		AsOf:        summary.AsOf.UTC().Format("2006-01-02 15:04 UTC"),
		Source:      summary.Source,
		Pairs:       summary.Pairs,
		Checked:     summary.Checked,
		Agreed:      summary.Agreed,
		Disagreed:   summary.Disagreed,
		FactIDs:     summary.FactIDs,
		Passed:      summary.Passed,
	}

	for _, m := range summary.Mismatches {
		vm.Mismatches = append(vm.Mismatches, BacktestMismatchRow{
			AID:         m.AID,
			BID:         m.BID,
			ATitle:      m.ATitle,
			BTitle:      m.BTitle,
			AOutcome:    m.AOutcome,
			BOutcome:    m.BOutcome,
			TypeBadge:   TypeBadge(m.Type),
			Confidence:  m.Confidence,
			Expectation: m.Expectation,
			Forcing:     m.Forcing,
			Forced:      m.Forced,
		})
	}

	for _, e := range summary.Exceptions {
		vm.Exceptions = append(vm.Exceptions, BacktestExceptionRow{
			BacktestMismatchRow: BacktestMismatchRow{
				AID:         e.AID,
				BID:         e.BID,
				ATitle:      e.ATitle,
				BTitle:      e.BTitle,
				AOutcome:    e.AOutcome,
				BOutcome:    e.BOutcome,
				TypeBadge:   TypeBadge(e.Type),
				Confidence:  e.Confidence,
				Expectation: e.Expectation,
				Forcing:     e.Forcing,
				Forced:      e.Forced,
			},
			Reason:   e.Reason,
			Decision: e.Decision,
		})
	}

	return vm, nil
}
