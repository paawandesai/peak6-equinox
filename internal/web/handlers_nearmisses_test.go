package web

import (
	"strconv"
	"strings"
	"testing"
)

// TestNearMissVetoChips verifies that vetoed near misses render the veto with
// a danger chip, while fact-consulted misses (no veto) show an info chip.
func TestNearMissVetoChips(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Vetoed pair (sd.relID) should have danger chip
	wantContains(t, body, `<span class="chip chip-danger"><code>subject_mismatch</code></span>`)

	// Fact-consulted pair should have info chip for "no veto"
	if !strings.Contains(body, `<span class="chip chip-info">no veto</span>`) {
		t.Error("fact-consulted near miss should show info chip for 'no veto'")
	}

	// The veto detail should appear after the chip
	wantContains(t, body, "BTC is not ETH")
}

// TestNearMissRowShape verifies the new stacking layout: veto first, contracts
// stacked, explanation in details.
func TestNearMissRowShape(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// The table should use the data stack classes
	wantContains(t, body, `<table class="data stack">`)

	// Each row should have data-label attributes for stacking
	wantContains(t, body, `data-label="Veto / Fact"`)
	wantContains(t, body, `data-label="Contracts"`)
	wantContains(t, body, `data-label="Explanation"`)

	// Contracts should be stacked (A and B on separate lines with <br>)
	if !strings.Contains(body, seedTitleA) || !strings.Contains(body, seedTitleB) {
		t.Error("contracts should be present in the output")
	}

	// Explanation should be in a details element
	wantContains(t, body, "<details>", "<summary>Why</summary>")
	wantContains(t, body, "Different subject: BTC vs ETH")
}

// TestNearMissStripSummary verifies the strip shows count and veto count.
func TestNearMissStripSummary(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// The strip should show the count and veto count
	wantContains(t, body, `<dl class="strip">`)
	wantContains(t, body, "Count:", "2 near misses")
	wantContains(t, body, "Stopped by a veto:", "1")
}

// TestNearMissRowOrdering verifies vetoed pairs still come first.
func TestNearMissRowOrdering(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	_, factMissID := seedMore(t, s, st, sd)
	rec := get(s.Handler(), "/near-misses")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// The vetoed pair should appear before the fact-consulted pair
	vetoIdx := strings.Index(body, "subject_mismatch")
	factIdx := strings.Index(body, "fed_next_meeting did not place it")
	if vetoIdx < 0 || factIdx < 0 {
		t.Fatal("missing veto or fact text")
	}
	if vetoIdx > factIdx {
		t.Error("vetoed pairs should come first in the listing")
	}

	// Both pairs should link to their detail pages
	wantContains(t, body,
		`/pairs/`+strconv.FormatInt(sd.relID, 10),
		`/pairs/`+strconv.FormatInt(factMissID, 10))
}
