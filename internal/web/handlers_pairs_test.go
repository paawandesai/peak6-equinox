package web

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

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

	// Strip fields
	wantContains(t, body,
		`<span class="chip chip-ok">equivalent_given_facts</span>`, // type chip
		"0.93 high",                   // confidence with bucket word
		"rules",                       // method
		"0.38 moderate",               // basis risk score with word
		"Routable</dt><dd>yes</dd>",   // routable: yes
		"as of",                       // round and as-of
		`<a href="#route">Route an order`, // route link
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
		"Routable</dt><dd>no</dd>", // routable: no
		"not scored",               // basis risk: not scored
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

	// Make the pair equivalent_given_facts with facts closing vetoes
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.Type = canonical.RelEquivalentGivenFacts
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

	// Diff row with fact closure annotation
	wantContains(t, body, "closed by fact fed_upper_bound=4.00")
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

	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Find section IDs in order
	diffPos := strings.Index(body, `id="diff"`)
	historyPos := strings.Index(body, `id="history"`)
	routePos := strings.Index(body, `id="route"`)

	if diffPos == -1 || historyPos == -1 || routePos == -1 {
		t.Fatal("missing section IDs")
	}

	// Verify order: diff < history < route
	if !(diffPos < historyPos && historyPos < routePos) {
		t.Errorf("section order wrong: diff=%d, history=%d, route=%d", diffPos, historyPos, routePos)
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
