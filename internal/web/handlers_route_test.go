package web

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func validRouteForm(pairID int64, target string) url.Values {
	return url.Values{
		"pair": {strconv.FormatInt(pairID, 10)}, "target": {target}, "side": {"buy"}, "outcome": {"yes"},
		"quantity": {"100"}, "type": {"market"}, "limit": {""}, "tif": {"IOC"},
	}
}

// countDecisions returns the stored routing decisions and routing log entries.
func countDecisions(t *testing.T, st *store.Store) (int, int) {
	t.Helper()
	ctx := context.Background()
	decs, err := st.ListRoutingDecisions(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := st.ListLog(ctx, store.LogFilter{Kind: store.LogRouting, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	return len(decs), len(logs)
}

func TestRouteHappyPathHTMX(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Error("htmx request got a full page, want a fragment")
	}
	wantContains(t, body,
		`id="route-result"`, "Decision #1", "buy 100 contracts YES, market IOC",
		"Partners considered:</strong> 2", // UI-13: Partners in verdict section (v1: "Partners considered: 2")
		// Polymarket fills 50 at 0.50 and 50 at 0.53 (VWAP 0.515, fee 0.0175, basis 0.001, all-in 0.5335); Kalshi fills all 100 at 0.52 (all-in 0.5375).
		"badge badge-l", "Routed", // UI-13: Outcome badge (v1: class="chip chip-ok")
		"Chosen legs", `<span class="venue">polymarket</span>`,
		"0.5150",                            // VWAP
		"0.5335",                            // All-in
		`<tr class="totals">`, "100 of 100", // Totals row
		"Ranked alternatives", "0.5375", // Gap to chosen
		"Eligibility", seedKalshiC, // UI-13: Exclusions in trace step 2 (v1: separate "Excluded candidates" section)
		`class="reason"`, "not routable", // UI-13: Reason with class (v1: <strong>not routable</strong>)
		"Explanation", "Simulated buy of 100 YES contracts on "+seedKalshiA,
		"basis penalty = score × 0.01", // Configured multiplier
	)

	decs, logs := countDecisions(t, st)
	if decs != 1 || logs != 1 {
		t.Fatalf("stored %d decisions and %d log entries, want 1 and 1", decs, logs)
	}
	dec, err := st.GetRoutingDecision(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if dec.RoundID != sd.round.ID || dec.Decision.TargetID != seedKalshiA || len(dec.Decision.Chosen) != 1 || dec.Decision.Chosen[0].Venue != canonical.VenuePolymarket {
		t.Errorf("stored decision = %+v", dec.Decision)
	}
	// Verify Inputs and TargetType are stored (UI-25, D-099)
	if dec.Decision.TargetType == "" {
		t.Error("Decision.TargetType is empty, want non-empty relationship type")
	}
	if dec.Decision.TargetType != canonical.RelEquivalent {
		t.Errorf("Decision.TargetType = %q, want %q", dec.Decision.TargetType, canonical.RelEquivalent)
	}
	if len(dec.Decision.Inputs) == 0 {
		t.Error("Decision.Inputs is empty, want at least target input")
	}
	// Should have inputs for target + partners considered (target + 2 partners in seedPublic)
	if len(dec.Decision.Inputs) < 2 {
		t.Errorf("Decision.Inputs count = %d, want >= 2 (target + partners)", len(dec.Decision.Inputs))
	}
	// Verify target input (first one should be target with RelationshipType="self")
	var foundTarget bool
	for _, input := range dec.Decision.Inputs {
		if input.RelationshipType == "self" {
			foundTarget = true
			if input.InstrumentID == "" {
				t.Error("target input InstrumentID is empty")
			}
			if input.Venue == "" {
				t.Error("target input Venue is empty")
			}
			if input.Confidence.IsZero() {
				t.Error("target input Confidence is zero")
			}
			break
		}
	}
	if !foundTarget {
		t.Error("Decision.Inputs missing target input with RelationshipType=self")
	}
	entries, err := st.ListLog(context.Background(), store.LogFilter{Kind: store.LogRouting, Ref: "1"})
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Summary, "route buy 100 contracts yes on "+seedKalshiA+": polymarket") {
		t.Errorf("log entries = %+v, %v", entries, err)
	}
}

func TestRouteFullPageWithoutHTMX(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	form := validRouteForm(sd.eqID, seedPolyB)
	form.Set("side", "sell")
	form.Set("outcome", "no")
	form.Set("quantity", "10")
	form.Set("type", "limit")
	form.Set("limit", "0.40")
	form.Set("tif", "FOK")
	rec := postForm(s.Handler(), "/route", form, false)
	wantHTML(t, rec, http.StatusOK)
	wantContains(t, rec.Body.String(), "<html", "Back to pair #"+strconv.FormatInt(sd.eqID, 10),
		"Where would this order fill best", "sell 10 contracts NO, limit FOK at 0.4000", "Decision #1")
}

func TestRouteNoVenueCanFill(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	form := validRouteForm(sd.eqID, seedKalshiA)
	form.Set("type", "limit")
	form.Set("limit", "0.10")
	rec := postForm(s.Handler(), "/route", form, true)
	wantHTML(t, rec, http.StatusOK)
	wantContains(t, rec.Body.String(), "No venue can fill this order", "limit_not_reachable")
	if decs, logs := countDecisions(t, st); decs != 1 || logs != 1 {
		t.Errorf("a no-fill decision must still be stored and logged; got %d, %d", decs, logs)
	}
}

func TestRouteValidation(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	type tc struct {
		name   string
		mutate func(url.Values)
		status int
		want   string
	}
	cases := []tc{
		{"missing pair", func(f url.Values) { f.Del("pair") }, 400, "pair must be a pair ID"},
		{"pair not numeric", func(f url.Values) { f.Set("pair", "1 OR 1=1") }, 400, "pair must be a pair ID"},
		{"pair leading zero", func(f url.Values) { f.Set("pair", "0"+f.Get("pair")) }, 400, "pair must be a pair ID"},
		{"unknown pair", func(f url.Values) { f.Set("pair", "999999") }, 404, "no such pair"},
		{"target not in pair", func(f url.Values) { f.Set("target", seedKalshiC) }, 400, "target must be one of the pair"},
		{"target malformed", func(f url.Values) { f.Set("target", "kalshi:<script>") }, 400, "target must be one of the pair"},
		{"side", func(f url.Values) { f.Set("side", "steal") }, 400, "side must be buy or sell"},
		{"outcome", func(f url.Values) { f.Set("outcome", "maybe") }, 400, "outcome must be yes or no"},
		{"type", func(f url.Values) { f.Set("type", "stop") }, 400, "type must be market or limit"},
		{"tif", func(f url.Values) { f.Set("tif", "GTC") }, 400, "tif must be IOC or FOK"},
		{"quantity zero", func(f url.Values) { f.Set("quantity", "0") }, 400, "quantity must be between 1 and 100000"},
		{"quantity too big", func(f url.Values) { f.Set("quantity", "100001") }, 400, "quantity must be between 1 and 100000"},
		{"quantity negative", func(f url.Values) { f.Set("quantity", "-5") }, 400, "quantity must be between 1 and 100000"},
		{"quantity text", func(f url.Values) { f.Set("quantity", "ten") }, 400, "quantity must be a number"},
		{"quantity missing", func(f url.Values) { f.Del("quantity") }, 400, "quantity must be a number"},
		{"quantity huge", func(f url.Values) { f.Set("quantity", "1e400") }, 400, "quantity must be between"},
		{"limit above 1", func(f url.Values) { f.Set("type", "limit"); f.Set("limit", "1.5") }, 400, "limit price must be a number between 0 and 1"},
		{"limit negative", func(f url.Values) { f.Set("type", "limit"); f.Set("limit", "-0.1") }, 400, "limit price must be a number between 0 and 1"},
		{"limit text", func(f url.Values) { f.Set("type", "limit"); f.Set("limit", "half") }, 400, "limit price must be a number between 0 and 1"},
		{"limit missing", func(f url.Values) { f.Set("type", "limit") }, 400, "a limit order needs a limit price"},
		{"limit on market", func(f url.Values) { f.Set("limit", "0.5") }, 400, "a market order takes no limit price"},
		{"field too long", func(f url.Values) { f.Set("side", strings.Repeat("b", 65)) }, 400, "side is too long"},
		{"several at once", func(f url.Values) { f.Set("side", "x"); f.Set("tif", "y"); f.Set("quantity", "0") }, 400, "tif must be IOC or FOK"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			form := validRouteForm(sd.eqID, seedKalshiA)
			c.mutate(form)
			rec := postForm(s.Handler(), "/route", form, true)
			wantHTML(t, rec, c.status)
			body := rec.Body.String()
			if strings.Contains(body, "<html") {
				t.Error("htmx error got a full page, want a fragment")
			}
			wantContains(t, body, `id="route-result"`, "The order was not routed.", c.want)
		})
	}
	if decs, logs := countDecisions(t, st); decs != 0 || logs != 0 {
		t.Errorf("invalid forms stored %d decisions and %d log entries", decs, logs)
	}

	// Without htmx, the same problems come back as a full page.
	form := validRouteForm(sd.eqID, seedKalshiA)
	form.Set("quantity", "0")
	rec := postForm(s.Handler(), "/route", form, false)
	wantHTML(t, rec, http.StatusBadRequest)
	wantContains(t, rec.Body.String(), "<html", "quantity must be between 1 and 100000", "Back to pair")
}

func TestRouteRejectsOversizedAndMalformedBodies(t *testing.T) {
	s, st := newTestServer(t)
	seedPublic(t, st)
	h := s.Handler()

	big := url.Values{"pair": {"1"}, "note": {strings.Repeat("x", maxRouteBody+1)}}
	rec := postForm(h, "/route", big, true)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: status %d, want 413", rec.Code)
	}
	wantContains(t, rec.Body.String(), "the form is too large")

	req := httptestPost("/route", "%zz=1", "application/x-www-form-urlencoded")
	rec = serve(h, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("malformed body: status %d, want 400", rec.Code)
	}
	wantContains(t, rec.Body.String(), "the form could not be read")

	if decs, logs := countDecisions(t, st); decs != 0 || logs != 0 {
		t.Errorf("stored %d decisions and %d log entries", decs, logs)
	}
}

func TestParseRouteFormNormalisesCase(t *testing.T) {
	form := url.Values{"pair": {"7"}, "target": {seedKalshiA}, "side": {" SELL "}, "outcome": {"No"},
		"quantity": {"2.5"}, "type": {"Limit"}, "limit": {"0.25"}, "tif": {"fok"}}
	in, errs := parseRouteForm(form.Get)
	if len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	o := in.Order
	if in.PairID != 7 || in.Target != seedKalshiA || o.Side != canonical.SideSell || o.Outcome != canonical.OutcomeNo ||
		o.Type != canonical.OrderLimit || o.TIF != canonical.TIFFOK || !o.Quantity.Equal(dec("2.5")) || !o.LimitPrice.Equal(dec("0.25")) {
		t.Errorf("input = %+v order = %+v", in, o)
	}
	if line := orderLine(o); line != "sell 2.5 contracts NO, limit FOK at 0.2500" {
		t.Errorf("orderLine = %q", line)
	}
}

func TestRouteLogSummary(t *testing.T) {
	q := dec("100")
	d := canonical.Decision{TargetID: seedKalshiA, Order: canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &q},
		Exclusions: []canonical.Exclusion{{}, {}}}
	if got := routeLogSummary(d); got != "route buy 100 contracts yes on "+seedKalshiA+": no venue can fill (2 excluded)" {
		t.Errorf("summary = %q", got)
	}
	d.Chosen = []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, AllInPerContract: dec("0.5325")}}
	if got := routeLogSummary(d); got != "route buy 100 contracts yes on "+seedKalshiA+": polymarket "+seedPolyB+" at 0.5325 per contract" {
		t.Errorf("summary = %q", got)
	}
	n := dec("25")
	d.Order.Quantity, d.Order.Notional = nil, &n
	if got := orderLine(d.Order); got != "buy $25 notional YES, market IOC" && !strings.HasPrefix(got, "buy $25 notional YES") {
		t.Errorf("orderLine = %q", got)
	}
}

// TestRouteOutcomeKinds tests the outcome strip for each outcome kind (AC from UI-05).
func TestRouteOutcomeKinds(t *testing.T) {
	qty := dec("100")
	cases := []struct {
		name        string
		decision    canonical.Decision
		wantChip    string
		wantPhrase  string
		wantSummary []string
	}{
		{
			name: "routed_single_venue",
			decision: canonical.Decision{
				TargetID: seedKalshiA,
				Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty},
				Chosen:   []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: qty, AllInPerContract: dec("0.5335"), Route: "direct", Outcome: canonical.OutcomeYes}},
			},
			wantChip:    "chip-ok",
			wantPhrase:  "Routed",
			wantSummary: []string{"buy 100 YES on polymarket", "filled 100 contracts"},
		},
		{
			name: "routed_across_venues",
			decision: canonical.Decision{
				TargetID: seedKalshiA,
				Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty},
				Chosen: []canonical.Leg{
					{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: dec("60"), AllInPerContract: dec("0.5335"), Route: "direct", Outcome: canonical.OutcomeYes},
					{Venue: canonical.VenueKalshi, InstrumentID: seedKalshiA, Quantity: dec("40"), AllInPerContract: dec("0.5375"), Route: "direct", Outcome: canonical.OutcomeYes},
				},
			},
			wantChip:    "chip-ok",
			wantPhrase:  "Routed across 2 venues",
			wantSummary: []string{"filled 100 contracts"},
		},
		{
			name: "partially_routed",
			decision: canonical.Decision{
				TargetID: seedKalshiA,
				Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty, TIF: canonical.TIFIOC},
				Chosen:   []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: dec("80"), AllInPerContract: dec("0.5335"), Route: "direct", Outcome: canonical.OutcomeYes}},
			},
			wantChip:    "chip-warn",
			wantPhrase:  "Partially routed",
			wantSummary: []string{"filled 80 of 100"},
		},
		{
			name: "complement_says_NO",
			decision: canonical.Decision{
				TargetID: seedKalshiA,
				Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty},
				Chosen:   []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: qty, AllInPerContract: dec("0.5335"), Route: "complement", Outcome: canonical.OutcomeNo}},
			},
			wantChip:    "chip-ok",
			wantPhrase:  "Routed as NO on polymarket (complement)",
			wantSummary: []string{"buy 100 NO on polymarket", "filled 100 contracts"},
		},
		{
			name: "notional_order",
			decision: canonical.Decision{
				TargetID: seedKalshiA,
				Order: func() canonical.Order {
					n := dec("50")
					return canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Notional: &n}
				}(),
				Chosen: []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: dec("100"), AllInPerContract: dec("0.5000"), Route: "direct", Outcome: canonical.OutcomeYes}},
			},
			wantChip:    "chip-ok",
			wantPhrase:  "Routed",
			wantSummary: []string{"filled 100 contracts"},
		},
		{
			name: "not_routed_sorted",
			decision: canonical.Decision{
				TargetID:   seedKalshiA,
				Order:      canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty},
				Exclusions: []canonical.Exclusion{{Reason: "stale"}, {Reason: "insufficient_depth"}, {Reason: "insufficient_depth"}},
			},
			wantChip:    "chip-danger",
			wantPhrase:  "Not routed",
			wantSummary: []string{"1 stale, 2 too shallow"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := newDecisionView(tc.decision, 2, time.Now(), map[string]string{}, dec("0.01"))
			if view.OutcomeChipClass != tc.wantChip {
				t.Errorf("chip class = %q, want %q", view.OutcomeChipClass, tc.wantChip)
			}
			if view.OutcomePhrase != tc.wantPhrase {
				t.Errorf("outcome phrase = %q, want %q", view.OutcomePhrase, tc.wantPhrase)
			}
			for _, want := range tc.wantSummary {
				if !strings.Contains(view.OutcomeSummary, want) {
					t.Errorf("summary %q lacks %q", view.OutcomeSummary, want)
				}
			}
		})
	}
}

// TestRouteExclusionReasonPhrases tests that exclusion reason codes are mapped to human-readable phrases (AC from UI-05).
func TestRouteExclusionReasonPhrases(t *testing.T) {
	cases := map[string]string{
		"stale":               "stale",
		"skew":                "skew",
		"no_book":             "no book",
		"insufficient_depth":  "too shallow",
		"not_routable":        "not routable",
		"low_confidence":      "low confidence",
		"limit_not_reachable": "limit not reachable",
		"unknown_code":        "unknown_code",
	}
	for code, wantPhrase := range cases {
		got := exclusionReasonPhrase(code)
		if got != wantPhrase {
			t.Errorf("exclusionReasonPhrase(%q) = %q, want %q", code, got, wantPhrase)
		}
	}
}

// TestRouteResultRendering tests that route_result renders correctly for sweeps and complements (Cursor review round 1).
// TestRouteResultRendering tests that route_result renders correctly for sweeps and complements (Cursor review round 2).
func TestRouteResultRendering(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Two-leg sweep: create a fixture decision with two chosen legs
	qty100 := dec("100")
	sweepDec := canonical.Decision{
		RoundID:  sd.round.ID,
		TargetID: seedKalshiA,
		Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty100, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
		Chosen: []canonical.Leg{
			{InstrumentID: seedPolyB, Venue: canonical.VenuePolymarket, Side: canonical.SideBuy, Outcome: canonical.OutcomeYes,
				Quantity: dec("60"), VWAP: dec("0.50"), FeePerContract: dec("0.015"), BasisPenaltyPerContract: dec("0.001"), AllInPerContract: dec("0.516"), Route: "direct"},
			{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi, Side: canonical.SideBuy, Outcome: canonical.OutcomeYes,
				Quantity: dec("40"), VWAP: dec("0.52"), FeePerContract: dec("0.012"), BasisPenaltyPerContract: dec("0.000"), AllInPerContract: dec("0.532"), Route: "direct"},
		},
		Alternatives: []canonical.Alternative{{Rank: 1, Legs: []canonical.Leg{{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi}}, AllInPerContract: dec("0.54"), Note: "single venue"}},
		Exclusions:   []canonical.Exclusion{{InstrumentID: seedKalshiC, Venue: canonical.VenueKalshi, Reason: "not_routable", Detail: "related only"}},
		Explanation:  "Sweep across 2 venues was cheaper.",
		DecidedAt:    sd.round.FinishedAt,
	}
	sweepID, err := st.InsertRoutingDecision(ctx, sweepDec)
	if err != nil {
		t.Fatal(err)
	}

	sweepRec := get(s.Handler(), "/decisions/"+strconv.FormatInt(sweepID, 10))
	wantHTML(t, sweepRec, http.StatusOK)
	sweepBody := sweepRec.Body.String()
	wantContains(t, sweepBody,
		"badge badge-l", "Routed across 2 venues", "Chosen legs", // UI-13: v2 badge (v1: class="chip chip-ok")
		"polymarket", "60", "0.5160", // First leg
		"kalshi", "40", "0.5320", // Second leg
		`<tr class="totals">`, "100 of 100", `<strong>Total</strong>`, // Totals row
	)

	// Complement leg: create a decision with a complement route
	compDec := canonical.Decision{
		RoundID:  sd.round.ID,
		TargetID: seedKalshiA,
		Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty100, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
		Chosen: []canonical.Leg{{
			InstrumentID: seedPolyB, Venue: canonical.VenuePolymarket, Side: canonical.SideBuy,
			Outcome: canonical.OutcomeNo, Quantity: dec("100"), VWAP: dec("0.50"),
			FeePerContract: dec("0.01"), BasisPenaltyPerContract: dec("0.005"), AllInPerContract: dec("0.515"),
			Route: "complement",
		}},
		Alternatives: []canonical.Alternative{{
			Rank: 1, Legs: []canonical.Leg{{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi}},
			AllInPerContract: dec("0.52"), Note: "direct route",
		}},
		Exclusions:  []canonical.Exclusion{{InstrumentID: seedKalshiC, Venue: canonical.VenueKalshi, Reason: "not_routable", Detail: "related only"}},
		Explanation: "Complement was cheaper than direct.",
		DecidedAt:   sd.round.FinishedAt,
	}
	compID, err := st.InsertRoutingDecision(ctx, compDec)
	if err != nil {
		t.Fatal(err)
	}

	compRec := get(s.Handler(), "/decisions/"+strconv.FormatInt(compID, 10))
	wantHTML(t, compRec, http.StatusOK)
	compBody := compRec.Body.String()
	wantContains(t, compBody,
		"Routed as NO on polymarket (complement)", "badge badge-l", // UI-13: v2 badge (v1: class="chip chip-ok")
		"complement: NO on",              // Exact Route cell with uppercase
		"0.0050",                         // Gap column (without hardcoded +)
		`class="reason"`, "not routable", // UI-13: reason with class (v1: <strong>not routable</strong>)
	)

	// Partial complement: should say "Partially routed", not override with complement phrase
	qty80 := dec("80")
	partialCompDec := canonical.Decision{
		RoundID:  sd.round.ID,
		TargetID: seedKalshiA,
		Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty100, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
		Chosen: []canonical.Leg{{
			InstrumentID: seedPolyB, Venue: canonical.VenuePolymarket, Side: canonical.SideBuy,
			Outcome: canonical.OutcomeNo, Quantity: qty80, VWAP: dec("0.50"),
			FeePerContract: dec("0.01"), BasisPenaltyPerContract: dec("0.005"), AllInPerContract: dec("0.515"),
			Route: "complement",
		}},
		Exclusions:  []canonical.Exclusion{{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi, Reason: "insufficient_depth", Detail: "only 80 available"}},
		Explanation: "Partial fill: complement venue had only 80 contracts.",
		DecidedAt:   sd.round.FinishedAt,
	}
	partialCompID, err := st.InsertRoutingDecision(ctx, partialCompDec)
	if err != nil {
		t.Fatal(err)
	}

	partialCompRec := get(s.Handler(), "/decisions/"+strconv.FormatInt(partialCompID, 10))
	wantHTML(t, partialCompRec, http.StatusOK)
	partialCompBody := partialCompRec.Body.String()
	wantContains(t, partialCompBody,
		"Partially routed", "badge badge-l", // UI-13: v2 badge (v1: class="chip chip-warn")
		"complement: NO on", // Route cell still shows complement
		"80 of 100",         // Partial fill in totals
	)

	// Notional order: totals row should not show "of 0"
	notional50 := dec("50")
	notionalDec := canonical.Decision{
		RoundID:  sd.round.ID,
		TargetID: seedKalshiA,
		Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Notional: &notional50, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
		Chosen: []canonical.Leg{{
			InstrumentID: seedPolyB, Venue: canonical.VenuePolymarket, Side: canonical.SideBuy,
			Outcome: canonical.OutcomeYes, Quantity: dec("100"), VWAP: dec("0.50"),
			FeePerContract: dec("0.01"), BasisPenaltyPerContract: dec("0.000"), AllInPerContract: dec("0.51"),
			Route: "direct",
		}},
		Alternatives: []canonical.Alternative{{Rank: 1, AllInPerContract: dec("0.51"), Note: "chosen", Legs: []canonical.Leg{{InstrumentID: seedPolyB, Venue: canonical.VenuePolymarket, Outcome: canonical.OutcomeYes, Side: canonical.SideBuy, Quantity: dec("100"), VWAP: dec("0.50"), FeePerContract: dec("0.01"), BasisPenaltyPerContract: dec("0.000"), AllInPerContract: dec("0.51")}}}},
		Explanation:  "Notional order filled with 100 contracts.",
		DecidedAt:    sd.round.FinishedAt,
	}
	notionalID, err := st.InsertRoutingDecision(ctx, notionalDec)
	if err != nil {
		t.Fatal(err)
	}

	notionalRec := get(s.Handler(), "/decisions/"+strconv.FormatInt(notionalID, 10))
	wantHTML(t, notionalRec, http.StatusOK)
	notionalBody := notionalRec.Body.String()
	wantContains(t, notionalBody,
		"Routed", "badge badge-l", // UI-13: v2 badge (v1: class="chip chip-ok")
		"<strong>100</strong>", // Totals should show just "100", not "100 of 0"
	)
	if strings.Contains(notionalBody, " of 0") {
		t.Error("notional order totals row shows 'of 0'")
	}
}

// TestPOSTRouteP95 measures p95 latency for POST /route HTTP endpoint (UI-25 AC ≤ 300ms).
func TestPOSTRouteP95(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping p95 measurement in short mode")
	}

	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	form := validRouteForm(sd.eqID, seedKalshiA)

	const samples = 1000
	durations := make([]time.Duration, samples)

	for i := 0; i < samples; i++ {
		start := time.Now()
		rec := postForm(s.Handler(), "/route", form, true)
		durations[i] = time.Since(start)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /route: status %d, want 200", rec.Code)
		}
	}

	// Calculate percentiles
	sortFunc := func(a, b time.Duration) int {
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	}
	slices.SortFunc(durations, sortFunc)
	p50 := durations[samples*50/100]
	p95 := durations[samples*95/100]
	p99 := durations[samples*99/100]

	t.Logf("POST /route HTTP endpoint latency over %d samples: p50=%v, p95=%v, p99=%v", samples, p50, p95, p99)
	if p95 > 300*time.Millisecond {
		t.Errorf("p95 latency %v > 300ms AC", p95)
	}
}

// UI-13: Test new v2 trace fields
func TestRouteResultV2Fields(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Check for new v2 elements
	wantContains(t, body,
		`<dl class="tiles">`,           // Cost report tiles
		"Avg all-in",                   // Key tile
		"Total cost",                   // Total cost tile
		"Slippage vs quoted",           // Slippage tile (UI-13)
		`<ol class="trace-steps">`,     // Decision trace
		"Order and input snapshot",     // Step 1
		"Eligibility",                  // Step 2
		"Fill simulation and fee math", // Step 3
		"Ranked alternatives",          // Step 4
		"Decision",                     // Step 5
		"Sign rule",                    // Sign rule text
		"permalink",                    // Permalink to decision
	)
}

// UI-13: Test routed state with slippage
func TestRouteResultRoutedState(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		"Routed",   // Outcome badge
		"Quoted",   // Quoted column header
		"Slippage", // Slippage column header
		"of 100",   // Filled quantity
	)
}

// UI-13: Test not routed state
func TestRouteResultNotRoutedState(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	form := validRouteForm(sd.eqID, seedKalshiA)
	form.Set("type", "limit")
	form.Set("limit", "0.01") // Price too low, no venue can fill
	rec := postForm(s.Handler(), "/route", form, true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		"Not routed",                   // Outcome badge
		"No venue can fill this order", // Not routed message
		"Eligibility",                  // UI-13: Exclusions in trace step 2 (v1: "Excluded candidates")
		"candidates kept",              // Kept count
	)
}

// UI-13: Test sign rule rendering (positive = saving, parentheses = loss)
func TestRouteResultSignRule(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()

	// Create decision with alternatives that have both savings (negative gap) and losses (positive gap)
	// Chosen route: 0.51 all-in
	// Alt rank 2: 0.50 all-in (BETTER = saving = negative gap = -0.01 = should render as 1.00¢ without parens)
	// Alt rank 3: 0.52 all-in (WORSE = loss = positive gap = +0.01 = should render as (1.00¢) with parens)
	qty := dec("100")
	d := canonical.Decision{
		RoundID:  sd.round.ID,
		TargetID: seedKalshiA,
		Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
		Inputs: []canonical.DecisionInput{
			{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi, RelationshipType: "self", Confidence: dec("1.0"), OutcomeSide: canonical.OutcomeYes, BestAsk: dec("0.52"), AskDepth: []canonical.Level{{Price: dec("0.52"), Size: dec("300")}}, FetchedAt: sd.round.FinishedAt},
			{InstrumentID: seedPolyB, Venue: canonical.VenuePolymarket, RelationshipType: canonical.RelEquivalent, Confidence: dec("0.97"), OutcomeSide: canonical.OutcomeYes, BestAsk: dec("0.50"), AskDepth: []canonical.Level{{Price: dec("0.50"), Size: dec("50")}}, FetchedAt: sd.round.FinishedAt},
		},
		Chosen: []canonical.Leg{{
			InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi, Side: canonical.SideBuy,
			Outcome: canonical.OutcomeYes, Quantity: dec("100"), VWAP: dec("0.50"),
			FeePerContract: dec("0.01"), BasisPenaltyPerContract: dec("0.000"), AllInPerContract: dec("0.51"),
			Route: "direct",
		}},
		Alternatives: []canonical.Alternative{
			{Rank: 1, AllInPerContract: dec("0.51"), Note: "chosen", Legs: []canonical.Leg{{InstrumentID: seedKalshiA, Venue: canonical.VenueKalshi, Outcome: canonical.OutcomeYes, Side: canonical.SideBuy, Quantity: dec("100"), VWAP: dec("0.50"), FeePerContract: dec("0.01"), BasisPenaltyPerContract: dec("0.000"), AllInPerContract: dec("0.51")}}},
			{Rank: 2, AllInPerContract: dec("0.50"), Note: "better than chosen", Legs: []canonical.Leg{{InstrumentID: seedPolyB, Venue: canonical.VenuePolymarket, Outcome: canonical.OutcomeYes, Side: canonical.SideBuy, Quantity: dec("100"), VWAP: dec("0.49"), FeePerContract: dec("0.01"), BasisPenaltyPerContract: dec("0.000"), AllInPerContract: dec("0.50")}}},
			{Rank: 3, AllInPerContract: dec("0.52"), Note: "worse than chosen", Legs: []canonical.Leg{{InstrumentID: seedKalshiC, Venue: canonical.VenueKalshi, Outcome: canonical.OutcomeYes, Side: canonical.SideBuy, Quantity: dec("100"), VWAP: dec("0.51"), FeePerContract: dec("0.01"), BasisPenaltyPerContract: dec("0.000"), AllInPerContract: dec("0.52")}}},
		},
		Explanation: "Test decision with savings and losses.",
		DecidedAt:   sd.round.FinishedAt,
	}
	decID, err := st.InsertRoutingDecision(ctx, d)
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/decisions/"+strconv.FormatInt(decID, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Sign rule text should be documented
	wantContains(t, body, "Sign rule", "positive", "saving", "parentheses", "loss", "unfavourable")

	// Check for cents format
	if !strings.Contains(body, "¢") {
		t.Error("should show gap in cents (¢)")
	}

	// Check for proper sign rule application per ticket AC:
	// Losses (unfavourable): in parentheses (X.XX¢)
	// Savings (favourable): positive, no parentheses X.XX¢

	// Check for loss format: (X.XX¢) - ¢ followed by )
	if !strings.Contains(body, "¢)") {
		t.Error("should show losses in parentheses as (X.XX¢) per sign rule")
	}

	// Check for saving format: X.XX¢ NOT in parentheses - ¢ followed by space or · but NOT )
	// Pattern: ¢ followed by space or · (the · separator before bps)
	hasSaving := strings.Contains(body, "¢ ·") || strings.Contains(body, "¢ bps")
	if !hasSaving {
		// Also check for ¢ at end of cell (before </td> or </text>)
		hasSaving = strings.Contains(body, "¢<")
	}
	if !hasSaving {
		t.Error("should show at least one saving as positive (X.XX¢ not in parentheses) per sign rule")
	}

	// Gap/slippage values in bps
	if !strings.Contains(body, "bps") {
		t.Error("should show gap/slippage in bps")
	}

	// Verify ¢ is INSIDE parentheses, not outside: (X.XX¢) not (X.XX)¢
	if strings.Contains(body, ")¢") && !strings.Contains(body, "¢)") {
		t.Error("found )¢ pattern - ¢ should be inside parentheses: (X.XX¢) not (X.XX)¢")
	}
}

// UI-13: Test input snapshots for decisions with stored inputs
func TestRouteResultWithInputs(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		"Order and input snapshot", // Step 1 title
		"Best ask",                 // Input snapshot column
		"Depth",                    // Input snapshot column
		"Fetched",                  // Input snapshot column
		"Relationship",             // Input snapshot column
	)

	// Should NOT show the pre-UI-25 message
	if strings.Contains(body, "Input quotes were not stored") {
		t.Error("should not show pre-UI-25 message for new decisions")
	}
}

// TestRouteResultPreUI25Message tests that decisions without stored inputs show the pre-UI-25 message.
func TestRouteResultPreUI25Message(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	// Create a decision with no Inputs (simulating pre-UI-25)
	qty := dec("100")
	decision := canonical.Decision{
		RoundID:      sd.round.ID,
		TargetID:     seedKalshiA,
		DecidedAt:    seedNow,
		Order:        canonical.Order{Side: "buy", Outcome: "yes", Quantity: &qty, Type: "market", TIF: "IOC"},
		Chosen:       []canonical.Leg{{Venue: "kalshi", InstrumentID: seedKalshiA, Outcome: "yes", Quantity: qty, VWAP: dec("0.50"), FeePerContract: dec("0.007"), BasisPenaltyPerContract: dec("0.01"), AllInPerContract: dec("0.517")}},
		Alternatives: nil,
		Exclusions:   nil,
		Inputs:       nil, // No inputs stored
		Explanation:  "Direct fill.",
	}
	id, err := st.InsertRoutingDecision(context.Background(), decision)
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/"+"decisions/"+strconv.FormatInt(id, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		"Order and input snapshot",                   // Step 1 title
		"Input quotes were not stored for decisions", // Pre-UI-25 message
		"2026-10-08", // Cutoff date
	)

	// Should NOT show input snapshot table
	if strings.Contains(body, "Best ask") || strings.Contains(body, "Depth") {
		t.Error("should not show input snapshot table for pre-UI-25 decisions")
	}
}

// TestRouteResultPartialFillBar tests that partial fills show the .seg-unfilled segment.
func TestRouteResultPartialFillBar(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	// Create a decision with a partial fill (requested 100, filled 60)
	qty := dec("100")
	filled := dec("60")
	decision := canonical.Decision{
		RoundID:     sd.round.ID,
		TargetID:    seedKalshiA,
		DecidedAt:   seedNow,
		Order:       canonical.Order{Side: "buy", Outcome: "yes", Quantity: &qty, Type: "market", TIF: "IOC"},
		Chosen:      []canonical.Leg{{Venue: "kalshi", InstrumentID: seedKalshiA, Outcome: "yes", Quantity: filled, VWAP: dec("0.50"), FeePerContract: dec("0.007"), BasisPenaltyPerContract: dec("0.01"), AllInPerContract: dec("0.517")}},
		Explanation: "Partial fill due to insufficient depth.",
	}
	id, err := st.InsertRoutingDecision(context.Background(), decision)
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/decisions/"+strconv.FormatInt(id, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Partial fill should show unfilled segment in split bar
	wantContains(t, body,
		"svg",             // SVG element exists
		"class=\"split\"", // Split bar class
		"seg-unfilled",    // Unfilled segment for partial (40% of 100)
	)
}

// M5-08: Test buildLeadSentence function directly
func TestM508BuildLeadSentence(t *testing.T) {
	qty := dec("100")

	// Test routed with gap to next alternative
	d := canonical.Decision{
		TargetID: seedKalshiA,
		Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty},
		Chosen:   []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: qty, AllInPerContract: dec("0.5000"), Route: "direct", Outcome: canonical.OutcomeYes}},
	}
	alts := []alternativeView{
		{Alternative: canonical.Alternative{Rank: 1, AllInPerContract: dec("0.5000"), Legs: []canonical.Leg{{Venue: canonical.VenuePolymarket}}}, GapToChosen: dec("0")},
		{Alternative: canonical.Alternative{Rank: 2, AllInPerContract: dec("0.5100"), Legs: []canonical.Leg{{Venue: canonical.VenueKalshi}}}, GapToChosen: dec("0.0100")}, // 1 cent more expensive
	}
	lead := buildLeadSentence(d, qty, qty, dec("0.5000"), alts)
	wantParts := []string{"Routed:", "buy", "100", "YES", "polymarket", "0.5000", "all-in", "cheaper than", "kalshi"}
	for _, want := range wantParts {
		if !strings.Contains(lead, want) {
			t.Errorf("lead sentence %q lacks %q", lead, want)
		}
	}

	// Test partial fill
	d.Chosen[0].Quantity = dec("80")
	lead = buildLeadSentence(d, dec("80"), qty, dec("0.5000"), alts)
	if !strings.Contains(lead, "Partially routed: 80 of 100 contracts") {
		t.Errorf("partial lead = %q", lead)
	}

	// Test not routed
	d.Chosen = nil
	d.Exclusions = []canonical.Exclusion{{Reason: "stale"}, {Reason: "insufficient_depth"}}
	lead = buildLeadSentence(d, dec("0"), qty, dec("0"), nil)
	if !strings.Contains(lead, "Not routed: 1 stale, 1 too shallow") {
		t.Errorf("not-routed lead = %q", lead)
	}

	// Test split across venues
	d.Chosen = []canonical.Leg{
		{Venue: canonical.VenuePolymarket, Quantity: dec("60"), AllInPerContract: dec("0.5000"), Route: "direct"},
		{Venue: canonical.VenueKalshi, Quantity: dec("40"), AllInPerContract: dec("0.5100"), Route: "direct"},
	}
	lead = buildLeadSentence(d, qty, qty, dec("0.5040"), alts)
	if !strings.Contains(lead, "Routed across 2 venues: 60 + 40 contracts") {
		t.Errorf("split lead = %q", lead)
	}

	// Test complement
	d.Chosen = []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: qty, AllInPerContract: dec("0.5000"), Route: "complement", Outcome: canonical.OutcomeNo}}
	lead = buildLeadSentence(d, qty, qty, dec("0.5000"), alts)
	if !strings.Contains(lead, "Routed as NO on polymarket (complement of the target)") {
		t.Errorf("complement lead = %q", lead)
	}
}

// M5-08: Test lead sentence appears in route result
func TestM508LeadSentenceInResult(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Should show lead sentence in design.md § 4 format
	wantContains(t, body,
		"Routed:",           // Lead sentence prefix
		"buy",               // Side from order
		"YES",               // Outcome from order
		"at",                // Preposition before all-in
		"all-in",            // All-in mention
		"outcome-summary",   // CSS class for lead sentence
	)
}

// M5-08: Test lead sentence for partial fill
func TestM508LeadSentencePartial(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	qty := dec("100")
	filled := dec("80")
	decision := canonical.Decision{
		RoundID:   sd.round.ID,
		TargetID:  seedKalshiA,
		DecidedAt: seedNow,
		Order:     canonical.Order{Side: "buy", Outcome: "yes", Quantity: &qty, Type: "market", TIF: "IOC"},
		Chosen:    []canonical.Leg{{Venue: "polymarket", InstrumentID: seedPolyB, Outcome: "yes", Quantity: filled, VWAP: dec("0.50"), FeePerContract: dec("0.007"), BasisPenaltyPerContract: dec("0.01"), AllInPerContract: dec("0.517"), Route: "direct"}},
		Alternatives: []canonical.Alternative{
			{Rank: 1, AllInPerContract: dec("0.517"), Legs: []canonical.Leg{{Venue: "polymarket", InstrumentID: seedPolyB, Quantity: filled}}},
		},
		Explanation: "Partial fill.",
	}
	id, err := st.InsertRoutingDecision(context.Background(), decision)
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/decisions/"+strconv.FormatInt(id, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		"Partially routed:", // Lead sentence for partial fill
		"80 of 100",         // Filled vs requested
		"contracts",         // Unit
	)
}

// M5-08: Test lead sentence for not-routed result
func TestM508LeadSentenceNotRouted(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	qty := dec("100")
	decision := canonical.Decision{
		RoundID:   sd.round.ID,
		TargetID:  seedKalshiA,
		DecidedAt: seedNow,
		Order:     canonical.Order{Side: "buy", Outcome: "yes", Quantity: &qty, Type: "market", TIF: "IOC"},
		Exclusions: []canonical.Exclusion{
			{InstrumentID: seedKalshiA, Venue: "kalshi", Reason: "stale"},
			{InstrumentID: seedPolyB, Venue: "polymarket", Reason: "insufficient_depth"},
			{InstrumentID: seedKalshiC, Venue: "kalshi", Reason: "insufficient_depth"},
		},
		Explanation: "All excluded.",
	}
	id, err := st.InsertRoutingDecision(context.Background(), decision)
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/decisions/"+strconv.FormatInt(id, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Should show not-routed lead sentence with reasons in words (not codes)
	wantContains(t, body,
		"Not routed:",   // Lead sentence prefix
		"stale",         // Reason phrase (not "stale" code)
		"too shallow",   // Reason phrase for insufficient_depth (not the code)
		"1 stale",       // Tally format
		"2 too shallow", // Tally format
	)

	// Should NOT show reason codes
	if strings.Contains(body, "insufficient_depth") {
		t.Error("should show 'too shallow' not 'insufficient_depth' code")
	}
}

// M5-08: Test lead sentence for split across venues
func TestM508LeadSentenceSplit(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	qty := dec("100")
	decision := canonical.Decision{
		RoundID:   sd.round.ID,
		TargetID:  seedKalshiA,
		DecidedAt: seedNow,
		Order:     canonical.Order{Side: "buy", Outcome: "yes", Quantity: &qty, Type: "market", TIF: "IOC"},
		Chosen: []canonical.Leg{
			{Venue: "polymarket", InstrumentID: seedPolyB, Outcome: "yes", Quantity: dec("60"), VWAP: dec("0.50"), FeePerContract: dec("0.007"), BasisPenaltyPerContract: dec("0.01"), AllInPerContract: dec("0.517"), Route: "direct"},
			{Venue: "kalshi", InstrumentID: seedKalshiA, Outcome: "yes", Quantity: dec("40"), VWAP: dec("0.52"), FeePerContract: dec("0.007"), BasisPenaltyPerContract: dec("0.01"), AllInPerContract: dec("0.537"), Route: "direct"},
		},
		Alternatives: []canonical.Alternative{
			{Rank: 1, AllInPerContract: dec("0.525"), Legs: []canonical.Leg{{Venue: "polymarket"}, {Venue: "kalshi"}}},
		},
		Explanation: "Split across venues.",
	}
	id, err := st.InsertRoutingDecision(context.Background(), decision)
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/decisions/"+strconv.FormatInt(id, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		"Routed across 2 venues", // Lead sentence for split
		"+ ",                      // Contract split uses + separator
		"contracts",               // Unit
	)
}

// M5-08: Test lead sentence for complement route
func TestM508LeadSentenceComplement(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)

	qty := dec("100")
	decision := canonical.Decision{
		RoundID:   sd.round.ID,
		TargetID:  seedKalshiA,
		DecidedAt: seedNow,
		Order:     canonical.Order{Side: "buy", Outcome: "yes", Quantity: &qty, Type: "market", TIF: "IOC"},
		Chosen:    []canonical.Leg{{Venue: "polymarket", InstrumentID: seedPolyB, Outcome: "no", Quantity: qty, VWAP: dec("0.50"), FeePerContract: dec("0.007"), BasisPenaltyPerContract: dec("0.01"), AllInPerContract: dec("0.517"), Route: "complement"}},
		Alternatives: []canonical.Alternative{
			{Rank: 1, AllInPerContract: dec("0.517"), Legs: []canonical.Leg{{Venue: "polymarket", Route: "complement", Outcome: "no"}}},
		},
		Explanation: "Complement route.",
	}
	id, err := st.InsertRoutingDecision(context.Background(), decision)
	if err != nil {
		t.Fatal(err)
	}

	rec := get(s.Handler(), "/decisions/"+strconv.FormatInt(id, 10))
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		"Routed as NO on polymarket", // Lead sentence for complement
		"(complement of the target)", // Explanation
	)
}

// M5-08: Test bps explanation and glossary link
func TestM508BpsExplanation(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	// Should have bps explanation note
	wantContains(t, body,
		"Basis points:",      // Explanation text
		"1 bp = 0.01 ¢",      // Conversion
		"per $1",             // Unit
		"/glossary#term-bps", // Link to glossary
	)

	// Should also have link on the "bps" text in slippage row
	if !strings.Contains(body, `<a href="/glossary#term-bps">bps</a>`) {
		t.Error("should link 'bps' in slippage row to glossary")
	}
}

// M5-08: Test decision log links
func TestM508DecisionLogLinks(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		"Recorded as decision #1",              // Decision reference
		"view in decision log",                 // Link text
		"/log?kind=routing",                    // Link to routing log
		`<a href="/decisions/1">permalink</a>`, // Permalink (this one is in header already)
	)
}

// M5-08: Test sign rule mentions D-098
func TestM508SignRuleD098(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	rec := postForm(s.Handler(), "/route", validRouteForm(sd.eqID, seedKalshiA), true)
	wantHTML(t, rec, http.StatusOK)
	body := rec.Body.String()

	wantContains(t, body,
		"Sign rule (D-098):", // Sign rule with D-098 reference
		"positive = saving",  // Sign rule text
		"parentheses = loss", // Sign rule text
	)
}
