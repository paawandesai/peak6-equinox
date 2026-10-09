package web

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// TestRoutableGroupingAndOrder verifies that routable types (equivalent, equivalent_given_facts, complement)
// are grouped together under the "Routable pairs" heading, in the correct order (UI-03).
func TestRoutableGroupingAndOrder(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	// Insert instruments.
	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A title", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B title", "rules", "BTC", "source", polyFee)
	c := seedInstrument("kalshi:C", canonical.VenueKalshi, "C title", "rules", "BTC", "source", kalshiFee)
	for _, in := range []canonical.Instrument{a, b, c} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	// Insert one of each routable type: complement (0.95), equivalent_given_facts (0.91), equivalent (0.90).
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelComplement, Confidence: dec("0.95"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "Complement",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "kalshi:C", Type: canonical.RelEquivalentGivenFacts, Confidence: dec("0.91"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, c.Terms), Explanation: "Eq given facts",
		Vetoes: []canonical.Veto{{Code: "threshold_mismatch", Field: "threshold", Detail: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	eqID, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "polymarket:B", BID: "kalshi:C", Type: canonical.RelEquivalent, Confidence: dec("0.90"),
		Method: canonical.MethodRules, Diff: match.Diff(b.Terms, c.Terms), Explanation: "Equivalent",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Routable section exists with the count.
	wantContains(t, body, `<h2 id="routable">Routable pairs (3)</h2>`)

	// All three types appear in the routable section (UI-11 Cursor round 2: badge template).
	badgeComplement := `<span class="badge b-complement">`
	badgeEqGivenFacts := `<span class="badge b-equivalent_given_facts">`
	badgeEquivalent := `<span class="badge b-equivalent">`
	wantContains(t, body, badgeComplement, badgeEqGivenFacts, badgeEquivalent)

	// UI-11: Order is by ReviewPriority (confidence - 0.90) asc, then ID.
	// complement 0.95 → +0.05, equivalent_given_facts 0.91 → +0.01, equivalent 0.90 → 0.00.
	// So order: equivalent (0.00), equivalent_given_facts (+0.01), complement (+0.05).
	idx1 := strings.Index(body, badgeEquivalent)
	idx2 := strings.Index(body, badgeEqGivenFacts)
	idx3 := strings.Index(body, badgeComplement)
	if idx1 < 0 || idx2 < 0 || idx3 < 0 || idx1 >= idx2 || idx2 >= idx3 {
		t.Errorf("routable type badges not in priority order: equivalent at %d, equivalent_given_facts at %d, complement at %d", idx1, idx2, idx3)
	}

	// Cursor round 2 issue 2: assert round id appears inside a row (not bare substring from strip).
	// Find a tbody row (not thead) and verify it contains "round N" text.
	tbodyStart := strings.Index(body, `<tbody>`)
	if tbodyStart < 0 {
		t.Fatal("no tbody found")
	}
	rowStart := strings.Index(body[tbodyStart:], `<tr>`)
	if rowStart < 0 {
		t.Fatal("no tr in tbody found")
	}
	rowStart += tbodyStart
	rowEnd := strings.Index(body[rowStart:], `</tr>`)
	if rowEnd < 0 {
		t.Fatal("no closing tr found")
	}
	firstDataRow := body[rowStart : rowStart+rowEnd]
	wantRoundText := "round " + strconv.FormatInt(round.ID, 10)
	if !strings.Contains(firstDataRow, wantRoundText) {
		t.Errorf("first data row lacks round text %q; row content: %s", wantRoundText, firstDataRow[:200])
	}

	// UI-11: 2-click path: / contains /pairs/{id}#route for a routable pair.
	wantContains(t, body, `href="/pairs/`+strconv.FormatInt(eqID, 10)+`#route"`, "Route →")
}

// TestConfidenceBucketWords verifies that confidence scores map to the correct bucket words:
// "high" ≥ 0.90, "medium" 0.70 to 0.89, "low" below 0.70 (design.md § 4).
func TestConfidenceBucketWords(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B", "rules", "BTC", "source", polyFee)
	c := seedInstrument("kalshi:C", canonical.VenueKalshi, "C", "rules", "ETH", "source", kalshiFee)
	d := seedInstrument("polymarket:D", canonical.VenuePolymarket, "D", "rules", "ETH", "source", polyFee)
	for _, in := range []canonical.Instrument{a, b, c, d} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	// Boundary cases: 0.89 is medium, 0.90 is high.
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalent, Confidence: dec("0.89"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "0.89",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:C", BID: "polymarket:D", Type: canonical.RelEquivalent, Confidence: dec("0.90"),
		Method: canonical.MethodRules, Diff: match.Diff(c.Terms, d.Terms), Explanation: "0.90",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// UI-11 Cursor round 2: conf template outputs score and word in separate elements.
	wantContains(t, body, "0.89", "medium", "0.90", "high")
}

// TestVetoPhraseForFactsClosedPair verifies that equivalent_given_facts pairs with vetoes
// show "closed by facts" instead of "N vetoes" (design.md § 6).
func TestVetoPhraseForFactsClosedPair(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B", "rules", "BTC", "source", polyFee)
	for _, in := range []canonical.Instrument{a, b} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	eqGivenID, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalentGivenFacts, Confidence: dec("0.93"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "Eq given facts",
		Vetoes: []canonical.Veto{
			{Code: "threshold_mismatch", Field: "threshold", Detail: "4.25 vs 0.5"},
			{Code: "comparator_mismatch", Field: "comparator", Detail: "gt vs gte"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// The veto phrase for equivalent_given_facts is "closed by facts", not "2 vetoes".
	wantContains(t, body, `<a href="/pairs/`+strconv.FormatInt(eqGivenID, 10)+`#diff">closed by facts</a>`)
	if strings.Contains(body, "2 vetoes") {
		t.Error("should show 'closed by facts', not '2 vetoes' for equivalent_given_facts")
	}
}

// TestRelatedCountAndDetails verifies that related pairs are listed inside a <details>
// element with the summary "related (n): same event, different payout; never routed" (UI-03).
func TestRelatedCountAndDetails(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B", "rules", "BTC", "source", polyFee)
	c := seedInstrument("kalshi:C", canonical.VenueKalshi, "C", "rules", "BTC", "source", kalshiFee)
	for _, in := range []canonical.Instrument{a, b, c} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	// Insert two related pairs.
	relID1, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelRelated, Confidence: dec("0.40"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "Related 1",
		Vetoes: []canonical.Veto{{Code: "threshold_mismatch", Field: "threshold", Detail: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "kalshi:C", Type: canonical.RelRelated, Confidence: dec("0.35"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, c.Terms), Explanation: "Related 2",
		Vetoes: []canonical.Veto{{Code: "deadline_mismatch", Field: "deadline", Detail: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// The strip shows related count as 2.
	wantContains(t, body, "<dt>Related</dt><dd>2</dd>")

	// The summary contains the count and the phrase.
	wantContains(t, body, `<summary>related (2): same event, different payout; never routed</summary>`)

	// UI-11: assert related rows sit inside details.fold element without "open".
	detailsStart := strings.Index(body, `<details class="fold">`)
	detailsEnd := strings.Index(body, "</details>")
	if detailsStart < 0 || detailsEnd < 0 {
		t.Fatal("related pairs should be inside a <details class=\"fold\"> element")
	}
	if strings.Contains(body[detailsStart:detailsStart+25], "open") {
		t.Error("details element should not have 'open' attribute (collapsed by default)")
	}
	// Verify related rows are inside the details element (UI-11 Cursor round 2: badge template).
	relatedSection := body[detailsStart:detailsEnd]
	wantContains(t, relatedSection, `<span class="badge b-related">`)

	// Cursor round 1 issue 3: veto phrase "1 veto" with link to #diff.
	wantContains(t, body, `<a href="/pairs/`+strconv.FormatInt(relID1, 10)+`#diff">1 veto</a>`)
}

// TestTwoClickPath verifies that the landing page contains a direct link to /pairs/{id}#route
// for a routable pair, so users can reach the diff in one click and the trace in two (NFR-16).
func TestTwoClickPath(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// UI-11: The matches page contains a link to /pairs/{id}#route for the equivalent pair.
	wantContains(t, body, `href="/pairs/`+strconv.FormatInt(sd.eqID, 10)+`#route"`)
	wantContains(t, body, "Route →")
}

// TestMatchesSortOrders verifies that sort query param changes the order (UI-11).
func TestMatchesSortOrders(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B", "rules", "BTC", "source", polyFee)
	c := seedInstrument("kalshi:C", canonical.VenueKalshi, "C", "rules", "ETH", "source", kalshiFee)
	for _, in := range []canonical.Instrument{a, b, c} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	// Insert three pairs with different confidences: 0.85, 0.92, 0.88.
	id1, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalent, Confidence: dec("0.85"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "Pair 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:C", BID: "polymarket:B", Type: canonical.RelEquivalent, Confidence: dec("0.92"),
		Method: canonical.MethodRules, Diff: match.Diff(c.Terms, b.Terms), Explanation: "Pair 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	id3, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "kalshi:C", Type: canonical.RelEquivalent, Confidence: dec("0.88"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, c.Terms), Explanation: "Pair 3",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test confidence sort (desc).
	rec := get(s.Handler(), "/?sort=confidence")
	wantHTML(t, rec, 200)
	body := rec.Body.String()
	idx2 := strings.Index(body, fmt.Sprintf(`href="/pairs/%d"`, id2))
	idx3 := strings.Index(body, fmt.Sprintf(`href="/pairs/%d"`, id3))
	idx1 := strings.Index(body, fmt.Sprintf(`href="/pairs/%d"`, id1))
	if idx2 < 0 || idx3 < 0 || idx1 < 0 || idx2 >= idx3 || idx3 >= idx1 {
		t.Errorf("confidence sort: want id2 (0.92) < id3 (0.88) < id1 (0.85), got positions %d, %d, %d", idx2, idx3, idx1)
	}

	// Test id sort (asc).
	rec = get(s.Handler(), "/?sort=id")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	idx1 = strings.Index(body, fmt.Sprintf(`href="/pairs/%d"`, id1))
	idx2 = strings.Index(body, fmt.Sprintf(`href="/pairs/%d"`, id2))
	idx3 = strings.Index(body, fmt.Sprintf(`href="/pairs/%d"`, id3))
	if idx1 < 0 || idx2 < 0 || idx3 < 0 || idx1 >= idx2 || idx2 >= idx3 {
		t.Errorf("id sort: want id1 < id2 < id3, got positions %d, %d, %d", idx1, idx2, idx3)
	}

	// Test priority sort (for routable: (conf - 0.90) asc).
	// 0.85 → -0.05, 0.88 → -0.02, 0.92 → 0.02. Order: id1, id3, id2.
	rec = get(s.Handler(), "/?sort=priority")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	idx1 = strings.Index(body, fmt.Sprintf(`href="/pairs/%d"`, id1))
	idx3 = strings.Index(body, fmt.Sprintf(`href="/pairs/%d"`, id3))
	idx2 = strings.Index(body, fmt.Sprintf(`href="/pairs/%d"`, id2))
	if idx1 < 0 || idx3 < 0 || idx2 < 0 || idx1 >= idx3 || idx3 >= idx2 {
		t.Errorf("priority sort: want id1 (0.85) < id3 (0.88) < id2 (0.92), got positions %d, %d, %d", idx1, idx3, idx2)
	}
}

// TestMatchesChecksMapping verifies that diff statuses map to the correct check glyphs (UI-11).
func TestMatchesChecksMapping(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B", "rules", "BTC", "source", polyFee)
	for _, in := range []canonical.Instrument{a, b} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	// Insert a pair with diff statuses: deadline match, threshold differ, comparator unknown, source differ.
	// For egf, differ → ◇; for non-egf, differ → ✕; match → ✓; unknown → ?.
	diff := []canonical.FieldDiff{
		{Field: "deadline", A: "2024-01-01", B: "2024-01-01", Status: canonical.DiffMatch},
		{Field: "threshold", A: "100", B: "200", Status: canonical.DiffDiffer},
		{Field: "comparator", A: "gte", B: "unknown", Status: canonical.DiffUnknown},
		{Field: "source", A: "src1", B: "src2", Status: canonical.DiffDiffer},
	}
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalent, Confidence: dec("0.90"),
		Method: canonical.MethodRules, Diff: diff, Explanation: "Checks test",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Check for glyphs in the checks list.
	wantContains(t, body, `<ul class="checks"`)
	wantContains(t, body, `<span aria-hidden="true" class="check-match">✓</span> Date`)        // deadline match
	wantContains(t, body, `<span aria-hidden="true" class="check-differ">✕</span> Strike`)     // threshold differ
	wantContains(t, body, `<span aria-hidden="true" class="check-unknown">?</span> Direction`) // comparator unknown
	wantContains(t, body, `<span aria-hidden="true" class="check-differ">✕</span> Source`)     // source differ

	// Now test egf: differ should show ◇.
	diffEGF := []canonical.FieldDiff{
		{Field: "deadline", A: "2024-01-01", B: "2024-01-01", Status: canonical.DiffMatch},
		{Field: "threshold", A: "100", B: "200", Status: canonical.DiffDiffer},
		{Field: "comparator", A: "gte", B: "gt", Status: canonical.DiffDiffer},
		{Field: "source", A: "src1", B: "src1", Status: canonical.DiffMatch},
	}
	c := seedInstrument("kalshi:C", canonical.VenueKalshi, "C", "rules", "ETH", "source", kalshiFee)
	if err := st.UpsertInstrument(ctx, round.ID, c); err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "kalshi:C", Type: canonical.RelEquivalentGivenFacts, Confidence: dec("0.93"),
		Method: canonical.MethodRules, Diff: diffEGF, Explanation: "EGF checks test",
		Vetoes: []canonical.Veto{{Code: "threshold_mismatch", Field: "threshold", Detail: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec = get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body = rec.Body.String()

	// For egf, differ shows ◇.
	wantContains(t, body, `<span aria-hidden="true" class="check-egf">◇</span> Strike`)    // threshold differ + egf
	wantContains(t, body, `<span aria-hidden="true" class="check-egf">◇</span> Direction`) // comparator differ + egf
}

// TestMatchesFiltering verifies that min and q filters work with chips (UI-11).
func TestMatchesFiltering(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "Bitcoin above 50k", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "Bitcoin over 50000", "rules", "BTC", "source", polyFee)
	c := seedInstrument("kalshi:C", canonical.VenueKalshi, "Ethereum above 3k", "rules", "ETH", "source", kalshiFee)
	d := seedInstrument("polymarket:D", canonical.VenuePolymarket, "ETH over 3000", "rules", "ETH", "source", polyFee)
	for _, in := range []canonical.Instrument{a, b, c, d} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	// Insert pairs: one with 0.85 (below min), one with 0.92 (above min).
	id1, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalent, Confidence: dec("0.85"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "BTC pair",
	})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:C", BID: "polymarket:D", Type: canonical.RelEquivalent, Confidence: dec("0.92"),
		Method: canonical.MethodRules, Diff: match.Diff(c.Terms, d.Terms), Explanation: "ETH pair",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Test min=0.90 filter: only id2 should appear.
	rec := get(s.Handler(), "/?min=0.90")
	wantHTML(t, rec, 200)
	body := rec.Body.String()
	// Check that the filter chip is shown.
	wantContains(t, body, "Confidence ≥ 0.90")
	// Check that the correct pair appears.
	wantContains(t, body, fmt.Sprintf(`href="/pairs/%d"`, id2))
	if strings.Contains(body, fmt.Sprintf(`href="/pairs/%d"`, id1)) {
		t.Error("min=0.90 filter: id1 (0.85) should be filtered out")
	}

	// Test q filter: q=bitcoin should show only id1.
	rec = get(s.Handler(), "/?q=bitcoin")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	// Check that the filter chip is shown (case-insensitive).
	wantContains(t, body, "bitcoin")
	wantContains(t, body, fmt.Sprintf(`href="/pairs/%d"`, id1))
	if strings.Contains(body, fmt.Sprintf(`href="/pairs/%d"`, id2)) {
		t.Error("q=bitcoin filter: id2 (Ethereum) should be filtered out")
	}

	// Test combined filters: min=0.90&q=eth should show only id2.
	rec = get(s.Handler(), "/?min=0.90&q=eth")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	wantContains(t, body, "≥ 0.90", "eth")
	wantContains(t, body, fmt.Sprintf(`href="/pairs/%d"`, id2))
	if strings.Contains(body, fmt.Sprintf(`href="/pairs/%d"`, id1)) {
		t.Error("combined filters: id1 should be filtered out")
	}

	// Test filter chips are removable.
	rec = get(s.Handler(), "/?min=0.90")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	wantContains(t, body, "chip-filter removable", "Confidence ≥ 0.90")
}

// TestComplementBadge verifies that complement rows have the correct blue badge (UI-11).
func TestComplementBadge(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B", "rules", "BTC", "source", polyFee)
	for _, in := range []canonical.Instrument{a, b} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelComplement, Confidence: dec("0.95"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "Complement",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// UI-11 Cursor round 2: Complement uses b-complement badge class (per UI-10 TypeBadge).
	wantContains(t, body, `<span class="badge b-complement">`, "complement")
}

// TestSectionOrder verifies that sections appear in the correct order: routable, then implies, then related (Cursor round 1 issue 3).
func TestSectionOrder(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B", "rules", "BTC", "source", polyFee)
	c := seedInstrument("kalshi:C", canonical.VenueKalshi, "C", "rules", "BTC", "source", kalshiFee)
	d := seedInstrument("polymarket:D", canonical.VenuePolymarket, "D", "rules", "BTC", "source", polyFee)
	for _, in := range []canonical.Instrument{a, b, c, d} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	// Insert one of each type: equivalent (routable), implies, related.
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalent, Confidence: dec("0.90"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "Eq",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:C", BID: "polymarket:D", Type: canonical.RelImplies, Confidence: dec("0.85"),
		Method: canonical.MethodRules, Diff: match.Diff(c.Terms, d.Terms), Explanation: "Implies",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "kalshi:C", Type: canonical.RelRelated, Confidence: dec("0.40"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, c.Terms), Explanation: "Related",
		Vetoes: []canonical.Veto{{Code: "subject_mismatch", Field: "subject", Detail: ""}},
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Find section positions.
	routablePos := strings.Index(body, `<h2 id="routable">`)
	impliesPos := strings.Index(body, `<h2 id="type-implies">`)
	relatedPos := strings.Index(body, `<summary>related`)

	if routablePos < 0 || impliesPos < 0 || relatedPos < 0 {
		t.Fatalf("missing sections: routable=%d, implies=%d, related=%d", routablePos, impliesPos, relatedPos)
	}

	// Assert order: routable < implies < related.
	if routablePos >= impliesPos || impliesPos >= relatedPos {
		t.Errorf("sections out of order: routable at %d, implies at %d, related at %d", routablePos, impliesPos, relatedPos)
	}
}
