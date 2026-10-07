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

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// MaxSummaryBytes caps a summary artifact on read; a real one is a few KB.
const MaxSummaryBytes = 1 << 20

// Summary is the read-only JSON artifact `equinox eval --out` writes for the
// web eval page (FR-21, US-06; contract in internal/web/handlers_eval.go,
// M2-07). The page only reads it, so a public request never runs the
// matcher. Field set and names are the contract: no extra fields.
type Summary struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Command     string        `json:"command"`
	RoundID     int64         `json:"round_id"`
	LabelsFile  string        `json:"labels_file"`
	Labels      int           `json:"labels"`
	Method      string        `json:"method"`
	Types       []TypeSummary `json:"types"`
	Notes       []string      `json:"notes"`
}

// TypeSummary is one relationship type's counts. Precision and recall are
// decimal strings in [0,1], or null when undefined.
type TypeSummary struct {
	Type      string           `json:"type"`
	Support   int              `json:"support"`
	TP        int              `json:"tp"`
	FP        int              `json:"fp"`
	FN        int              `json:"fn"`
	Precision *decimal.Decimal `json:"precision"`
	Recall    *decimal.Decimal `json:"recall"`
}

// Summary shapes the report as the artifact. command is the invocation as
// typed, roundID the labels' provenance round, labelsFile the path read and
// labels the number of entries in it (evaluated or not). The notes carry the
// NFR-06 line, the M3 caveat and one line per label not evaluated.
func (r Report) Summary(now time.Time, command string, roundID int64, labelsFile string, labels int) Summary {
	s := Summary{
		GeneratedAt: now.UTC().Truncate(time.Second),
		Command:     command,
		RoundID:     roundID,
		LabelsFile:  labelsFile,
		Labels:      labels,
		Method:      string(r.Method),
		Types:       make([]TypeSummary, 0, len(r.ByType)),
		Notes:       []string{},
	}
	for _, m := range r.ByType {
		s.Types = append(s.Types, TypeSummary{Type: string(m.Type), Support: m.Labelled, TP: m.TP, FP: m.FP, FN: m.FN,
			Precision: optional(m.Precision()), Recall: optional(m.Recall())})
	}
	if labels == 0 {
		s.Notes = append(s.Notes, "no confirmed labels yet: run `equinox golden draft`, confirm pairs in the labels file (Tier 3), then re-run `equinox eval`")
	}
	eq := r.Metrics(canonical.RelEquivalent)
	switch met, measured := r.MeetsTarget(); {
	case !measured:
		s.Notes = append(s.Notes, fmt.Sprintf("equivalent precision not measurable: no equivalent prediction among the labelled pairs (target >= %s, NFR-06)", EquivalentPrecisionTarget))
	case met:
		s.Notes = append(s.Notes, fmt.Sprintf("equivalent precision %s on %d predictions meets the %s target (NFR-06)", fmtRatio(eq.Precision()), eq.Predicted, EquivalentPrecisionTarget))
	default:
		s.Notes = append(s.Notes, fmt.Sprintf("equivalent precision %s on %d predictions is BELOW the %s target (NFR-06)", fmtRatio(eq.Precision()), eq.Predicted, EquivalentPrecisionTarget))
	}
	s.Notes = append(s.Notes, "rules+llm: not evaluated (M3)")
	for _, l := range r.Missing {
		s.Notes = append(s.Notes, fmt.Sprintf("not evaluated: %s / %s (instrument not found)", l.AID, l.BID))
	}
	return s
}

// Validate applies the page's acceptance rules, so a summary that passes
// here is one the page renders.
func (s Summary) Validate() error {
	var errs []error
	if s.GeneratedAt.IsZero() {
		errs = append(errs, errors.New("generated_at is required"))
	}
	if s.Labels < 0 {
		errs = append(errs, errors.New("labels must not be negative"))
	}
	if !canonical.Method(s.Method).Valid() {
		errs = append(errs, fmt.Errorf("method %q is not valid", truncate(s.Method, 64)))
	}
	for i, m := range s.Types {
		if !canonical.RelationshipType(m.Type).Valid() {
			errs = append(errs, fmt.Errorf("types[%d]: unknown relationship type %q", i, truncate(m.Type, 64)))
		}
		if m.Support < 0 || m.TP < 0 || m.FP < 0 || m.FN < 0 {
			errs = append(errs, fmt.Errorf("types[%d]: counts must not be negative", i))
		}
		for _, v := range []struct {
			name string
			d    *decimal.Decimal
		}{{"precision", m.Precision}, {"recall", m.Recall}} {
			if v.d != nil && (v.d.IsNegative() || v.d.GreaterThan(decimal.NewFromInt(1))) {
				errs = append(errs, fmt.Errorf("types[%d]: %s %s is outside [0,1]", i, v.name, v.d))
			}
		}
	}
	return errors.Join(errs...)
}

// WriteSummary writes s as indented JSON to path atomically (temp file +
// rename), creating the directory.
func WriteSummary(path string, s Summary) error {
	if err := s.Validate(); err != nil {
		return fmt.Errorf("writing eval summary: %w", err)
	}
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding eval summary: %w", err)
	}
	body = append(body, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("writing eval summary: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".eval_summary-*.json")
	if err != nil {
		return fmt.Errorf("writing eval summary: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing eval summary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing eval summary: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing eval summary: %w", err)
	}
	return nil
}

// LoadSummary reads an artifact the way the page does: size-capped, unknown
// fields rejected, then validated.
func LoadSummary(path string) (Summary, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return Summary{}, fmt.Errorf("loading eval summary: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxSummaryBytes+1))
	if err != nil {
		return Summary{}, fmt.Errorf("loading eval summary: %w", err)
	}
	if len(raw) > MaxSummaryBytes {
		return Summary{}, fmt.Errorf("loading eval summary: file exceeds %d bytes", MaxSummaryBytes)
	}
	var s Summary
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return Summary{}, fmt.Errorf("loading eval summary: %w", err)
	}
	if err := s.Validate(); err != nil {
		return Summary{}, fmt.Errorf("loading eval summary: %w", err)
	}
	return s, nil
}

// optional turns a (ratio, ok) pair into a nullable decimal.
func optional(d decimal.Decimal, ok bool) *decimal.Decimal {
	if !ok {
		return nil
	}
	return &d
}
