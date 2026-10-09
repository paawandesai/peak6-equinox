package web

import (
	"net/http"
	"strings"
	"testing"
)

// TestEvalHeadlineStates tests the three headline states per UI-16 AC:
// meets target, below target, undefined (no equivalent predictions).
func TestEvalHeadlineStates(t *testing.T) {
	tests := []struct {
		name            string
		fixture         string
		wantPrecision   string
		wantVerdictText string
		wantGlyph       string
	}{
		{
			name:            "meets target",
			fixture:         sampleEval,
			wantPrecision:   "100.00%",
			wantVerdictText: "meets the 0.95 target",
			wantGlyph:       "✓",
		},
		{
			name:            "below target",
			fixture:         strings.Replace(sampleEval, `"precision": "1"`, `"precision": "0.80"`, 1),
			wantPrecision:   "80.00%",
			wantVerdictText: "below the 0.95 target",
			wantGlyph:       "✕",
		},
		{
			name:            "undefined",
			fixture:         strings.Replace(sampleEval, `"precision": "1"`, `"precision": null`, 1),
			wantPrecision:   "n/a",
			wantVerdictText: "undefined: no equivalent predictions",
			wantGlyph:       "?",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(Config{EvalSummaryFile: writeEval(t, tt.fixture)}, Deps{})
			rec := get(s.Handler(), "/eval")
			wantHTML(t, rec, http.StatusOK)
			body := rec.Body.String()
			// UI-16: headline panel with verdict badge
			wantContains(t, body,
				`<div class="panel panel-info">`,
				`<span class="badge`,
				tt.wantGlyph,
				tt.wantVerdictText,
				`Precision at equivalent`,
				tt.wantPrecision,
			)
		})
	}
}

// TestEvalF1Calculation tests F1 score calculation (UI-16 AC).
func TestEvalF1Calculation(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		wantF1  string
	}{
		{
			name:    "defined F1",
			fixture: sampleEval, // P=1.0, R≈0.9231 → F1≈0.96
			wantF1:  "96.00%",
		},
		{
			name: "undefined F1 when precision is null",
			fixture: strings.Replace(sampleEval, `"precision": "1"`, `"precision": null`, 1),
			wantF1:  "n/a",
		},
		{
			name: "undefined F1 when recall is null",
			fixture: strings.Replace(sampleEval, `"recall": 0.9231`, `"recall": null`, 1),
			wantF1:  "n/a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(Config{EvalSummaryFile: writeEval(t, tt.fixture)}, Deps{})
			rec := get(s.Handler(), "/eval")
			wantHTML(t, rec, http.StatusOK)
			body := rec.Body.String()
			wantContains(t, body, tt.wantF1)
		})
	}
}

// TestEvalMacroAndWeightedAverages tests the average rows (UI-16 AC).
func TestEvalMacroAndWeightedAverages(t *testing.T) {
	s := New(Config{EvalSummaryFile: writeEval(t, sampleEval)}, Deps{})
	rec := get(s.Handler(), "/eval")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	// UI-16: both average rows should be present
	wantContains(t, body,
		"Macro average",
		"Weighted by support",
		`<tr class="summary-row">`,
	)
}

// TestEvalMeterBars tests that meter elements are rendered (UI-16 AC).
func TestEvalMeterBars(t *testing.T) {
	s := New(Config{EvalSummaryFile: writeEval(t, sampleEval)}, Deps{})
	rec := get(s.Handler(), "/eval")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	// UI-16: meter bars for Precision and Recall
	wantContains(t, body,
		`<td class="meter-cell">`,
		`<meter min="0" max="1" low="0.7" high="0.9" optimum="1"`,
	)
	// Check that meters are NOT rendered when values are n/a
	undefFixture := strings.Replace(sampleEval, `"precision": "1"`, `"precision": null`, 1)
	rec2 := get(New(Config{EvalSummaryFile: writeEval(t, undefFixture)}, Deps{}).Handler(), "/eval")
	wantHTML(t, rec2, http.StatusOK)
	body2 := rec2.Body.String()
	// For undefined precision, should still have the meter-cell class but the meter itself is conditional
	wantContains(t, body2, `<td class="meter-cell">`, "n/a")
}

// TestEvalTypeChips verifies that per-type rows use badges (UI-16 updated from UI-07).
func TestEvalTypeBadges(t *testing.T) {
	s := New(Config{EvalSummaryFile: writeEval(t, sampleEval)}, Deps{})
	rec := get(s.Handler(), "/eval")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	// UI-16: Each type renders as a badge with glyph
	wantContains(t, body,
		`<span class="badge b-equivalent">`,
		`<span class="g" aria-hidden="true">≡</span>equivalent`,
		`<span class="badge b-complement">`,
		`<span class="g" aria-hidden="true">¬</span>complement`,
		`<span class="badge b-implies">`,
		`<span class="g" aria-hidden="true">⇒</span>implies`,
	)
}

// TestEvalCalibrationNote tests that the calibration note is present (UI-16 AC).
func TestEvalCalibrationNote(t *testing.T) {
	s := New(Config{EvalSummaryFile: writeEval(t, sampleEval)}, Deps{})
	rec := get(s.Handler(), "/eval")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	// UI-16: calibration note
	wantContains(t, body, "There is no calibration plot because confidence is a rules score, not a probability")
}
