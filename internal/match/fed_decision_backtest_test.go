package match_test

import (
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
)

// Recorded text of the settled September 2026 Fed decision markets
// (testdata/fixtures/kalshi/markets_series_ticker-KXFEDDECISION_status-settled_*
// and the Polymarket "Fed Decision in September?" event). The Kalshi
// rules_secondary states a hypothetical ("Therefore, if the Federal Reserve
// hikes by 50bps, the 50bps market will resolve to Yes") that CI #45765 read
// as every bucket's condition, so H25 "implied" the 50+ bps hike bucket and
// the back-test disagreed (H25 settled YES, 50+ settled NO).
const (
	kalshiSepSecondary = "This market is mutually exclusive. Therefore, if the Federal Reserve hikes by 50bps, the 50bps market will " +
		"resolve to Yes and the 25bps market will resolve to No. Only one bucket, at maximum, can resolve to Yes. Note 4/28/25: For " +
		"the markets beginning after the May meeting, if a scheduled FOMC meeting is canceled and does not occur on its scheduled " +
		"date, then the strike for \"Fed maintains rate\" will resolve to Yes and all others will resolve to No."
	polySepRules = "The FED interest rates are defined in this market by the upper bound of the target federal funds range. The " +
		"decisions on the target federal funds range are made by the Federal Open Market Committee (FOMC) meetings.\n\n" +
		"This market will resolve to the amount of basis points the upper bound of the target federal funds rate is changed by " +
		"versus the level it was prior to the Federal Reserve's September 2026 meeting.\n\n" +
		"If the target federal funds rate is changed to a level not expressed in the displayed options, the change will be rounded " +
		"up to the nearest 25 and will resolve to the relevant bracket. (e.g. if there's a cut/increase of 12.5 bps it will be " +
		"considered to be 25 bps)\n\n" +
		"The resolution source for this market is the FOMC’s statement after its meeting scheduled for September 15-16, 2026 " +
		"according to the official calendar: https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm.\n\n" +
		"This market may resolve as soon as the FOMC’s statement for their September meeting with relevant data is issued. If no " +
		"statement is released by the end date of the next scheduled meeting, this market will resolve to the \"No change\" bracket."
)

var (
	sepKalshiClose = time.Date(2026, 9, 16, 17, 59, 0, 0, time.UTC)
	sepPolyClose   = time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
)

func sepKalshi(t *testing.T, ticker, title, primary string) canonical.Instrument {
	t.Helper()
	return instrumentOf(t, pairSide{Venue: canonical.VenueKalshi, Key: ticker, Category: canonical.CategoryFed, Close: sepKalshiClose,
		Title: title, Rules: primary + "\n\n" + kalshiSepSecondary})
}

func sepPoly(t *testing.T, key, title string) canonical.Instrument {
	t.Helper()
	return instrumentOf(t, pairSide{Venue: canonical.VenuePolymarket, Key: key, Category: canonical.CategoryFed, Close: sepPolyClose,
		Title: title, Rules: polySepRules})
}

// sepFacts are the back-test facts (testdata/fixtures/backtest/facts.yaml),
// optionally with fed_meeting_dates so the D-109 same-metric rule applies.
func sepFacts(t *testing.T, withMeetings bool) facts.Set {
	t.Helper()
	asOf := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	src := "https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm"
	list := []facts.Fact{
		{ID: match.FactFedUpperBound, Value: "3.75", Source: src, AsOf: asOf},
		{ID: match.FactFedNextMeeting, Value: "2026-09-16", Source: src, AsOf: asOf},
		{ID: match.FactFedRateStep, Value: "0.25", Source: src, AsOf: asOf},
	}
	if withMeetings {
		list = append(list, facts.Fact{ID: "fed_meeting_dates", Value: "2026-09-16,2026-10-28,2026-12-09", Source: src, AsOf: asOf})
	}
	fs, err := facts.New(asOf, list...)
	if err != nil {
		t.Fatal(err)
	}
	return fs
}

// TestFedDecisionSepBacktestPairs pins the settled September pairs: an exact
// decision bucket never implies a different bucket, and the pairs the
// back-test now reports are the true relations.
func TestFedDecisionSepBacktestPairs(t *testing.T) {
	h25 := sepKalshi(t, "KXFEDDECISION-26SEP-H25", "Will the Federal Reserve Hike rates by 25bps at their September 2026 meeting? — Hike 25bps",
		"If the Federal Reserve does a Hike of 25bps on September 16, 2026, then the market resolves to Yes.")
	h26 := sepKalshi(t, "KXFEDDECISION-26SEP-H26", "Will the Federal Reserve Hike rates by >25bps at their September 2026 meeting? — Hike >25bps",
		"If the Federal Reserve does a Hike of   25bps on September 16, 2026, then the market resolves to Yes.")
	c26 := sepKalshi(t, "KXFEDDECISION-26SEP-C26", "Will the Federal Reserve Cut rates by >25bps at their September 2026 meeting? — Cut >25bps",
		"If the Federal Reserve does a Cut of   25bps on September 16, 2026, then the market resolves to Yes.")
	inc50 := sepPoly(t, "88912926533493988427719291698947688154042720958310632316541141466409683822293",
		"Will the Fed increase interest rates by 50+ bps after the September 2026 meeting?")
	inc25 := sepPoly(t, "63842529068710005716169325380315470359047749786610778647370693404952498013178",
		"Will the Fed increase interest rates by 25 bps after the September 2026 meeting?")
	dec50 := sepPoly(t, "97186030785608128217926542396950266594898339988989015155120280107165449433603",
		"Will the Fed decrease interest rates by 50+ bps after the September 2026 meeting?")

	cases := []struct {
		name         string
		a, b         canonical.Instrument
		withMeetings bool
		want         canonical.RelationshipType
	}{
		// The CI #45765 pair: disjoint buckets, at most related.
		{"H25 vs 50+ hike", h25, inc50, false, canonical.RelRelated},
		{"H25 vs 50+ hike with meeting dates", h25, inc50, true, canonical.RelRelated},
		{"H25 vs 25 hike", h25, inc25, false, canonical.RelEquivalent},
		{"H25 vs 25 hike with meeting dates", h25, inc25, true, canonical.RelEquivalentGivenFacts},
		// Without the grid rule only containment holds: 50+ forces >25.
		{"H26 vs 50+ hike", h26, inc50, false, canonical.RelImplies},
		{"H26 vs 50+ hike with meeting dates", h26, inc50, true, canonical.RelEquivalentGivenFacts},
		{"C26 vs 50+ cut", c26, dec50, false, canonical.RelImplies},
		{"C26 vs 50+ hike", c26, inc50, true, canonical.RelRelated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rel := match.Compare(tc.a, tc.b, sepFacts(t, tc.withMeetings))
			if rel.Type != tc.want {
				t.Errorf("type = %s, want %s\n  a=%+v\n  b=%+v\n  %s", rel.Type, tc.want, tc.a.Terms, tc.b.Terms, rel.Explanation)
			}
			if rel.Type == canonical.RelImplies && rel.Explanation == "" {
				t.Error("implies without explanation")
			}
		})
	}
}
