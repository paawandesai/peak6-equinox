package web

import (
	"net/http"
	"strings"
	"testing"
)

// TestEvalHeadlineStates tests the three headline states per UI-07 AC:
// meets target, below target, undefined (no equivalent predictions).
func TestEvalHeadlineStates(t *testing.T) {
	tests := []struct {
		name            string
		fixture         string
		wantPrecision   string
		wantVerdictText string
		wantChip        string
	}{
		{
			name:            "meets target",
			fixture:         sampleEval,
			wantPrecision:   "100.00%",
			wantVerdictText: "meets the 0.95 target",
			wantChip:        "chip-ok",
		},
		{
			name:            "below target",
			fixture:         strings.Replace(sampleEval, `"precision": "1"`, `"precision": "0.80"`, 1),
			wantPrecision:   "80.00%",
			wantVerdictText: "below the 0.95 target",
			wantChip:        "chip-danger",
		},
		{
			name:            "undefined",
			fixture:         strings.Replace(sampleEval, `"precision": "1"`, `"precision": null`, 1),
			wantPrecision:   "n/a",
			wantVerdictText: "undefined: no equivalent predictions",
			wantChip:        "chip-muted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(Config{EvalSummaryFile: writeEval(t, tt.fixture)}, Deps{})
			rec := get(s.Handler(), "/eval")
			wantHTML(t, rec, http.StatusOK)
			body := rec.Body.String()
			// Check the headline strip with precision and verdict.
			wantContains(t, body,
				`<dl class="strip">`,
				`Precision at equivalent`,
				tt.wantPrecision,
				tt.wantVerdictText,
				tt.wantChip,
			)
		})
	}
}

// TestEvalTypeChips verifies that per-type rows use type chips (UI-07 AC).
func TestEvalTypeChips(t *testing.T) {
	s := New(Config{EvalSummaryFile: writeEval(t, sampleEval)}, Deps{})
	rec := get(s.Handler(), "/eval")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	// Each type renders as a chip with the correct class from TypeChipClass.
	wantContains(t, body,
		`<span class="chip chip-ok">equivalent</span>`, // RelEquivalent -> chip-ok
		`<span class="chip chip-ok">complement</span>`, // RelComplement -> chip-ok
		`<span class="chip chip-info">implies</span>`,  // RelImplies -> chip-info
	)
}
