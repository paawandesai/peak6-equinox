package web

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Fixtures for the presenter golden tests: the Fed instruments and decisions
// #96, #97 and #99 as stored on the live site in round 904 (2026-10-10),
// rebuilt field by field from their permalinks.

const (
	fxK425   = "kalshi:KXFED-26OCT-T4.25"
	fxK350   = "kalshi:KXFED-26OCT-T3.50"
	fxPHold  = "polymarket:111061902544814266207267295505639408607400625795891618462682726460921782993748"
	fxPCut50 = "polymarket:17010377994663817312158123655937348199252960045746746128731746451645055725586"
	fxPUp50  = "polymarket:18246296022868501258060085368606280205967114341141276439803424448463050819498"
	fxPCut25 = "polymarket:33268510350568915228897273528353467773422190590579607471427765079749830445947"
	fxPUp25  = "polymarket:55159722761418013044126414276680602270318000841690689684819994448621694923050"
)

func dp(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func dpp(s string) *decimal.Decimal {
	d := dp(s)
	return &d
}

func tp(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func fedTerms(metric string, c canonical.Comparator, th string, deadline, tz string) canonical.Terms {
	t := canonical.UnknownTerms()
	t.Subject, t.Metric, t.Comparator, t.Threshold = termFedFunds, metric, c, dpp(th)
	t.Unit, t.Deadline, t.Polarity, t.ResolutionSource = termPercent, tp(deadline), canonical.PolarityPositive, "federalreserve_gov"
	if tz != "" {
		t.DeadlineTZ = tz
	}
	return t
}

func fxInstruments() map[string]canonical.Instrument {
	const kd, pd = "2026-10-28T18:05:00Z", "2026-10-29T03:59:00Z"
	k := func(id, th, title string) canonical.Instrument {
		return canonical.Instrument{ID: id, Venue: canonical.VenueKalshi, Title: title,
			Terms: fedTerms(termRateLevel, canonical.ComparatorGT, th, kd, "America/New_York")}
	}
	p := func(id string, c canonical.Comparator, th, title string) canonical.Instrument {
		return canonical.Instrument{ID: id, Venue: canonical.VenuePolymarket, Title: title,
			Terms: fedTerms(termRateChange, c, th, pd, "")}
	}
	m := map[string]canonical.Instrument{}
	for _, in := range []canonical.Instrument{
		k(fxK425, "4.25", "Will the upper bound of the federal funds rate be above 4.25% following the Fed's Oct 28, 2026 meeting? — Above 4.25%"),
		k(fxK350, "3.50", "Will the upper bound of the federal funds rate be above 3.50% following the Fed's Oct 28, 2026 meeting? — Above 3.50%"),
		p(fxPHold, canonical.ComparatorEQ, "0", "Will there be no change in Fed interest rates after the October 2026 meeting?"),
		p(fxPCut50, canonical.ComparatorLTE, "-0.50", "Will the Fed decrease interest rates by 50+ bps after the October 2026 meeting?"),
		p(fxPUp50, canonical.ComparatorGTE, "0.50", "Will the Fed increase interest rates by 50+ bps after the October 2026 meeting?"),
		p(fxPCut25, canonical.ComparatorEQ, "-0.25", "Will the Fed decrease interest rates by 25 bps after the October 2026 meeting?"),
		p(fxPUp25, canonical.ComparatorEQ, "0.25", "Will the Fed increase interest rates by 25 bps after the October 2026 meeting?"),
	} {
		m[in.ID] = in
	}
	return m
}

func fxInput(id string, rt canonical.RelationshipType, ask string) canonical.DecisionInput {
	return canonical.DecisionInput{InstrumentID: id, Venue: canonical.VenueOf(id), RelationshipType: rt,
		BestAsk: dp(ask), FetchedAt: *tp("2026-10-10T18:10:27Z"), RoundID: 904}
}

func notRoutable(id, rt string) canonical.Exclusion {
	return canonical.Exclusion{InstrumentID: id, Venue: canonical.VenuePolymarket, Reason: exNotRoutable,
		Detail: "relationship type " + rt + " is never routed"}
}

func fxOrder(outcome canonical.Outcome) canonical.Order {
	q := dp("100")
	return canonical.Order{Side: canonical.SideBuy, Outcome: outcome, Quantity: &q, Type: canonical.OrderMarket, TIF: canonical.TIFIOC}
}

// fxDecision96: buy 100 YES on Kalshi above 4.25%, routed to the Polymarket
// "+50 bps" contract (equivalent given facts) with a safety charge.
func fxDecision96() canonical.Decision {
	poly := canonical.Leg{InstrumentID: fxPUp50, Venue: canonical.VenuePolymarket, Side: canonical.SideBuy, Outcome: canonical.OutcomeYes,
		Quantity: dp("100"), VWAP: dp("0.0040"), FeePerContract: dp("0.000199"), BasisPenaltyPerContract: dp("0.00383"),
		AllInPerContract: dp("0.008029"), Route: "direct"}
	kal := canonical.Leg{InstrumentID: fxK425, Venue: canonical.VenueKalshi, Side: canonical.SideBuy, Outcome: canonical.OutcomeYes,
		Quantity: dp("100"), VWAP: dp("0.0100"), FeePerContract: dp("0.0007"), AllInPerContract: dp("0.0107"), Route: "direct"}
	return canonical.Decision{
		RoundID: 904, TargetID: fxK425, TargetType: canonical.RelEquivalentGivenFacts, Order: fxOrder(canonical.OutcomeYes),
		Chosen: []canonical.Leg{poly},
		Alternatives: []canonical.Alternative{
			{Rank: 1, Legs: []canonical.Leg{poly}, AllInPerContract: poly.AllInPerContract, Note: "chosen"},
			{Rank: 2, Legs: []canonical.Leg{kal}, AllInPerContract: kal.AllInPerContract},
		},
		Exclusions: []canonical.Exclusion{
			notRoutable(fxPHold, "related"), notRoutable(fxPCut50, "related"),
			notRoutable(fxPCut25, "related"), notRoutable(fxPUp25, "related"),
		},
		Warnings:    []string{},
		Explanation: "Simulated buy of 100 YES contracts on kalshi:KXFED-26OCT-T4.25 as a market IOC order. Best route: polymarket (" + fxPUp50 + ") fills all 100 contracts at a VWAP of 0.0040 plus a fee of 0.0002 and basis penalty of 0.0038 per contract, an all-in cost of 0.0080 per $1 payout Total cost: 0.8029.",
		DecidedAt:   *tp("2026-10-10T18:12:54Z"),
		Inputs: []canonical.DecisionInput{
			fxInput(fxK425, "self", "0.0100"), fxInput(fxPHold, canonical.RelRelated, "0.8400"),
			fxInput(fxPCut50, canonical.RelRelated, "0.0020"), fxInput(fxPUp50, canonical.RelEquivalentGivenFacts, "0.0040"),
			fxInput(fxPCut25, canonical.RelRelated, "0.0050"), fxInput(fxPUp25, canonical.RelRelated, "0.1600"),
		},
	}
}

// fxDecision97: buy 100 NO on Kalshi above 3.50%, routed as YES on the
// Polymarket "cut 50+ bps" contract, its complement.
func fxDecision97() canonical.Decision {
	poly := canonical.Leg{InstrumentID: fxPCut50, Venue: canonical.VenuePolymarket, Side: canonical.SideBuy, Outcome: canonical.OutcomeYes,
		Quantity: dp("100"), VWAP: dp("0.0020"), FeePerContract: dp("0.0001"), BasisPenaltyPerContract: dp("0.00283"),
		AllInPerContract: dp("0.00493"), Route: "complement"}
	kal := canonical.Leg{InstrumentID: fxK350, Venue: canonical.VenueKalshi, Side: canonical.SideBuy, Outcome: canonical.OutcomeNo,
		Quantity: dp("100"), VWAP: dp("0.0100"), FeePerContract: dp("0.0007"), AllInPerContract: dp("0.0107"), Route: "direct"}
	return canonical.Decision{
		RoundID: 904, TargetID: fxK350, TargetType: canonical.RelComplement, Order: fxOrder(canonical.OutcomeNo),
		Chosen: []canonical.Leg{poly},
		Alternatives: []canonical.Alternative{
			{Rank: 1, Legs: []canonical.Leg{poly}, AllInPerContract: poly.AllInPerContract, Note: "chosen"},
			{Rank: 2, Legs: []canonical.Leg{kal}, AllInPerContract: kal.AllInPerContract},
		},
		Exclusions: []canonical.Exclusion{
			notRoutable(fxPHold, "implies"), notRoutable(fxPUp50, "implies"),
			notRoutable(fxPCut25, "implies"), notRoutable(fxPUp25, "implies"),
		},
		Warnings:    []string{},
		Explanation: "Simulated buy of 100 NO contracts on kalshi:KXFED-26OCT-T3.50 as a market IOC order.",
		DecidedAt:   *tp("2026-10-10T18:12:55Z"),
		Inputs: []canonical.DecisionInput{
			fxInput(fxK350, "self", "0.0100"), fxInput(fxPHold, canonical.RelImplies, "0.1700"),
			fxInput(fxPCut50, canonical.RelComplement, "0.0020"), fxInput(fxPUp50, canonical.RelImplies, "0.9970"),
			fxInput(fxPCut25, canonical.RelImplies, "0.9960"), fxInput(fxPUp25, canonical.RelImplies, "0.8500"),
		},
	}
}

// fxDecision99Explanation is #99's stored explanation, verbatim.
const fxDecision99Explanation = "Simulated buy of 100 YES contracts on kalshi:KXFED-26OCT-T3.50 as a market IOC order. No venue can fill this order: every candidate was excluded. Excluded: kalshi:KXFED-26OCT-T3.50 (insufficient_depth: 0 contracts available on consumed side); " +
	fxPHold + " (not_routable: relationship type implies is never routed); " + fxPUp50 + " (not_routable: relationship type implies is never routed); " +
	fxPCut25 + " (not_routable: relationship type implies is never routed); " + fxPUp25 + " (not_routable: relationship type implies is never routed); " +
	fxPCut50 + " (sure_loss: all-in cost 1.0019 per $1 payout is a guaranteed loss). Fees: none computed. Basis-risk penalty is applied as score × 0.01 per contract, and partners below a relationship confidence of 0.9 are excluded."

// fxDecision99: buy 100 YES on Kalshi above 3.50% with no YES offers; the only
// complement is a sure loss and four partners are one-way.
func fxDecision99() canonical.Decision {
	return canonical.Decision{
		RoundID: 904, TargetID: fxK350, TargetType: canonical.RelComplement, Order: fxOrder(canonical.OutcomeYes),
		Chosen: []canonical.Leg{}, Alternatives: []canonical.Alternative{},
		Exclusions: []canonical.Exclusion{
			{InstrumentID: fxK350, Venue: canonical.VenueKalshi, Reason: exInsufficientDepth, Detail: "0 contracts available on consumed side"},
			notRoutable(fxPHold, "implies"), notRoutable(fxPUp50, "implies"),
			notRoutable(fxPCut25, "implies"), notRoutable(fxPUp25, "implies"),
			{InstrumentID: fxPCut50, Venue: canonical.VenuePolymarket, Reason: exSureLoss, Detail: "all-in cost 1.0019 per $1 payout is a guaranteed loss"},
		},
		Warnings:    []string{},
		Explanation: fxDecision99Explanation,
		DecidedAt:   *tp("2026-10-10T19:44:47Z"),
		Inputs: []canonical.DecisionInput{
			fxInput(fxK350, "self", "0"), fxInput(fxPHold, canonical.RelImplies, "0.8400"),
			fxInput(fxPCut50, canonical.RelComplement, "0.9990"), fxInput(fxPUp50, canonical.RelImplies, "0.0040"),
			fxInput(fxPCut25, canonical.RelImplies, "0.0050"), fxInput(fxPUp25, canonical.RelImplies, "0.1600"),
		},
	}
}

// fxQuote99 is the Kalshi 3.50% book behind #99: YES bids only (best 0.99
// for 8912.33), so no YES offers and NO offers at 1¢.
func fxQuote99() canonical.Quote {
	return canonical.Quote{InstrumentID: fxK350,
		YesBids:   []canonical.Level{{Price: dp("0.99"), Size: dp("8912.33")}, {Price: dp("0.98"), Size: dp("500")}},
		FetchedAt: *tp("2026-10-10T19:42:25Z")}
}

// fxContext99 is #99's context as the decision page builds it: the stored
// Kalshi book, and the suggestion pointing at the complement pair.
func fxContext99(t *testing.T) decisionContext {
	t.Helper()
	d := fxDecision99()
	q := fxQuote99()
	s := suggestOpposite(d, map[string]canonical.Quote{fxK350: q}, 2390)
	if s == nil {
		t.Fatal("no suggestion for #99")
	}
	return decisionContext{Instruments: fxInstruments(), TargetQuote: &q, Suggestion: s}
}
