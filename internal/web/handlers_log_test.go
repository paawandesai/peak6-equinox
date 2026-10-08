package web

import (
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// TestLogKindChips verifies each decision-log kind renders with the correct chip class.
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
