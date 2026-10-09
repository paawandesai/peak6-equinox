package web

import (
	"strconv"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestNearMissGrouping verifies rows are grouped by veto code.
func TestNearMissGrouping(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Groups should have sections with anchors
	wantContains(t, body, `id="group-subject-mismatch"`, `<code>subject_mismatch</code>`)
	wantContains(t, body, `id="group-fact-did-not-close"`, `<code>fact_did_not_close</code>`)

	// Group chips should appear
	wantContains(t, body, `href="#group-subject-mismatch"`, `href="#group-fact-did-not-close"`)

	// Descriptions should appear
	wantContains(t, body, "Different underlying subject", "A fact was consulted but did not close the difference")
}

// TestNearMissCountCells verifies strip shows count, veto count and fact-did-not-close.
func TestNearMissCountCells(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	wantContains(t, body, `<dl class="strip">`)
	wantContains(t, body, "<dt>Count:</dt>", "<dd>2 near misses</dd>")
	wantContains(t, body, `<div class="strip-danger">`, "<dt>Stopped by a veto:</dt>", "<dd>1</dd>")
	wantContains(t, body, "<dt>Fact did not close:</dt>", "<dd>1</dd>")
}

// TestNearMissVetoBadge verifies the veto badge renders with glyph.
func TestNearMissVetoBadge(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Badge with glyph
	wantContains(t, body, `<span class="badge b-danger mono">`, `<span class="g" aria-hidden="true">✕</span>subject_mismatch`)
	wantContains(t, body, "<strong>subject</strong>: BTC is not ETH")

	// No-veto case
	wantContains(t, body, `<span class="badge b-info">`, `<span class="g" aria-hidden="true">◇</span>no veto`)
}

// TestNearMissTermGapThreshold verifies TermGap for a numeric threshold.
func TestNearMissTermGapThreshold(t *testing.T) {
	views := map[string]instrumentView{"kalshi:A": {}, "kalshi:B": {}}
	rels := []canonical.Relationship{{
		ID: 1, AID: "kalshi:A", BID: "kalshi:B", Type: canonical.RelRelated,
		Confidence: dec("0.5"),
		Diff: []canonical.FieldDiff{
			{Field: "threshold", A: "4.25", B: "0.5", Status: canonical.DiffDiffer},
		},
		Vetoes: []canonical.Veto{{Code: "threshold_mismatch", Field: "threshold", Detail: "4.25 vs 0.5"}},
	}}
	rows := nearMissRows(rels, views)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].TermGap != "3.75" {
		t.Errorf("TermGap = %q, want 3.75", rows[0].TermGap)
	}
}

// TestNearMissTermGapDeadline verifies TermGap for a deadline.
func TestNearMissTermGapDeadline(t *testing.T) {
	views := map[string]instrumentView{"kalshi:A": {}, "kalshi:B": {}}
	t1 := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	t2 := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	rels := []canonical.Relationship{{
		ID: 1, AID: "kalshi:A", BID: "kalshi:B", Type: canonical.RelRelated,
		Confidence: dec("0.5"),
		Diff: []canonical.FieldDiff{
			{Field: "deadline", A: t1, B: t2, Status: canonical.DiffDiffer},
		},
		Vetoes: []canonical.Veto{{Code: "deadline_mismatch", Field: "deadline", Detail: "2 days"}},
	}}
	rows := nearMissRows(rels, views)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].TermGap != "2 days" {
		t.Errorf("TermGap = %q, want \"2 days\"", rows[0].TermGap)
	}
}

// TestNearMissNoVetoGroup verifies the "fact did not close" group.
func TestNearMissNoVetoGroup(t *testing.T) {
	views := map[string]instrumentView{"kalshi:A": {}, "kalshi:B": {}, "kalshi:C": {}}
	rels := []canonical.Relationship{
		{ID: 1, AID: "kalshi:A", BID: "kalshi:B", Type: canonical.RelRelated, Confidence: dec("0.5"),
			Vetoes: []canonical.Veto{{Code: "x"}}},
		{ID: 2, AID: "kalshi:A", BID: "kalshi:C", Type: canonical.RelRelated, Confidence: dec("0.6")},
	}
	rows := nearMissRows(rels, views)
	groups := groupNearMisses(rows)
	var codes []string
	for _, g := range groups {
		codes = append(codes, g.Code)
	}
	// fact_did_not_close should be last
	if len(codes) != 2 || codes[1] != "fact_did_not_close" {
		t.Errorf("group codes = %v, want [x fact_did_not_close]", codes)
	}
	if groups[1].Count != 1 {
		t.Errorf("fact_did_not_close count = %d, want 1", groups[1].Count)
	}
}

// TestNearMissLogLink verifies the log link is present.
func TestNearMissLogLink(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	_, factMissID := seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Log links should be present
	wantContains(t, body,
		`href="/pairs/`+strconv.FormatInt(sd.relID, 10)+`">Not-a-match record →</a>`,
		`href="/pairs/`+strconv.FormatInt(factMissID, 10)+`">Not-a-match record →</a>`)
}

// TestFormatDurationCoverage tests all branches of formatNearMissDuration.
func TestFormatDurationCoverage(t *testing.T) {
	tests := []struct {
		dur  time.Duration
		want string
	}{
		{48 * time.Hour, "2 days"},
		{24 * time.Hour, "1 day"},
		{2 * time.Hour, "2 hours"},
		{1 * time.Hour, "1 hour"},
		{30 * time.Minute, "30 minutes"},
		{1 * time.Minute, "1 minute"},
	}
	for _, tt := range tests {
		got := formatNearMissDuration(tt.dur)
		if got != tt.want {
			t.Errorf("formatNearMissDuration(%v) = %q, want %q", tt.dur, got, tt.want)
		}
	}
}

// TestPluralS tests the pluralS helper.
func TestPluralS(t *testing.T) {
	if got := pluralS(0); got != "s" {
		t.Errorf("pluralS(0) = %q, want \"s\"", got)
	}
	if got := pluralS(1); got != "" {
		t.Errorf("pluralS(1) = %q, want \"\"", got)
	}
	if got := pluralS(2); got != "s" {
		t.Errorf("pluralS(2) = %q, want \"s\"", got)
	}
}

// TestVetoDescriptionCoverage tests all veto codes.
func TestVetoDescriptionCoverage(t *testing.T) {
	tests := map[string]string{
		"subject_mismatch":   "Different underlying subject",
		"threshold_mismatch": "Strike or threshold differs",
		"deadline_mismatch":  "Resolution deadline differs",
		"direction_mismatch": "Comparison direction differs (over vs under)",
		"source_mismatch":    "Data source differs",
		"fact_did_not_close": "A fact was consulted but did not close the difference",
		"unknown_code":       "unknown code",
	}
	for code, want := range tests {
		got := vetoDescription(code)
		if got != want {
			t.Errorf("vetoDescription(%q) = %q, want %q", code, got, want)
		}
	}
}
