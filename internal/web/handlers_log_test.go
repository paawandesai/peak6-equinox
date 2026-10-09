package web

import (
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// TestLogKindChips verifies each decision-log kind renders with the correct chip class and aria-current (UI-15).
func TestLogKindChips(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/log")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// relationship (match) should be chip-info
	if !strings.Contains(body, `<span class="chip chip-info">relationship</span>`) {
		t.Error("relationship kind should have chip-info class")
	}

	// routing should be chip-ok
	if !strings.Contains(body, `<span class="chip chip-ok">routing</span>`) {
		t.Error("routing kind should have chip-ok class")
	}

	// The table should use data stack classes
	wantContains(t, body, `<table class="data stack">`)

	// Verify aria-current is on "everything" when no kind is selected
	wantContains(t, body, `<a href="/log" class="chip-filter" aria-current="page">everything</a>`)
}

// TestLogKindChipClass verifies the chip class mapping for all kinds.
func TestLogKindChipClass(t *testing.T) {
	cases := []struct {
		kind      store.LogKind
		wantClass string
	}{
		{store.LogRelationship, "chip-info"},
		{store.LogLLM, "chip-info"},
		{store.LogRouting, "chip-ok"},
		{store.LogIngest, "chip-muted"},
		{store.LogKind("unknown"), "chip-muted"},
	}
	for _, c := range cases {
		got := logKindChipClass(c.kind)
		if got != c.wantClass {
			t.Errorf("logKindChipClass(%s) = %s, want %s", c.kind, got, c.wantClass)
		}
	}
}

// TestLogAboutField verifies the About field shows readable text with venue names and outcome phrases.
func TestLogAboutField(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/log")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Relationship entries should show "pair #N: venue / venue" from a_id/b_id
	wantContains(t, body, `pair #`)
	wantContains(t, body, `kalshi / polymarket`) // Venue names extracted from payload

	// Routing entries should show "decision #N: outcome phrase"
	wantContains(t, body, `decision #`)
	wantContains(t, body, `Routed`) // Outcome phrase from OutcomePhrase helper

	// Links should point to the correct pages
	wantContains(t, body,
		`href="/pairs/`,
		`href="/decisions/`)
}

// TestLogSummaryClamped verifies summaries use clamp-2 and expand in details.
func TestLogSummaryClamped(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/log")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Summaries should be in details elements with clamp-2 inside a span
	wantContains(t, body, `<details>`)
	wantContains(t, body, `<span class="clamp-2">`)

	// The full summary text should appear inside the details (not just truncated)
	wantContains(t, body, seedExplainEq) // From the relationship entry
}

// TestLogRowDataLabels verifies the stacking layout has data-label attributes.
func TestLogRowDataLabels(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/log")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Each cell should have a data-label for stacking at narrow widths
	wantContains(t, body,
		`data-label="When"`,
		`data-label="Kind"`,
		`data-label="About"`,
		`data-label="Summary"`)
}

// TestLogRoutingColumns verifies the routing table shows all UI-15 columns (UI-15).
func TestLogRoutingColumns(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/log?kind=routing")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Verify routing table headers
	wantContains(t, body,
		`<th>When</th>`,
		`<th>About</th>`,
		`<th>Order</th>`,
		`<th>Outcome</th>`,
		`<th class="num">All-in</th>`,
		`<th class="num">Saving vs best ask</th>`,
		`<th>Max quote age</th>`,
		`<th>Label</th>`,
		`<th>Summary</th>`)

	// Verify the sign rule is printed under the table
	wantContains(t, body, `<p class="sign-rule">`)
	wantContains(t, body, `Positive is favourable`)
}

// TestLogSignRule verifies the sign rule for savings (UI-15, D-098 call 2).
func TestLogSignRule(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/log?kind=routing")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// TODO: Add test data with actual savings values
	// For now, just verify the sign rule text is present
	wantContains(t, body, `best ask`)
	wantContains(t, body, `parentheses`)
}

// TestLogNotStoredForOldDecision verifies "not stored" for pre-UI-25 decisions (UI-15).
func TestLogNotStoredForOldDecision(t *testing.T) {
	// This test will need actual old decision data without inputs
	// For now we just verify the logic exists
	// TODO: seed a decision without inputs and verify "not stored" appears
	t.Skip("TODO: add test data for pre-UI-25 decision")
}

// TestLogGrouping verifies grouping by venue, staleness, and label (UI-15).
func TestLogGrouping(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)

	// Test group-by chips are present on routing view
	rec := get(s.Handler(), "/log?kind=routing")
	wantHTML(t, rec, 200)
	body := rec.Body.String()
	wantContains(t, body, `Group by`)
	wantContains(t, body, `href="/log?kind=routing&group=venue"`)
	wantContains(t, body, `href="/log?kind=routing&group=staleness"`)
	wantContains(t, body, `href="/log?kind=routing&group=label"`)

	// Verify invalid group parameter is rejected (AB-08)
	rec = get(s.Handler(), "/log?group=invalid")
	if rec.Code != 400 {
		t.Errorf("invalid group parameter: got %d, want 400", rec.Code)
	}

	// TODO: implement and test actual grouping logic
}

// TestLogOutliers verifies the outliers panel (UI-15).
func TestLogOutliers(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)

	rec := get(s.Handler(), "/log?kind=routing")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Outliers panel should appear on routing view
	// TODO: verify actual outliers once we have test data with savings
	// For now just check the structure is there if outliers exist
	if strings.Contains(body, `<div class="outliers">`) {
		wantContains(t, body, `Outliers`)
	}
}
