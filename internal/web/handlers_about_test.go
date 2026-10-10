package web

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// inlineScriptOrStyleRe matches what the CSP forbids in a page body: a style
// attribute, a <style> block, an inline <script> (no src) or an on* handler.
var inlineScriptOrStyleRe = regexp.MustCompile(`(?i)<[a-z][^>]*\sstyle\s*=|<style|<script>|<script\s+[^>]*>[^<]+</script>|<[a-z][^>]*\son[a-z]+\s*=`)

// TestAboutPage: GET /about returns 200 with every section the evaluator
// needs (P12): the two questions, the payout definition, the ladder with
// routable types marked, the pipeline, the cost formula, the AI section,
// the known limits, the pinned-round note and GitLab doc links. It needs no
// database.
func TestAboutPage(t *testing.T) {
	s := New(Config{}, Deps{})
	rec := get(s.Handler(), "/about")
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body,
		`<h1>About Equinox</h1>`,
		`<p class="question">What is Equinox for, how does it decide, and where does it stop?</p>`,
		`id="questions"`, `id="equivalent"`, `id="ladder"`, `id="pipeline"`, `id="cost"`, `id="ai"`, `id="limits"`, `id="pinned"`, `id="docs"`,
		// the two questions from the brief
		"Can software tell that a Kalshi contract and a Polymarket contract are the same bet",
		"Can it compare their prices like for like and decide where an order would execute best?",
		// the payout definition (D-017)
		"if and only if buying YES on both pays out identically in every reachable world state",
		// the cost formula with the default multiplier (routing.md, BASIS_PENALTY_MULTIPLIER)
		"all-in cost per contract = VWAP + fee per contract + basis-risk score × 0.01",
		"A score of 0.5 adds $0.005 per contract",
		// AI: admin-only, downgrade-only, real prod status (Makefile deploy)
		"Only an admin can trigger it", "it can never create or upgrade a relationship",
		"<code>make deploy</code> sets <code>LLM_ENABLED=false</code>",
		// known limits
		"Edge rules are scored, not vetoed.", "There is no mutually-exclusive type.",
		`"Vance wins" and "Harris wins"`, "blocking classifies the pair <code>unrelated</code>",
		"A live back-test against the venues has not been run.",
		// D-117
		"Prices and routing still use the newest collected quotes", "D-117",
		// links to live numbers instead of copied ones
		`<a href="/eval">Eval</a>`, `<a href="/eval#backtest">Eval</a>`, `<a href="/">Matches</a>`, `<a href="/log">Decision log</a>`,
	)
	for _, doc := range []string{"equivalence.md", "routing.md", "decisions.md", "ai-usage.md"} {
		wantContains(t, body, `href="https://labs.gauntletai.com/paawandesai/peak6-equinox/-/blob/main/docs/`+doc+`"`)
	}
	if strings.Contains(body, `href="/docs`) {
		t.Error("/about links /docs, which the app does not serve")
	}
}

// TestAboutLadderMarksRoutableTypes: every relationship type has a row,
// and the routable column says yes exactly for the routable ones (P12).
func TestAboutLadderMarksRoutableTypes(t *testing.T) {
	body := get(New(Config{}, Deps{}).Handler(), "/about").Body.String()
	for _, typ := range canonical.RelationshipTypes() {
		re := regexp.MustCompile(`(?s)<tr id="about-type-` + regexp.QuoteMeta(string(typ)) + `">.*?<td data-label="Routable">(.*?)</td>`)
		m := re.FindStringSubmatch(body)
		if m == nil {
			t.Errorf("no ladder row for %s", typ)
			continue
		}
		want := "no"
		if typ.Routable() {
			want = "<strong>yes</strong>"
		}
		if m[1] != want {
			t.Errorf("%s routable cell = %q, want %q", typ, m[1], want)
		}
	}
}

// TestAboutPipelineSVG: the pipeline is an inline, accessible SVG with all
// seven steps in order, styled only by presentation attributes and classes,
// with no script (P12, CSP).
func TestAboutPipelineSVG(t *testing.T) {
	rec := get(New(Config{}, Deps{}).Handler(), "/about")
	body := rec.Body.String()
	svg := regexp.MustCompile(`(?s)<svg class="pipeline".*?</svg>`).FindString(body)
	if svg == "" {
		t.Fatal("/about has no pipeline <svg>")
	}
	wantContains(t, svg, `role="img"`, `<title id="pipeline-title">`, `<desc id="pipeline-desc">`, `marker-end="url(#pipeline-arrowhead)"`)
	last := -1
	for i, name := range []string{"Collect", "Normalise", "Block", "Veto / facts", "Classify", "Route", "Log"} {
		label := strconv.Itoa(i+1) + ". " + name
		at := strings.Index(svg, label)
		if at < 0 {
			t.Errorf("svg lacks step %q", label)
			continue
		}
		if at < last {
			t.Errorf("svg step %q is out of order", label)
		}
		last = at
	}
	if got := strings.Count(svg, "<line "); got != 6 {
		t.Errorf("svg has %d connectors, want 6", got)
	}
	if m := inlineScriptOrStyleRe.FindString(body); m != "" {
		t.Errorf("/about has inline script or style: %q", m)
	}
	if strings.Contains(strings.ToLower(svg), "<script") {
		t.Error("svg contains a script")
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Errorf("/about CSP = %q", csp)
	}
}

// TestAboutCostUsesConfiguredMultiplier: the formula shows the running
// BASIS_PENALTY_MULTIPLIER, not a copied constant (P12).
func TestAboutCostUsesConfiguredMultiplier(t *testing.T) {
	s := New(Config{BasisPenaltyMult: dec("0.02"), MinRouteConfidence: dec("0.85")}, Deps{})
	body := get(s.Handler(), "/about").Body.String()
	wantContains(t, body,
		"basis-risk score × 0.02", "A score of 0.5 adds $0.01 per contract", "the most it can add is $0.02",
		"confidence 0.85 or more",
	)
}

// TestAboutPinnedRoundNamed: with a pinned round the D-117 note names it;
// the page renders with a store too (P12).
func TestAboutPinnedRoundNamed(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	if err := st.PinRound(context.Background(), sd.round.ID); err != nil {
		t.Fatal(err)
	}
	rec := get(s.Handler(), "/about")
	wantHTML(t, rec, http.StatusOK)
	wantContains(t, rec.Body.String(), "The operator has pinned round "+strconv.FormatInt(sd.round.ID, 10)+" for the review")
}

// TestAboutNavLinkOnEveryPage: every public page's header nav links /about,
// and /about marks itself current (P12).
func TestAboutNavLinkOnEveryPage(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	for _, path := range []string{"/", "/near-misses", "/log", "/eval", "/glossary", "/about", "/pairs/" + strconv.FormatInt(sd.eqID, 10)} {
		body := get(s.Handler(), path).Body.String()
		if !strings.Contains(body, `<li><a href="/about" `) {
			t.Errorf("%s: header nav lacks the About link", path)
		}
	}
	about := get(s.Handler(), "/about").Body.String()
	wantContains(t, about, `<a href="/about" aria-current="page">About</a>`)
}
