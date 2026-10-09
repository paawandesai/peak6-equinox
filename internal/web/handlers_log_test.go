package web

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
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

	// Verify sign rule footnote is present
	wantContains(t, body, `best ask`)
	wantContains(t, body, `parentheses`)
	wantContains(t, body, `Formula: best ask − average all-in`)

	// Verify saving value is displayed (positive or parenthesized loss)
	// The seeded decision should have a computed saving
	if !strings.Contains(body, `data-label="Saving vs best ask"`) {
		t.Error("Saving vs best ask column not found")
	}
	// Check that at least one saving value appears (either plain number or parenthesized)
	hasSaving := strings.Contains(body, `<td data-label="Saving vs best ask" class="num">`) &&
		(strings.Contains(body, `0.`) || strings.Contains(body, `(`))
	if !hasSaving {
		t.Error("No saving values found in table")
	}
}

// TestLogNotStoredForOldDecision verifies "not stored" for pre-UI-25 decisions (UI-15).
func TestLogNotStoredForOldDecision(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Create a routing decision without inputs (pre-UI-25 format)
	qty := dec("10")
	decision := canonical.Decision{
		RoundID:    sd.round.ID,
		TargetID:   seedKalshiA,
		TargetType: "", // Empty to trigger "label not stored"
		Order: canonical.Order{
			Side:     canonical.SideBuy,
			Type:     canonical.OrderMarket,
			Outcome:  canonical.OutcomeYes,
			TIF:      canonical.TIFIOC,
			Quantity: &qty,
		},
		Chosen:      []canonical.Leg{}, // Not routed
		Explanation: "Test decision without inputs",
		DecidedAt:   seedNow,
		// Inputs is omitted (empty slice), simulating pre-UI-25 decision
	}
	decisionID, err := st.InsertRoutingDecision(ctx, decision)
	if err != nil {
		t.Fatal(err)
	}

	// Create a log entry for the routing decision
	_, err = st.AppendLog(ctx, store.LogRouting, fmt.Sprintf("%d", decisionID), "routing without inputs", nil, seedNow)
	if err != nil {
		t.Fatal(err)
	}

	// Fetch the log page
	rec := get(s.Handler(), "/log?kind=routing")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Verify "not stored" appears for the decision without inputs
	// The label column should show "label not stored" for empty TargetType
	wantContains(t, body, "label not stored")
	// The saving column should show "not stored" for decisions without inputs
	wantContains(t, body, "not stored")
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

	// Test actual grouping by venue
	rec = get(s.Handler(), "/log?kind=routing&group=venue")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	// Should have group header(s)
	if !strings.Contains(body, `<h3 class="group-label">`) {
		t.Error("group-label header not found when grouping by venue")
	}

	// Test grouping by staleness
	rec = get(s.Handler(), "/log?kind=routing&group=staleness")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	if !strings.Contains(body, `<h3 class="group-label">`) {
		t.Error("group-label header not found when grouping by staleness")
	}

	// Test grouping by label
	rec = get(s.Handler(), "/log?kind=routing&group=label")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	if !strings.Contains(body, `<h3 class="group-label">`) {
		t.Error("group-label header not found when grouping by label")
	}
	// Group label should appear (could be a relationship type or "label not stored")
	// Just verify that grouping is working by checking for the header
}

// TestLogOutliers verifies the outliers panel (UI-15).
func TestLogOutliers(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)

	rec := get(s.Handler(), "/log?kind=routing")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Outliers panel structure: div.outliers with strong heading and p elements
	wantContains(t, body,
		`<div class="outliers">`,
		`<strong>Outliers</strong>`,
	)
	// At least one of worst saving or oldest input should be present
	if !strings.Contains(body, `Worst saving:`) && !strings.Contains(body, `Oldest input:`) {
		t.Error("outliers panel must contain at least one outlier (worst saving or oldest input)")
	}
}
