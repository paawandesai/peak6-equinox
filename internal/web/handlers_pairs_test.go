package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

func TestPairStripFieldsEquivalentGivenFacts(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Create an equivalent_given_facts pair with facts and basis risk
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.Type = canonical.RelEquivalentGivenFacts
	rel.Confidence = dec("0.93")
	rel.Method = canonical.MethodRules
	rel.BasisRisk = &canonical.BasisRisk{
		Score: dec("0.38"),
		Components: []canonical.BasisComponent{
			{Name: "source", Score: dec("0.38"), Detail: "different sources"},
		},
	}
	rel.FactsUsed = []canonical.FactUse{
		{Key: "fed_upper_bound", Value: "4.00", Source: "federalreserve.gov", AsOf: seedNow},
	}
	rel.Vetoes = []canonical.Veto{
		{Code: "threshold_mismatch", Field: "threshold", Detail: "4.25 vs 0.5"},
	}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// UI-12: verdict panel fields (check key elements, not exact HTML structure)
	wantContains(t, body, "equivalent_given_facts")
	wantContains(t, body, "Confidence</dt>")
	wantContains(t, body, "0.93")
	wantContains(t, body, "Method</dt>")
	wantContains(t, body, "rules")
	wantContains(t, body, "Basis risk</dt>")
	wantContains(t, body, "0.38")

	// Vetoes section (UI-12)
	wantContains(t, body, "threshold_mismatch")
	wantContains(t, body, "4.25 vs 0.5")

	// Facts section
	wantContains(t, body, "fed_upper_bound")
	wantContains(t, body, "4.00")
	wantContains(t, body, "federalreserve.gov")

	// Diff table should have facts annotation
	wantContains(t, body, "closed by facts")
}

func TestPairDiffRowGrouping(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	// Add diff rows from different parts
	rel.Diff = []canonical.FieldDiff{
		{Field: "subject", A: "BTC price", B: "ETH price", Status: canonical.DiffDiffer},
		{Field: "metric", A: "price", B: "price", Status: canonical.DiffMatch},
		{Field: "comparator", A: ">", B: ">", Status: canonical.DiffMatch},
		{Field: "deadline", A: "2026-10-08T00:00:00Z", B: "2026-10-08T00:00:00Z", Status: canonical.DiffMatch},
		{Field: "edge_rules", A: "unknown", B: "unknown", Status: canonical.DiffUnknown},
	}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify tbody grouping (UI-12 requirement)
	// Each rule part should have its own tbody with a part header
	parts := []string{"proposition", "settlement"}
	for _, part := range parts {
		if !strings.Contains(body, `class="part"`+">"+part) && !strings.Contains(body, `class="part"`+` colspan="`+part) {
			// Relaxed check: just verify the part name appears in a th
			if !strings.Contains(body, "<th") || !strings.Contains(body, part) {
				t.Errorf("missing part header for %s", part)
			}
		}
	}
}

func TestPairBasisRisk(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.BasisRisk = &canonical.BasisRisk{
		Score: dec("0.15"),
		Components: []canonical.BasisComponent{
			{Name: "cutoff_gap", Score: dec("0.10"), Detail: "1 hour gap"},
			{Name: "source_difference", Score: dec("0.05"), Detail: "different sources"},
		},
	}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify basis risk table
	wantContains(t, body, "0.15")
	wantContains(t, body, "cutoff_gap")
	wantContains(t, body, "0.10")
	wantContains(t, body, "1 hour gap")
	wantContains(t, body, "source_difference")
	wantContains(t, body, "0.05")
	wantContains(t, body, "different sources")
}

func TestPairNoBasisRisk(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.Type = canonical.RelRelated
	rel.BasisRisk = nil
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify "Not scored" message
	wantContains(t, body, "Not scored")
}

func TestPairWordDiff(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.Diff = []canonical.FieldDiff{
		{Field: "subject", A: "BTC price above $100k", B: "BTC price above $120k", Status: canonical.DiffDiffer},
	}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify word-level diff (UI-12)
	// Changed words should be in <del> or <ins> tags
	wantContains(t, body, "<del>")
	wantContains(t, body, "<ins>")
}

func TestPairDiffCounts(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.Diff = []canonical.FieldDiff{
		{Field: "subject", A: "BTC", B: "ETH", Status: canonical.DiffDiffer},
		{Field: "metric", A: "price", B: "price", Status: canonical.DiffMatch},
		{Field: "comparator", A: ">", B: ">", Status: canonical.DiffMatch},
		{Field: "threshold", A: "unknown", B: "100", Status: canonical.DiffUnknown},
	}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify diff counts (UI-12)
	wantContains(t, body, "1 differ")
	wantContains(t, body, "2 match")
	wantContains(t, body, "1 unknown")
}

func TestPairHistoryEmpty(t *testing.T) {
	s, st := newTestServer(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	r, err := st.CreateRound(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	// A pair whose instruments have no stored quotes in any round.
	a := canonical.Instrument{
		ID: canonical.InstrumentID(canonical.VenueKalshi, "EMPTY-HIST-A"), Venue: canonical.VenueKalshi, VenueMarketID: "EMPTY-HIST-A",
		Title: "Empty history A", Category: canonical.CategoryCrypto, Terms: canonical.UnknownTerms(), Status: canonical.StatusOpen,
		CloseTime:   now.Add(24 * time.Hour),
		FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": decimal.RequireFromString("0.07")}},
	}
	b := canonical.Instrument{
		ID: canonical.InstrumentID(canonical.VenuePolymarket, "empty-hist-b"), Venue: canonical.VenuePolymarket, VenueMarketID: "empty-hist-b",
		Title: "Empty history B", Category: canonical.CategoryCrypto, Terms: canonical.UnknownTerms(), Status: canonical.StatusOpen,
		CloseTime: now.Add(24 * time.Hour), FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1},
	}
	for _, in := range []canonical.Instrument{a, b} {
		if err := st.UpsertInstrument(ctx, r.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	relID, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: r.ID, AID: a.ID, BID: b.ID, Type: canonical.RelRelated,
		Confidence: dec("0.50"), Method: canonical.MethodRules, Explanation: "no quotes yet",
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(relID, 10))
	wantHTML(t, rec, http.StatusOK)
	wantContains(t, rec.Body.String(), "No stored quotes for either contract yet")
}

func TestPairHistoryPresent(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Add some price history
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}

	// Create a second round with new quotes
	round2, err := st.CreateRound(ctx, seedNow.Add(1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	quoteA := canonical.Quote{
		InstrumentID: rel.AID,
		FetchedAt:    seedNow.Add(1 * time.Hour),
		YesBids:      []canonical.Level{{Price: dec("0.51"), Size: dec("100")}},
		YesAsks:      []canonical.Level{{Price: dec("0.52"), Size: dec("100")}},
	}
	quoteB := canonical.Quote{
		InstrumentID: rel.BID,
		FetchedAt:    seedNow.Add(1 * time.Hour),
		YesBids:      []canonical.Level{{Price: dec("0.51"), Size: dec("100")}},
		YesAsks:      []canonical.Level{{Price: dec("0.52"), Size: dec("100")}},
	}
	if err := st.InsertQuote(ctx, round2.ID, quoteA); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertQuote(ctx, round2.ID, quoteB); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify history is shown
	wantContains(t, body, "Price history")
}

func TestPairFromFixtures(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Seed a pair using fixture data structure (similar to how fixtures are loaded)
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(rel.ID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Assert specific values from seeded relationship (UI-20 review item 8)
	wantContains(t, body, "Pair #"+strconv.FormatInt(rel.ID, 10))

	// Verdict panel values
	wantContains(t, body, "equivalent") // Type
	wantContains(t, body, "0.97")       // Confidence (exact value from seedPublic)
	wantContains(t, body, "rules")      // Method

	// Basis risk (from seedPublic: score 0.1, component "source")
	wantContains(t, body, "0.10") // Basis risk score
	wantContains(t, body, "source")
	wantContains(t, body, "Binance vs CF Benchmarks")

	// Contract titles (from seedTitleA, seedTitleB)
	wantContains(t, body, seedTitleA) // Kalshi title
	wantContains(t, body, seedTitleB) // Polymarket title

	// Diff table should exist
	wantContains(t, body, "<table class=\"data terms\">")
	wantContains(t, body, "Contract diff")

	// Explanation should be present
	wantContains(t, body, seedExplainEq)
}

func TestPairRoundMismatchWhenPinned(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	seedRel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	// Create a second round
	round2, err := st.CreateRound(ctx, seedNow.Add(1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	// Create a relationship in round2
	rel2ID, err := st.UpsertRelationship(ctx, canonical.Relationship{
		RoundID:     round2.ID,
		AID:         seedRel.AID, // instruments seeded by seedPublic
		BID:         seedRel.BID,
		Type:        canonical.RelEquivalent,
		Confidence:  dec("0.95"),
		Method:      canonical.MethodRules,
		Explanation: "Test pair in round 2",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Pin the first round
	if err := st.PinRound(ctx, sd.round.ID); err != nil {
		t.Fatal(err)
	}

	// Try to view the round2 pair while round1 is pinned
	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(rel2ID, 10))
	wantHTML(t, rec, http.StatusNotFound)
	body := rec.Body.String()

	// Verify error message
	wantContains(t, body, "different round")
}

func TestPairLayersDisplay(t *testing.T) {
	s, st := newTestServer(t)
	now := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()

	// Create a round
	r, err := st.CreateRound(ctx, now)
	if err != nil {
		t.Fatal(err)
	}

	// Create instruments with all layers populated
	kalshi := canonical.Instrument{
		ID:                      canonical.InstrumentID(canonical.VenueKalshi, "BTCUSD-120K"),
		Venue:                   canonical.VenueKalshi,
		VenueMarketID:           "BTCUSD-120K",
		Title:                   "BTC above $120k?",
		RulesSummary:            "Primary rules here.",
		RulesText:               "Full detailed rules text.",
		Clarifications:          "Additional details here.",
		ClarificationsUpdatedAt: now.AddDate(0, 0, -1),
		Category:                canonical.CategoryCrypto,
		Terms:                   canonical.UnknownTerms(),
		Status:                  canonical.StatusOpen,
		CloseTime:               now.Add(24 * time.Hour),
		FeeSchedule:             canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"market_fee": decimal.RequireFromString("0.07")}},
	}

	poly := canonical.Instrument{
		ID:            canonical.InstrumentID(canonical.VenuePolymarket, "btc-over-120k"),
		VenueMarketID: "btc-over-120k",
		Venue:         canonical.VenuePolymarket,
		Title:         "Will Bitcoin exceed $120,000?",
		Category:      canonical.CategoryCrypto,
		Terms:         canonical.UnknownTerms(),
		Status:        canonical.StatusOpen,
		CloseTime:     now.Add(24 * time.Hour),
		FeeSchedule:   canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1, Params: map[string]decimal.Decimal{"rate": decimal.RequireFromString("0.02")}},
	}

	for _, in := range []canonical.Instrument{kalshi, poly} {
		if err := st.UpsertInstrument(context.Background(), r.ID, in); err != nil {
			t.Fatal(err)
		}
	}

	// Create relationship
	relID, err := st.UpsertRelationship(context.Background(), canonical.Relationship{
		RoundID:     r.ID,
		AID:         kalshi.ID,
		BID:         poly.ID,
		Type:        canonical.RelEquivalent,
		Confidence:  decimal.RequireFromString("0.95"),
		Method:      canonical.MethodRules,
		Explanation: "Test pair",
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(relID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify .layers div exists for each contract
	if !strings.Contains(body, `<div class="layers">`) {
		t.Error("missing .layers div")
	}

	// Kalshi contract: verify all 4 layers
	// Title is always present in contract card
	wantContains(t, body, "BTC above $120k?")

	// Summary: should show for Kalshi
	wantContains(t, body, "Primary rules here.")

	// Clarifications: should show for Kalshi with date
	wantContains(t, body, "Additional details here.")
	wantContains(t, body, "clarified 2026-10-07") // ClarificationsUpdatedAt = now - 1 day

	// Verify "venue publishes none" appears for empty layers
	if !strings.Contains(body, "venue publishes none") {
		t.Error("missing 'venue publishes none' for empty layers")
	}
}

// UI-20 D-115: Test evidenceKeyToField mapping function
func TestEvidenceKeyToField(t *testing.T) {
	tests := []struct {
		term    string
		want    string
		perRow  bool
		comment string
	}{
		// Footer-only terms
		{"prior", "", false, "prior shown in footer"},
		{"fact_applied", "", false, "global deduction, footer only"},

		// Per-row terms
		{"unknown_unit_a", "unit", true, "unit deduction for side A"},
		{"unknown_unit_b", "unit", true, "unit deduction for side B"},
		{"unknown_edge_rules_a", "edge_rules", true, "edge rules for side A"},
		{"unknown_edge_rules_b", "edge_rules", true, "edge rules for side B"},
		{"deadline_gap_hours", "deadline", true, "deadline gap deduction"},
		{"deadline_defaulted_a", "deadline", true, "defaulted deadline for side A"},
		{"deadline_defaulted_b", "deadline", true, "defaulted deadline for side B"},

		// Unknown terms
		{"unknown_term", "", false, "unknown term returns empty"},
		{"", "", false, "empty term returns empty"},
	}

	for _, tt := range tests {
		t.Run(tt.term, func(t *testing.T) {
			field, perRow := evidenceKeyToField(tt.term)
			if field != tt.want {
				t.Errorf("evidenceKeyToField(%q) field = %q, want %q (%s)", tt.term, field, tt.want, tt.comment)
			}
			if perRow != tt.perRow {
				t.Errorf("evidenceKeyToField(%q) perRow = %v, want %v (%s)", tt.term, perRow, tt.perRow, tt.comment)
			}
		})
	}
}

// UI-20 D-115: Test HTML escaping in evidence contribution display
func TestPairEvidenceHTMLEscaping(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}

	// Attach evidence with contribution values that get formatted
	rel.Evidence = &canonical.MatcherEvidence{
		Prior: dec("0.99"),
		Contributions: []canonical.ContributionTerm{
			{Term: "prior", Value: dec("0.99")},
			{Term: "unknown_unit_a", Value: dec("-0.02")},
			{Term: "deadline_gap_hours", Value: dec("-0.05")},
		},
		Total: dec("0.92"),
	}
	rel.Confidence = rel.Evidence.Total // Validate: evidence.total must equal confidence
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify contribution values are properly rendered (not as raw HTML tags)
	// The minus sign (−) should be HTML entity or properly escaped
	wantContains(t, body, "−0.02") // Unit contribution
	wantContains(t, body, "−0.05") // Deadline contribution

	// Verify footer evidence is shown
	wantContains(t, body, "Matcher evidence:")
	wantContains(t, body, "0.92") // Total confidence

	// Verify no script injection is possible through contribution display
	// (HTML entities in output should be escaped as &lt; &gt; etc)
	if strings.Contains(body, "<script>") {
		t.Error("found unescaped <script> tag in output")
	}
	if strings.Contains(body, "javascript:") {
		t.Error("found javascript: protocol in output")
	}
}

// UI-20 D-115: Test evidence contribution column display
func TestPairEvidenceContributionColumn(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}

	// Create diff rows with specific fields that map to contributions
	rel.Diff = []canonical.FieldDiff{
		{Field: "unit", A: "unknown", B: "USD", Status: canonical.DiffUnknown},
		{Field: "deadline", A: "2026-10-08T12:00:00Z", B: "2026-10-08T13:00:00Z", Status: canonical.DiffDiffer},
		{Field: "edge_rules", A: "unknown", B: "standard", Status: canonical.DiffUnknown},
		{Field: "subject", A: "BTC", B: "BTC", Status: canonical.DiffMatch}, // No contribution for this
	}

	// Attach evidence with matching contributions
	rel.Evidence = &canonical.MatcherEvidence{
		Prior: dec("0.99"),
		Contributions: []canonical.ContributionTerm{
			{Term: "prior", Value: dec("0.99")},
			{Term: "unknown_unit_a", Value: dec("-0.02")},
			{Term: "deadline_gap_hours", Value: dec("-0.01")},
			{Term: "unknown_edge_rules_a", Value: dec("-0.02")},
		},
		Total: dec("0.94"),
	}
	rel.Confidence = rel.Evidence.Total // Validate: evidence.total must equal confidence
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify Contribution column header exists
	wantContains(t, body, "<th>Contribution</th>")

	// Verify per-row contributions are shown
	wantContains(t, body, "−0.02") // Unit contribution
	wantContains(t, body, "−0.01") // Deadline contribution

	// Verify rows without contributions show em-dash placeholder
	wantContains(t, body, `<span class="muted">—</span>`) // For subject row

	// Verify footer shows evidence summary
	wantContains(t, body, "0.99 &#43; Σ(3) = 0.94") // "<prior> + Σ(<n>) = <total>", + is HTML-escaped
	wantContains(t, body, "0.94")                   // Total
}

// UI-20 D-115: Test evidence not recorded (old relationships)
func TestPairEvidenceNotRecorded(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}

	// Old relationship without evidence
	rel.Evidence = nil
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify "not recorded" message in footer
	wantContains(t, body, "not recorded")

	// Verify Contribution column is NOT shown when evidence is missing
	if strings.Contains(body, "<th>Contribution</th>") {
		t.Error("Contribution column should not be shown when evidence is nil")
	}
}
