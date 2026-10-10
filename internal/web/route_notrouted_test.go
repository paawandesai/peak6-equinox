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
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/route"
)

// complementCase reproduces the live #2390 shape with the real router: the
// Kalshi target publishes YES bids only, the only complement would cost more
// than $1 per $1 payout, and four partners are implies.
func complementCase(t *testing.T) (canonical.Decision, map[string]canonical.Quote) {
	t.Helper()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	qty := dec("100")
	order := canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty, Type: canonical.OrderMarket, TIF: canonical.TIFIOC}
	inst := func(id string) canonical.Instrument {
		in := seedInstrument(id, canonical.VenueOf(id), id, "r", "fed", "src", kalshiFee)
		if canonical.VenueOf(id) == canonical.VenuePolymarket {
			in.FeeSchedule = polyFee
		}
		return in
	}
	target := canonical.Candidate{Instrument: inst("kalshi:KXFED-26OCT-T3.50"),
		Quote: canonical.Quote{InstrumentID: "kalshi:KXFED-26OCT-T3.50", FetchedAt: now,
			YesBids: []canonical.Level{{Price: dec("0.40"), Size: dec("200")}}}}
	compID := "polymarket:fed-oct-down-50"
	partners := []canonical.Candidate{{Instrument: inst(compID),
		Quote: canonical.Quote{InstrumentID: compID, FetchedAt: now,
			YesBids: []canonical.Level{{Price: dec("0.01"), Size: dec("500")}}},
		Relationship: &canonical.Relationship{AID: target.Instrument.ID, BID: compID, Type: canonical.RelComplement,
			Confidence: dec("0.95"), BasisRisk: &canonical.BasisRisk{Score: dec("1")}}}}
	for i := 1; i <= 4; i++ {
		id := "polymarket:implies-" + strconv.Itoa(i)
		partners = append(partners, canonical.Candidate{Instrument: inst(id),
			Quote:        canonical.Quote{InstrumentID: id, FetchedAt: now, YesAsks: []canonical.Level{{Price: dec("0.30"), Size: dec("100")}}},
			Relationship: &canonical.Relationship{AID: target.Instrument.ID, BID: id, Type: canonical.RelImplies, Confidence: dec("0.95")}})
	}
	cfg := route.Config{RoundID: 904, MinRouteConfidence: dec("0.9"), BasisPenaltyMultiplier: dec("0.02"),
		Eligibility: route.EligibilityConfig{Snapshot: now, StaleAfter: time.Minute, SkewWarn: time.Minute, SkewMax: time.Hour, MinBookDepth: decimal.NewFromInt(1)}}
	d, err := route.Route(order, target, partners, cfg, canonical.RelComplement, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Chosen) != 0 {
		t.Fatalf("precondition: routed %+v", d.Chosen)
	}
	return d, candidateQuotes(target, partners)
}

// TestNotRoutedSentenceComplementCase (M6 P2): one sentence names the
// deciding reason per class, from the stored exclusions and inputs.
func TestNotRoutedSentenceComplementCase(t *testing.T) {
	d, _ := complementCase(t)
	got := notRoutedSentence(d)
	for _, want := range []string{
		"Nothing routed: the Kalshi book has no YES offers, ",
		"the only complement would cost $1.0",
		"per $1 payout (a sure loss), and 4 partners are 'implies', which are never routed.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("sentence %q lacks %q", got, want)
		}
	}
	if strings.Count(got, ".") > 3 || strings.Contains(got, "insufficient_depth") {
		t.Errorf("sentence %q is not one plain sentence", got)
	}
	if lead := buildLeadSentence(d, decimal.Zero, dec("100"), decimal.Zero, nil); lead != got {
		t.Errorf("lead sentence = %q, want the not-routed sentence", lead)
	}
}

// TestSuggestOppositeAppears (M6 P2): the Kalshi book has YES bids, so buy NO
// has a candidate; the link presets the opposite order on the pair page.
func TestSuggestOppositeAppears(t *testing.T) {
	d, quotes := complementCase(t)
	sg := suggestOpposite(d, quotes, 2390)
	if sg == nil {
		t.Fatal("want a suggestion")
	}
	if sg.Order != "buy NO" || !strings.Contains(sg.Why, "The Kalshi book has NO offers (best 0.6000, 200 contracts)") {
		t.Errorf("suggestion = %+v", sg)
	}
	want := "/pairs/2390?outcome=no&qty=100&side=buy&target=kalshi%3AKXFED-26OCT-T3.50&tif=IOC&type=market#route"
	if sg.Link != want {
		t.Errorf("link = %q, want %q", sg.Link, want)
	}
	if suggestOpposite(d, quotes, 0).Link != "" {
		t.Error("no pair id: want no link")
	}
}

// TestSuggestOppositeAbsent (M6 P2): no suggestion for a routed decision, or
// when no stored book has depth for the opposite order; implies partners are
// never counted.
func TestSuggestOppositeAbsent(t *testing.T) {
	d, quotes := complementCase(t)
	empty := map[string]canonical.Quote{}
	for id, q := range quotes {
		q.YesBids, q.YesAsks = nil, nil
		if strings.Contains(id, "implies") {
			q.YesBids = []canonical.Level{{Price: dec("0.50"), Size: dec("100")}} // implies: never a candidate
		}
		empty[id] = q
	}
	if sg := suggestOpposite(d, empty, 1); sg != nil {
		t.Errorf("want no suggestion, got %+v", sg)
	}
	routed := d
	routed.Chosen = []canonical.Leg{{InstrumentID: d.TargetID, Quantity: dec("1")}}
	if sg := suggestOpposite(routed, quotes, 1); sg != nil {
		t.Errorf("routed: want no suggestion, got %+v", sg)
	}
}

// TestSuggestOppositeViaComplement: a complement with offers on the order's
// own outcome makes the opposite order a candidate.
func TestSuggestOppositeViaComplement(t *testing.T) {
	d, quotes := complementCase(t)
	tq := quotes[d.TargetID]
	tq.YesBids = nil
	quotes[d.TargetID] = tq
	cq := quotes["polymarket:fed-oct-down-50"]
	cq.YesAsks = []canonical.Level{{Price: dec("0.55"), Size: dec("40")}}
	quotes["polymarket:fed-oct-down-50"] = cq
	sg := suggestOpposite(d, quotes, 1)
	if sg == nil || !strings.Contains(sg.Why, "The Polymarket complement has YES offers (best 0.5500, 40 contracts)") {
		t.Errorf("suggestion = %+v", sg)
	}
}

// TestGroupPhrases covers the other reasons' wording.
func TestGroupPhrases(t *testing.T) {
	o := canonical.Order{Side: canonical.SideSell, Outcome: canonical.OutcomeYes}
	ex := func(id, reason, detail string) canonical.Exclusion {
		return canonical.Exclusion{InstrumentID: id, Venue: canonical.VenueOf(id), Reason: reason, Detail: detail}
	}
	cases := []struct {
		exs  []canonical.Exclusion
		want string
	}{
		{[]canonical.Exclusion{ex("kalshi:T", "no_book", "no book available")}, "the Kalshi book is empty (no bids or offers)"},
		{[]canonical.Exclusion{ex("kalshi:T", "no_book", "no sell on the yes side")}, "the Kalshi book has no YES bids"},
		{[]canonical.Exclusion{ex("kalshi:T", "stale", "fetched 300 s ago"), ex("kalshi:T", "no_book", "x")}, "the Kalshi book is stale (fetched 300 s ago)."},
		{[]canonical.Exclusion{ex("kalshi:T", "insufficient_depth", "5 contracts available on consumed side")}, "is too shallow (5 YES contracts on that side)"},
		{[]canonical.Exclusion{ex("kalshi:T", "insufficient_depth", "FOK requires 100 contracts; kalshi:T can fill 40")}, "can fill only 40 of the 100 contracts fill-or-kill needs"},
		{[]canonical.Exclusion{ex("kalshi:T", "sure_loss", "net proceeds -0.0100 per contract is a guaranteed loss")}, "would net $-0.0100 per contract (a sure loss)"},
		{[]canonical.Exclusion{ex("kalshi:T", "limit_not_reachable", "best bid is 0.4, outside the 0.5 limit")}, "cannot fill within the limit"},
		{[]canonical.Exclusion{ex("kalshi:T", "skew", "older by 90 s")}, "is too far behind the freshest quote"},
		{[]canonical.Exclusion{ex("kalshi:T", "not_open", "instrument is closed")}, "is not open"},
		{[]canonical.Exclusion{ex("kalshi:T", "fee_unknown", "x")}, "is excluded: fee unknown"},
		{[]canonical.Exclusion{ex("polymarket:P", "low_confidence", "x")}, "1 partner is below the routing confidence floor"},
		{[]canonical.Exclusion{ex("polymarket:P", "not_routable", "relationship type related is never routed"), ex("polymarket:Q", "not_routable", "relationship type implies is never routed")}, "1 partner is 'related', which is never routed and 1 partner is 'implies'"},
		{[]canonical.Exclusion{ex("polymarket:P", "insufficient_depth", "odd")}, "1 partner is too shallow"},
		{nil, "Nothing routed: every candidate was excluded."},
	}
	for _, tc := range cases {
		d := canonical.Decision{TargetID: "kalshi:T", Order: o, Exclusions: tc.exs}
		if got := notRoutedSentence(d); !strings.Contains(got, tc.want) {
			t.Errorf("sentence %q lacks %q", got, tc.want)
		}
	}
	// Several complements: counted, with the plural sure-loss wording.
	d := canonical.Decision{TargetID: "kalshi:T", Order: o,
		Inputs:     []canonical.DecisionInput{{InstrumentID: "polymarket:A", RelationshipType: canonical.RelComplement}, {InstrumentID: "polymarket:B", RelationshipType: canonical.RelComplement}},
		Exclusions: []canonical.Exclusion{ex("polymarket:A", "sure_loss", "x"), ex("polymarket:B", "sure_loss", "x")}}
	if got := notRoutedSentence(d); !strings.Contains(got, "2 complements would each be a sure loss") {
		t.Errorf("got %q", got)
	}
	d.Exclusions = d.Exclusions[:1]
	d.Exclusions[0].Detail = "all-in cost 1.0019 per $1 payout is a guaranteed loss"
	if got := notRoutedSentence(d); !strings.Contains(got, "the Polymarket complement would cost $1.0019") {
		t.Errorf("got %q", got)
	}
}

// TestRouteResultNotRoutedSentenceAndTry (M6 P2): POST /route on a one-sided
// pair leads with the deciding reasons and offers "Try: buy NO" linking to
// the pair page with the order preset; the exclusion table stays.
func TestRouteResultNotRoutedSentenceAndTry(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()
	tID, cID := "kalshi:ONESIDED", "polymarket:ONESIDED-C"
	for _, in := range []canonical.Instrument{
		seedInstrument(tID, canonical.VenueKalshi, "One-sided", "r", "BTC", "CF Benchmarks", kalshiFee),
		seedInstrument(cID, canonical.VenuePolymarket, "Complement", "r", "BTC", "CF Benchmarks", polyFee),
	} {
		if err := st.UpsertInstrument(ctx, sd.round.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	at := seedNow.Add(3 * time.Minute)
	for _, q := range []canonical.Quote{
		{InstrumentID: tID, YesBids: []canonical.Level{{Price: dec("0.40"), Size: dec("200")}}, FetchedAt: at},
		{InstrumentID: cID, YesAsks: []canonical.Level{{Price: dec("0.70"), Size: dec("50")}}, FetchedAt: at},
	} {
		if err := st.InsertQuote(ctx, sd.round.ID, q); err != nil {
			t.Fatal(err)
		}
	}
	pairID, err := st.UpsertRelationship(ctx, canonical.Relationship{RoundID: sd.round.ID, AID: tID, BID: cID,
		Type: canonical.RelComplement, Confidence: dec("0.95"), Method: canonical.MethodRules, Explanation: "complement",
		BasisRisk: &canonical.BasisRisk{Score: dec("0.1")}})
	if err != nil {
		t.Fatal(err)
	}
	rec := postForm(s.Handler(), "/route", validRouteForm(pairID, tID), true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	wantContains(t, body,
		"Nothing routed: the Kalshi book has no YES offers and the only complement has no NO offers.",
		"<strong>Try: buy NO.</strong> The Kalshi book has NO offers (best 0.6000, 200 contracts).",
		`href="/pairs/`+strconv.FormatInt(pairID, 10)+`?outcome=no&amp;qty=100&amp;side=buy&amp;target=kalshi%3AONESIDED&amp;tif=IOC&amp;type=market#route"`,
		"<summary>Eligibility</summary>", "✕ too shallow</strong>: 0 contracts available on consumed side")
}

// TestRouteResultRoutedHasNoTry (M6 P2): a routed result keeps its lead and
// shows no suggestion.
func TestRouteResultRoutedHasNoTry(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	body := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true).Body.String()
	wantContains(t, body, "Routed:")
	if strings.Contains(body, "Try:") || strings.Contains(body, "Nothing routed") {
		t.Error("routed result must not suggest another order")
	}
}

// TestDecisionPageNotRoutedTry (M6 P2): /decisions/{id} for a stored
// not-routed decision leads with the sentence and links the pair resolved by
// the target's instrument ID (preferring the decision's target type).
func TestDecisionPageNotRoutedTry(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	qty := dec("100")
	d := canonical.Decision{RoundID: sd.round.ID, TargetID: seedKalshiA, TargetType: canonical.RelEquivalent, DecidedAt: seedNow,
		Order:       canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
		Inputs:      []canonical.DecisionInput{{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi, RelationshipType: "self", OutcomeSide: canonical.OutcomeYes, FetchedAt: seedNow, RoundID: sd.round.ID}},
		Exclusions:  []canonical.Exclusion{{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi, Reason: "insufficient_depth", Detail: "0 contracts available on consumed side"}},
		Explanation: "No venue can fill.",
	}
	id, err := st.InsertRoutingDecision(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	body := get(s.Handler(), "/decisions/"+strconv.FormatInt(id, 10)).Body.String()
	wantContains(t, body, "Nothing routed: the Kalshi book has no YES offers.",
		"<strong>Try: buy NO.</strong> The Kalshi book has NO offers (best 0.5200, 200 contracts).",
		`href="/pairs/`+strconv.FormatInt(sd.eqID, 10)+`?outcome=no&amp;`)
}
