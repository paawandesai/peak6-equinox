package web

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
)

// Fed instruments and facts shaped like config/facts.yaml in round 904:
// fed_upper_bound is the level before the October meeting, so it does not
// apply to the December meeting.
var (
	decMeeting = time.Date(2026, 12, 9, 19, 0, 0, 0, time.UTC)
	decClose   = time.Date(2026, 12, 10, 3, 59, 0, 0, time.UTC)
	decFallbk  = time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC)
)

func fedTestFacts(t *testing.T) facts.Set {
	t.Helper()
	asOf := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	f := func(id, v string) facts.Fact {
		return facts.Fact{ID: id, Value: v, Source: "https://www.federalreserve.gov/x", AsOf: asOf}
	}
	set, err := facts.New(time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC),
		f("fed_upper_bound", "4.00"), f("fed_next_meeting", "2026-10-28"), f("fed_rate_step", "0.25"),
		f("fed_meeting_dates", "2026-10-28,2026-12-09,2027-01-27"))
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func fedTestInstrument(id, metric string, cmp canonical.Comparator, threshold string, deadline time.Time) canonical.Instrument {
	dl := deadline
	th := dec(threshold)
	venue := canonical.VenueOf(id)
	fee := canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1}
	if venue == canonical.VenuePolymarket {
		fee = canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1}
	}
	return canonical.Instrument{
		ID: id, Venue: venue, VenueMarketID: id, Category: canonical.CategoryFed,
		Title: id, RulesText: "r", CloseTime: deadline, Status: canonical.StatusOpen,
		Terms: canonical.Terms{
			Subject: "fed_funds_rate", Metric: metric, Comparator: cmp, Threshold: &th, Unit: "percent",
			Deadline: &dl, DeadlineTZ: "America/New_York", ResolutionSource: "federalreserve_gov",
			EdgeRules: canonical.Unknown, Polarity: canonical.PolarityPositive,
		},
		FeeSchedule: fee,
	}
}

// decLevelVsChange is the December KXFED level contract against a Polymarket
// change market, compared by the real matcher.
func decLevelVsChange(t *testing.T) (a, b canonical.Instrument, rel canonical.Relationship) {
	t.Helper()
	a = fedTestInstrument("kalshi:KXFED-26DEC-T4.25", "rate_upper_bound", canonical.ComparatorGT, "4.25", decMeeting)
	b = fedTestInstrument("polymarket:fed-dec-up-50", "rate_change", canonical.ComparatorGTE, "0.5", decClose)
	rel = match.Compare(a, b, fedTestFacts(t))
	if rel.Type != canonical.RelRelated || !hasFact(rel.FactsUsed, "fed_upper_bound") {
		t.Fatalf("precondition: want related with fed_upper_bound consulted, got %s %v: %s", rel.Type, rel.FactsUsed, rel.Explanation)
	}
	return a, b, rel
}

// TestFactDidNotCloseFedLaterMeeting (M6 P4a): the December level-vs-change
// pair, where fed_upper_bound was consulted but applies only to October, is
// grouped under fact_did_not_close from its stored fields, and the row says
// why the fact did not apply.
func TestFactDidNotCloseFedLaterMeeting(t *testing.T) {
	_, _, rel := decLevelVsChange(t)
	rel.ID = 7
	rows := nearMissRows([]canonical.Relationship{rel}, map[string]instrumentView{})
	if rows[0].Group != factDidNotCloseCode {
		t.Fatalf("group = %q, want %s", rows[0].Group, factDidNotCloseCode)
	}
	if rows[0].VetoBadge.Text != "metric_mismatch" {
		t.Errorf("veto badge = %q, want metric_mismatch still shown", rows[0].VetoBadge.Text)
	}
	for _, want := range []string{"fed_upper_bound=4.00", "does not apply"} {
		if !strings.Contains(rows[0].FactNote, want) {
			t.Errorf("FactNote %q lacks %q", rows[0].FactNote, want)
		}
	}
	groups := groupNearMisses(rows, "", false)
	if len(groups) != 1 || groups[0].Code != factDidNotCloseCode || groups[0].Count != 1 {
		t.Fatalf("groups = %+v", groups)
	}
	// A pair with a veto and no fed_upper_bound consulted stays in its veto group.
	plain := canonical.Relationship{ID: 8, Type: canonical.RelRelated,
		Vetoes:    []canonical.Veto{{Code: "metric_mismatch", Field: "metric", Detail: "x vs y"}},
		FactsUsed: []canonical.FactUse{{Key: "fed_meeting_dates"}}}
	if factDidNotClose(plain) {
		t.Error("fed_meeting_dates alone (blocking) must not count as a fact that did not close")
	}
}

// TestNearMissPageFactDidNotCloseSection (M6 P4a): the page renders the
// #group-fact-did-not-close section with a heading and the December row, and
// the strip counts it as "fact did not close", not "stopped by a veto".
func TestNearMissPageFactDidNotCloseSection(t *testing.T) {
	s, st := newTestServer(t)
	sd := seedPublic(t, st)
	ctx := context.Background()
	a, b, rel := decLevelVsChange(t)
	for _, in := range []canonical.Instrument{a, b} {
		if err := st.UpsertInstrument(ctx, sd.round.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	rel.RoundID = sd.round.ID
	id, err := st.UpsertRelationship(ctx, rel)
	if err != nil {
		t.Fatal(err)
	}
	body := get(s.Handler(), "/near-misses").Body.String()
	sec := body[strings.Index(body, `id="group-fact-did-not-close"`):]
	wantContains(t, sec, "<h2>A fact was consulted but did not close the difference",
		`href="/pairs/`+strconv.FormatInt(id, 10)+`"`, "Fact consulted, did not close:", "does not apply",
		"n/a: different measures (rate_upper_bound vs rate_change)")
	wantContains(t, body, "<dt>Fact did not close:</dt><dd>1</dd>")
	// The filter link for the group works.
	wantHTML(t, get(s.Handler(), "/near-misses?veto=fact_did_not_close&all=1"), 200)
}

// TestNearMissEmptyFactGroupKeepsAnchor (M6 P4a): with near misses but none
// in the fact group, the anchor still exists with an empty-state line.
func TestNearMissEmptyFactGroupKeepsAnchor(t *testing.T) {
	rows := nearMissRows([]canonical.Relationship{{ID: 1, Type: canonical.RelRelated,
		Vetoes: []canonical.Veto{{Code: "subject_mismatch", Field: "subject", Detail: "BTC is not ETH"}}}}, map[string]instrumentView{})
	groups := groupNearMisses(rows, "", false)
	if len(groups) != 2 || groups[1].Code != factDidNotCloseCode || groups[1].Count != 0 {
		t.Fatalf("groups = %+v", groups)
	}
	if got := groupNearMisses(nil, "", false); len(got) != 0 {
		t.Errorf("no rows: groups = %+v, want none", got)
	}
}

// TestReadableExplanation (M6 P4b): "within" appears only when true; outside
// the window the explanation says why the pair was still compared.
func TestReadableExplanation(t *testing.T) {
	meeting := []canonical.FactUse{{Key: "fed_meeting_dates"}}
	cases := []struct {
		name, in string
		used     []canonical.FactUse
		want     string
		not      string
	}{
		{"inside", "related: same subject BTC with deadlines 30h0m0s apart (within 168h0m0s), but vetoed on x.", nil,
			"deadlines 1 day 6 h apart (within 7 days)", "window"},
		{"outside via Fed meeting", "related: same subject fed_funds_rate with deadlines 526h54m0s apart (within 168h0m0s), but vetoed on metric.", meeting,
			"deadlines 21 days 22 h apart (window 7 days; compared anyway because fact fed_meeting_dates puts both deadlines on the same FOMC meeting)", "within"},
		{"outside, no recorded reason", "deadlines 200h0m0s apart (within 168h0m0s)", nil,
			"the stored record does not say why", "within"},
		{"bare durations", "2026-10-28: 30h0m0s apart, not within 24h0m0s", nil, "1 day 6 h apart, not within 1 day", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readableExplanation(tc.in, tc.used)
			if !strings.Contains(got, tc.want) {
				t.Errorf("got %q, want it to contain %q", got, tc.want)
			}
			if tc.not != "" && strings.Contains(got, tc.not) {
				t.Errorf("got %q, must not contain %q", got, tc.not)
			}
			if strings.Contains(got, "h0m0s") {
				t.Errorf("got %q, still has a Go duration", got)
			}
		})
	}
}

// TestReadableExplanationFromMatcher (M6 P4b): a real metric_unknown Fed pair
// 21 days apart (Polymarket's December 31 fallback) reads truthfully.
func TestReadableExplanationFromMatcher(t *testing.T) {
	a := fedTestInstrument("kalshi:KXFED-26DEC-T4.25", "rate_upper_bound", canonical.ComparatorGT, "4.25", decMeeting)
	b := fedTestInstrument("polymarket:fed-dec-unknown", canonical.Unknown, canonical.ComparatorGT, "4.25", decFallbk)
	rel := match.Compare(a, b, fedTestFacts(t))
	if rel.Type != canonical.RelRelated || !strings.Contains(rel.Explanation, "(within 168h0m0s)") {
		t.Fatalf("precondition: %s %q", rel.Type, rel.Explanation)
	}
	got := readableExplanation(rel.Explanation, rel.FactsUsed)
	if strings.Contains(got, "(within") || !strings.Contains(got, "compared anyway because fact fed_meeting_dates") {
		t.Errorf("readable explanation = %q", got)
	}
}

// TestTermGapUnits (M6 P4c): "How close" carries the unit of the data.
func TestTermGapUnits(t *testing.T) {
	d := func(field, a, b string, st canonical.DiffStatus) canonical.FieldDiff {
		return canonical.FieldDiff{Field: field, A: a, B: b, Status: st}
	}
	t1 := time.Date(2026, 12, 9, 19, 0, 0, 0, time.UTC)
	t2 := t1.Add(526*time.Hour + 54*time.Minute)
	cases := []struct {
		name string
		diff []canonical.FieldDiff
		want string
	}{
		{"crypto USD", []canonical.FieldDiff{d("subject", "BTC", "BTC", canonical.DiffMatch), d("threshold", "120000", "119500", canonical.DiffDiffer), d("unit", "USD", "USD", canonical.DiffMatch)}, "500.00 USD"},
		{"fed pp and bps", []canonical.FieldDiff{d("subject", "fed_funds_rate", "fed_funds_rate", canonical.DiffMatch), d("threshold", "4.25", "4.50", canonical.DiffDiffer), d("unit", "percent", "percent", canonical.DiffMatch)}, "0.25 pp (25 bps)"},
		{"other percent", []canonical.FieldDiff{d("subject", "cpi", "cpi", canonical.DiffMatch), d("threshold", "3.1", "3.0", canonical.DiffDiffer), d("unit", "percent", "percent", canonical.DiffMatch)}, "0.1 pp"},
		{"bps", []canonical.FieldDiff{d("threshold", "25", "50", canonical.DiffDiffer), d("unit", "bps", "bps", canonical.DiffMatch)}, "25 bps"},
		{"other unit", []canonical.FieldDiff{d("threshold", "1", "3", canonical.DiffDiffer), d("unit", "outcome", "outcome", canonical.DiffMatch)}, "2 outcome"},
		{"units differ", []canonical.FieldDiff{d("threshold", "1", "3", canonical.DiffDiffer), d("unit", "USD", "percent", canonical.DiffDiffer)}, "2 (units differ: USD vs percent)"},
		{"unit unknown", []canonical.FieldDiff{d("threshold", "1", "3", canonical.DiffDiffer), d("unit", "unknown", "USD", canonical.DiffUnknown)}, "2 (unit unknown)"},
		{"different metrics", []canonical.FieldDiff{d("metric", "rate_upper_bound", "rate_change", canonical.DiffDiffer), d("threshold", "4.25", "0.5", canonical.DiffDiffer)}, "n/a: different measures (rate_upper_bound vs rate_change)"},
		{"deadline hours/days", []canonical.FieldDiff{d("threshold", "1", "1", canonical.DiffMatch), d("deadline", t1.Format(time.RFC3339), t2.Format(time.RFC3339), canonical.DiffDiffer)}, "deadlines 21 days 22 h apart"},
		{"bad deadline", []canonical.FieldDiff{d("deadline", "x", "y", canonical.DiffDiffer)}, "—"},
		{"non-numeric threshold falls through", []canonical.FieldDiff{d("threshold", "a", "b", canonical.DiffDiffer)}, "—"},
	}
	for _, tc := range cases {
		if got := termGap(tc.diff); got != tc.want {
			t.Errorf("%s: termGap = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestFactNote: the derivation note is cut out of a related explanation.
func TestFactNote(t *testing.T) {
	if got := factNote("related: x. Facts: fact a=4.25 does not apply."); got != "fact a=4.25 does not apply" {
		t.Errorf("got %q", got)
	}
	if got := factNote("related: x. Facts: r. Uncertain: y."); got != "r" {
		t.Errorf("got %q", got)
	}
	if got := factNote("no note"); got != "" {
		t.Errorf("got %q", got)
	}
}
