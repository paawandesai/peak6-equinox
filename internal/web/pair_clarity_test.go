package web

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestConfidenceBreakdown renders stored evidence as one line (M6 P6).
func TestConfidenceBreakdown(t *testing.T) {
	tests := []struct {
		name string
		typ  canonical.RelationshipType
		ev   canonical.MatcherEvidence
		want string
	}{
		{"grouped deductions", canonical.RelEquivalentGivenFacts, canonical.MatcherEvidence{
			Prior: dec("0.97"), Total: dec("0.93"), Contributions: []canonical.ContributionTerm{
				{Term: "prior", Value: dec("0.97")}, {Term: "unknown_edge_rules_a", Value: dec("-0.02")}, {Term: "unknown_edge_rules_b", Value: dec("-0.02")}}},
			"base 0.97 for equivalent_given_facts, −0.02 × 2 for unknown edge rules = 0.93"},
		{"no deductions", canonical.RelEquivalent, canonical.MatcherEvidence{
			Prior: dec("0.97"), Total: dec("0.97"), Contributions: []canonical.ContributionTerm{{Term: "prior", Value: dec("0.97")}}},
			"base 0.97 for equivalent, no deductions = 0.97"},
		{"unlabelled term and clamp", canonical.RelComplement, canonical.MatcherEvidence{
			Prior: dec("0.95"), Total: dec("0.50"), Contributions: []canonical.ContributionTerm{
				{Term: "prior", Value: dec("0.95")}, {Term: "new_rule", Value: dec("0.01")}, {Term: "fact_applied", Value: dec("-0.01")}}},
			"base 0.95 for complement, +0.01 for new rule, −0.01 for fact applied = 0.95; stored confidence 0.50"},
		{"zero term", canonical.RelImplies, canonical.MatcherEvidence{
			Prior: dec("0.90"), Total: dec("0.90"), Contributions: []canonical.ContributionTerm{
				{Term: "prior", Value: dec("0.90")}, {Term: "deadline_gap_hours", Value: dec("0")}}},
			"base 0.90 for implies, 0.00 for deadline gap = 0.90"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := confidenceBreakdown(tt.typ, tt.ev); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestRouteDefaultsText states the form's defaults from defaultRouteForm (M6 P6).
func TestRouteDefaultsText(t *testing.T) {
	f := defaultRouteForm(canonical.Relationship{ID: 1, AID: "kalshi:A", BID: "polymarket:B"})
	if got, want := routeDefaultsText(f), "Defaults: buy YES 100 contracts, market IOC (fill what you can), target A."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	f.TIF = "FOK"
	if got := routeDefaultsText(f); !strings.Contains(got, "FOK (all or nothing)") {
		t.Errorf("FOK wording: %q", got)
	}
}

// TestPairPageClarity: the diff legend, the confidence breakdown, the
// summary wording, the close-time line and the route defaults (M6 P6).
func TestPairPageClarity(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()
	rel, err := st.GetRelationship(ctx, sd.eqID)
	if err != nil {
		t.Fatal(err)
	}
	rel.Evidence = &canonical.MatcherEvidence{Prior: dec("0.97"), Total: dec("0.93"), Contributions: []canonical.ContributionTerm{
		{Term: "prior", Value: dec("0.97")}, {Term: "unknown_edge_rules_a", Value: dec("-0.02")}, {Term: "unknown_edge_rules_b", Value: dec("-0.02")}}}
	rel.Confidence = dec("0.93")
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}
	rec := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10))
	wantHTML(t, rec, 200)
	body := rec.Body.String()
	wantContains(t, body,
		`<small class="conf-breakdown">base 0.97 for equivalent, −0.02 × 2 for unknown edge rules = 0.93</small>`,
		`A's words are <del>struck through</del> and B's are <ins>underlined</ins>`,
		"◇ closed by fact", "? unknown",
		"no separate summary; full rules below",
		`"closes" is when the venue stops trading`,
		`<p class="route-defaults"><small>Defaults: buy YES 100 contracts, market IOC (fill what you can), target A.</small></p>`,
	)

	// Without evidence: no breakdown, the footer says so.
	rel.Evidence = nil
	rel.Confidence = dec("0.97")
	if _, err := st.UpsertRelationship(ctx, rel); err != nil {
		t.Fatal(err)
	}
	body = get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10)).Body.String()
	if strings.Contains(body, "conf-breakdown") {
		t.Error("breakdown shown without stored evidence")
	}
	wantContains(t, body, "Matcher evidence: not recorded")
}

// TestGlossarySplitsBps: rate bps and price bps are separate entries (M6 P6),
// and #term-bps (linked from route traces) stays the price meaning.
func TestGlossarySplitsBps(t *testing.T) {
	s, _ := newTestServer(t)
	body := get(s.Handler(), "/glossary").Body.String()
	wantContains(t, body,
		`<dt id="term-rate-bps">rate bps (Fed moves)</dt>`,
		`<dt id="term-bps">price bps (of a $1 payout)</dt>`,
		"1 bp is 0.01 percentage point",
	)
}
