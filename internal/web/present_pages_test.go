package web

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Rendered-page checks for the plain-language layers (ui/plain-language):
// the answer and trader layers carry no instrument IDs or snake_case codes,
// and Technical details keeps the router's explanation verbatim.

var (
	tagRe       = regexp.MustCompile(`(?s)<[^>]+>`)
	snakeCodeRe = regexp.MustCompile(`\b[a-z]+_[a-z_]+\b`)
)

// plainRegion is the visible text of <main> before the Technical details.
func plainRegion(t *testing.T, body string) string {
	t.Helper()
	_, main, ok := strings.Cut(body, "<main")
	if !ok {
		t.Fatal("no <main>")
	}
	before, _, ok := strings.Cut(main, `<details class="tech`)
	if !ok {
		t.Fatal("no Technical details section")
	}
	return html.UnescapeString(tagRe.ReplaceAllString(before, " "))
}

func assertNoCodes(t *testing.T, page, text string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if strings.Contains(text, id) {
			t.Errorf("%s: instrument ID %s outside Technical details", page, id)
		}
	}
	if m := snakeCodeRe.FindAllString(text, -1); len(m) > 0 {
		t.Errorf("%s: snake_case codes outside Technical details: %v", page, m)
	}
}

func TestDecisionPage_PlainLayersAndTechnicalDetails(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	form := validRouteForm(sd.eqID, seedKalshiA)
	rec := postForm(s.Handler(), "/route", form, false)
	wantHTML(t, rec, http.StatusOK)
	id := decisionIDFrom(t, rec.Body.String())

	page := get(s.Handler(), "/decisions/"+strconv.FormatInt(id, 10))
	wantHTML(t, page, http.StatusOK)
	body := page.Body.String()
	plain := plainRegion(t, body)
	assertNoCodes(t, "/decisions", plain, seedKalshiA, seedPolyB, seedKalshiC)
	wantContains(t, plain, "Where it costs least", "Total per contract", "per contract")
	if !regexp.MustCompile(`<title>Decision #\d+ · Routed to (Kalshi|Polymarket) · Equinox</title>`).MatchString(body) {
		t.Errorf("decision title: %s", regexp.MustCompile(`<title>.*?</title>`).FindString(body))
	}

	// The router's explanation appears verbatim (escaped once) under Technical details.
	rd, err := st.GetRoutingDecision(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	_, tech, _ := strings.Cut(body, `<details class="tech">`)
	if !strings.Contains(tech, `<p class="explanation">`+html.EscapeString(rd.Decision.Explanation)+`</p>`) {
		t.Error("Technical details does not carry the stored explanation unchanged")
	}
	wantContains(t, tech, `href="/decisions/`+strconv.FormatInt(id, 10)+`/json"`, "STALE_AFTER_SECONDS", "MIN_ROUTE_CONFIDENCE", `title="`+seedKalshiA+`"`)
}

func TestDecisionPage_NotRoutedPlainAnswer(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	qty := dec("100")
	d := canonical.Decision{RoundID: sd.round.ID, TargetID: seedKalshiA, TargetType: canonical.RelEquivalent, DecidedAt: seedNow,
		Order: canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
		Inputs: []canonical.DecisionInput{
			{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi, RelationshipType: "self", FetchedAt: seedNow, RoundID: sd.round.ID},
			{InstrumentID: seedPolyB, Venue: canonical.VenuePolymarket, RelationshipType: canonical.RelEquivalent, FetchedAt: seedNow, RoundID: sd.round.ID},
			{InstrumentID: seedKalshiC, Venue: canonical.VenueKalshi, RelationshipType: canonical.RelRelated, FetchedAt: seedNow, RoundID: sd.round.ID},
		},
		Exclusions: []canonical.Exclusion{
			{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi, Reason: exInsufficientDepth, Detail: "0 contracts available on consumed side"},
			{InstrumentID: seedPolyB, Venue: canonical.VenuePolymarket, Reason: exStale, Detail: "fetched 300 s ago"},
			{InstrumentID: seedKalshiC, Venue: canonical.VenueKalshi, Reason: exNotRoutable, Detail: "relationship type related is never routed"},
		},
		Explanation: "No venue can fill this order: every candidate was excluded (insufficient_depth, stale, not_routable).",
	}
	id, err := st.InsertRoutingDecision(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	body := get(s.Handler(), "/decisions/"+strconv.FormatInt(id, 10)).Body.String()
	plain := plainRegion(t, body)
	assertNoCodes(t, "/decisions not routed", plain, seedKalshiA, seedPolyB, seedKalshiC)
	wantContains(t, plain,
		"No trade. You asked to buy YES on 'BTC $120,000 or higher on Oct 7', which the market prices at about 50%. Nobody on Kalshi is selling it.",
		"The only true substitute has a price 5 minutes old, too old to trust.",
		"One other Kalshi contract looks similar but pays on something different, so it isn't a substitute.",
		"Try the other side: buy NO on Kalshi at 52¢",
		"Skipped: no venue was eligible, so nothing was priced.",
	)
	if !strings.Contains(body, "<title>Decision #"+strconv.FormatInt(id, 10)+" · No trade · Equinox</title>") {
		t.Error("not-routed title")
	}
	_, tech, _ := strings.Cut(body, `<details class="tech">`)
	wantContains(t, tech, "<code>insufficient_depth</code>", "<code>stale</code>", "<code>not_routable</code>",
		"Fill simulation and fee math. Skipped: no venue was eligible, so nothing was priced.",
		"Ranked alternatives. Skipped: nothing was priced, so there was nothing to rank.")
	if strings.Contains(body, "<s class") {
		t.Error("excluded rows must not be struck through")
	}
}

func TestDecisionJSON(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), false)
	id := decisionIDFrom(t, rec.Body.String())
	r := get(s.Handler(), "/decisions/"+strconv.FormatInt(id, 10)+"/json")
	if r.Code != http.StatusOK || !strings.HasPrefix(r.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("status %d type %q", r.Code, r.Header().Get("Content-Type"))
	}
	var d canonical.Decision
	if err := json.Unmarshal(r.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.TargetID != seedKalshiA || d.Explanation == "" || d.RoundID != sd.round.ID {
		t.Errorf("decoded %+v", d)
	}
	for _, path := range []string{"/decisions/999999/json", "/decisions/abc/json"} {
		if r := get(s.Handler(), path); r.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, r.Code)
		}
	}
}

func TestPairPage_PlainTopCard(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	body := get(s.Handler(), "/pairs/"+strconv.FormatInt(sd.eqID, 10)).Body.String()
	plain := plainRegion(t, body)
	assertNoCodes(t, "/pairs", plain, seedKalshiA, seedPolyB)
	wantContains(t, plain, "Same bet", "These pay out on the same outcome", "Confidence 0.97 (high)", "Safety risk 0.10 (low)",
		"Routable", "Try an order", "Buy 100 YES on Kalshi", "Buy 100 NO on Kalshi", "Kalshi · BTC $120,000 or higher on Oct 7")
	if !strings.Contains(body, "<title>BTC $120,000 or higher ↔ BTC $120,000 or higher · Equinox</title>") {
		t.Errorf("pair title: %s", regexp.MustCompile(`<title>.*?</title>`).FindString(body))
	}
	// The route form sits in the top row, before the reasons and the audit layer.
	if f, w := strings.Index(body, `<form method="post" action="/route"`), strings.Index(body, `<details class="tech`); f < 0 || f > w {
		t.Error("route form is not above Technical details")
	}
	_, tech, _ := strings.Cut(body, `<details class="tech`)
	wantContains(t, tech, `<table class="data terms">`, seedExplainEq, "Current touch", "Price history")
}

func TestMatchesPage_StartCards(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	body := get(s.Handler(), "/").Body.String()
	wantContains(t, body,
		"Equinox looks for contracts on Kalshi and Polymarket that pay out on the same thing, and shows where buying would be cheapest. It never places real orders.",
		`<h3><span class="start-num">1.</span> See a match</h3>`, `<h3><span class="start-num">2.</span> Route an order</h3>`,
		`href="/pairs/`+strconv.FormatInt(sd.eqID, 10)+`#route" class="btn-next">Try an order</a>`,
		"lookalikes we rejected", "Filters and technical details")
	_, cards, _ := strings.Cut(body, `<h2 id="pairs">`)
	cards, _, _ = strings.Cut(cards, `<details class="tech`)
	assertNoCodes(t, "/ cards", html.UnescapeString(tagRe.ReplaceAllString(cards, " ")), seedKalshiA, seedPolyB)
}

// decisionIDFrom reads the decision number from a rendered result.
func decisionIDFrom(t *testing.T, body string) int64 {
	t.Helper()
	m := regexp.MustCompile(`href="/decisions/(\d+)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no decision link in the result")
	}
	id, _ := strconv.ParseInt(m[1], 10, 64)
	return id
}
