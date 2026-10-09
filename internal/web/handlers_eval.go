package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Eval artifact contract (FR-21 eval page, US-06). `equinox eval` (M2-05)
// writes this JSON file; /eval only reads it. The page never runs the
// matcher or the LLM: a public request must not trigger either (D-035).
//
// Example:
//
//	{
//	  "generated_at": "2026-10-07T12:00:00Z",
//	  "command": "equinox eval --round 3",
//	  "round_id": 3,
//	  "labels_file": "testdata/golden/labels.yaml",
//	  "labels": 40,
//	  "method": "rules",
//	  "types": [
//	    {"type": "equivalent", "support": 12, "tp": 12, "fp": 0, "fn": 1, "precision": "1.00", "recall": "0.92"}
//	  ],
//	  "notes": ["3 labels skipped: instrument not in round"]
//	}
//
// precision and recall are decimal strings or numbers in [0,1], or null when
// undefined (no predictions, or no labelled positives).
type evalSummary struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Command     string               `json:"command"`
	RoundID     int64                `json:"round_id"`
	LabelsFile  string               `json:"labels_file"`
	Labels      int                  `json:"labels"`
	Method      string               `json:"method"`
	Types       []evalTypeMetrics    `json:"types"`
	Notes       []string             `json:"notes"`
	Confusion   *evalConfusionMatrix `json:"confusion,omitempty"` // UI-23
}

type evalTypeMetrics struct {
	Type      string           `json:"type"`
	Support   int              `json:"support"`
	TP        int              `json:"tp"`
	FP        int              `json:"fp"`
	FN        int              `json:"fn"`
	Precision *decimal.Decimal `json:"precision"`
	Recall    *decimal.Decimal `json:"recall"`
}

// evalConfusionMatrix and evalConfusionCell mirror the eval.Summary UI-23 types.
type evalConfusionMatrix struct {
	Cells map[string]evalConfusionCell `json:"cells"`
}

type evalConfusionCell struct {
	Count   int      `json:"count"`
	PairIDs []string `json:"pair_ids,omitempty"`
}

// maxEvalArtifactBytes bounds the artifact read; a real summary is a few KB.
const maxEvalArtifactBytes = 1 << 20

// equivalentPrecisionTarget is the M2 exit bar for the `equivalent` tier
// (requirements.md NFR-06, plan.md M2).
var equivalentPrecisionTarget = decimal.RequireFromString("0.95")

// evalPage is the data for eval.html.
type evalPage struct {
	page
	File            string
	Missing         bool   // no artifact yet
	Broken          string // the artifact could not be read; fixed wording, no file content
	Summary         *evalSummary
	Rows            []evalRow
	Target          string
	Equiv           *evalRow // the equivalent tier, if present
	Notes           []string
	Produced        time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
	MacroAverage    *evalRow  // UI-16: macro average row
	WeightedAverage *evalRow  // UI-16: weighted by support row
}

// evalRow is one relationship type's metrics, formatted.
type evalRow struct {
	Type           string
	TypeBadge      Badge
	TypeChipClass  string
	Label          string
	Support        int
	TP, FP, FN     int
	Precision      string
	Recall         string
	F1             string  // UI-16: F1 score, "n/a" when undefined
	PrecisionFloat float64 // UI-16: for meter bars
	RecallFloat    float64 // UI-16: for meter bars
	MeetsTarget    bool    // precision >= target (equivalent tier only)
	HasPrec        bool    // precision is not null
	VerdictText    string  // for the headline: "meets the 0.95 target", "below the 0.95 target", "undefined: no equivalent predictions"
	VerdictBadge   Badge   // UI-16: verdict badge for headline panel
	VerdictChip    string  // chip class for the verdict (kept for compatibility)
}

// handleEval answers GET /eval: the latest `equinox eval` summary from the
// configured artifact, read-only.
func (s *Server) handleEval(w http.ResponseWriter, r *http.Request) {
	round := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	data := evalPage{
		page:   s.pageBase(round, "Eval", "How accurate is the matcher on human-labelled pairs, per relationship type?", tz, "eval"),
		File:   s.cfg.EvalSummaryFile,
		Target: equivalentPrecisionTarget.StringFixed(2),
	}
	sum, err := readEvalSummary(s.cfg.EvalSummaryFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		data.Missing = true
	case err != nil:
		s.log.Error("reading eval summary", "file", s.cfg.EvalSummaryFile, "err", err)
		data.Broken = "The eval artifact could not be read; see the server log."
	default:
		data.Summary = sum
		data.Produced = sum.GeneratedAt
		data.Notes = sum.Notes
		for _, m := range sum.Types {
			row := newEvalRow(m, data.Target)
			data.Rows = append(data.Rows, row)
			if m.Type == string(canonical.RelEquivalent) {
				r := row
				data.Equiv = &r
			}
		}
		// UI-16: compute macro and weighted averages
		data.MacroAverage = computeMacroAverage(sum.Types)
		data.WeightedAverage = computeWeightedAverage(sum.Types)
	}
	s.render(w, http.StatusOK, "eval", data)
}

// handleEvalCell answers GET /eval/cell?label=X&predicted=Y: drill-down into one
// confusion matrix cell. UI-23.
func (s *Server) handleEvalCell(w http.ResponseWriter, r *http.Request) {
	label := r.URL.Query().Get("label")
	predicted := r.URL.Query().Get("predicted")
	// AB-08: validate params against known types
	labelType := canonical.RelationshipType(label)
	predType := canonical.RelationshipType(predicted)
	if !labelType.Valid() || !predType.Valid() {
		http.Error(w, "Invalid label or predicted type", http.StatusBadRequest)
		return
	}
	sum, err := readEvalSummary(s.cfg.EvalSummaryFile)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			http.Error(w, "Eval summary not found", http.StatusNotFound)
		} else {
			s.log.Error("reading eval summary for cell", "file", s.cfg.EvalSummaryFile, "err", err)
			http.Error(w, "Could not read eval summary", http.StatusInternalServerError)
		}
		return
	}
	if sum.Confusion == nil {
		http.Error(w, "Confusion matrix not available in summary", http.StatusNotFound)
		return
	}
	key := fmt.Sprintf("%s:%s", label, predicted)
	cell, ok := sum.Confusion.Cells[key]
	if !ok {
		// Empty cell: valid but no pairs
		cell = evalConfusionCell{Count: 0, PairIDs: []string{}}
	}
	round := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	data := struct {
		page
		Label     string
		Predicted string
		Count     int
		PairIDs   []string
	}{
		page:      s.pageBase(round, "Eval Cell", fmt.Sprintf("Pairs labelled %s, predicted %s", label, predicted), tz, "eval"),
		Label:     label,
		Predicted: predicted,
		Count:     cell.Count,
		PairIDs:   cell.PairIDs,
	}
	s.render(w, http.StatusOK, "eval_cell", data)
}

func newEvalRow(m evalTypeMetrics, target string) evalRow {
	relType := canonical.RelationshipType(m.Type)
	row := evalRow{
		Type: m.Type, Label: typeLabels[relType], Support: m.Support, TP: m.TP, FP: m.FP, FN: m.FN,
		Precision: formatPercent(m.Precision), Recall: formatPercent(m.Recall), HasPrec: m.Precision != nil,
		TypeChipClass: TypeChipClass(relType),
		TypeBadge:     TypeBadge(relType), // UI-16
	}
	// UI-16: compute F1 score (harmonic mean of precision and recall)
	if m.Precision != nil && m.Recall != nil && !m.Precision.IsZero() && !m.Recall.IsZero() {
		two := decimal.NewFromInt(2)
		sum := m.Precision.Add(*m.Recall)
		product := m.Precision.Mul(*m.Recall)
		f1 := two.Mul(product).Div(sum)
		row.F1 = formatPercent(&f1)
	} else {
		row.F1 = "n/a"
	}
	// UI-16: float values for meter bars
	if m.Precision != nil {
		row.PrecisionFloat, _ = m.Precision.Float64()
		row.MeetsTarget = m.Precision.GreaterThanOrEqual(equivalentPrecisionTarget)
	}
	if m.Recall != nil {
		row.RecallFloat, _ = m.Recall.Float64()
	}
	// For the equivalent tier, build the verdict text and chip for the headline.
	if relType == canonical.RelEquivalent {
		if !row.HasPrec {
			row.VerdictText = "undefined: no equivalent predictions"
			row.VerdictChip = "chip-muted"
			row.VerdictBadge = Badge{Class: "v-unknown", Glyph: "?", Text: row.VerdictText} // UI-16
		} else if row.MeetsTarget {
			row.VerdictText = fmt.Sprintf("meets the %s target", target)
			row.VerdictChip = "chip-ok"
			row.VerdictBadge = Badge{Class: "v-match", Glyph: "✓", Text: row.VerdictText} // UI-16
		} else {
			row.VerdictText = fmt.Sprintf("below the %s target", target)
			row.VerdictChip = "chip-danger"
			row.VerdictBadge = Badge{Class: "v-differs", Glyph: "✕", Text: row.VerdictText} // UI-16
		}
	}
	return row
}

// computeMacroAverage computes the macro average (unweighted mean) of precision, recall and F1
// across all types. UI-16 AC.
func computeMacroAverage(types []evalTypeMetrics) *evalRow {
	if len(types) == 0 {
		return nil
	}
	var precSum, recSum, f1Sum decimal.Decimal
	precCount, recCount, f1Count := 0, 0, 0
	for _, m := range types {
		if m.Precision != nil {
			precSum = precSum.Add(*m.Precision)
			precCount++
		}
		if m.Recall != nil {
			recSum = recSum.Add(*m.Recall)
			recCount++
		}
		// Compute F1 for this type
		if m.Precision != nil && m.Recall != nil && !m.Precision.IsZero() && !m.Recall.IsZero() {
			two := decimal.NewFromInt(2)
			sum := m.Precision.Add(*m.Recall)
			product := m.Precision.Mul(*m.Recall)
			f1 := two.Mul(product).Div(sum)
			f1Sum = f1Sum.Add(f1)
			f1Count++
		}
	}
	row := evalRow{Type: "macro_average", Label: "Macro average"}
	if precCount > 0 {
		avgPrec := precSum.Div(decimal.NewFromInt(int64(precCount)))
		row.Precision = formatPercent(&avgPrec)
		row.PrecisionFloat, _ = avgPrec.Float64()
	} else {
		row.Precision = "n/a"
	}
	if recCount > 0 {
		avgRec := recSum.Div(decimal.NewFromInt(int64(recCount)))
		row.Recall = formatPercent(&avgRec)
		row.RecallFloat, _ = avgRec.Float64()
	} else {
		row.Recall = "n/a"
	}
	if f1Count > 0 {
		avgF1 := f1Sum.Div(decimal.NewFromInt(int64(f1Count)))
		row.F1 = formatPercent(&avgF1)
	} else {
		row.F1 = "n/a"
	}
	return &row
}

// computeWeightedAverage computes the weighted average (by support) of precision, recall and F1
// across all types. UI-16 AC.
func computeWeightedAverage(types []evalTypeMetrics) *evalRow {
	if len(types) == 0 {
		return nil
	}
	var precSum, recSum, f1Sum, totalSupport decimal.Decimal
	for _, m := range types {
		support := decimal.NewFromInt(int64(m.Support))
		if m.Precision != nil {
			precSum = precSum.Add(m.Precision.Mul(support))
		}
		if m.Recall != nil {
			recSum = recSum.Add(m.Recall.Mul(support))
		}
		// Compute F1 for this type
		if m.Precision != nil && m.Recall != nil && !m.Precision.IsZero() && !m.Recall.IsZero() {
			two := decimal.NewFromInt(2)
			sum := m.Precision.Add(*m.Recall)
			product := m.Precision.Mul(*m.Recall)
			f1 := two.Mul(product).Div(sum)
			f1Sum = f1Sum.Add(f1.Mul(support))
		}
		totalSupport = totalSupport.Add(support)
	}
	row := evalRow{Type: "weighted_average", Label: "Weighted by support"}
	if totalSupport.IsPositive() {
		avgPrec := precSum.Div(totalSupport)
		row.Precision = formatPercent(&avgPrec)
		row.PrecisionFloat, _ = avgPrec.Float64()
		avgRec := recSum.Div(totalSupport)
		row.Recall = formatPercent(&avgRec)
		row.RecallFloat, _ = avgRec.Float64()
		if !f1Sum.IsZero() {
			avgF1 := f1Sum.Div(totalSupport)
			row.F1 = formatPercent(&avgF1)
		} else {
			row.F1 = "n/a"
		}
	} else {
		row.Precision = "n/a"
		row.Recall = "n/a"
		row.F1 = "n/a"
	}
	return &row
}

// readEvalSummary loads and validates the artifact. An empty path or a
// missing file returns fs.ErrNotExist so the page can say "no eval yet".
func readEvalSummary(path string) (*evalSummary, error) {
	if path == "" {
		return nil, fs.ErrNotExist
	}
	f, err := os.Open(path) //nolint:gosec // path is EVAL_SUMMARY_FILE (operator config), never from a request
	if err != nil {
		return nil, fmt.Errorf("opening eval summary: %w", err)
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxEvalArtifactBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading eval summary: %w", err)
	}
	if len(raw) > maxEvalArtifactBytes {
		return nil, fmt.Errorf("reading eval summary: file exceeds %d bytes", maxEvalArtifactBytes)
	}
	var sum evalSummary
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sum); err != nil {
		return nil, fmt.Errorf("decoding eval summary: %w", err)
	}
	if err := sum.validate(); err != nil {
		return nil, fmt.Errorf("validating eval summary: %w", err)
	}
	return &sum, nil
}

func (e evalSummary) validate() error {
	var errs []error
	if e.GeneratedAt.IsZero() {
		errs = append(errs, errors.New("generated_at is required"))
	}
	if e.Labels < 0 {
		errs = append(errs, errors.New("labels must not be negative"))
	}
	for i, m := range e.Types {
		if !canonical.RelationshipType(m.Type).Valid() {
			errs = append(errs, fmt.Errorf("types[%d]: unknown relationship type", i))
		}
		if m.Support < 0 || m.TP < 0 || m.FP < 0 || m.FN < 0 {
			errs = append(errs, fmt.Errorf("types[%d]: counts must not be negative", i))
		}
		for name, v := range map[string]*decimal.Decimal{"precision": m.Precision, "recall": m.Recall} {
			if v != nil && (v.IsNegative() || v.GreaterThan(decimal.NewFromInt(1))) {
				errs = append(errs, fmt.Errorf("types[%d]: %s %s is outside [0,1]", i, name, v))
			}
		}
	}
	return errors.Join(errs...)
}
