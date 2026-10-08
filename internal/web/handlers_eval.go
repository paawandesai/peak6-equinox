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
	GeneratedAt time.Time         `json:"generated_at"`
	Command     string            `json:"command"`
	RoundID     int64             `json:"round_id"`
	LabelsFile  string            `json:"labels_file"`
	Labels      int               `json:"labels"`
	Method      string            `json:"method"`
	Types       []evalTypeMetrics `json:"types"`
	Notes       []string          `json:"notes"`
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

// maxEvalArtifactBytes bounds the artifact read; a real summary is a few KB.
const maxEvalArtifactBytes = 1 << 20

// equivalentPrecisionTarget is the M2 exit bar for the `equivalent` tier
// (requirements.md NFR-06, plan.md M2).
var equivalentPrecisionTarget = decimal.RequireFromString("0.95")

// evalPage is the data for eval.html.
type evalPage struct {
	page
	File     string
	Missing  bool   // no artifact yet
	Broken   string // the artifact could not be read; fixed wording, no file content
	Summary  *evalSummary
	Rows     []evalRow
	Target   string
	Equiv    *evalRow // the equivalent tier, if present
	Notes    []string
	Produced time.Time // Raw UTC time, formatted with formatTime in templates (M3-08)
}

// evalRow is one relationship type's metrics, formatted.
type evalRow struct {
	Type          string
	TypeChipClass string
	Label         string
	Support       int
	TP, FP, FN    int
	Precision     string
	Recall        string
	MeetsTarget   bool   // precision >= target (equivalent tier only)
	HasPrec       bool   // precision is not null
	VerdictText   string // for the headline: "meets the 0.95 target", "below the 0.95 target", "undefined: no equivalent predictions"
	VerdictChip   string // chip class for the verdict
}

// handleEval answers GET /eval: the latest `equinox eval` summary from the
// configured artifact, read-only.
func (s *Server) handleEval(w http.ResponseWriter, r *http.Request) {
	round := roundFromContext(r.Context())
	tz := timezoneFromRequest(r)
	data := evalPage{
		page:   s.pageBase(round, "Eval", "How accurate is the matcher on human-labelled pairs, per relationship type?", tz),
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
	}
	s.render(w, http.StatusOK, "eval", data)
}

func newEvalRow(m evalTypeMetrics, target string) evalRow {
	relType := canonical.RelationshipType(m.Type)
	row := evalRow{
		Type: m.Type, Label: typeLabels[relType], Support: m.Support, TP: m.TP, FP: m.FP, FN: m.FN,
		Precision: formatPercent(m.Precision), Recall: formatPercent(m.Recall), HasPrec: m.Precision != nil,
		TypeChipClass: TypeChipClass(relType),
	}
	if m.Precision != nil {
		row.MeetsTarget = m.Precision.GreaterThanOrEqual(equivalentPrecisionTarget)
	}
	// For the equivalent tier, build the verdict text and chip for the headline.
	if relType == canonical.RelEquivalent {
		if !row.HasPrec {
			row.VerdictText = "undefined: no equivalent predictions"
			row.VerdictChip = "chip-muted"
		} else if row.MeetsTarget {
			row.VerdictText = fmt.Sprintf("meets the %s target", target)
			row.VerdictChip = "chip-ok"
		} else {
			row.VerdictText = fmt.Sprintf("below the %s target", target)
			row.VerdictChip = "chip-danger"
		}
	}
	return row
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
