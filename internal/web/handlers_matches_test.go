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

// TestRelatedCountAndDetails verifies the related summary on / (M5-01,
// replacing the UI-03 fold): the strip and heading keep the full count, only
// the top relatedPreviewRows rows render, the link goes to /near-misses, and
// ?q= still finds every matching related pair.
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

	// One Kalshi anchor and relatedPreviewRows+2 Polymarket contracts, each
	// related to the anchor with a distinct confidence, so 12 related pairs.
	const n = relatedPreviewRows + 2
	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "Anchor A", "rules", "BTC", "source", kalshiFee)
	if err := st.UpsertInstrument(ctx, round.ID, a); err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("polymarket:R%02d", i)
		b := seedInstrument(id, canonical.VenuePolymarket, fmt.Sprintf("Related market %02d", i), "rules", "BTC", "source", polyFee)
		if err := st.UpsertInstrument(ctx, round.ID, b); err != nil {
			t.Fatal(err)
		}
		// Confidence falls with i, so review priority (-confidence asc) ranks i=0 first.
		ids[i], err = st.UpsertRelationship(ctx, canonical.Relationship{
			RoundID: round.ID, AID: a.ID, BID: id, Type: canonical.RelRelated,
			Confidence: dec(fmt.Sprintf("0.%02d", 60-i)),
			Method:     canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "Related",
			Vetoes: []canonical.Veto{{Code: "threshold_mismatch", Field: "threshold", Detail: ""}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	pairLink := func(i int) string { return `<a href="/pairs/` + strconv.FormatInt(ids[i], 10) + `">` }

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Counts are the full count, before the cut.
	wantContains(t, body,
		"<dt>Related</dt><dd>12</dd>",
		`<h2 id="type-related">related (12)</h2>`,
		"12 related pairs: same event, different payout; never routed.",
		"Top 10 by review priority.",
		`<a href="/near-misses">See all 12 with the rule that separated them →</a>`,
	)
	if strings.Contains(body, `<details class="fold">`) {
		t.Error("related pairs must no longer sit in a <details> fold")
	}
	section := body[strings.Index(body, `<h2 id="type-related">`):]
	if got := strings.Count(section, "<tr>"); got != relatedPreviewRows+1 { // +1 header row
		t.Errorf("related section has %d <tr>, want %d rows plus the header", got, relatedPreviewRows)
	}
	wantContains(t, section, pairLink(0), pairLink(relatedPreviewRows-1), `<span class="badge b-related">`)
	for _, i := range []int{relatedPreviewRows, relatedPreviewRows + 1} {
		if strings.Contains(section, pairLink(i)) {
			t.Errorf("pair %d is below the top %d and should not render without a search", ids[i], relatedPreviewRows)
		}
	}
	// Veto phrase still links to the diff.
	wantContains(t, body, `<a href="/pairs/`+strconv.FormatInt(ids[0], 10)+`#diff">1 veto</a>`)

	// ?q= finds a related pair below the cut, and shows every match.
	rec = get(s.Handler(), "/?q=market+11")
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	wantContains(t, body, pairLink(n-1), "<dt>Related</dt><dd>1</dd>", `<a href="/near-misses">See every related pair with the rule that separated them →</a>`)
	rec = get(s.Handler(), "/?q=related+market")
	body = rec.Body.String()
	for i := range ids {
		wantContains(t, body, pairLink(i))
	}
	if strings.Contains(body, "Top 10 by review priority.") {
		t.Error("a search must show every matching related pair, not the top 10")
	}

	// ?sort=id keeps the cut and says so.
	rec = get(s.Handler(), "/?sort=id")
	body = rec.Body.String()
	wantContains(t, body, "First 10 in the chosen order.", pairLink(0))
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

	// Find section positions (ui/plain-language: routable cards, then the
	// one-way summary, then the lookalikes summary; the full tables follow
	// under "Filters and technical details").
	routablePos := strings.Index(body, `<h2 id="pairs">`)
	impliesPos := strings.Index(body, `<details id="type-implies">`)
	relatedPos := strings.Index(body, `class="fold-summary related-summary"`)

	if routablePos < 0 || impliesPos < 0 || relatedPos < 0 {
		t.Fatalf("missing sections: routable=%d, implies=%d, related=%d", routablePos, impliesPos, relatedPos)
	}

	// Assert order: routable < implies < related.
	if routablePos >= impliesPos || impliesPos >= relatedPos {
		t.Errorf("sections out of order: routable at %d, implies at %d, related at %d", routablePos, impliesPos, relatedPos)
	}
}

// TestStartHereCard verifies the "Start here" card with links to the featured pair and near-misses (M5-05 AC 1, 2).
func TestStartHereCard(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A title", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B title", "rules", "BTC", "source", polyFee)
	// Add instruments for a fact_did_not_close near-miss (C and D are related, fact consulted but did not close)
	c := seedInstrument("kalshi:C", canonical.VenueKalshi, "C title", "rules", "ETH", "source", kalshiFee)
	d := seedInstrument("polymarket:D", canonical.VenuePolymarket, "D title", "rules", "ETH", "source", polyFee)
	for _, in := range []canonical.Instrument{a, b, c, d} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	// Insert routable pair (will be featured).
	pairID, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalent, Confidence: dec("0.90"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "Equivalent",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Insert a near-miss pair: related with no explicit veto (fact_did_not_close case).
	// This creates the group-fact-did-not-close anchor on /near-misses.
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:C", BID: "polymarket:D", Type: canonical.RelRelated, Confidence: dec("0.70"),
		Method: canonical.MethodRules, Diff: match.Diff(c.Terms, d.Terms), Explanation: "Related: fact consulted but did not close",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// Start here card exists.
	wantContains(t, body, `<div class="start-here">`, `<h2>Start here</h2>`)

	// Link 1: See why two contracts are the same bet → the pair page (ui/plain-language: its top card).
	wantContains(t, body, fmt.Sprintf(`href="/pairs/%d"`, pairID), "See why two contracts are the same bet")

	// Link 2: Route a hypothetical order on it → pair route.
	wantContains(t, body, fmt.Sprintf(`href="/pairs/%d#route"`, pairID), "Route a hypothetical order on it")

	// M6 P1: with no Fed demo pairs in the round the card falls back to the
	// featured pair only; the near-miss step is resolved per pair (see
	// start_here_test.go) and never links a group that may be empty.
	if strings.Contains(body, `href="/near-misses#group-fact-did-not-close"`) {
		t.Error("fallback card links the near-misses group anchor")
	}

	// Verify that the anchor exists on the /near-misses page.
	nearMissesRec := get(s.Handler(), "/near-misses")
	wantHTML(t, nearMissesRec, 200)
	nearMissesBody := nearMissesRec.Body.String()
	wantContains(t, nearMissesBody, `id="group-fact-did-not-close"`)

	// Routable section intro sentence (M5-05). Singular "pair" when count is 1.
	wantContains(t, body, "1 pair Equinox will route an order across: same bet (equivalent), same bet once a stated fact is applied (equivalent given facts), or exact opposites (complement).")
}

// TestStartHereEmptyState verifies the empty state when no routable pairs exist (M5-05 AC 3).
func TestStartHereEmptyState(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(60*time.Second)); err != nil {
		t.Fatal(err)
	}

	// No routable pairs, only a related pair.
	a := seedInstrument("kalshi:A", canonical.VenueKalshi, "A", "rules", "BTC", "source", kalshiFee)
	b := seedInstrument("polymarket:B", canonical.VenuePolymarket, "B", "rules", "BTC", "source", polyFee)
	for _, in := range []canonical.Instrument{a, b} {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	_, err = st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelRelated, Confidence: dec("0.80"),
		Method: canonical.MethodRules, Diff: match.Diff(a.Terms, b.Terms), Explanation: "Related",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()

	// No start-here card.
	if strings.Contains(body, `<div class="start-here">`) {
		t.Error("empty state should not show start-here card when no routable pairs")
	}

	// Empty state message with links to near-misses and eval.
	wantContains(t, body,
		"No routable pairs in this round",
		`href="/near-misses"`,
		`href="/eval"`,
	)
}
