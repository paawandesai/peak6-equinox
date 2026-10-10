package web

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

const (
	demoKHigh  = "kalshi:KXFED-26OCT-T4.25"
	demoKLow   = "kalshi:KXFED-26OCT-T3.50"
	demoKDec   = "kalshi:KXFED-26DEC-T3.75"
	demoPUp50  = "polymarket:111"
	demoPUp25  = "polymarket:112"
	demoPDn50  = "polymarket:113"
	demoPDec25 = "polymarket:114"
)

// fedInstrument is a Fed contract with the stored terms the guided demo reads.
func fedInstrument(id string, v canonical.Venue, metric, threshold, title string) canonical.Instrument {
	th := dec(threshold)
	deadline := time.Date(2026, 10, 28, 18, 0, 0, 0, time.UTC)
	fee := kalshiFee
	if v == canonical.VenuePolymarket {
		fee = polyFee
	}
	return canonical.Instrument{
		ID: id, Venue: v, VenueMarketID: strings.SplitN(id, ":", 2)[1], VenueEventID: "evt-fed",
		Category: canonical.CategoryFed, Title: title, RulesText: "rules",
		Terms: canonical.Terms{Subject: "fed_funds_rate", Metric: metric, Comparator: canonical.ComparatorGT, Threshold: &th,
			Unit: "percent", Deadline: &deadline, DeadlineTZ: "UTC", ResolutionSource: "federalreserve.gov", EdgeRules: canonical.Unknown,
			Polarity: canonical.PolarityPositive},
		CloseTime: deadline, Status: canonical.StatusOpen, FeeSchedule: fee,
	}
}

func demoInstruments() []canonical.Instrument {
	return []canonical.Instrument{
		fedInstrument(demoKHigh, canonical.VenueKalshi, fedMetricUpperBound, "4.25", "Fed upper bound above 4.25%"),
		fedInstrument(demoKLow, canonical.VenueKalshi, fedMetricUpperBound, "3.50", "Fed upper bound above 3.50%"),
		fedInstrument(demoKDec, canonical.VenueKalshi, fedMetricUpperBound, "3.75", "Fed upper bound above 3.75% in December"),
		fedInstrument(demoPUp50, canonical.VenuePolymarket, fedMetricChange, "0.50", "Fed increases 50+ bps after October 2026 meeting?"),
		fedInstrument(demoPUp25, canonical.VenuePolymarket, fedMetricChange, "0.25", "Fed increases 25 bps after October 2026 meeting?"),
		fedInstrument(demoPDn50, canonical.VenuePolymarket, fedMetricChange, "-0.50", "Fed decreases 50+ bps after October 2026 meeting?"),
		fedInstrument(demoPDec25, canonical.VenuePolymarket, fedMetricChange, "-0.25", "Fed decreases 25 bps after December 2026 meeting?"),
	}
}

var demoFacts = []canonical.FactUse{
	{Key: factFedUpperBound, Value: "4.00", Source: "https://www.federalreserve.gov/", AsOf: time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)},
	{Key: factFedNextMeeting, Value: "2026-10-28", Source: "https://www.federalreserve.gov/", AsOf: time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)},
}

const demoNotApply = "related: vetoed on metric. Facts: fact fed_upper_bound=4.00 is the level before the 2026-10-28 meeting (fed_next_meeting), but polymarket:114 resolves on 2026-12-10: a later meeting's change depends on the prior decision, so the fact does not apply."

// demoRelationships is the guided demo's data plus decoys that must not be
// picked: an implies pair on the T4.25 contract and a related pair with no
// fact consulted.
func demoRelationships() []canonical.Relationship {
	return []canonical.Relationship{
		{ID: 10, AID: demoKHigh, BID: demoPUp25, Type: canonical.RelImplies, Confidence: dec("0.95"), Explanation: "implies"},
		{ID: 11, AID: demoKHigh, BID: demoPUp50, Type: canonical.RelEquivalentGivenFacts, Confidence: dec("0.93"), Explanation: "egf", FactsUsed: demoFacts},
		{ID: 12, AID: demoKLow, BID: demoPUp50, Type: canonical.RelRelated, Confidence: dec("0.40"), Explanation: "related, no fact"},
		{ID: 13, AID: demoKDec, BID: demoPDec25, Type: canonical.RelRelated, Confidence: dec("0.40"), Explanation: demoNotApply, FactsUsed: demoFacts[:1],
			Vetoes: []canonical.Veto{{Code: "metric_mismatch", Field: "metric", Detail: "rate_upper_bound vs rate_change"}}},
		{ID: 14, AID: demoKLow, BID: demoPDn50, Type: canonical.RelComplement, Confidence: dec("0.95"), Explanation: "complement"},
	}
}

func demoRecords() map[string]store.InstrumentRecord {
	out := map[string]store.InstrumentRecord{}
	for _, in := range demoInstruments() {
		out[in.ID] = store.InstrumentRecord{Instrument: in}
	}
	return out
}

func TestFedMeetingCode(t *testing.T) {
	for in, want := range map[string]string{"2026-10-28": "26OCT", "2026-12-09": "26DEC", " 2027-01-27 ": "27JAN"} {
		if got, ok := fedMeetingCode(in); !ok || got != want {
			t.Errorf("fedMeetingCode(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "Oct 28", "2026-13-01"} {
		if _, ok := fedMeetingCode(bad); ok {
			t.Errorf("fedMeetingCode(%q) resolved; want not ok", bad)
		}
	}
}

// TestResolveStartHereSteps: all four steps resolve by instrument ID, in
// order, with the presets the demo needs (M6 P1).
func TestResolveStartHereSteps(t *testing.T) {
	steps := resolveStartHere(demoRelationships(), demoRecords(), "2026-10-28")
	if len(steps) != 4 {
		t.Fatalf("got %d steps, want 4: %+v", len(steps), steps)
	}
	if steps[0].Href != "/pairs/11#diff" {
		t.Errorf("step 1 href = %q, want the egf pair's diff", steps[0].Href)
	}
	wantPreset(t, steps[1].Href, 11, demoKHigh, "buy", "yes")
	if steps[2].Href != "/pairs/13#diff" {
		t.Errorf("step 3 href = %q, want the near miss where the fact does not apply", steps[2].Href)
	}
	wantPreset(t, steps[3].Href, 14, demoKLow, "buy", "no")
	if !strings.Contains(steps[0].Note, "KXFED-26OCT-T4.25") || !strings.Contains(steps[3].Note, "KXFED-26OCT-T3.50") {
		t.Errorf("notes lack the contract codes: %q / %q", steps[0].Note, steps[3].Note)
	}
}

func wantPreset(t *testing.T, href string, pairID int64, target, side, outcome string) {
	t.Helper()
	u, err := url.Parse(href)
	if err != nil {
		t.Fatalf("bad href %q: %v", href, err)
	}
	if u.Path != fmt.Sprintf("/pairs/%d", pairID) || u.Fragment != "route" {
		t.Errorf("href %q: want /pairs/%d#route", href, pairID)
	}
	q := u.Query()
	want := map[string]string{"target": target, "side": side, "outcome": outcome, "qty": "100", "type": "market", "tif": "IOC"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("href %q: %s = %q, want %q", href, k, q.Get(k), v)
		}
	}
}

// TestResolveStartHereStoredMeeting: with no facts file the meeting comes
// from the facts the matcher stored on the round.
func TestResolveStartHereStoredMeeting(t *testing.T) {
	steps := resolveStartHere(demoRelationships(), demoRecords(), "")
	if len(steps) != 4 {
		t.Fatalf("got %d steps, want 4", len(steps))
	}
}

// TestResolveStartHereHidesUnresolved: a step whose pair is not in the round
// is left out, never linked to nothing.
func TestResolveStartHereHidesUnresolved(t *testing.T) {
	rels := demoRelationships()
	noComplement := rels[:4]
	steps := resolveStartHere(noComplement, demoRecords(), "2026-10-28")
	if len(steps) != 3 {
		t.Fatalf("got %d steps without the complement, want 3", len(steps))
	}
	for _, s := range steps {
		if strings.Contains(s.Href, "outcome=no") {
			t.Errorf("complement step shown although unresolved: %q", s.Href)
		}
	}

	// A different meeting: the October contracts are not that meeting's,
	// so only the near miss resolves.
	steps = resolveStartHere(rels, demoRecords(), "2026-12-09")
	if len(steps) != 1 || steps[0].Href != "/pairs/13#diff" {
		t.Fatalf("December meeting: got %+v, want only the near-miss step", steps)
	}

	// Nothing Fed-shaped in the round: no steps.
	if steps := resolveStartHere(nil, demoRecords(), "2026-10-28"); len(steps) != 0 {
		t.Fatalf("empty round: got %+v, want none", steps)
	}
	// A missing instrument record hides the step too.
	recs := demoRecords()
	delete(recs, demoPUp50)
	for _, s := range resolveStartHere(rels, recs, "2026-10-28") {
		if strings.HasPrefix(s.Href, "/pairs/11") {
			t.Errorf("step for pair 11 shown without its instrument: %q", s.Href)
		}
	}
}

// TestApplyRoutePreset covers valid, invalid and partial preset params (AB-08:
// a bad value falls back to the default, nothing is submitted).
func TestApplyRoutePreset(t *testing.T) {
	base := defaultRouteForm(canonical.Relationship{ID: 7, AID: demoKHigh, BID: demoPUp50})
	tests := []struct {
		name    string
		query   string
		want    routeFormView
		applied bool
	}{
		{"none", "", base, false},
		{"valid full", "target=" + url.QueryEscape(demoPUp50) + "&side=sell&outcome=no&qty=250&type=limit&tif=fok",
			func() routeFormView {
				f := base
				f.Target, f.Side, f.Outcome, f.Quantity, f.Type, f.TIF = demoPUp50, "sell", "no", "250", "limit", "FOK"
				return f
			}(), true},
		{"invalid all", "target=kalshi:OTHER&side=short&outcome=maybe&qty=0&type=stop&tif=GTC", base, false},
		{"qty not a number", "qty=abc", base, false},
		{"qty too big", "qty=100001", base, false},
		{"qty fractional", "qty=1.5", base, false},
		{"too long", "side=" + strings.Repeat("b", maxFieldLen+1), base, false},
		{"partial outcome", "outcome=NO", func() routeFormView { f := base; f.Outcome = "no"; return f }(), true},
		{"venue target", "target=polymarket", func() routeFormView { f := base; f.Target = demoPUp50; return f }(), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := url.ParseQuery(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			got, applied := applyRoutePreset(base, q)
			if applied != tt.applied {
				t.Errorf("applied = %v, want %v", applied, tt.applied)
			}
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("form = %+v\nwant %+v", got, tt.want)
			}
		})
	}
	// A venue that matches both sides picks neither.
	both := defaultRouteForm(canonical.Relationship{ID: 8, AID: demoKHigh, BID: demoKLow})
	if got, _ := applyRoutePreset(both, url.Values{"target": {"kalshi"}}); got.Target != demoKHigh {
		t.Errorf("ambiguous venue target changed the default: %q", got.Target)
	}
}

// seedDemoRound stores the guided-demo data in one finished round and returns
// the stored ids by the test ids in demoRelationships.
func seedDemoRound(t *testing.T, st *store.Store, rels []canonical.Relationship) map[int64]int64 {
	t.Helper()
	ctx := context.Background()
	round, err := st.CreateRound(ctx, seedNow)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, round.ID, store.RoundOK, nil, seedNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	byID := map[string]canonical.Instrument{}
	for _, in := range demoInstruments() {
		byID[in.ID] = in
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	ids := map[int64]int64{}
	for _, r := range rels {
		testID := r.ID
		r.ID, r.RoundID, r.Method = 0, round.ID, canonical.MethodRules
		r.Diff = match.Diff(byID[r.AID].Terms, byID[r.BID].Terms)
		id, err := st.UpsertRelationship(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		ids[testID] = id
	}
	return ids
}

// TestStartHereGuidedDemo renders the card on / from stored data and follows
// the route preset to a pre-filled, unsubmitted form (M6 P1).
func TestStartHereGuidedDemo(t *testing.T) {
	_, st := newTestServer(t)
	s := New(Config{MinRouteConfidence: dec("0.9"), FedNextMeeting: "2026-10-28"},
		Deps{Store: st, Now: func() time.Time { return seedNow.Add(3 * time.Minute) }})
	ids := seedDemoRound(t, st, demoRelationships())

	rec := get(s.Handler(), "/")
	wantHTML(t, rec, 200)
	body := rec.Body.String()
	wantContains(t, body,
		fmt.Sprintf(`href="/pairs/%d#diff"`, ids[11]),
		fmt.Sprintf(`href="/pairs/%d?outcome=yes&amp;qty=100&amp;side=buy&amp;target=kalshi%%3AKXFED-26OCT-T4.25&amp;tif=IOC&amp;type=market#route"`, ids[11]),
		fmt.Sprintf(`href="/pairs/%d#diff"`, ids[13]),
		fmt.Sprintf(`href="/pairs/%d?outcome=no&amp;qty=100&amp;side=buy&amp;target=kalshi%%3AKXFED-26OCT-T3.50&amp;tif=IOC&amp;type=market#route"`, ids[14]),
		"See a near miss the Fed fact did not close",
	)
	if strings.Contains(body, `/near-misses#group-fact-did-not-close"`) {
		t.Error("start here still links the near-misses group anchor")
	}

	path := fmt.Sprintf("/pairs/%d?outcome=no&qty=100&side=buy&target=%s&tif=IOC&type=market", ids[14], url.QueryEscape(demoKLow))
	rec = get(s.Handler(), path)
	wantHTML(t, rec, 200)
	body = rec.Body.String()
	wantContains(t, body,
		`<option value="`+demoKLow+`" selected>`,
		`<option value="no" selected>`,
		`<option value="buy" selected>`,
		`Pre-filled from the guided demo`,
	)
	if strings.Contains(body, `id="route-result"></div>`) == false {
		t.Error("route result should be empty: a preset never submits the form")
	}
	// Without presets there is no pre-filled note.
	rec = get(s.Handler(), fmt.Sprintf("/pairs/%d", ids[14]))
	if strings.Contains(rec.Body.String(), "Pre-filled from the guided demo") {
		t.Error("plain pair page claims a preset")
	}
}

// TestStartHereGuidedDemoHiddenStep: with no near miss in the round, step 3
// is not shown and the other steps still are.
func TestStartHereGuidedDemoHiddenStep(t *testing.T) {
	_, st := newTestServer(t)
	s := New(Config{MinRouteConfidence: dec("0.9"), FedNextMeeting: "2026-10-28"},
		Deps{Store: st, Now: func() time.Time { return seedNow.Add(3 * time.Minute) }})
	rels := demoRelationships()
	ids := seedDemoRound(t, st, []canonical.Relationship{rels[1], rels[4]})
	body := get(s.Handler(), "/").Body.String()
	wantContains(t, body, fmt.Sprintf(`href="/pairs/%d#diff"`, ids[11]), "Route the opposite side on the complement pair")
	if strings.Contains(body, "See a near miss the Fed fact did not close") {
		t.Error("near-miss step shown although no pair resolves")
	}
}
