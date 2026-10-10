package eval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// MaxBacktestSummaryBytes caps a backtest summary artifact on read; a real one
// is a few KB.
const MaxBacktestSummaryBytes = 1 << 20

// BacktestSummary is the read-only JSON artifact `equinox backtest` writes for
// the web eval page (FR-19, NFR-07; US-06). The page only reads it, so a
// public request never runs the back-test.
type BacktestSummary struct {
	GeneratedAt time.Time `json:"generated_at"`
	Command     string    `json:"command"`
	AsOf        time.Time `json:"as_of"`
	Source      string    `json:"source"` // "fixtures" or "live"
	// Pairs is the total number of cross-venue pairs compared.
	Pairs int `json:"pairs"`
	// Checked is the number of pairs that were checked (equivalent tier,
	// complement, implies).
	Checked int `json:"checked"`
	// Agreed is the number of checked pairs that agreed.
	Agreed int `json:"agreed"`
	// Disagreed is the number of checked pairs that disagreed (including
	// allowlisted exceptions).
	Disagreed int `json:"disagreed"`
	// Mismatches lists the pairs that disagreed and are not allowlisted.
	Mismatches []BacktestMismatch `json:"mismatches"`
	// Exceptions lists the pairs that disagreed but are allowlisted.
	Exceptions []BacktestException `json:"exceptions"`
	// FactIDs are the ids of the facts the matcher had available.
	FactIDs []string `json:"fact_ids"`
	// Passed reports whether NFR-07 gate passed (0 unallowlisted disagreements
	// on the equivalent tier).
	Passed bool `json:"passed"`
}

// BacktestMismatch is one disagreement that is not allowlisted.
type BacktestMismatch struct {
	AID         string                     `json:"a_id"`
	BID         string                     `json:"b_id"`
	ATitle      string                     `json:"a_title"`
	BTitle      string                     `json:"b_title"`
	AOutcome    string                     `json:"a_outcome"`
	BOutcome    string                     `json:"b_outcome"`
	Type        canonical.RelationshipType `json:"type"`
	Confidence  string                     `json:"confidence"`
	Expectation string                     `json:"expectation"`
	Forcing     string                     `json:"forcing,omitempty"`
	Forced      string                     `json:"forced,omitempty"`
}

// BacktestException is one disagreement that is allowlisted.
type BacktestException struct {
	BacktestMismatch
	Reason   string `json:"reason"`
	Decision string `json:"decision"`
}

// BuildBacktestSummary creates a summary from a BacktestReport.
func BuildBacktestSummary(now time.Time, command string, asOf time.Time, source string, r BacktestReport) BacktestSummary {
	s := BacktestSummary{
		GeneratedAt: now.UTC().Truncate(time.Second),
		Command:     command,
		AsOf:        asOf.UTC().Truncate(time.Second),
		Source:      source,
		Pairs:       r.Pairs,
		Checked:     len(r.Checked),
		FactIDs:     r.FactIDs,
		Passed:      r.Passed(),
	}
	for _, p := range r.Checked {
		if p.Agree {
			s.Agreed++
		} else {
			s.Disagreed++
		}
	}
	for _, p := range r.Mismatches(false) {
		s.Mismatches = append(s.Mismatches, BacktestMismatch{
			AID:         p.A.Instrument.ID,
			BID:         p.B.Instrument.ID,
			ATitle:      clipTitle(p.A.Instrument.Title),
			BTitle:      clipTitle(p.B.Instrument.Title),
			AOutcome:    string(p.A.Outcome),
			BOutcome:    string(p.B.Outcome),
			Type:        p.Rel.Type,
			Confidence:  p.Rel.Confidence.String(),
			Expectation: string(p.Expect),
			Forcing:     p.Forcing,
			Forced:      p.Forced,
		})
	}
	for _, p := range r.Exceptions() {
		s.Exceptions = append(s.Exceptions, BacktestException{
			BacktestMismatch: BacktestMismatch{
				AID:         p.A.Instrument.ID,
				BID:         p.B.Instrument.ID,
				ATitle:      clipTitle(p.A.Instrument.Title),
				BTitle:      clipTitle(p.B.Instrument.Title),
				AOutcome:    string(p.A.Outcome),
				BOutcome:    string(p.B.Outcome),
				Type:        p.Rel.Type,
				Confidence:  p.Rel.Confidence.String(),
				Expectation: string(p.Expect),
				Forcing:     p.Forcing,
				Forced:      p.Forced,
			},
			Reason:   p.Exception.Reason,
			Decision: p.Exception.Decision,
		})
	}
	return s
}

// clipTitle shortens a title to 120 characters for the summary.
const maxSummaryTitle = 120

func clipTitle(s string) string {
	if len(s) > maxSummaryTitle {
		return s[:maxSummaryTitle-1] + "…"
	}
	return s
}

// Validate applies the page's acceptance rules.
func (s BacktestSummary) Validate() error {
	var errs []error
	if s.GeneratedAt.IsZero() {
		errs = append(errs, errors.New("generated_at is required"))
	}
	if s.AsOf.IsZero() {
		errs = append(errs, errors.New("as_of is required"))
	}
	if s.Source != "fixtures" && s.Source != "live" {
		errs = append(errs, fmt.Errorf("source must be 'fixtures' or 'live', got %q", truncate(s.Source, 64)))
	}
	if s.Pairs < 0 || s.Checked < 0 || s.Agreed < 0 || s.Disagreed < 0 {
		errs = append(errs, errors.New("counts must not be negative"))
	}
	for i, m := range s.Mismatches {
		if err := canonical.ValidateInstrumentID(m.AID); err != nil {
			errs = append(errs, fmt.Errorf("mismatches[%d]: invalid a_id: %w", i, err))
		}
		if err := canonical.ValidateInstrumentID(m.BID); err != nil {
			errs = append(errs, fmt.Errorf("mismatches[%d]: invalid b_id: %w", i, err))
		}
		if !m.Type.Valid() {
			errs = append(errs, fmt.Errorf("mismatches[%d]: unknown relationship type %q", i, truncate(string(m.Type), 64)))
		}
		if !canonical.Outcome(m.AOutcome).Valid() {
			errs = append(errs, fmt.Errorf("mismatches[%d]: invalid a_outcome %q", i, truncate(m.AOutcome, 64)))
		}
		if !canonical.Outcome(m.BOutcome).Valid() {
			errs = append(errs, fmt.Errorf("mismatches[%d]: invalid b_outcome %q", i, truncate(m.BOutcome, 64)))
		}
	}
	for i, e := range s.Exceptions {
		if err := canonical.ValidateInstrumentID(e.AID); err != nil {
			errs = append(errs, fmt.Errorf("exceptions[%d]: invalid a_id: %w", i, err))
		}
		if err := canonical.ValidateInstrumentID(e.BID); err != nil {
			errs = append(errs, fmt.Errorf("exceptions[%d]: invalid b_id: %w", i, err))
		}
		if !e.Type.Valid() {
			errs = append(errs, fmt.Errorf("exceptions[%d]: unknown relationship type %q", i, truncate(string(e.Type), 64)))
		}
		if e.Reason == "" {
			errs = append(errs, fmt.Errorf("exceptions[%d]: reason is required", i))
		}
		if e.Decision == "" {
			errs = append(errs, fmt.Errorf("exceptions[%d]: decision is required", i))
		}
	}
	return errors.Join(errs...)
}

// WriteBacktestSummary writes s as indented JSON to path atomically (temp file +
// rename), creating the directory.
func WriteBacktestSummary(path string, s BacktestSummary) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("writing backtest summary: %w", err)
	}
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding backtest summary: %w", err)
	}
	body = append(body, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("writing backtest summary: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".backtest_summary-*.json")
	if err != nil {
		return fmt.Errorf("writing backtest summary: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing backtest summary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing backtest summary: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing backtest summary: %w", err)
	}
	return nil
}

// LoadBacktestSummary reads an artifact the way the page does: size-capped,
// unknown fields rejected, then validated.
func LoadBacktestSummary(path string) (BacktestSummary, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return BacktestSummary{}, fmt.Errorf("loading backtest summary: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxBacktestSummaryBytes+1))
	if err != nil {
		return BacktestSummary{}, fmt.Errorf("loading backtest summary: %w", err)
	}
	if len(raw) > MaxBacktestSummaryBytes {
		return BacktestSummary{}, fmt.Errorf("loading backtest summary: file exceeds %d bytes", MaxBacktestSummaryBytes)
	}
	var s BacktestSummary
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return BacktestSummary{}, fmt.Errorf("loading backtest summary: %w", err)
	}
	if err := s.Validate(); err != nil {
		return BacktestSummary{}, fmt.Errorf("loading backtest summary: %w", err)
	}
	return s, nil
}
