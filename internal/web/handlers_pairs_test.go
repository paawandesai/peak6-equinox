package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
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

	// Strip fields - assert specific dt/dd pairs
	wantContains(t, body,
		`<dt>Type</dt><dd><span class="chip chip-ok">equivalent_given_facts</span></dd>`,
		`<dt>Confidence</dt><dd>0.93 high</dd>`,
		`<dt>Method</dt><dd>rules</dd>`,
		`<dt>Basis risk</dt><dd>0.38 moderate</dd>`,
		`<dt>Routable</dt><dd>yes</dd>`,
		`<dt>Round</dt><dd>`, // contains round ID and as-of with timezone
		"2026-10-06",         // as-of date in strip
		"EDT",                // timezone in strip
		`<a href="#route">Route an order`,
	)
}

func TestPairStripFieldsRelatedNearMiss(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.relID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Strip fields for a related (near miss)
	wantContains(t, body,
		`<span class="chip chip-warn">related</span>`, // type chip
		"Routable</dt><dd>no</dd>",                    // routable: no
		"not scored",                                  // basis risk: not scored
	)
}

func TestPairDiffRowAnnotations(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Make the equivalent pair have vetoes
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.Vetoes = []canonical.Veto{
		{Code: "threshold_mismatch", Field: "threshold", Detail: "120000 vs 120001"},
	}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Diff row with veto annotation
	wantContains(t, body, "veto threshold_mismatch")
}

func TestPairDiffRowAnnotationsEquivalentGivenFacts(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Test 1: Multiple facts, listed veto code
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.Type = canonical.RelEquivalentGivenFacts
	rel.FactsUsed = []canonical.FactUse{
		{Key: "fed_upper_bound", Value: "4.00", Source: "federalreserve.gov", AsOf: seedNow},
		{Key: "fed_rate_step", Value: "0.25", Source: "FOMC", AsOf: seedNow},
	}
	rel.Vetoes = []canonical.Veto{
		{Code: "threshold_mismatch", Field: "threshold", Detail: "4.25 vs 0.5"},
		{Code: "metric_mismatch", Field: "metric", Detail: "rate vs change"},
	}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// All facts listed in annotation
	wantContains(t, body,
		"veto threshold_mismatch, closed by facts: fed_upper_bound=4.00, fed_rate_step=0.25",
		"veto metric_mismatch, closed by facts: fed_upper_bound=4.00, fed_rate_step=0.25",
	)

	// Test 2: Non-listed veto code (still shows veto)
	rel.Vetoes = append(rel.Vetoes, canonical.Veto{
		Code: "source_mismatch", Field: "resolution_source", Detail: "different sources",
	})
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}
	body = get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10)).Body.String()
	wantContains(t, body, "veto source_mismatch, closed by facts:")

	// Test 3: Facts-closed pair with no facts (should show just veto, not "closed by facts")
	rel.FactsUsed = nil
	// Reset to just one veto so the heading doesn't say "closed by facts" either
	rel.Vetoes = []canonical.Veto{
		{Code: "threshold_mismatch", Field: "threshold", Detail: "4.25 vs 0.5"},
	}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}
	body = get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10)).Body.String()
	wantContains(t, body, "veto threshold_mismatch")
	// Check diff row annotation doesn't say "closed by facts"
	if strings.Contains(body, "closed by facts:") {
		t.Error("diff row should not say 'closed by facts:' when no facts present")
	}
}

func TestPairVetoHeadingVariants(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Test 1: equivalent_given_facts heading
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.Type = canonical.RelEquivalentGivenFacts
	rel.Vetoes = []canonical.Veto{
		{Code: "threshold_mismatch", Field: "threshold", Detail: "4.25 vs 0.5"},
		{Code: "metric_mismatch", Field: "metric", Detail: "rate vs change"},
		{Code: "comparator_mismatch", Field: "comparator", Detail: "gt vs gte"},
	}
	rel.FactsUsed = []canonical.FactUse{
		{Key: "fed_upper_bound", Value: "4.00", Source: "federalreserve.gov", AsOf: seedNow},
	}
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body, "3 term differences, closed by facts")

	// Test 2: related heading with vetoes
	body = get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.relID, 10)).Body.String()
	wantContains(t, body, "Vetoes (1): each blocks equivalent")
}

func TestPairSectionOrder(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Seed a decision-log entry for this pair
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendLog(ctx, store.LogRelationship, strconv.FormatInt(rel.ID, 10),
		"equivalent (0.97, rules): "+seedKalshiA,
		map[string]interface{}{"pair_id": rel.ID}, seedNow); err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Find section markers in order: contracts (h2), diff (#diff), facts (h3),
	// basis (h3 after facts), explanation (h2), touch (h2), history (#history),
	// log (h3 "Decision-log entries"), route (#route)
	contractsPos := strings.Index(body, `<h2>The two contracts</h2>`)
	diffPos := strings.Index(body, `<section id="diff">`)
	factsPos := strings.Index(body, `<h3>External facts`)
	basisPos := strings.Index(body, `<h3>Basis risk`)
	explanationPos := strings.Index(body, `<h2>Why: the explanation</h2>`)
	touchPos := strings.Index(body, `<h2>Current touch`)
	historyPos := strings.Index(body, `<section id="history">`)
	logPos := strings.Index(body, `<h3>Decision-log entries for this pair</h3>`)
	routePos := strings.Index(body, `<section id="route">`)

	positions := map[string]int{
		"contracts":   contractsPos,
		"diff":        diffPos,
		"facts":       factsPos,
		"basis":       basisPos,
		"explanation": explanationPos,
		"touch":       touchPos,
		"history":     historyPos,
		"log":         logPos,
		"route":       routePos,
	}

	for name, pos := range positions {
		if pos == -1 {
			t.Errorf("missing section: %s", name)
		}
	}

	// Verify full order: contracts < diff < facts < basis < explanation < touch < history < log < route
	if contractsPos >= diffPos ||
		diffPos >= factsPos ||
		factsPos >= basisPos ||
		basisPos >= explanationPos ||
		explanationPos >= touchPos ||
		touchPos >= historyPos ||
		historyPos >= logPos ||
		logPos >= routePos {
		t.Errorf("section order wrong: %+v", positions)
	}
}

func TestPairRouteAnchor(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Verify route anchor exists and strip links to it
	wantContains(t, body,
		`<a href="#route">Route an order`,
		`<section id="route">`,
	)
}
