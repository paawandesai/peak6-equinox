package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/decisionlog"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

var matchT0 = time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)

// Real venue phrasing (see testdata/match/crypto_pairs.yaml). The Polymarket
// "cf" market is synthetic: Polymarket wording on Kalshi's source, so one pair
// is equivalent.
const (
	kalshiRules = "If the simple average of the sixty seconds of CF Benchmarks' Bitcoin Real-Time Index (BRTI) before 6 PM EDT " +
		"is above 76599.99 at 6 PM EDT on Oct 6, 2026, then the market resolves to Yes."
	kalshiRulesNext = "If the simple average of the sixty seconds of CF Benchmarks' Bitcoin Real-Time Index (BRTI) before 6 PM EDT " +
		"is above 76699.99 at 6 PM EDT on Oct 6, 2026, then the market resolves to Yes."
	polyCFRules = `This market will resolve to "Yes" if the CF Benchmarks Bitcoin Real-Time Index (BRTI) at 6:00 PM ET on the date ` +
		`specified in the title is higher than the price specified in the title. The resolution source for this market is CF Benchmarks.`
	polyBinanceRules = `This market will resolve to "Yes" if the Binance 1 minute candle for BTC/USDT closing at 12:00 in the ET timezone (noon) ` +
		`on the date specified in the title has a final "Close" price higher than the price specified in the title. ` +
		`The resolution source for this market is Binance, specifically the BTC/USDT "Close" prices.`
)

func matchInstrument(venue canonical.Venue, key, title, rules string, closeTime time.Time, cat canonical.Category) canonical.Instrument {
	fee := canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": decimal.RequireFromString("0.07")}}
	if venue == canonical.VenuePolymarket {
		fee = canonical.FeeSchedule{ID: canonical.FeeSchedulePolymarketV1}
	}
	return canonical.Instrument{
		ID: canonical.InstrumentID(venue, key), Venue: venue, VenueMarketID: key, VenueEventID: "EVT", Category: cat,
		Title: title, RulesText: rules, Terms: canonical.UnknownTerms(), CloseTime: closeTime, Status: canonical.StatusOpen, FeeSchedule: fee,
	}
}

// seedMatchRound stores a finished round with instruments on both venues.
func seedMatchRound(t *testing.T, st *store.Store, status store.RoundStatus) store.Round {
	t.Helper()
	ctx := context.Background()
	round, err := st.CreateRound(ctx, matchT0)
	if err != nil {
		t.Fatal(err)
	}
	ins := []canonical.Instrument{
		matchInstrument(canonical.VenueKalshi, "KXBTCD-26OCT0618-T76599.99", "Bitcoin price on Oct 6, 2026? — $76,600 or above", kalshiRules, matchT0, canonical.CategoryCrypto),
		matchInstrument(canonical.VenueKalshi, "KXBTCD-26OCT0618-T76699.99", "Bitcoin price on Oct 6, 2026? — $76,700 or above", kalshiRulesNext, matchT0, canonical.CategoryCrypto),
		// equivalent to the first Kalshi market
		matchInstrument(canonical.VenuePolymarket, "1001", "Will the price of Bitcoin be above $76,599.99 on October 6?", polyCFRules, matchT0, canonical.CategoryCrypto),
		// near miss: one cent off, Binance, noon
		matchInstrument(canonical.VenuePolymarket, "1002", "Will the price of Bitcoin be above $76,600 on October 6?", polyBinanceRules, matchT0.Add(-6*time.Hour), canonical.CategoryCrypto),
		// ladder far away: related but not a near miss
		matchInstrument(canonical.VenuePolymarket, "1003", "Will the price of Bitcoin be above $90,000 on October 6?", polyBinanceRules, matchT0.Add(-6*time.Hour), canonical.CategoryCrypto),
		// other subject: unrelated
		matchInstrument(canonical.VenuePolymarket, "1004", "Will the price of Ethereum be above $2,000 on October 6?", polyBinanceRules, matchT0.Add(-6*time.Hour), canonical.CategoryCrypto),
		// other category: never paired with crypto
		matchInstrument(canonical.VenuePolymarket, "1005", "Will 1 Fed rate cut happen in 2026?", "Resolves per FOMC statements.", matchT0, canonical.CategoryFed),
	}
	for _, in := range ins {
		if err := st.UpsertInstrument(ctx, round.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.FinishRound(ctx, round.ID, status, nil, matchT0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	return round
}

func TestMatchRoundStoresAndLogs(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	ctx := context.Background()
	round := seedMatchRound(t, st, store.RoundOK)

	sum, err := matchRound(ctx, st, round.ID, false, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("matchRound: %v", err)
	}
	// 2 Kalshi × 4 Polymarket crypto = 8 pairs; the fed instrument is never paired.
	if sum.Pairs != 8 || sum.Instruments != 7 {
		t.Errorf("pairs = %d, instruments = %d", sum.Pairs, sum.Instruments)
	}
	if sum.ByType[canonical.RelEquivalent] != 1 || sum.ByType[canonical.RelUnrelated] != 2 || sum.ByType[canonical.RelRelated] != 5 {
		t.Errorf("by type = %v", sum.ByType)
	}
	// Stored: the equivalent pair and the one-cent near miss (76599.99 vs 76600).
	// The next strike (76699.99) is 0.13% away from both, so it is not a near miss.
	if sum.Stored != 2 || sum.Logged != 2 || sum.Skipped != 4 {
		t.Errorf("stored %d logged %d skipped %d", sum.Stored, sum.Logged, sum.Skipped)
	}
	rels, err := st.ListRelationships(ctx, store.RelationshipFilter{RoundID: round.ID})
	if err != nil || len(rels) != 2 {
		t.Fatalf("relationships = %d, %v", len(rels), err)
	}
	for _, r := range rels {
		if r.RoundID != round.ID || canonical.VenueOf(r.AID) != canonical.VenueKalshi || canonical.VenueOf(r.BID) != canonical.VenuePolymarket {
			t.Errorf("relationship %+v", r)
		}
		if r.Type == canonical.RelEquivalent && (r.AID != "kalshi:KXBTCD-26OCT0618-T76599.99" || r.BID != "polymarket:1001") {
			t.Errorf("equivalent pair = %s / %s", r.AID, r.BID)
		}
		if r.Type == canonical.RelRelated && r.BID != "polymarket:1002" {
			t.Errorf("near miss = %s / %s", r.AID, r.BID)
		}
		entries, err := decisionlog.New(st).ForRelationship(ctx, r.ID)
		if err != nil || len(entries) != 1 {
			t.Fatalf("log entries for %d = %v, %v", r.ID, entries, err)
		}
		var p decisionlog.RelationshipPayload
		if err := json.Unmarshal(entries[0].Payload, &p); err != nil || p.Type != r.Type || p.RoundID != round.ID {
			t.Errorf("payload %+v, %v", p, err)
		}
	}
	// Running again upserts rather than duplicating.
	if _, err := matchRound(ctx, st, round.ID, false, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	if rels, _ = st.ListRelationships(ctx, store.RelationshipFilter{RoundID: round.ID}); len(rels) != 2 {
		t.Errorf("second run left %d relationships", len(rels))
	}
	text := sum.String()
	for _, want := range []string{"8 cross-venue pairs", "equivalent 1", "related 5", "unrelated 2", "relationships stored 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("summary %q lacks %q", text, want)
		}
	}
}

func TestMatchRoundStoreAll(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	round := seedMatchRound(t, st, store.RoundPartial)
	sum, err := matchRound(context.Background(), st, 0, true, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if sum.RoundID != round.ID || sum.Stored != 6 || sum.Logged != 6 || sum.Skipped != 0 {
		t.Errorf("summary = %+v", sum)
	}
}

func TestMatchRoundErrors(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)
	if _, err := matchRound(ctx, st, 0, false, logger); err == nil || !strings.Contains(err.Error(), "no finished round") {
		t.Errorf("empty db: %v", err)
	}
	if _, err := matchRound(ctx, st, 999, false, logger); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing round: %v", err)
	}
	running, err := st.CreateRound(ctx, matchT0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := matchRound(ctx, st, running.ID, false, logger); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Errorf("running round: %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	round := seedMatchRound(t, st, store.RoundOK)
	if _, err := matchRound(cancelled, st, round.ID, false, logger); err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("cancelled: %v", err)
	}
}

func TestMatchCommand(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	round := seedMatchRound(t, st, store.RoundOK)
	setAppEnv(t, tdb.DSN(t))

	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"match"}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "equivalent 1") || !strings.Contains(out.String(), fmt.Sprintf("round %d:", round.ID)) {
		t.Errorf("stdout = %q", out.String())
	}
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"match", "--bogus"}, 2, "flag provided but not defined"},
		{[]string{"match", "extra"}, 2, "no positional arguments"},
		{[]string{"match", "--round", "999"}, 1, "not found"},
	} {
		errOut.Reset()
		if code := run(context.Background(), tc.args, io.Discard, &errOut); code != tc.code || !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%v: exit %d, stderr %q; want %d containing %q", tc.args, code, errOut.String(), tc.code, tc.want)
		}
	}
	t.Setenv("APP_ENV", "")
	errOut.Reset()
	if code := run(context.Background(), []string{"match"}, io.Discard, &errOut); code != 1 || !strings.Contains(errOut.String(), "APP_ENV is required") {
		t.Errorf("missing env: exit %d, stderr %q", code, errOut.String())
	}
}

func TestNearMiss(t *testing.T) {
	rel := func(status canonical.DiffStatus, a, b string) canonical.Relationship {
		return canonical.Relationship{Diff: []canonical.FieldDiff{{Field: "threshold", A: a, B: b, Status: status}}}
	}
	cases := []struct {
		name string
		rel  canonical.Relationship
		want bool
	}{
		{"equal", rel(canonical.DiffMatch, "76600", "76600"), true},
		{"one cent", rel(canonical.DiffDiffer, "76599.99", "76600"), true},
		{"0.1 percent", rel(canonical.DiffDiffer, "100000", "100100"), true},
		{"next strike", rel(canonical.DiffDiffer, "76599.99", "76699.99"), false},
		{"unknown", rel(canonical.DiffUnknown, "unknown", "76600"), false},
		{"garbage", rel(canonical.DiffDiffer, "x", "76600"), false},
		{"both zero", rel(canonical.DiffDiffer, "0", "0.0"), true},
		{"no threshold field", canonical.Relationship{}, false},
	}
	for _, tc := range cases {
		if got := nearMiss(tc.rel); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}

func TestCrossVenuePairsOrder(t *testing.T) {
	ins := []canonical.Instrument{
		{ID: "polymarket:1", Venue: canonical.VenuePolymarket, Category: canonical.CategoryCrypto},
		{ID: "kalshi:B", Venue: canonical.VenueKalshi, Category: canonical.CategoryCrypto},
		{ID: "kalshi:A", Venue: canonical.VenueKalshi, Category: canonical.CategoryCrypto},
		{ID: "kalshi:F", Venue: canonical.VenueKalshi, Category: canonical.CategoryFed},
		{ID: "kalshi:C", Venue: canonical.VenueKalshi, Category: canonical.CategoryCrypto},
	}
	pairs := crossVenuePairs(ins)
	if len(pairs) != 3 {
		t.Fatalf("%d pairs", len(pairs))
	}
	for _, p := range pairs {
		if p.a.Venue != canonical.VenueKalshi || p.b.ID != "polymarket:1" {
			t.Errorf("pair %s/%s", p.a.ID, p.b.ID)
		}
	}
	if pairs[0].a.ID != "kalshi:B" || pairs[1].a.ID != "kalshi:A" || pairs[2].a.ID != "kalshi:C" {
		t.Errorf("order changed: %s %s %s", pairs[0].a.ID, pairs[1].a.ID, pairs[2].a.ID)
	}
}
