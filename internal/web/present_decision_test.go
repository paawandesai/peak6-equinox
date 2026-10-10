package web

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Golden plain answers for the three demo decisions (ui/plain-language).

func TestPresentDecision_Golden96(t *testing.T) {
	p := presentDecision(fxDecision96(), decisionContext{Instruments: fxInstruments()})
	want := "Buy on Polymarket. 100 contracts cost $0.80 there versus $1.07 on Kalshi, so you save $0.27. " +
		"We added a small safety charge (0.38¢ each) because the two contracts could settle differently in rare cases. " +
		"Polymarket is still cheaper."
	if p.Answer != want {
		t.Errorf("answer\n got: %s\nwant: %s", p.Answer, want)
	}
	if p.Badge != "Routed to Polymarket" || p.NextStep != nil {
		t.Errorf("badge %q next %v", p.Badge, p.NextStep)
	}
	if len(p.Compare) != 2 {
		t.Fatalf("compare rows = %d, want 2", len(p.Compare))
	}
	w, r := p.Compare[0], p.Compare[1]
	if !w.Winner || w.Where != "Polymarket" || w.Contract != "Fed raises 50+ bps in October" ||
		w.Price != "0.40¢" || w.Fee != "0.02¢" || w.Safety != "0.38¢" || w.Total != "0.80¢" || w.OrderTotal != "$0.80" || w.Verdict != "Best" {
		t.Errorf("winner row = %+v", w)
	}
	if r.Winner || r.Where != "Kalshi" || r.Contract != "Fed rate above 4.25% after Oct 28" || r.Safety != "none" ||
		r.Total != "1.07¢" || r.OrderTotal != "$1.07" || r.Verdict != "+0.27¢ more" {
		t.Errorf("runner-up row = %+v", r)
	}
	if len(p.WhyNot) != 1 || p.WhyNot[0].Summary != "4 Polymarket contracts look similar but pay on something different" || len(p.WhyNot[0].Items) != 4 {
		t.Errorf("why not = %+v", p.WhyNot)
	}
	if p.Bars == nil || len(p.Bars.Rows) != 2 || !p.Bars.Rows[0].Winner || len(p.Bars.Rows[0].Segs) != 3 {
		t.Errorf("bars = %+v", p.Bars)
	}
}

func TestPresentDecision_Golden97(t *testing.T) {
	p := presentDecision(fxDecision97(), decisionContext{Instruments: fxInstruments()})
	want := "Buy on Polymarket, using the opposite contract. Betting YES on 'Fed cuts 50+ bps in October' pays exactly when your Kalshi NO would. " +
		"It costs $0.49 for 100 versus $1.07 on Kalshi."
	if p.Answer != want {
		t.Errorf("answer\n got: %s\nwant: %s", p.Answer, want)
	}
	if !p.Compare[0].Opposite || p.Compare[1].Verdict != "+0.58¢ more" {
		t.Errorf("compare = %+v", p.Compare)
	}
	if got := p.WhyNot[0].Summary; got != "4 Polymarket contracts only cover some of your outcomes" {
		t.Errorf("why-not summary = %q", got)
	}
}

func TestPresentDecision_Golden99(t *testing.T) {
	p := presentDecision(fxDecision99(), fxContext99(t))
	want := "No trade. You asked to buy YES on 'Fed rate above 3.50% after Oct 28', which the market prices at about 99%. " +
		"Nobody on Kalshi is selling it. " +
		"The only true substitute would cost $1.0019 for a contract that pays at most $1, a guaranteed loss. " +
		"Four other Polymarket contracts only cover some of your outcomes, so they aren't substitutes."
	if p.Answer != want {
		t.Errorf("answer\n got: %s\nwant: %s", p.Answer, want)
	}
	if p.NextStep == nil {
		t.Fatal("no next step")
	}
	if got := p.NextStep.Text(); got != "Try the other side: buy NO on Kalshi at 1¢ (8,912 on offer)." {
		t.Errorf("next step = %q", got)
	}
	if !strings.HasPrefix(p.NextStep.Href, "/pairs/2390?") {
		t.Errorf("next step href = %q", p.NextStep.Href)
	}
	if p.Badge != "No trade" {
		t.Errorf("badge = %q", p.Badge)
	}
	// The sure-loss candidate was priced, so the table shows it, muted.
	if len(p.Compare) != 1 || !p.Compare[0].Excluded || p.Compare[0].Total != "100.19¢" ||
		p.Compare[0].Verdict != "would cost more than it can ever pay out" {
		t.Errorf("compare = %+v", p.Compare)
	}
	if p.Bars != nil {
		t.Errorf("bars drawn with nothing ranked: %+v", p.Bars)
	}
	wantGroups := []string{
		"Your Kalshi contract has nobody selling at any price",
		"1 Polymarket substitute would cost more than it can ever pay out ($1.0019 per $1)",
		"4 Polymarket contracts only cover some of your outcomes",
	}
	if len(p.WhyNot) != len(wantGroups) {
		t.Fatalf("why-not groups = %+v", p.WhyNot)
	}
	for i, w := range wantGroups {
		if p.WhyNot[i].Summary != w {
			t.Errorf("group %d = %q, want %q", i, p.WhyNot[i].Summary, w)
		}
	}
}

// No instrument ID or snake_case code may reach the plain layers.
func TestPresentDecision_NoCodesOrIDs(t *testing.T) {
	code := regexp.MustCompile(`\b[a-z]+_[a-z_]+\b|kalshi:|polymarket:|KXFED`)
	for name, p := range map[string]*plainDecision{
		"96": presentDecision(fxDecision96(), decisionContext{Instruments: fxInstruments()}),
		"97": presentDecision(fxDecision97(), decisionContext{Instruments: fxInstruments()}),
		"99": presentDecision(fxDecision99(), fxContext99(t)),
	} {
		texts := []string{p.Answer, p.Badge, p.OrderText, p.CompareSkip}
		if p.NextStep != nil {
			texts = append(texts, p.NextStep.Label, p.NextStep.Detail)
		}
		for _, r := range p.Compare {
			texts = append(texts, r.Where, r.Contract, r.Verdict)
		}
		for _, g := range p.WhyNot {
			texts = append(texts, g.Summary)
			for _, it := range g.Items {
				texts = append(texts, it.Name, it.Reason)
			}
		}
		for _, s := range texts {
			if code.MatchString(s) {
				t.Errorf("#%s: plain text carries a code or ID: %q", name, s)
			}
		}
	}
}

// Every sentence in the plain answers stays short (≤ 20 words, principle 7).
func TestPresentDecision_ShortSentences(t *testing.T) {
	for _, p := range []*plainDecision{
		presentDecision(fxDecision96(), decisionContext{Instruments: fxInstruments()}),
		presentDecision(fxDecision97(), decisionContext{Instruments: fxInstruments()}),
		presentDecision(fxDecision99(), fxContext99(t)),
	} {
		for _, s := range regexp.MustCompile(`[.!?] `).Split(p.Answer, -1) {
			if n := len(strings.Fields(s)); n > 22 {
				t.Errorf("sentence has %d words: %q", n, s)
			}
		}
	}
}

func excl(reason, detail string) canonical.Exclusion {
	return canonical.Exclusion{InstrumentID: fxPUp25, Venue: canonical.VenuePolymarket, Reason: reason, Detail: detail}
}

// Each router code translates at the point of use.
func TestReasonPhrase_Translations(t *testing.T) {
	buy := canonical.Decision{Order: fxOrder(canonical.OutcomeYes)}
	sell := buy
	sell.Order.Side = canonical.SideSell
	cases := []struct {
		d    canonical.Decision
		ex   canonical.Exclusion
		many bool
		want string
	}{
		{buy, excl(exStale, "fetched 312 s ago"), false, "has a price 5 minutes old, too old to trust"},
		{buy, excl(exStale, "fetched 40 s ago"), false, "has a price 40 seconds old, too old to trust"},
		{buy, excl(exStale, "fetched_at is missing or zero"), false, "has a price too old to trust"},
		{buy, excl(exSkew, "7 s behind"), false, "has prices taken 7 seconds apart from the others, too far to compare"},
		{buy, excl(exLowConfidence, "confidence 0.87 is below the 0.9 floor"), false, "might be the same bet, but we're not sure enough (0.87, need 0.90)"},
		{buy, excl(exInsufficientDepth, "0 contracts available on consumed side"), false, "has nobody selling at any price"},
		{sell, excl(exInsufficientDepth, "0 contracts available on consumed side"), true, "have nobody buying at any price"},
		{buy, excl(exInsufficientDepth, "40 contracts available on consumed side"), false, "has only 40 contracts on offer"},
		{buy, excl(exInsufficientDepth, "FOK requires 100 contracts; x can fill 40"), false, "can fill only 40 of the 100 contracts you need all at once"},
		{buy, excl(exNoBook, "no book available"), false, "has no prices at all right now"},
		{buy, excl(exSureLoss, "all-in cost 1.0019 per $1 payout is a guaranteed loss"), false, "would cost more than it can ever pay out"},
		{buy, excl(exSureLoss, ""), true, "would cost more than they can ever pay out"},
		{buy, excl(exLimitNotReachable, "40 of 100 contracts at or better than limit 0.5000"), false, "has only 40 of 100 contracts at your limit"},
		{buy, excl(exLimitNotReachable, "best ask is outside the 0.5000 limit"), false, "has nothing on offer at your limit price"},
		{buy, excl(exNotOpen, "closed"), false, "is not open for trading"},
		{buy, excl(exFeeUnknown, ""), false, "has a fee we don't know, so we can't price it"},
		{buy, excl(exBasisRiskUnknown, ""), true, "have a safety risk we can't size, so we skipped them"},
		{buy, excl(exComplementUnsupp, ""), false, "is an opposite contract, which wasn't supported when this was decided"},
		{buy, excl(exNoQuantity, ""), false, "couldn't be sized for this order"},
		{buy, excl(exQuoteMismatch, "quote is for x"), false, "is missing data we need, so we skipped it"},
		{buy, excl(exNotRoutable, "relationship type unrelated is never routed"), false, "pays on something unrelated, so it isn't a substitute"},
	}
	for _, c := range cases {
		if got := reasonPhrase(c.d, c.ex, c.many); got != c.want {
			t.Errorf("%s %q many=%v: got %q, want %q", c.ex.Reason, c.ex.Detail, c.many, got, c.want)
		}
	}
	if got := otherPredicate(canonical.RelImplies, excl(exNotRoutable, ""), false); got != "only covers some of your outcomes, so it isn't a substitute" {
		t.Errorf("implies = %q", got)
	}
	if got := otherPredicate(canonical.RelRelated, excl(exNotRoutable, ""), true); got != "look similar but pay on something different, so they aren't substitutes" {
		t.Errorf("related = %q", got)
	}
}

func TestTargetSentence(t *testing.T) {
	buy := canonical.Decision{Order: fxOrder(canonical.OutcomeYes)}
	sell := buy
	sell.Order.Side = canonical.SideSell
	k := func(reason, detail string) canonical.Exclusion {
		return canonical.Exclusion{InstrumentID: fxK350, Venue: canonical.VenueKalshi, Reason: reason, Detail: detail}
	}
	cases := []struct {
		d    canonical.Decision
		ex   canonical.Exclusion
		want string
	}{
		{buy, k(exInsufficientDepth, "0 contracts available on consumed side"), "Nobody on Kalshi is selling it."},
		{sell, k(exInsufficientDepth, "0 contracts available on consumed side"), "Nobody on Kalshi is buying it."},
		{buy, k(exInsufficientDepth, "12.5 contracts available on consumed side"), "Only 12 are on offer on Kalshi."},
		{buy, k(exInsufficientDepth, "FOK requires 100 contracts; x can fill 40"), "Only 40 of your 100 are on offer on Kalshi, and you asked for all or nothing."},
		{buy, k(exNoBook, "no book available"), "Kalshi has no prices for it right now."},
		{buy, k(exStale, "fetched 600 s ago"), "The Kalshi contract has a price 10 minutes old, too old to trust."},
	}
	for _, c := range cases {
		if got := targetSentence(c.d, c.ex); got != c.want {
			t.Errorf("%s: got %q, want %q", c.ex.Detail, got, c.want)
		}
	}
}

func TestRoutedAnswer_Variants(t *testing.T) {
	ins := fxInstruments()
	// Chosen on the target itself with no alternative and no safety charge.
	d := fxDecision96()
	d.Chosen = d.Alternatives[1].Legs
	d.Alternatives = []canonical.Alternative{{Rank: 1, Legs: d.Chosen, AllInPerContract: d.Chosen[0].AllInPerContract}}
	if got := routedAnswer(d, decisionContext{Instruments: ins}); got != "Buy on Kalshi. 100 contracts cost $1.07. No other venue could fill this order." {
		t.Errorf("lone route = %q", got)
	}
	// A sell compares proceeds.
	s := fxDecision96()
	s.Order.Side = canonical.SideSell
	s.Chosen[0].AllInPerContract = dp("0.0120")
	s.Chosen[0].BasisPenaltyPerContract = decimal.Zero
	s.Alternatives[1].AllInPerContract = dp("0.0100")
	if got := routedAnswer(s, decisionContext{Instruments: ins}); got != "Sell on Polymarket. 100 contracts bring in $1.20 there versus $1.00 on Kalshi, so you get $0.20 more." {
		t.Errorf("sell = %q", got)
	}
	// Partial and split fills.
	p := fxDecision96()
	p.Chosen[0].Quantity = dp("60")
	pd := presentDecision(p, decisionContext{Instruments: ins})
	if pd.Badge != "Partly filled on Polymarket" || !strings.HasPrefix(pd.Answer, "Partly filled. You can buy 60 of your 100 contracts on Polymarket") {
		t.Errorf("partial = %q / %q", pd.Badge, pd.Answer)
	}
	sp := fxDecision96()
	k := sp.Alternatives[1].Legs[0]
	k.Quantity = dp("40")
	sp.Chosen[0].Quantity = dp("60")
	sp.Chosen = append(sp.Chosen, k)
	spd := presentDecision(sp, decisionContext{Instruments: ins})
	if spd.Badge != "Split across 2 venues" || !strings.HasPrefix(spd.Answer, "Split across two venues: buy 60 on Polymarket and 40 on Kalshi.") {
		t.Errorf("split = %q / %q", spd.Badge, spd.Answer)
	}
}

func TestPlainOrder(t *testing.T) {
	o := fxOrder(canonical.OutcomeNo)
	if got := plainOrder(o); got != "buy 100 NO at market, fill what you can" {
		t.Errorf("market = %q", got)
	}
	o.Type, o.LimitPrice, o.TIF = canonical.OrderLimit, dpp("0.45"), canonical.TIFFOK
	if got := plainOrder(o); got != "buy 100 NO with a limit of 45¢, all or nothing" {
		t.Errorf("limit = %q", got)
	}
	o.Quantity, o.Notional = nil, dpp("50")
	if got := plainOrder(o); got != "buy $50.00 of NO with a limit of 45¢, all or nothing" {
		t.Errorf("notional = %q", got)
	}
}

func TestMarketPercent(t *testing.T) {
	q := fxQuote99()
	if p, ok := marketPercent(&q, canonical.OutcomeYes); !ok || percentText(p) != "99%" {
		t.Errorf("yes = %v %v", p, ok)
	}
	if p, ok := marketPercent(&q, canonical.OutcomeNo); !ok || percentText(p) != "1%" {
		t.Errorf("no = %v %v", p, ok)
	}
	q.YesAsks = []canonical.Level{{Price: dp("0.995"), Size: dp("1")}}
	if p, _ := marketPercent(&q, canonical.OutcomeYes); percentText(p) != "over 99%" {
		t.Errorf("mid = %s", percentText(p))
	}
	if _, ok := marketPercent(&canonical.Quote{}, canonical.OutcomeYes); ok {
		t.Error("empty book priced")
	}
	if _, ok := marketPercent(nil, canonical.OutcomeYes); ok {
		t.Error("nil quote priced")
	}
}

func TestNextStepFor(t *testing.T) {
	d := fxDecision99()
	if nextStepFor(d, nil) != nil {
		t.Error("next step without a suggestion")
	}
	s := &routeSuggestion{Venue: canonical.VenuePolymarket, Outcome: canonical.OutcomeNo, Price: dp("0.4"), Size: dp("1234.9"), Complement: true}
	n := nextStepFor(d, s)
	if got := n.Text(); got != "Try the other side: buy NO through the opposite Polymarket contract at 40¢ (1,234 on offer)." {
		t.Errorf("complement next step = %q", got)
	}
	if (nextStep{Label: "x"}).Text() != "x." {
		t.Error("label-only next step")
	}
}
