package web

import (
	"net/http"
	"net/http/httptest"
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
			name:    "undefined F1 when precision is null",
			fixture: strings.Replace(sampleEval, `"precision": "1"`, `"precision": null`, 1),
			wantF1:  "n/a",
		},
		{
			name:    "undefined F1 when recall is null",
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

// UI-23 tests

func TestEvalCellValidParams(t *testing.T) {
	summary := `{
		"generated_at": "2026-10-09T12:00:00Z",
		"method": "rules",
		"labels": 2,
		"round_id": 1,
		"labels_file": "labels.yaml",
		"command": "equinox eval",
		"types": [],
		"notes": [],
		"confusion": {
			"cells": {
				"equivalent:equivalent": {"count": 1, "pair_ids": ["kalshi:A / polymarket:1"]},
				"equivalent:related": {"count": 1, "pair_ids": ["kalshi:B / polymarket:2"]}
			}
		}
	}`
	s := New(Config{EvalSummaryFile: writeEval(t, summary)}, Deps{})
	rec := get(s.Handler(), "/eval/cell?label=equivalent&predicted=equivalent")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body, "kalshi:A / polymarket:1", "Confusion Matrix Cell")
}

func TestEvalCellInvalidParams(t *testing.T) {
	summary := `{
		"generated_at": "2026-10-09T12:00:00Z",
		"method": "rules",
		"labels": 0,
		"round_id": 1,
		"labels_file": "labels.yaml",
		"command": "equinox eval",
		"types": [],
		"notes": [],
		"confusion": {"cells": {}}
	}`
	s := New(Config{EvalSummaryFile: writeEval(t, summary)}, Deps{})

	// Invalid label (AB-08)
	rec := get(s.Handler(), "/eval/cell?label=invalid&predicted=equivalent")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid label: got %d, want 400", rec.Code)
	}

	// Invalid predicted (AB-08)
	rec = get(s.Handler(), "/eval/cell?label=equivalent&predicted=<script>")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid predicted: got %d, want 400", rec.Code)
	}
}

// TestEvalWithConfusionMatrix tests that /eval renders successfully with UI-23 fields.
func TestEvalWithConfusionMatrix(t *testing.T) {
	summary := `{
		"generated_at": "2026-10-09T12:00:00Z",
		"method": "rules",
		"labels": 5,
		"round_id": 1,
		"labels_file": "labels.yaml",
		"command": "equinox eval",
		"types": [
			{"type": "equivalent", "support": 2, "tp": 2, "fp": 0, "fn": 0, "precision": "1.0", "recall": "1.0"}
		],
		"notes": ["test note"],
		"confusion": {
			"cells": {
				"equivalent:equivalent": {"count": 2, "pair_ids": ["kalshi:A / polymarket:1", "kalshi:B / polymarket:2"]},
				"equivalent:related": {"count": 1, "pair_ids": ["kalshi:C / polymarket:3"]}
			}
		},
		"thresholds": [
			{"threshold": "0.90", "tp": 2, "fp": 0, "precision": "1.0", "recall": "1.0"}
		],
		"labels_sha256": "abc123",
		"previous": {
			"generated_at": "2026-10-08T12:00:00Z",
			"labels_sha256": "abc123",
			"types": {
				"equivalent": {"precision": "0.95", "recall": "0.90"}
			}
		}
	}`
	s := New(Config{EvalSummaryFile: writeEval(t, summary)}, Deps{})
	rec := get(s.Handler(), "/eval")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	// Verify UI-23 elements render
	wantContains(t, body,
		"Confusion matrix",
		"Golden-set version",
		"abc123",
		"Δ vs previous run",
		"P/R at confidence thresholds",
		"≥ 0.90",
	)
}

// TestEvalCellRateLimit tests that /eval/cell is rate-limited (AB-03).
func TestEvalCellRateLimit(t *testing.T) {
	summary := `{
		"generated_at": "2026-10-09T12:00:00Z",
		"method": "rules",
		"labels": 1,
		"round_id": 1,
		"labels_file": "labels.yaml",
		"command": "equinox eval",
		"types": [],
		"notes": [],
		"confusion": {
			"cells": {"equivalent:equivalent": {"count": 1, "pair_ids": ["kalshi:A / polymarket:1"]}}
		}
	}`
	s := New(Config{EvalSummaryFile: writeEval(t, summary), PublicRateLimitPerMin: 60, AppEnv: "ci"}, Deps{})
	h := s.Handler()

	// Fill the 60 req/min limit
	for i := 0; i < 60; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/eval/cell?label=equivalent&predicted=equivalent", nil)
		req.RemoteAddr = "203.0.113.42:51000"
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: got %d, want 200", i+1, rec.Code)
		}
	}

	// Next request should be rate-limited (429)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/eval/cell?label=equivalent&predicted=equivalent", nil)
	req.RemoteAddr = "203.0.113.42:51000"
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("request 61: got %d, want 429", rec.Code)
	}
}

// TestConfusionClassBuckets tests the bucket assignment based on count (UI-23 round 2).
func TestConfusionClassBuckets(t *testing.T) {
	tests := []struct {
		label     string
		predicted string
		count     int
		wantClass string
	}{
		{"equivalent", "equivalent", 1, "d1"},   // diagonal, light
		{"equivalent", "equivalent", 5, "d1"},   // diagonal, light (boundary)
		{"equivalent", "equivalent", 6, "d2"},   // diagonal, medium
		{"equivalent", "equivalent", 15, "d2"},  // diagonal, medium (boundary)
		{"equivalent", "equivalent", 16, "d3"},  // diagonal, dark
		{"equivalent", "equivalent", 99, "d3"},  // diagonal, dark (high)
		{"equivalent", "related", 1, "e1"},      // off-diagonal, light
		{"equivalent", "related", 5, "e1"},      // off-diagonal, light (boundary)
		{"equivalent", "related", 6, "e2"},      // off-diagonal, medium
		{"equivalent", "related", 15, "e2"},     // off-diagonal, medium (boundary)
		{"equivalent", "related", 16, "e3"},     // off-diagonal, dark
		{"equivalent", "related", 99, "e3"},     // off-diagonal, dark (high)
	}
	funcs := templateFuncs
	classFunc := funcs["ConfusionClass"].(func(string, string, int) string)
	for _, tt := range tests {
		got := classFunc(tt.label, tt.predicted, tt.count)
		if got != tt.wantClass {
			t.Errorf("ConfusionClass(%q, %q, %d) = %q, want %q", tt.label, tt.predicted, tt.count, got, tt.wantClass)
		}
	}
}

// TestEvalCellWithoutStore tests that eval cell handles missing store gracefully (UI-23 round 2).
func TestEvalCellWithoutStore(t *testing.T) {
	summary := `{
		"generated_at": "2026-10-09T12:00:00Z",
		"method": "rules",
		"labels": 1,
		"round_id": 1,
		"labels_file": "labels.yaml",
		"command": "equinox eval",
		"types": [],
		"notes": [],
		"confusion": {
			"cells": {
				"equivalent:equivalent": {"count": 2, "pair_ids": ["kalshi:A / polymarket:1", "kalshi:B / polymarket:2"]}
			}
		}
	}`
	// No store provided - should fall back to showing canonical IDs
	s := New(Config{EvalSummaryFile: writeEval(t, summary)}, Deps{})
	rec := get(s.Handler(), "/eval/cell?label=equivalent&predicted=equivalent")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	// Should show canonical IDs as code blocks (fallback when no store)
	wantContains(t, body, "kalshi:A / polymarket:1", "kalshi:B / polymarket:2")
}
