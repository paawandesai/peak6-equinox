package route

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

const (
	kalshiID     = "kalshi:KXBTCD-26OCT0617-T120000"
	polymarketID = "polymarket:1234567890"
	otherID      = "kalshi:KXETHD-26OCT0617-T4000"
)

var (
	t0  = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	cfg = Config{
		RoundID:                7,
		MinRouteConfidence:     d("0.9"),
		BasisPenaltyMultiplier: d("0.01"),
		Eligibility: EligibilityConfig{
			Snapshot:     t0, // quotes are fetched at t0, so they're fresh (0s old)
			StaleAfter:   120 * time.Second,
			SkewWarn:     5 * time.Second,
			SkewMax:      30 * time.Second,
			MinBookDepth: d("1"),
		},
	}
)

// instrument builds a minimal open instrument whose fee schedule follows its
// venue: Kalshi's is unverified, Polymarket's verified (domain.md § Fees).
func instrument(id string) canonical.Instrument {
	in := canonical.Instrument{ID: id, Venue: canonical.VenueOf(id), VenueMarketID: id, Title: id, Status: canonical.StatusOpen}
	if in.Venue == canonical.VenueKalshi {
		in.FeeSchedule = canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": d("0.07")}}
	} else {
		in.FeeSchedule = canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1, Params: map[string]decimal.Decimal{"rate": d("0.07")},
			Verified: true, Source: "https://docs.polymarket.com/trading/fees"}
	}
	return in
}

func quote(id string, bids, asks []canonical.Level) canonical.Quote {
	return canonical.Quote{InstrumentID: id, YesBids: bids, YesAsks: asks, FetchedAt: t0}
}

func rel(a, b string, typ canonical.RelationshipType, conf string) *canonical.Relationship {
	return &canonical.Relationship{
		RoundID: 7, AID: a, BID: b, Type: typ, Confidence: d(conf), Method: canonical.MethodRules, Explanation: "test",
		BasisRisk: &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}},
	}
}

func cand(id string, bids, asks []canonical.Level, r *canonical.Relationship) canonical.Candidate {
	return canonical.Candidate{Instrument: instrument(id), Quote: quote(id, bids, asks), Relationship: r}
}

func order(side canonical.Side, outcome canonical.Outcome, qty string) canonical.Order {
	q := d(qty)
	return canonical.Order{Side: side, Outcome: outcome, Quantity: &q, Type: canonical.OrderMarket, TIF: canonical.TIFIOC}
}

func buy(qty string) canonical.Order { return order(canonical.SideBuy, canonical.OutcomeYes, qty) }

func sell(qty, outcome string) canonical.Order {
	out := canonical.OutcomeYes
	if outcome == "NO" {
		out = canonical.OutcomeNo
	}
	return order(canonical.SideSell, out, qty)
}

func limit(o canonical.Order, price string) canonical.Order {
	o.Type, o.LimitPrice = canonical.OrderLimit, ptr(price)
	return o
}

func fok(o canonical.Order) canonical.Order {
	o.TIF = canonical.TIFFOK
	return o
}

// scenario: Kalshi target with one 0.52 ask level; Polymarket partner with
// 50 at 0.50 and 100 at 0.53.
func scenario() (canonical.Candidate, []canonical.Candidate) {
	target := cand(kalshiID, []canonical.Level{lv("0.48", "200")}, []canonical.Level{lv("0.52", "100")}, nil)
	partner := cand(polymarketID, []canonical.Level{lv("0.49", "80")}, []canonical.Level{lv("0.50", "50"), lv("0.53", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	return target, []canonical.Candidate{partner}
}

func mustRoute(t *testing.T, o canonical.Order, target canonical.Candidate, partners []canonical.Candidate) canonical.Decision {
	t.Helper()
	// Default targetType for tests: use "equivalent" unless partners define otherwise.
	targetType := canonical.RelEquivalent
	for _, p := range partners {
		if p.Relationship != nil && p.Relationship.Type == canonical.RelComplement {
			targetType = canonical.RelComplement
			break
		}
	}
	dec, err := Route(o, target, partners, cfg, targetType, t0)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if err := dec.Validate(); err != nil {
		t.Fatalf("decision invalid: %v", err)
	}
	return dec
}

func exclusion(t *testing.T, dec canonical.Decision, id string) canonical.Exclusion {
	t.Helper()
	for _, ex := range dec.Exclusions {
		if ex.InstrumentID == id {
			return ex
		}
	}
	t.Fatalf("%s not excluded; exclusions = %+v, chosen = %+v", id, dec.Exclusions, dec.Chosen)
	return canonical.Exclusion{}
}

// TestRouteChoosesLowestAllIn: Polymarket's VWAP 0.515 + fee beats Kalshi's
// 0.52 + fee, so Polymarket is chosen and Kalshi ranks second with its
// per-term cost.
func TestRouteChoosesLowestAllIn(t *testing.T) {
	target, partners := scenario()
	dec := mustRoute(t, buy("100"), target, partners)
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID {
		t.Fatalf("chosen = %+v", dec.Chosen)
	}
	leg := dec.Chosen[0]
	if !leg.Quantity.Equal(d("100")) || !leg.VWAP.Equal(d("0.515")) || leg.Route != RouteDirect {
		t.Errorf("leg = %+v", leg)
	}
	// 50 × 0.07 × 0.50 × 0.50 = 0.875; 50 × 0.07 × 0.53 × 0.47 = 0.87185 → 1.74685 / 100
	if !leg.FeePerContract.Equal(d("0.0174685")) {
		t.Errorf("fee per contract = %s", leg.FeePerContract)
	}
	if !leg.AllInPerContract.Equal(d("0.5324685")) || !leg.BasisPenaltyPerContract.IsZero() {
		t.Errorf("all-in = %s basis = %s", leg.AllInPerContract, leg.BasisPenaltyPerContract)
	}
	if len(dec.Alternatives) != 2 || dec.Alternatives[0].Rank != 1 || dec.Alternatives[0].Note != NoteChosen {
		t.Fatalf("alternatives = %+v", dec.Alternatives)
	}
	second := dec.Alternatives[1]
	// Kalshi: 100 × 0.07 × 0.52 × 0.48 = 1.7472 → 1.75 → 0.0175 per contract.
	if second.Rank != 2 || second.Legs[0].InstrumentID != kalshiID || !second.AllInPerContract.Equal(d("0.5375")) {
		t.Errorf("second = %+v", second)
	}
	if len(dec.Exclusions) != 0 {
		t.Errorf("exclusions = %+v", dec.Exclusions)
	}
	for _, want := range []string{polymarketID, kalshiID, "Best route: polymarket", "ranks #2", "approximate for kalshi", "verified for polymarket", "Basis-risk penalty is applied"} {
		if !strings.Contains(dec.Explanation, want) {
			t.Errorf("explanation lacks %q: %s", want, dec.Explanation)
		}
	}
	if !hasWarning(dec, "fees: approximate for kalshi") {
		t.Errorf("warnings = %v", dec.Warnings)
	}
	if dec.RoundID != 7 || dec.TargetID != kalshiID || !dec.DecidedAt.Equal(t0) {
		t.Errorf("decision header = %+v", dec)
	}
}

func hasWarning(dec canonical.Decision, substr string) bool {
	for _, w := range dec.Warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

// TestRouteFOKNeverPartiallyFills: with 150 wanted, Polymarket (150 deep) fills
// and Kalshi (100 deep) is excluded for depth under FOK; under IOC Kalshi is a
// partial alternative ranked after the full fill.
func TestRouteFOKNeverPartiallyFills(t *testing.T) {
	target, partners := scenario()
	dec := mustRoute(t, fok(buy("150")), target, partners)
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID || !dec.Chosen[0].Quantity.Equal(d("150")) {
		t.Fatalf("chosen = %+v", dec.Chosen)
	}
	ex := exclusion(t, dec, kalshiID)
	if ex.Reason != ReasonInsufficientDepth || !strings.Contains(ex.Detail, "FOK requires 150") {
		t.Errorf("exclusion = %+v", ex)
	}

	// Finding #5: FOK Alternatives should not include partials (only polymarket full fill)
	if len(dec.Alternatives) != 1 || dec.Alternatives[0].Legs[0].InstrumentID != polymarketID {
		t.Errorf("FOK alternatives should only include full fills, got %d: %+v", len(dec.Alternatives), dec.Alternatives)
	}

	dec = mustRoute(t, buy("150"), target, partners)
	if len(dec.Alternatives) != 2 || dec.Alternatives[1].Legs[0].InstrumentID != kalshiID {
		t.Fatalf("IOC alternatives = %+v", dec.Alternatives)
	}
	// Finding #5: IOC partial alternative should have NotePartial prefix
	if alt := dec.Alternatives[1]; !strings.HasPrefix(alt.Note, NotePartial) || !alt.Legs[0].Quantity.Equal(d("100")) {
		t.Errorf("IOC partial = %+v", alt)
	}
}

// TestRouteIOCPartialWhenNothingFillsFully: wanting 500, every book is short;
// M3-03 greedy sweep fills across venues (one leg per venue, not per price level).
func TestRouteIOCPartialWhenNothingFillsFully(t *testing.T) {
	target, partners := scenario()
	dec := mustRoute(t, buy("500"), target, partners)
	// Greedy sweep creates one leg per venue: poly (150 total) + kalshi (100) = 250
	if len(dec.Chosen) != 2 {
		t.Fatalf("chosen should have 2 legs (one per venue), got %d: %+v", len(dec.Chosen), dec.Chosen)
	}
	totalFilled := decimal.Zero
	for _, leg := range dec.Chosen {
		totalFilled = totalFilled.Add(leg.Quantity)
	}
	if !totalFilled.Equal(d("250")) {
		t.Errorf("total filled = %s, want 250 (greedy sweep across venues)", totalFilled)
	}
	if !hasWarning(dec, "fills 250 of 500") {
		t.Errorf("warnings = %v (should mention partial fill)", dec.Warnings)
	}
	dec = mustRoute(t, fok(buy("500")), target, partners)
	if len(dec.Chosen) != 0 || !strings.Contains(dec.Explanation, "No venue can fill") {
		t.Errorf("FOK 500: chosen = %+v explanation = %s", dec.Chosen, dec.Explanation)
	}
	// Round 3 #3: FOK should exclude both candidates (insufficient combined depth)
	if len(dec.Exclusions) != 2 {
		t.Errorf("FOK 500: expected 2 exclusions, got %d: %+v", len(dec.Exclusions), dec.Exclusions)
	}
	for _, ex := range dec.Exclusions {
		if ex.Reason != ReasonInsufficientDepth {
			t.Errorf("FOK 500 exclusion: reason = %s, want %s: %+v", ex.Reason, ReasonInsufficientDepth, ex)
		}
	}
}

// TestRouteLimit: a 0.51 limit excludes Kalshi (best ask 0.52) outright and
// stops Polymarket at its 0.50 level: FOK rejects it, IOC takes the 50.
func TestRouteLimit(t *testing.T) {
	target, partners := scenario()
	dec := mustRoute(t, fok(limit(buy("100"), "0.51")), target, partners)
	if len(dec.Chosen) != 0 {
		t.Fatalf("chosen = %+v", dec.Chosen)
	}
	if ex := exclusion(t, dec, kalshiID); ex.Reason != ReasonLimitNotReachable || !strings.Contains(ex.Detail, "0.52") {
		t.Errorf("kalshi exclusion = %+v", ex)
	}
	if ex := exclusion(t, dec, polymarketID); ex.Reason != ReasonLimitNotReachable || !strings.Contains(ex.Detail, "50 of 100") {
		t.Errorf("polymarket exclusion = %+v", ex)
	}

	dec = mustRoute(t, limit(buy("100"), "0.51"), target, partners)
	if len(dec.Chosen) != 1 || !dec.Chosen[0].Quantity.Equal(d("50")) || !dec.Chosen[0].VWAP.Equal(d("0.50")) {
		t.Fatalf("IOC chosen = %+v", dec.Chosen)
	}
	if !strings.Contains(dec.Explanation, "limit IOC order at 0.5100") {
		t.Errorf("explanation = %s", dec.Explanation)
	}
}

// TestRouteSell: a sell hits the bids and ranks by net proceeds (VWAP − fee),
// highest first.
func TestRouteSell(t *testing.T) {
	target, partners := scenario()
	dec := mustRoute(t, order(canonical.SideSell, canonical.OutcomeYes, "80"), target, partners)
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID {
		t.Fatalf("chosen = %+v", dec.Chosen)
	}
	leg := dec.Chosen[0]
	// 80 × 0.07 × 0.49 × 0.51 = 1.39944 → 0.017493 per contract; proceeds 0.49 − 0.017493.
	if !leg.VWAP.Equal(d("0.49")) || !leg.AllInPerContract.Equal(d("0.472507")) {
		t.Errorf("sell leg = %+v", leg)
	}
	// Kalshi: 0.48 bid; 80 × 0.07 × 0.48 × 0.52 = 1.39776 → 1.40 → 0.0175; proceeds 0.4625.
	if alt := dec.Alternatives[1]; !alt.AllInPerContract.Equal(d("0.4625")) {
		t.Errorf("sell alternative = %+v", alt)
	}
	if !strings.Contains(dec.Explanation, "net proceeds of 0.4725") || !strings.Contains(dec.Explanation, "0.0100 worse") {
		t.Errorf("explanation = %s", dec.Explanation)
	}
	// A sell limit above the bid is unreachable.
	dec = mustRoute(t, fok(limit(order(canonical.SideSell, canonical.OutcomeYes, "10"), "0.495")), target, partners)
	if ex := exclusion(t, dec, polymarketID); ex.Reason != ReasonLimitNotReachable || !strings.Contains(ex.Detail, "best bids is 0.49") {
		t.Errorf("sell limit exclusion = %+v", ex)
	}
}

// TestRouteNoOutcome: buying NO consumes the YES bids at 1 − p.
func TestRouteNoOutcome(t *testing.T) {
	target, partners := scenario()
	dec := mustRoute(t, order(canonical.SideBuy, canonical.OutcomeNo, "80"), target, partners)
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID {
		t.Fatalf("chosen = %+v", dec.Chosen)
	}
	if leg := dec.Chosen[0]; !leg.VWAP.Equal(d("0.51")) || leg.Outcome != canonical.OutcomeNo {
		t.Errorf("NO leg = %+v", leg)
	}
	if alt := dec.Alternatives[1]; !alt.Legs[0].VWAP.Equal(d("0.52")) {
		t.Errorf("NO alternative = %+v", alt)
	}
	if !strings.Contains(dec.Explanation, "80 NO contracts") {
		t.Errorf("explanation = %s", dec.Explanation)
	}
}

func TestRoutePartnerExclusions(t *testing.T) {
	target, _ := scenario()
	asks := []canonical.Level{lv("0.50", "500")}
	closed := cand(polymarketID, nil, asks, rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.95"))
	closed.Instrument.Status = canonical.StatusClosed
	noBook := cand(polymarketID, nil, asks, rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.95"))
	noBook.Quote.Quality.Add(canonical.QualityNoBook, "", "venue returned no book")
	badQuote := cand(polymarketID, nil, asks, rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.95"))
	badQuote.Quote.InstrumentID = otherID
	unknownFee := cand(polymarketID, nil, asks, rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.95"))
	unknownFee.Instrument.FeeSchedule = canonical.FeeSchedule{ID: "mystery_v1"}

	cases := []struct {
		name    string
		partner canonical.Candidate
		reason  string
	}{
		{"nil relationship", cand(polymarketID, nil, asks, nil), ReasonNoRelationship},
		{"relationship to another instrument", cand(polymarketID, nil, asks, rel(otherID, polymarketID, canonical.RelEquivalent, "0.95")), ReasonRelationshipMismatch},
		{"related", cand(polymarketID, nil, asks, rel(polymarketID, kalshiID, canonical.RelRelated, "0.95")), ReasonNotRoutable},
		{"implies", cand(polymarketID, nil, asks, rel(kalshiID, polymarketID, canonical.RelImplies, "0.95")), ReasonNotRoutable},
		// M3-04: complement removed from exclusions (now routable, but this test case has no NO book so it's excluded for insufficient depth)
		{"complement", cand(polymarketID, nil, asks, rel(kalshiID, polymarketID, canonical.RelComplement, "0.95")), ReasonInsufficientDepth},
		{"low confidence", cand(polymarketID, nil, asks, rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.89")), ReasonLowConfidence},
		{"quote mismatch", badQuote, ReasonQuoteMismatch},
		{"closed", closed, ReasonNotOpen},
		{"no book flag", noBook, ReasonNoBook},
		{"empty side", cand(polymarketID, []canonical.Level{lv("0.4", "1")}, nil, rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.95")), ReasonInsufficientDepth},
		{"unknown fee schedule", unknownFee, ReasonFeeUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := mustRoute(t, buy("10"), target, []canonical.Candidate{tc.partner})
			ex := exclusion(t, dec, polymarketID)
			if ex.Reason != tc.reason || ex.Detail == "" || ex.Venue != canonical.VenuePolymarket {
				t.Errorf("exclusion = %+v, want reason %s", ex, tc.reason)
			}
			if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != kalshiID {
				t.Errorf("chosen = %+v, want the target", dec.Chosen)
			}
			if !strings.Contains(dec.Explanation, "Excluded: "+polymarketID+" ("+tc.reason) {
				t.Errorf("explanation = %s", dec.Explanation)
			}
		})
	}
	// equivalent_given_facts is routable and the relationship may be stored either way round.
	dec := mustRoute(t, buy("10"), target, []canonical.Candidate{cand(polymarketID, nil, asks, rel(polymarketID, kalshiID, canonical.RelEquivalentGivenFacts, "0.9"))})
	if len(dec.Exclusions) != 0 || len(dec.Alternatives) != 2 {
		t.Errorf("given-facts partner: exclusions = %+v alternatives = %d", dec.Exclusions, len(dec.Alternatives))
	}
}

// TestRouteNotional: a USD notional converts at the target's touch to whole
// contracts; without any touch the order has no size.
func TestRouteNotional(t *testing.T) {
	target, partners := scenario()
	o := canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Notional: ptr("50"), Type: canonical.OrderMarket, TIF: canonical.TIFIOC}
	dec := mustRoute(t, o, target, partners)
	if len(dec.Chosen) != 1 || !dec.Chosen[0].Quantity.Equal(d("96")) { // floor(50 / 0.52)
		t.Fatalf("chosen = %+v", dec.Chosen)
	}
	if !hasWarning(dec, "quantity 96 derived from notional 50 at the 0.52 touch") || !strings.Contains(dec.Explanation, "sized from a 50.0000 notional at the 0.5200 touch") {
		t.Errorf("warnings = %v\nexplanation = %s", dec.Warnings, dec.Explanation)
	}

	// The target has no asks: excluded for insufficient depth on consumed side.
	bare := target
	bare.Quote.YesAsks = nil
	dec = mustRoute(t, o, bare, partners)
	if len(dec.Chosen) != 1 || !dec.Chosen[0].Quantity.Equal(d("100")) { // floor(50 / 0.50)
		t.Fatalf("partner-touch chosen = %+v", dec.Chosen)
	}
	// Buy YES with no asks → insufficient_depth on consumed side.
	if ex := exclusion(t, dec, kalshiID); ex.Reason != ReasonInsufficientDepth {
		t.Errorf("bare target exclusion = %+v", ex)
	}

	// Too small for one contract.
	o.Notional = ptr("0.10")
	dec = mustRoute(t, o, target, partners)
	if len(dec.Chosen) != 0 {
		t.Fatalf("tiny notional chosen = %+v", dec.Chosen)
	}
	if ex := exclusion(t, dec, kalshiID); ex.Reason != ReasonNoQuantity || !strings.Contains(ex.Detail, "less than one contract") {
		t.Errorf("tiny notional exclusion = %+v", ex)
	}

	// No touch anywhere.
	none := cand(polymarketID, nil, nil, rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.95"))
	dec = mustRoute(t, o, bare, []canonical.Candidate{none})
	if len(dec.Chosen) != 0 || len(dec.Exclusions) != 2 || !strings.Contains(dec.Explanation, "no candidate could price") {
		t.Errorf("no-touch decision = %+v", dec)
	}
	// A touch of zero cannot price a notional and must not divide by zero.
	zero := cand(polymarketID, nil, []canonical.Level{lv("0", "10")}, rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.95"))
	dec = mustRoute(t, o, bare, []canonical.Candidate{zero})
	if ex := exclusion(t, dec, polymarketID); ex.Reason != ReasonNoQuantity {
		t.Errorf("zero-touch exclusion = %+v", ex)
	}
}

// TestRouteTieBreaks: equal all-in costs go to the larger fill, then the
// deeper book, then the lower instrument ID.
func TestRouteTieBreaks(t *testing.T) {
	a := cand(kalshiID, nil, []canonical.Level{lv("0.50", "100")}, nil)
	b := cand(otherID, nil, []canonical.Level{lv("0.50", "300")}, rel(kalshiID, otherID, canonical.RelEquivalent, "0.95"))
	// Add BasisRisk (partners must have it, or they're excluded per M3-02).
	b.Relationship.BasisRisk = &canonical.BasisRisk{Score: d("0"), Components: []canonical.BasisComponent{}}
	dec := mustRoute(t, buy("100"), a, []canonical.Candidate{b})
	if dec.Chosen[0].InstrumentID != otherID {
		t.Errorf("deeper book should win the tie: %+v", dec.Chosen)
	}
	b.Quote.YesAsks = []canonical.Level{lv("0.50", "100")}
	dec = mustRoute(t, buy("100"), a, []canonical.Candidate{b})
	if dec.Chosen[0].InstrumentID != kalshiID {
		t.Errorf("lower ID should win the full tie: %+v", dec.Chosen)
	}
	b.Quote.YesAsks = []canonical.Level{lv("0.50", "60")}
	dec = mustRoute(t, buy("100"), a, []canonical.Candidate{b})
	if dec.Chosen[0].InstrumentID != kalshiID || dec.Alternatives[1].Legs[0].Quantity.GreaterThan(d("60")) {
		t.Errorf("full fill should rank first: %+v", dec.Alternatives)
	}
}

func TestRouteErrors(t *testing.T) {
	target, partners := scenario()
	bad := buy("10")
	bad.Side = "hold"
	if _, err := Route(bad, target, partners, cfg, canonical.RelEquivalent, t0); err == nil || !strings.Contains(err.Error(), "order") {
		t.Errorf("invalid order err = %v", err)
	}
	if _, err := Route(buy("10"), target, partners, Config{}, canonical.RelEquivalent, t0); err == nil || !strings.Contains(err.Error(), "round id") {
		t.Errorf("no round err = %v", err)
	}
	badTarget := target
	badTarget.Instrument.ID = "nowhere:1"
	if _, err := Route(buy("10"), badTarget, partners, cfg, canonical.RelEquivalent, t0); err == nil || !strings.Contains(err.Error(), "target") {
		t.Errorf("bad target err = %v", err)
	}
	badTarget = target
	badTarget.Quote.InstrumentID = otherID
	if _, err := Route(buy("10"), badTarget, partners, cfg, canonical.RelEquivalent, t0); err == nil || !strings.Contains(err.Error(), "target quote") {
		t.Errorf("target quote mismatch err = %v", err)
	}
}

// TestRouteDecidedAtIsUTC: the decision time is normalised to UTC.
func TestRouteDecidedAtIsUTC(t *testing.T) {
	target, partners := scenario()
	dec := mustRoute(t, buy("1"), target, partners)
	if dec.DecidedAt.Location() != time.UTC {
		t.Errorf("decided_at zone = %v", dec.DecidedAt.Location())
	}
	dec, err := Route(buy("1"), target, partners, cfg, canonical.RelEquivalent, t0.In(time.FixedZone("X", 3600)))
	if err != nil || dec.DecidedAt.Location() != time.UTC || !dec.DecidedAt.Equal(t0) {
		t.Errorf("decided_at = %v (%v)", dec.DecidedAt, err)
	}
}

// genBook draws a valid quote for id: bids highest first, asks lowest first,
// never crossed, either side possibly empty.
func genBook(id string) *rapid.Generator[canonical.Quote] {
	return rapid.Custom(func(t *rapid.T) canonical.Quote {
		mid := rapid.Int64Range(1, 9998).Draw(t, "mid")
		q := canonical.Quote{InstrumentID: id, FetchedAt: t0}
		nb := rapid.IntRange(0, 5).Draw(t, "bids")
		for i := int64(0); i < int64(nb) && mid-1-i >= 0; i++ {
			q.YesBids = append(q.YesBids, canonical.Level{Price: decimal.New(mid-1-i, -4), Size: decimal.New(rapid.Int64Range(1, 50000).Draw(t, "bid_size"), -2)})
		}
		na := rapid.IntRange(0, 5).Draw(t, "asks")
		for i := int64(0); i < int64(na) && mid+1+i <= 10000; i++ {
			q.YesAsks = append(q.YesAsks, canonical.Level{Price: decimal.New(mid+1+i, -4), Size: decimal.New(rapid.Int64Range(1, 50000).Draw(t, "ask_size"), -2)})
		}
		return q
	})
}

func genOrder() *rapid.Generator[canonical.Order] {
	return rapid.Custom(func(t *rapid.T) canonical.Order {
		o := canonical.Order{Type: canonical.OrderMarket, TIF: canonical.TIFIOC}
		o.Side = rapid.SampledFrom([]canonical.Side{canonical.SideBuy, canonical.SideSell}).Draw(t, "side")
		o.Outcome = rapid.SampledFrom([]canonical.Outcome{canonical.OutcomeYes, canonical.OutcomeNo}).Draw(t, "outcome")
		if rapid.Bool().Draw(t, "fok") {
			o.TIF = canonical.TIFFOK
		}
		if rapid.Bool().Draw(t, "notional") {
			n := decimal.New(rapid.Int64Range(1, 1_000_000).Draw(t, "notional"), -2)
			o.Notional = &n
		} else {
			q := decimal.New(rapid.Int64Range(1, 2_000_000).Draw(t, "qty"), -2)
			o.Quantity = &q
		}
		if rapid.Bool().Draw(t, "limit") {
			o.Type = canonical.OrderLimit
			o.LimitPrice = ptr(decimal.New(rapid.Int64Range(0, 10000).Draw(t, "limit_price"), -4).String())
		}
		return o
	})
}

// TestRouteProperties: for any valid order and books, the decision validates,
// every leg's fee is ≥ 0, its quantity never exceeds the order or the side's
// depth, its VWAP lies within the consumed prices, and FOK never leaves a
// partial fill.
func TestRouteProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		o := genOrder().Draw(t, "order")
		target := canonical.Candidate{Instrument: instrument(kalshiID), Quote: genBook(kalshiID).Draw(t, "target_book")}
		partner := canonical.Candidate{Instrument: instrument(polymarketID), Quote: genBook(polymarketID).Draw(t, "partner_book"),
			Relationship: rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.95")}
		if err := target.Quote.Validate(); err != nil {
			t.Fatalf("generator produced an invalid book: %v", err)
		}
		dec, err := Route(o, target, []canonical.Candidate{partner}, cfg, canonical.RelEquivalent, t0)
		if err != nil {
			t.Fatalf("Route: %v", err)
		}
		if err := dec.Validate(); err != nil {
			t.Fatalf("decision invalid: %v", err)
		}
		books := map[string]canonical.Quote{kalshiID: target.Quote, polymarketID: partner.Quote}
		var orderQty decimal.Decimal
		if o.Quantity != nil {
			orderQty = *o.Quantity
		}
		for _, alt := range dec.Alternatives {
			// M3-03: alternatives can have multiple legs (multi-venue fills)
			// For multi-leg alternatives, check total quantity vs order
			if len(alt.Legs) > 1 {
				if o.TIF == canonical.TIFFOK && o.Quantity != nil {
					altTotal := decimal.Zero
					for _, leg := range alt.Legs {
						altTotal = altTotal.Add(leg.Quantity)
					}
					if !altTotal.Equal(orderQty) {
						t.Fatalf("FOK multi-leg alternative partial: %s of %s", altTotal, orderQty)
					}
				}
				if o.TIF == canonical.TIFFOK && strings.HasPrefix(alt.Note, NotePartial) {
					t.Fatalf("FOK alternative marked partial: %+v", alt)
				}
				continue // Skip per-leg checks for multi-leg alternatives
			}

			leg := alt.Legs[0]
			if leg.FeePerContract.IsNegative() {
				t.Fatalf("negative fee on %s: %s", leg.InstrumentID, leg.FeePerContract)
			}
			if !leg.Quantity.IsPositive() {
				t.Fatalf("non-positive leg quantity: %+v", leg)
			}
			levels := bookSide(books[leg.InstrumentID], o.Side, o.Outcome)
			if leg.Quantity.GreaterThan(sumSize(levels)) {
				t.Fatalf("leg %s fills %s but the side holds %s", leg.InstrumentID, leg.Quantity, sumSize(levels))
			}
			if o.Quantity != nil && leg.Quantity.GreaterThan(orderQty) {
				t.Fatalf("leg fills %s > order %s", leg.Quantity, orderQty)
			}
			if o.TIF == canonical.TIFFOK && o.Quantity != nil && !leg.Quantity.Equal(orderQty) {
				t.Fatalf("FOK partial fill: %s of %s", leg.Quantity, orderQty)
			}
			if o.TIF == canonical.TIFFOK && strings.HasPrefix(alt.Note, NotePartial) {
				t.Fatalf("FOK alternative marked partial: %+v", alt)
			}
			f := walk(levels, o.Side, leg.Quantity, o.LimitPrice)
			best, _ := f.Best()
			worst, _ := f.Worst()
			if leg.VWAP.LessThan(decimal.Min(best, worst)) || leg.VWAP.GreaterThan(decimal.Max(best, worst)) {
				t.Fatalf("VWAP %s outside [%s, %s]", leg.VWAP, best, worst)
			}
			if o.LimitPrice != nil && !withinLimit(o.Side, worst, o.LimitPrice) {
				t.Fatalf("leg consumed %s beyond limit %s", worst, *o.LimitPrice)
			}
		}
		for _, ex := range dec.Exclusions {
			if ex.Reason == "" || ex.Detail == "" {
				t.Fatalf("exclusion without reason: %+v", ex)
			}
		}
		if len(dec.Chosen)+len(dec.Exclusions) == 0 {
			t.Fatal("every candidate must be chosen, ranked or excluded")
		}
	})
}

func TestRouteEligibilityAndSkew(t *testing.T) {
	target, partners := scenario()
	now := t0.Add(150 * time.Second) // 150s after t0

	// Stale target: all quotes fetched at t0 are now 150s old (> 120s threshold).
	// Use a config with snapshot=now so quotes at t0 are 150s old.
	staleCfg := cfg
	staleCfg.Eligibility.Snapshot = now
	dec, err := Route(buy("10"), target, partners, staleCfg, canonical.RelEquivalent, now)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if ex := exclusion(t, dec, kalshiID); ex.Reason != ReasonStale {
		t.Errorf("stale target exclusion = %+v, want stale", ex)
	}
	if ex := exclusion(t, dec, polymarketID); ex.Reason != ReasonStale {
		t.Errorf("stale partner exclusion = %+v, want stale", ex)
	}

	// Skewed partner: target fresh, partner 40s old (> 30s SkewMax).
	freshTarget := target
	freshTarget.Quote.FetchedAt = now
	skewedPartner := partners[0]
	skewedPartner.Quote.FetchedAt = now.Add(-40 * time.Second)
	dec, err = Route(buy("10"), freshTarget, []canonical.Candidate{skewedPartner}, cfg, canonical.RelEquivalent, now)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if ex := exclusion(t, dec, polymarketID); ex.Reason != ReasonSkew || !strings.Contains(ex.Detail, "40 s behind") {
		t.Errorf("skewed partner exclusion = %+v, want skew with 40s detail", ex)
	}

	// Skew warning: partner 10s old (between 5s SkewWarn and 30s SkewMax).
	warnPartner := partners[0]
	warnPartner.Quote.FetchedAt = now.Add(-10 * time.Second)
	dec, err = Route(buy("10"), freshTarget, []canonical.Candidate{warnPartner}, cfg, canonical.RelEquivalent, now)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if !strings.Contains(strings.Join(dec.Warnings, " "), "skew_warning") {
		t.Errorf("warnings = %v, want skew_warning\nexclusions = %+v\nchosen = %+v", dec.Warnings, dec.Exclusions, dec.Chosen)
	}

	// Shallow book: total depth < MinBookDepth (1 contract).
	shallow := cand(polymarketID, []canonical.Level{lv("0.49", "0.3")}, []canonical.Level{lv("0.50", "0.4")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	shallow.Quote.FetchedAt = now
	dec, err = Route(buy("10"), freshTarget, []canonical.Candidate{shallow}, cfg, canonical.RelEquivalent, now)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if ex := exclusion(t, dec, polymarketID); ex.Reason != ReasonInsufficientDepth {
		t.Errorf("shallow partner exclusion = %+v, want insufficient_depth", ex)
	}
}

func TestRouteTargetExcludedBySkew(t *testing.T) {
	target, partners := scenario()
	now := t0.Add(150 * time.Second)

	t.Run("target older by skew", func(t *testing.T) {
		// Make partner fresh and target old.
		freshPartner := partners[0]
		freshPartner.Quote.FetchedAt = now
		oldTarget := target
		oldTarget.Quote.FetchedAt = now.Add(-50 * time.Second) // 50s skew > 30s max

		dec, err := Route(buy("10"), oldTarget, []canonical.Candidate{freshPartner}, cfg, canonical.RelEquivalent, now)
		if err != nil {
			t.Fatalf("Route: %v", err)
		}

		// Target should be excluded due to skew, exactly once.
		targetExclusionCount := 0
		for _, ex := range dec.Exclusions {
			if ex.InstrumentID == kalshiID {
				targetExclusionCount++
				if ex.Reason != ReasonSkew {
					t.Errorf("target exclusion reason = %q, want %q", ex.Reason, ReasonSkew)
				}
				if !strings.Contains(ex.Detail, "50 s behind") {
					t.Errorf("skew detail = %q, want '50 s behind'", ex.Detail)
				}
			}
		}
		if targetExclusionCount != 1 {
			t.Errorf("target excluded %d times, want exactly 1; exclusions = %+v", targetExclusionCount, dec.Exclusions)
		}

		// Partner should still be chosen.
		if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID {
			t.Errorf("partner not chosen; chosen = %+v, want polymarket", dec.Chosen)
		}
	})

	t.Run("target stale, partner fresh", func(t *testing.T) {
		// Target is stale (> 120s), partner is fresh.
		// Use snapshot=now so target at now-150s is stale.
		staleCfg := cfg
		staleCfg.Eligibility.Snapshot = now
		freshPartner := partners[0]
		freshPartner.Quote.FetchedAt = now
		staleTarget := target
		staleTarget.Quote.FetchedAt = now.Add(-150 * time.Second) // stale, also skewed

		dec, err := Route(buy("10"), staleTarget, []canonical.Candidate{freshPartner}, staleCfg, canonical.RelEquivalent, now)
		if err != nil {
			t.Fatalf("Route: %v", err)
		}

		// Target should be excluded for staleness, not skew (staleness check comes first).
		targetExclusionCount := 0
		foundStale := false
		for _, ex := range dec.Exclusions {
			if ex.InstrumentID == kalshiID {
				targetExclusionCount++
				if ex.Reason == ReasonStale {
					foundStale = true
				}
			}
		}
		if !foundStale {
			t.Errorf("target not excluded for staleness; exclusions = %+v", dec.Exclusions)
		}
		if targetExclusionCount != 1 {
			t.Errorf("target excluded %d times, want exactly 1; exclusions = %+v", targetExclusionCount, dec.Exclusions)
		}

		// Partner should still be chosen despite target being ineligible.
		if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID {
			t.Errorf("partner not chosen; chosen = %+v, want polymarket", dec.Chosen)
		}
	})
}

func TestRoutePartnerVsPartnerSkew(t *testing.T) {
	// Regression test for reviewer finding: partner-vs-partner skew must be enforced.
	// If partner A is fresh, partner B is 40s old, and target is 20s old, then B should
	// be excluded for skew (40s > 30s SkewMax), even though target-vs-B is only 20s.
	now := t0.Add(100 * time.Second)
	freshPartner := cand(polymarketID, []canonical.Level{lv("0.49", "100")}, []canonical.Level{lv("0.50", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	freshPartner.Quote.FetchedAt = now

	oldPartner := cand(otherID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.49", "100")},
		rel(kalshiID, otherID, canonical.RelEquivalent, "0.97"))
	oldPartner.Quote.FetchedAt = now.Add(-40 * time.Second) // 40s old

	target := cand(kalshiID, []canonical.Level{lv("0.495", "100")}, []canonical.Level{lv("0.505", "100")}, nil)
	target.Quote.FetchedAt = now.Add(-20 * time.Second) // 20s old

	staleCfg := cfg
	staleCfg.Eligibility.Snapshot = now
	dec, err := Route(buy("10"), target, []canonical.Candidate{freshPartner, oldPartner}, staleCfg, canonical.RelEquivalent, now)
	if err != nil {
		t.Fatalf("Route: %v", err)
	}

	// The old partner (40s behind freshest) should be excluded for skew.
	foundOldPartnerSkew := false
	for _, ex := range dec.Exclusions {
		if ex.InstrumentID == otherID && ex.Reason == ReasonSkew {
			foundOldPartnerSkew = true
			if !strings.Contains(ex.Detail, "40 s behind") {
				t.Errorf("old partner skew detail = %q, want '40 s behind'", ex.Detail)
			}
		}
	}
	if !foundOldPartnerSkew {
		t.Errorf("old partner not excluded for skew; exclusions = %+v", dec.Exclusions)
	}

	// The fresh partner should be chosen.
	if len(dec.Chosen) != 1 || dec.Chosen[0].InstrumentID != polymarketID {
		t.Errorf("chosen = %+v, want fresh partner %s", dec.Chosen, polymarketID)
	}
}

func TestRouteSkewOrderIndependence(t *testing.T) {
	// Order-independence test: shuffling partners should not change the decision.
	now := t0.Add(100 * time.Second)
	fresh := cand(polymarketID, []canonical.Level{lv("0.49", "100")}, []canonical.Level{lv("0.50", "100")},
		rel(kalshiID, polymarketID, canonical.RelEquivalent, "0.97"))
	fresh.Quote.FetchedAt = now

	old := cand(otherID, []canonical.Level{lv("0.48", "100")}, []canonical.Level{lv("0.49", "100")},
		rel(kalshiID, otherID, canonical.RelEquivalent, "0.97"))
	old.Quote.FetchedAt = now.Add(-40 * time.Second)

	target := cand(kalshiID, []canonical.Level{lv("0.495", "100")}, []canonical.Level{lv("0.505", "100")}, nil)
	target.Quote.FetchedAt = now.Add(-10 * time.Second)

	staleCfg := cfg
	staleCfg.Eligibility.Snapshot = now

	// Run with partners in both orders.
	dec1, err := Route(buy("10"), target, []canonical.Candidate{fresh, old}, staleCfg, canonical.RelEquivalent, now)
	if err != nil {
		t.Fatalf("Route order 1: %v", err)
	}
	dec2, err := Route(buy("10"), target, []canonical.Candidate{old, fresh}, staleCfg, canonical.RelEquivalent, now)
	if err != nil {
		t.Fatalf("Route order 2: %v", err)
	}

	// Both should exclude the old partner and choose the fresh one.
	if len(dec1.Chosen) != 1 || dec1.Chosen[0].InstrumentID != polymarketID {
		t.Errorf("order 1 chosen = %+v, want %s", dec1.Chosen, polymarketID)
	}
	if len(dec2.Chosen) != 1 || dec2.Chosen[0].InstrumentID != polymarketID {
		t.Errorf("order 2 chosen = %+v, want %s", dec2.Chosen, polymarketID)
	}

	oldExcluded1 := false
	oldExcluded2 := false
	for _, ex := range dec1.Exclusions {
		if ex.InstrumentID == otherID && ex.Reason == ReasonSkew {
			oldExcluded1 = true
		}
	}
	for _, ex := range dec2.Exclusions {
		if ex.InstrumentID == otherID && ex.Reason == ReasonSkew {
			oldExcluded2 = true
		}
	}
	if !oldExcluded1 || !oldExcluded2 {
		t.Errorf("old partner not excluded in both orders; dec1 exclusions = %+v, dec2 exclusions = %+v", dec1.Exclusions, dec2.Exclusions)
	}
}

func TestEligibilityOneSidedBook(t *testing.T) {
	// One-sided book test: buy YES order needs YesAsks, not YesBids.
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cfg := EligibilityConfig{
		Snapshot:     now,
		StaleAfter:   120 * time.Second,
		MinBookDepth: decimal.NewFromInt(1),
	}

	// Quote with only bids (no asks) should be ineligible for buy YES.
	onlyBids := canonical.Quote{
		InstrumentID: "test:1",
		FetchedAt:    now.Add(-10 * time.Second),
		YesBids:      []canonical.Level{{Price: dec("0.5"), Size: dec("10")}},
		YesAsks:      []canonical.Level{},
	}

	ok, exs := IsEligible(onlyBids, canonical.SideBuy, canonical.OutcomeYes, cfg)
	if ok {
		t.Errorf("buy YES on bid-only quote should be ineligible; got eligible")
	}
	if len(exs) == 0 || exs[0].Reason != ReasonInsufficientDepth {
		t.Errorf("expected insufficient_depth exclusion, got %+v", exs)
	}

	// Quote with only asks should be eligible for buy YES.
	onlyAsks := canonical.Quote{
		InstrumentID: "test:2",
		FetchedAt:    now.Add(-10 * time.Second),
		YesBids:      []canonical.Level{},
		YesAsks:      []canonical.Level{{Price: dec("0.6"), Size: dec("10")}},
	}

	ok, exs = IsEligible(onlyAsks, canonical.SideBuy, canonical.OutcomeYes, cfg)
	if !ok {
		t.Errorf("buy YES on ask-only quote should be eligible; got %+v", exs)
	}
}
