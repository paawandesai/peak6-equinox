package eval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
// M2-07, extended by UI-23/D-104). The page only reads it, so a public request
// never runs the matcher. Field set and names are the contract: no extra fields
// beyond those listed here. UI-23 fields are optional so old summaries still load.
type Summary struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Command     string        `json:"command"`
	RoundID     int64         `json:"round_id"`
	LabelsFile  string        `json:"labels_file"`
	Labels      int           `json:"labels"`
	Method      string        `json:"method"`
	Types       []TypeSummary `json:"types"`
	Notes       []string      `json:"notes"`
	// UI-23: optional fields, omitted when empty
	Confusion    *ConfusionMatrix  `json:"confusion,omitempty"`     // counts and pair ids per (label, predicted)
	Thresholds   []ThresholdMetric `json:"thresholds,omitempty"`    // P/R/TP/FP for equivalent at confidence cut-offs
	LabelsSHA256 string            `json:"labels_sha256,omitempty"` // golden-set version (hex)
	Previous     *PreviousRun      `json:"previous,omitempty"`      // previous summary's timestamp, hash and per-type P/R
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

// ConfusionMatrix is counts and pair ids per (label, predicted) cell. UI-23.
type ConfusionMatrix struct {
	Cells map[string]ConfusionCell `json:"cells"` // key: "label:predicted"
}

// ConfusionCell is one (label, predicted) cell's count and pair ids.
type ConfusionCell struct {
	Count   int      `json:"count"`
	PairIDs []string `json:"pair_ids,omitempty"` // "a_id / b_id" in file order
}

// ThresholdMetric is P/R/TP/FP for equivalent at one confidence threshold. UI-23.
type ThresholdMetric struct {
	Threshold decimal.Decimal  `json:"threshold"` // confidence cut-off
	Precision *decimal.Decimal `json:"precision"` // null when no predictions above threshold
	Recall    *decimal.Decimal `json:"recall"`
	TP        int              `json:"tp"`
	FP        int              `json:"fp"`
}

// PreviousRun holds the previous summary's timestamp, labels hash and per-type P/R. UI-23.
type PreviousRun struct {
	GeneratedAt  time.Time                     `json:"generated_at"`
	LabelsSHA256 string                        `json:"labels_sha256"`
	Types        map[string]PreviousTypeMetric `json:"types"` // key: type string
}

// PreviousTypeMetric is one type's P/R from the previous run.
type PreviousTypeMetric struct {
	Precision *decimal.Decimal `json:"precision"`
	Recall    *decimal.Decimal `json:"recall"`
}

// Summary shapes the report as the artifact. command is the invocation as
// typed, roundID the labels' provenance round, labelsFile the path read and
// labels the number of entries in it (evaluated or not). The notes carry the
// NFR-06 line, the M3 caveat and one line per label not evaluated.
// UI-23: labelsHash is the SHA256 of the labels file (hex), and prevPath is
// where to look for the previous summary to record deltas (empty: no previous).
func (r Report) Summary(now time.Time, command string, roundID int64, labelsFile string, labels int, labelsHash string, prevPath string) Summary {
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
	// UI-23: add optional fields
	if len(r.Outcomes) > 0 {
		s.Confusion = buildConfusionMatrix(r.Outcomes)
		s.Thresholds = buildThresholds(r.Outcomes)
	}
	s.LabelsSHA256 = labelsHash
	if prevPath != "" {
		if prev, err := LoadSummary(prevPath); err == nil {
			s.Previous = &PreviousRun{
				GeneratedAt:  prev.GeneratedAt,
				LabelsSHA256: prev.LabelsSHA256,
				Types:        make(map[string]PreviousTypeMetric),
			}
			for _, t := range prev.Types {
				s.Previous.Types[t.Type] = PreviousTypeMetric{Precision: t.Precision, Recall: t.Recall}
			}
		}
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

// buildConfusionMatrix computes counts and pair ids per (label, predicted). UI-23.
func buildConfusionMatrix(outcomes []Outcome) *ConfusionMatrix {
	cells := make(map[string]ConfusionCell)
	for _, o := range outcomes {
		key := confusionKey(o.Label.Label, o.Predicted)
		cell := cells[key]
		cell.Count++
		pairID := fmt.Sprintf("%s / %s", o.Label.AID, o.Label.BID)
		cell.PairIDs = append(cell.PairIDs, pairID)
		cells[key] = cell
	}
	if len(cells) == 0 {
		return nil
	}
	return &ConfusionMatrix{Cells: cells}
}

// confusionKey returns the map key for a (label, predicted) cell.
func confusionKey(label canonical.RelationshipType, predicted canonical.RelationshipType) string {
	return fmt.Sprintf("%s:%s", label, predicted)
}

// buildThresholds computes P/R/TP/FP for equivalent at confidence cut-offs,
// including the 0.90 floor. Outcomes must include confidence scores. UI-23.
func buildThresholds(outcomes []Outcome) []ThresholdMetric {
	// Define thresholds: 0.70, 0.75, 0.80, 0.85, 0.90, 0.95, 1.00
	thresholds := []decimal.Decimal{
		decimal.RequireFromString("0.70"),
		decimal.RequireFromString("0.75"),
		decimal.RequireFromString("0.80"),
		decimal.RequireFromString("0.85"),
		decimal.RequireFromString("0.90"),
		decimal.RequireFromString("0.95"),
		decimal.RequireFromString("1.00"),
	}
	// Count labelled equivalents
	equivalentLabels := 0
	for _, o := range outcomes {
		if o.Label.Label == canonical.RelEquivalent {
			equivalentLabels++
		}
	}
	var results []ThresholdMetric
	for _, thresh := range thresholds {
		tp, fp := 0, 0
		for _, o := range outcomes {
			// Filter: only count predictions where type is equivalent AND confidence >= threshold
			if o.Predicted == canonical.RelEquivalent && o.Confidence.GreaterThanOrEqual(thresh) {
				if o.Label.Label == canonical.RelEquivalent {
					tp++
				} else {
					fp++
				}
			}
		}
		var prec, rec *decimal.Decimal
		if tp+fp > 0 {
			p := decimal.NewFromInt(int64(tp)).DivRound(decimal.NewFromInt(int64(tp+fp)), 4)
			prec = &p
		}
		if equivalentLabels > 0 {
			r := decimal.NewFromInt(int64(tp)).DivRound(decimal.NewFromInt(int64(equivalentLabels)), 4)
			rec = &r
		}
		results = append(results, ThresholdMetric{
			Threshold: thresh,
			Precision: prec,
			Recall:    rec,
			TP:        tp,
			FP:        fp,
		})
	}
	return results
}

// HashLabelsFile computes SHA256 of the labels file for version tracking. UI-23.
func HashLabelsFile(path string) (string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("hashing labels file: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hashing labels file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// RotatePreviousSummary saves the current summary as .prev.json before writing
// the new one. Returns the previous summary path or empty if rotation failed. UI-23.
func RotatePreviousSummary(currentPath string) string {
	if currentPath == "" {
		return ""
	}
	prevPath := filepath.Join(filepath.Dir(currentPath), filepath.Base(currentPath[:len(currentPath)-len(filepath.Ext(currentPath))])+".prev.json")
	// Try to copy current to .prev
	if current, err := os.ReadFile(filepath.Clean(currentPath)); err == nil {
		if err := os.WriteFile(prevPath, current, 0o640); err == nil {
			return prevPath
		}
	}
	return ""
}

// optional turns a (ratio, ok) pair into a nullable decimal.
func optional(d decimal.Decimal, ok bool) *decimal.Decimal {
	if !ok {
		return nil
	}
	return &d
}
