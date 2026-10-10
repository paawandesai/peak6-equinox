package web

import (
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Golden meanings for the two demo pairs (ui/plain-language), rebuilt from
// /pairs/2406 and /pairs/2390 in round 904.

func fxFact(key, value, src, asOf string) canonical.FactUse {
	t, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		panic(err)
	}
	return canonical.FactUse{Key: key, Value: value, Source: src, AsOf: t}
}

var (
	fxFactUpper   = fxFact("fed_upper_bound", "4.00", "https://www.federalreserve.gov/newsevents/pressreleases/monetary20260916a.htm", "2026-09-16")
	fxFactMeeting = fxFact("fed_next_meeting", "2026-10-28", "https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm", "2026-10-06")
	fxFactStep    = fxFact("fed_rate_step", "0.25", "https://www.federalreserve.gov/monetarypolicy/openmarket.htm", "2026-10-06")
)

func fxFedDiff(threshA, threshB, compA, compB string) []canonical.FieldDiff {
	return []canonical.FieldDiff{
		{Field: "subject", A: "fed_funds_rate", B: "fed_funds_rate", Status: canonical.DiffMatch},
		{Field: "metric", A: "rate_upper_bound", B: "rate_change", Status: canonical.DiffDiffer},
		{Field: "threshold", A: threshA, B: threshB, Status: canonical.DiffDiffer},
		{Field: "comparator", A: compA, B: compB, Status: canonical.DiffDiffer},
		{Field: "deadline", A: "2026-10-28T18:05:00Z", B: "2026-10-29T03:59:00Z", Status: canonical.DiffDiffer},
		{Field: "resolution_source", A: "federalreserve_gov", B: "federalreserve_gov", Status: canonical.DiffMatch},
		{Field: "edge_rules", A: "unknown", B: "unknown", Status: canonical.DiffUnknown},
	}
}

func fxPair2406() pairInput {
	ins := fxInstruments()
	return pairInput{
		Rel: canonical.Relationship{ID: 2406, RoundID: 904, AID: fxK425, BID: fxPUp50, Type: canonical.RelEquivalentGivenFacts,
			Confidence: dp("0.93"), Method: canonical.MethodRules, Diff: fxFedDiff("4.25", "0.5", "gt", "gte"),
			FactsUsed: []canonical.FactUse{fxFactUpper, fxFactMeeting, fxFactStep},
			BasisRisk: &canonical.BasisRisk{Score: dp("0.38")}},
		A: ins[fxK425], B: ins[fxPUp50], MinConfidence: dp("0.9"),
	}
}

func fxPair2390() pairInput {
	ins := fxInstruments()
	return pairInput{
		Rel: canonical.Relationship{ID: 2390, RoundID: 904, AID: fxK350, BID: fxPCut50, Type: canonical.RelComplement,
			Confidence: dp("0.91"), Method: canonical.MethodRules, Diff: fxFedDiff("3.5", "-0.5", "gt", "lte"),
			FactsUsed: []canonical.FactUse{fxFactUpper, fxFactMeeting},
			BasisRisk: &canonical.BasisRisk{Score: dp("0.28")}},
		A: ins[fxK350], B: ins[fxPCut50], MinConfidence: dp("0.9"),
	}
}

func TestPresentPair_Golden2406(t *testing.T) {
	p := presentPair(fxPair2406())
	want := "These pay out on the same outcome: the Fed raising rates to 4.50% or more at the October meeting. " +
		"That holds as long as the current rate is 4.00% and the Fed moves in quarter-point steps."
	if p.Meaning != want {
		t.Errorf("meaning\n got: %s\nwant: %s", p.Meaning, want)
	}
	if p.Title != "Fed above 4.25% ↔ Fed +50 bps" {
		t.Errorf("title = %q", p.Title)
	}
	if p.Badge != "Same bet, given 3 facts" {
		t.Errorf("badge = %q", p.Badge)
	}
	if p.A.Name != "Fed rate above 4.25% after Oct 28" || p.B.Name != "Fed raises 50+ bps in October" {
		t.Errorf("names = %q / %q", p.A.Name, p.B.Name)
	}
	wantChips := []string{"Confidence 0.93 (high)", "Safety risk 0.38 (moderate)", "Routable"}
	for i, c := range p.Chips {
		if c.Text != wantChips[i] || c.Caption == "" {
			t.Errorf("chip %d = %+v", i, c)
		}
	}
	wantWhy := []string{
		"Both settle on the Fed's own announcement.",
		"Kalshi asks for a rate level, Polymarket for a change; the current rate (4.00%, from the Fed's statement on Sep 16) converts one into the other.",
		"Kalshi's 'Fed above 4.25%' and Polymarket's 'Fed +50 bps' both mean 4.50% or more, because the Fed moves in quarter-point steps.",
		"Unknown: how each venue handles edge cases, such as a moved meeting or a late statement; this is part of the safety risk.",
	}
	if strings.Join(p.Why, "\n") != strings.Join(wantWhy, "\n") {
		t.Errorf("why\n got: %q\nwant: %q", p.Why, wantWhy)
	}
	if p.WhyTitle != "Why we think they're the same bet" {
		t.Errorf("why title = %q", p.WhyTitle)
	}
	if len(p.Presets) != 2 || p.Presets[0].Label != "Buy 100 YES on Kalshi" || p.Presets[1].Label != "Buy 100 NO on Kalshi" ||
		!strings.Contains(p.Presets[1].Href, "outcome=no") || !strings.HasSuffix(p.Presets[1].Href, "#route") {
		t.Errorf("presets = %+v", p.Presets)
	}
}

func TestPresentPair_Golden2390(t *testing.T) {
	p := presentPair(fxPair2390())
	want := "These are opposites: one pays if the rate stays above 3.50%, the other if the Fed cuts by half a point or more. " +
		"Buying YES on one is the same as buying NO on the other."
	if p.Meaning != want {
		t.Errorf("meaning\n got: %s\nwant: %s", p.Meaning, want)
	}
	if p.Badge != "Opposite contracts" || p.WhyTitle != "Why we think they're opposites" {
		t.Errorf("badge %q why %q", p.Badge, p.WhyTitle)
	}
	if len(p.Why) != 4 || !strings.Contains(p.Why[2], "means above 3.50%") || !strings.Contains(p.Why[2], "means 3.50% or less") {
		t.Errorf("why = %q", p.Why)
	}
}

func TestPresentPair_OtherTypes(t *testing.T) {
	in := fxPair2390()
	in.Rel.Type = canonical.RelRelated
	in.Rel.BasisRisk = nil
	in.Rel.Vetoes = []canonical.Veto{{Code: "threshold_mismatch", Field: "threshold"}, {Code: "odd_code", Field: "edge_rules"}}
	p := presentPair(in)
	if !strings.HasPrefix(p.Meaning, "These look alike but pay on different things: one pays if the rate stays above 3.50%") {
		t.Errorf("related meaning = %q", p.Meaning)
	}
	if p.Chips[1].Text != "Safety risk: not scored" || p.Chips[2].Text != "Not routable" || p.Presets != nil {
		t.Errorf("chips %+v presets %+v", p.Chips, p.Presets)
	}
	if p.Why[0] != "They use different strike levels." || p.Why[1] != "A term differs (edge rules)." {
		t.Errorf("why = %q", p.Why)
	}
	in.Rel.FactsUsed = []canonical.FactUse{fxFactUpper}
	in.Rel.Explanation = "related: fed_upper_bound does not apply to a later meeting"
	if p := presentPair(in); !strings.HasPrefix(p.Why[0], "The Fed's current rate only converts a change at the next meeting") {
		t.Errorf("fact-did-not-apply bullet = %q", p.Why)
	}
	in.Rel.Type = canonical.RelImplies
	if p := presentPair(in); !strings.HasPrefix(p.Meaning, "These overlap but aren't the same bet") || p.Badge != "One-way: not a substitute" {
		t.Errorf("implies = %q %q", p.Meaning, p.Badge)
	}
	in.Rel.Type = canonical.RelUnrelated
	if p := presentPair(in); p.Badge != "Unrelated" {
		t.Errorf("unrelated badge = %q", p.Badge)
	}
	// An equivalent pair with no Fed terms falls back to the names.
	eq := pairInput{Rel: canonical.Relationship{Type: canonical.RelEquivalent, Confidence: dp("0.97")},
		A: canonical.Instrument{ID: "kalshi:X", Venue: canonical.VenueKalshi, Title: "Will it rain in NYC on Oct 31?"},
		B: canonical.Instrument{ID: "polymarket:1", Venue: canonical.VenuePolymarket, Title: "Rain in NYC on Oct 31?"}}
	if p := presentPair(eq); p.Meaning != "These pay out on the same outcome, so a YES on one is a YES on the other." || p.Specific ||
		p.A.Name != "Will it rain in NYC on Oct 31?" || p.Title != "Pair #0" {
		t.Errorf("fallback = %q specific=%v name=%q title=%q", p.Meaning, p.Specific, p.A.Name, p.Title)
	}
	eq.Rel.Type = canonical.RelComplement
	if p := presentPair(eq); !strings.HasPrefix(p.Meaning, "These are opposites: one pays exactly when the other doesn't.") {
		t.Errorf("complement fallback = %q", p.Meaning)
	}
}

func TestPairSidePrice(t *testing.T) {
	q := fxQuote99()
	s := pairSideOf(canonical.Instrument{ID: fxK350, Venue: canonical.VenueKalshi}, contractName{Long: "x"}, &q, "27 s")
	if s.Price != "No YES offers; NO costs 1¢" || s.Age != "price 27 s old" {
		t.Errorf("side = %+v", s)
	}
	q.YesAsks = []canonical.Level{{Price: dp("0.004"), Size: dp("10")}}
	if s := pairSideOf(canonical.Instrument{Venue: canonical.VenueKalshi}, contractName{}, &q, ""); s.Price != "YES costs 0.40¢" {
		t.Errorf("ask price = %q", s.Price)
	}
	if s := pairSideOf(canonical.Instrument{Venue: canonical.VenueKalshi}, contractName{}, nil, ""); s.Price != "No price collected yet" {
		t.Errorf("no quote = %q", s.Price)
	}
}

func TestFedWords(t *testing.T) {
	for in, want := range map[string]string{"0.25": "a quarter point", "-0.5": "half a point", "0.75": "three quarters of a point", "1": "a full point", "1.5": "150 bps"} {
		if got := amountWords(dp(in)); got != want {
			t.Errorf("amountWords(%s) = %q", in, got)
		}
	}
	for in, want := range map[string]string{"0.25": "quarter-point steps", "0.5": "half-point steps", "0.1": "10-basis-point steps"} {
		if got := stepWords(dp(in)); got != want {
			t.Errorf("stepWords(%s) = %q", in, got)
		}
	}
	cur := dp("4.00")
	for _, c := range []struct {
		s    levelSet
		want string
	}{
		{levelSet{canonical.ComparatorGTE, dp("3.75")}, "the rate staying at 3.75% or higher"},
		{levelSet{canonical.ComparatorGTE, dp("4.00")}, "the Fed holding or raising rates"},
		{levelSet{canonical.ComparatorLTE, dp("3.50")}, "the Fed cutting rates to 3.50% or less"},
		{levelSet{canonical.ComparatorLTE, dp("4.25")}, "the rate staying at 4.25% or lower"},
		{levelSet{canonical.ComparatorEQ, dp("4.00")}, "the Fed holding rates at 4.00%"},
		{levelSet{canonical.ComparatorEQ, dp("4.25")}, "the Fed raising rates to exactly 4.25%"},
		{levelSet{canonical.ComparatorGT, dp("3.50")}, "the rate staying above 3.50%"},
		{levelSet{canonical.ComparatorLT, dp("3.50")}, "the Fed cutting rates below 3.50%"},
	} {
		if got := setPhrase(c.s, cur); got != c.want {
			t.Errorf("setPhrase(%v) = %q, want %q", c.s, got, c.want)
		}
	}
	f := pairFacts{cur: &cur}
	for _, c := range []struct {
		metric string
		comp   canonical.Comparator
		th     string
		want   string
	}{
		{termRateChange, canonical.ComparatorEQ, "0", "the Fed holds rates"},
		{termRateChange, canonical.ComparatorEQ, "-0.25", "the Fed cuts by exactly a quarter point"},
		{termRateChange, canonical.ComparatorGT, "0.25", "the Fed raises by more than a quarter point"},
		{termRateLevel, canonical.ComparatorGT, "4.25", "the rate rises above 4.25%"},
		{termRateLevel, canonical.ComparatorLT, "3.75", "the rate falls below 3.75%"},
		{termRateLevel, canonical.ComparatorLTE, "4.25", "the rate stays at 4.25% or lower"},
		{termRateLevel, canonical.ComparatorEQ, "4.00", "the rate is exactly 4.00%"},
	} {
		got, ok := fedClause(fedTerms(c.metric, c.comp, c.th, "2026-10-28T18:05:00Z", ""), f)
		if !ok || got != c.want {
			t.Errorf("fedClause(%s %s %s) = %q %v, want %q", c.metric, c.comp, c.th, got, ok, c.want)
		}
	}
	if got, _ := fedClause(fedTerms(termRateLevel, canonical.ComparatorGT, "4.25", "2026-10-28T18:05:00Z", ""), pairFacts{}); got != "the rate ends above 4.25%" {
		t.Errorf("no current rate = %q", got)
	}
}

func TestDeadlineGap(t *testing.T) {
	for _, c := range []struct{ a, b, want string }{
		{"2026-10-28T18:05:00Z", "2026-10-29T03:59:00Z", "10 hours"},
		{"2026-10-28T18:05:00Z", "2026-10-28T18:35:00Z", "30 minutes"},
		{"2026-10-28T18:05:00Z", "2026-10-25T18:05:00Z", "3 days"},
	} {
		if got, ok := deadlineGap(canonical.FieldDiff{A: c.a, B: c.b}); !ok || got != c.want {
			t.Errorf("gap(%s,%s) = %q", c.a, c.b, got)
		}
	}
	if _, ok := deadlineGap(canonical.FieldDiff{A: "unknown", B: "x"}); ok {
		t.Error("parsed an unknown deadline")
	}
}
