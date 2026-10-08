package web

import (
	"context"
	"net/http"
	"net/url"
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
		`id="route-result"`, "Decision #1", "buy 100 contracts YES, market IOC", "Partners considered: 2",
		// Polymarket fills 50 at 0.50 and 50 at 0.53 (VWAP 0.515, fee 0.0175, basis 0.001, all-in 0.5335); Kalshi fills all 100 at 0.52 (all-in 0.5375).
		"Chosen legs", `<span class="venue">polymarket</span>`, "0.5150", "0.5335",
		"Ranked alternatives", "0.5375",
		"Excluded candidates", seedKalshiC, "not routable",
		"Explanation", "Simulated buy of 100 YES contracts on "+seedKalshiA,
		"basis penalty = score",
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
		name     string
		decision canonical.Decision
		wantChip string
		want     []string
	}{
		{
			name: "routed_single_venue",
			decision: canonical.Decision{
				TargetID: seedKalshiA,
				Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty},
				Chosen:   []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: qty, AllInPerContract: dec("0.5335"), Route: "direct"}},
			},
			wantChip: "chip-ok",
			want:     []string{"Routed", "filled 100 contracts"},
		},
		{
			name: "routed_across_venues",
			decision: canonical.Decision{
				TargetID: seedKalshiA,
				Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty},
				Chosen: []canonical.Leg{
					{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: dec("60"), AllInPerContract: dec("0.5335"), Route: "direct"},
					{Venue: canonical.VenueKalshi, InstrumentID: seedKalshiA, Quantity: dec("40"), AllInPerContract: dec("0.5375"), Route: "direct"},
				},
			},
			wantChip: "chip-ok",
			want:     []string{"Routed across 2 venues", "filled 100 contracts"},
		},
		{
			name: "partially_routed",
			decision: canonical.Decision{
				TargetID: seedKalshiA,
				Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty, TIF: canonical.TIFIOC},
				Chosen:   []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: dec("80"), AllInPerContract: dec("0.5335"), Route: "direct"}},
			},
			wantChip: "chip-warn",
			want:     []string{"Partially routed", "filled 80 of 100"},
		},
		{
			name: "complement_route",
			decision: canonical.Decision{
				TargetID: seedKalshiA,
				Order:    canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty},
				Chosen:   []canonical.Leg{{Venue: canonical.VenuePolymarket, InstrumentID: seedPolyB, Quantity: qty, AllInPerContract: dec("0.5335"), Route: "complement", Outcome: canonical.OutcomeNo}},
			},
			wantChip: "chip-ok",
			want:     []string{"Routed as complement", "filled 100 contracts"},
		},
		{
			name: "not_routed",
			decision: canonical.Decision{
				TargetID:   seedKalshiA,
				Order:      canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty},
				Exclusions: []canonical.Exclusion{{Reason: "stale"}, {Reason: "insufficient_depth"}, {Reason: "insufficient_depth"}},
			},
			wantChip: "chip-danger",
			want:     []string{"Not routed", "1 stale", "2 too shallow"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view := newDecisionView(tc.decision, 2, time.Now(), map[string]string{})
			if view.OutcomeChipClass != tc.wantChip {
				t.Errorf("chip class = %q, want %q", view.OutcomeChipClass, tc.wantChip)
			}
			for _, want := range tc.want {
				if !strings.Contains(view.OutcomePhrase, want) && !strings.Contains(view.OutcomeSummary, want) {
					t.Errorf("outcome phrase %q or summary %q lacks %q", view.OutcomePhrase, view.OutcomeSummary, want)
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
