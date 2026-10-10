package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

var (
	t0  = time.Date(2026, 10, 6, 17, 0, 0, 0, time.UTC)
	ctx = context.Background()
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func newStore(t *testing.T) *store.Store {
	t.Helper()
	s := store.New(tdb.Pool(t))
	tdb.Reset(t)
	return s
}

func mustRound(t *testing.T, s *store.Store) store.Round {
	t.Helper()
	r, err := s.CreateRound(ctx, t0)
	if err != nil {
		t.Fatalf("CreateRound: %v", err)
	}
	return r
}

func instrument(id string) canonical.Instrument {
	v := canonical.VenueOf(id)
	key := strings.TrimPrefix(id, string(v)+":")
	deadline := t0.Add(time.Hour)
	threshold := d("120000")
	terms := canonical.UnknownTerms()
	terms.Subject, terms.Comparator, terms.Threshold, terms.Deadline = "BTC", canonical.ComparatorGT, &threshold, &deadline
	terms.Polarity = canonical.PolarityPositive
	return canonical.Instrument{
		ID: id, Venue: v, VenueMarketID: key, VenueEventID: "EVT", Category: canonical.CategoryCrypto,
		Title: "BTC above 120k?", RulesText: "rules", Terms: terms, CloseTime: t0.Add(time.Hour),
		Status: canonical.StatusOpen,
		FeeSchedule: canonical.FeeSchedule{
			ID: canonical.FeeScheduleKalshiTakerV1, Params: map[string]decimal.Decimal{"multiplier": d("0.07")},
		},
	}
}

// TestInstrumentRuleLayers tests rule layers persistence (UI-21).
func TestInstrumentRuleLayers(t *testing.T) {
	s := newStore(t)
	r := mustRound(t, s)

	// Test with all fields populated
	clarTime := t0.Add(24 * time.Hour)
	ins := instrument("kalshi:TEST-1")
	ins.RulesSummary = "Brief summary"
	ins.Clarifications = "Additional details and clarifications"
	ins.ClarificationsUpdatedAt = clarTime

	if err := s.UpsertInstrument(ctx, r.ID, ins); err != nil {
		t.Fatalf("UpsertInstrument: %v", err)
	}

	got, err := s.GetInstrument(ctx, ins.ID)
	if err != nil {
		t.Fatalf("GetInstrument: %v", err)
	}

	if got.RulesSummary != "Brief summary" {
		t.Errorf("RulesSummary = %q, want %q", got.RulesSummary, "Brief summary")
	}
	if got.Clarifications != "Additional details and clarifications" {
		t.Errorf("Clarifications = %q, want %q", got.Clarifications, "Additional details and clarifications")
	}
	if !got.ClarificationsUpdatedAt.Equal(clarTime) {
		t.Errorf("ClarificationsUpdatedAt = %v, want %v", got.ClarificationsUpdatedAt, clarTime)
	}

	// Test with empty fields
	ins2 := instrument("polymarket:TEST-2")
	ins2.RulesSummary = ""
	ins2.Clarifications = ""
	// ClarificationsUpdatedAt zero by default

	if err := s.UpsertInstrument(ctx, r.ID, ins2); err != nil {
		t.Fatalf("UpsertInstrument empty: %v", err)
	}

	got2, err := s.GetInstrument(ctx, ins2.ID)
	if err != nil {
		t.Fatalf("GetInstrument empty: %v", err)
	}

	if got2.RulesSummary != "" {
		t.Errorf("empty RulesSummary = %q, want empty", got2.RulesSummary)
	}
	if got2.Clarifications != "" {
		t.Errorf("empty Clarifications = %q, want empty", got2.Clarifications)
	}
	if !got2.ClarificationsUpdatedAt.IsZero() {
		t.Errorf("ClarificationsUpdatedAt = %v, want zero", got2.ClarificationsUpdatedAt)
	}
}

func TestRounds(t *testing.T) {
	s := newStore(t)
	if _, err := s.CurrentRound(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("CurrentRound on empty db = %v", err)
	}
	r1 := mustRound(t, s)
	if r1.Status != store.RoundRunning || !r1.StartedAt.Equal(t0) || r1.StartedAt.Location() != time.UTC || !r1.FinishedAt.IsZero() {
		t.Errorf("new round = %+v", r1)
	}
	if _, err := s.LatestRound(ctx); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("LatestRound with only a running round = %v", err)
	}
	vs := map[canonical.Venue]store.VenueStatus{
		canonical.VenueKalshi:     {OK: true, Instruments: 10, Quotes: 9, DurationMS: 1200},
		canonical.VenuePolymarket: {OK: false, Error: "HTTP 500"},
	}
	if err := s.FinishRound(ctx, r1.ID, store.RoundPartial, vs, t0.Add(time.Minute)); err != nil {
		t.Fatalf("FinishRound: %v", err)
	}
	got, err := s.GetRound(ctx, r1.ID)
	if err != nil {
		t.Fatalf("GetRound: %v", err)
	}
	if got.Status != store.RoundPartial || !got.FinishedAt.Equal(t0.Add(time.Minute)) || !reflect.DeepEqual(got.VenueStatus, vs) {
		t.Errorf("finished round = %+v", got)
	}
	if err := s.FinishRound(ctx, r1.ID, store.RoundRunning, nil, t0); err == nil {
		t.Error("FinishRound accepted status running")
	}
	if err := s.FinishRound(ctx, 9999, store.RoundOK, nil, t0); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("FinishRound missing = %v", err)
	}
	r2 := mustRound(t, s)
	if err := s.FinishRound(ctx, r2.ID, store.RoundOK, nil, t0); err != nil {
		t.Fatal(err)
	}
	r3 := mustRound(t, s)
	if err := s.FinishRound(ctx, r3.ID, store.RoundFailed, nil, t0); err != nil {
		t.Fatal(err)
	}
	latest, err := s.LatestRound(ctx)
	if err != nil || latest.ID != r2.ID {
		t.Errorf("LatestRound = %d %v, want %d", latest.ID, err, r2.ID)
	}
	list, err := s.ListRounds(ctx, 0)
	if err != nil || len(list) != 3 || list[0].ID != r3.ID {
		t.Errorf("ListRounds = %v %v", list, err)
	}
	if list, _ := s.ListRounds(ctx, 1); len(list) != 1 {
		t.Errorf("ListRounds(1) len = %d", len(list))
	}

	// Pinning: only ok/partial rounds; at most one pinned; CurrentRound prefers it.
	if err := s.PinRound(ctx, r3.ID); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("pinning a failed round = %v", err)
	}
	if err := s.PinRound(ctx, r2.ID); err != nil {
		t.Fatalf("PinRound: %v", err)
	}
	if err := s.PinRound(ctx, r1.ID); err != nil {
		t.Fatalf("PinRound r1: %v", err)
	}
	pinned, err := s.PinnedRound(ctx)
	if err != nil || pinned.ID != r1.ID || !pinned.Pinned {
		t.Errorf("PinnedRound = %+v %v", pinned, err)
	}
	if cur, err := s.CurrentRound(ctx); err != nil || cur.ID != r1.ID {
		t.Errorf("CurrentRound = %d %v, want pinned %d", cur.ID, err, r1.ID)
	}
	if err := s.UnpinRounds(ctx); err != nil {
		t.Fatal(err)
	}
	if cur, err := s.CurrentRound(ctx); err != nil || cur.ID != r2.ID {
		t.Errorf("CurrentRound after unpin = %d %v, want latest %d", cur.ID, err, r2.ID)
	}
	if _, err := s.GetRound(ctx, 424242); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetRound missing = %v", err)
	}
}

func TestRawResponses(t *testing.T) {
	s := newStore(t)
	r := mustRound(t, s)
	body := []byte(`{"events":[{"ticker":"KXBTCD"}]}` + strings.Repeat(" ", 4096))
	id, err := s.InsertRawResponse(ctx, store.RawResponse{
		RoundID: r.ID, Venue: canonical.VenueKalshi, Endpoint: "/events?series_ticker=KXBTCD", HTTPStatus: 200,
		Body: body, FetchedAt: t0.In(time.FixedZone("CDT", -5*3600)),
	})
	if err != nil {
		t.Fatalf("InsertRawResponse: %v", err)
	}
	got, err := s.GetRawResponse(ctx, id)
	if err != nil {
		t.Fatalf("GetRawResponse: %v", err)
	}
	if string(got.Body) != string(body) || got.Endpoint != "/events?series_ticker=KXBTCD" || got.HTTPStatus != 200 ||
		got.FetchedAt.Location() != time.UTC || !got.FetchedAt.Equal(t0) {
		t.Errorf("raw = %+v", got)
	}
	var stored int
	if err := tdb.Pool(t).QueryRow(ctx, `SELECT length(body_gz) FROM raw_responses WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored >= len(body) {
		t.Errorf("body not compressed: %d stored bytes for %d", stored, len(body))
	}
	list, err := s.ListRawResponses(ctx, r.ID)
	if err != nil || len(list) != 1 || list[0].ID != id {
		t.Errorf("ListRawResponses = %v %v", list, err)
	}
	if _, err := s.InsertRawResponse(ctx, store.RawResponse{RoundID: r.ID, Venue: "nyse"}); err == nil {
		t.Error("unsupported venue accepted")
	}
	if _, err := s.InsertRawResponse(ctx, store.RawResponse{RoundID: 999, Venue: canonical.VenueKalshi, FetchedAt: t0}); err == nil {
		t.Error("raw response for a missing round accepted")
	}
	if _, err := s.GetRawResponse(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetRawResponse missing = %v", err)
	}
	// A corrupt body is reported, not panicked on.
	if _, err := tdb.Pool(t).Exec(ctx, `UPDATE raw_responses SET body_gz = '\x00ff' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRawResponse(ctx, id); err == nil || errors.Is(err, store.ErrNotFound) {
		t.Errorf("corrupt body = %v", err)
	}
	if _, err := s.ListRawResponses(ctx, r.ID); err == nil {
		t.Error("ListRawResponses with corrupt body succeeded")
	}
}

func TestInstruments(t *testing.T) {
	s := newStore(t)
	r1, r2 := mustRound(t, s), mustRound(t, s)
	k := instrument("kalshi:KXBTCD-26OCT0617-T120000")
	k.Quality.Add(canonical.QualityDeadlineDefaulted, "deadline", "from close_time")
	p := instrument("polymarket:12345")
	p.Category = canonical.CategoryFed
	p.CloseTime = time.Time{}
	for _, in := range []canonical.Instrument{k, p} {
		if err := s.UpsertInstrument(ctx, r1.ID, in); err != nil {
			t.Fatalf("UpsertInstrument %s: %v", in.ID, err)
		}
	}
	got, err := s.GetInstrument(ctx, k.ID)
	if err != nil {
		t.Fatalf("GetInstrument: %v", err)
	}
	if !reflect.DeepEqual(got.Instrument, k) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got.Instrument, k)
	}
	if got.FirstSeenRound != r1.ID || got.LastSeenRound != r1.ID {
		t.Errorf("seen rounds = %d/%d", got.FirstSeenRound, got.LastSeenRound)
	}
	k.Title = "updated title"
	k.Quality = nil
	if err := s.UpsertInstrument(ctx, r2.ID, k); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetInstrument(ctx, k.ID)
	if got.Title != "updated title" || got.FirstSeenRound != r1.ID || got.LastSeenRound != r2.ID || got.Quality != nil {
		t.Errorf("after update = %+v", got)
	}
	gotP, err := s.GetInstrument(ctx, p.ID)
	if err != nil || !gotP.CloseTime.IsZero() {
		t.Errorf("zero close time round trip = %v %v", gotP.CloseTime, err)
	}

	all, err := s.ListInstruments(ctx, store.InstrumentFilter{})
	if err != nil || len(all) != 2 || all[0].ID != k.ID {
		t.Errorf("ListInstruments = %d %v", len(all), err)
	}
	for _, tc := range []struct {
		f    store.InstrumentFilter
		want int
	}{
		{store.InstrumentFilter{Venue: canonical.VenuePolymarket}, 1},
		{store.InstrumentFilter{Category: canonical.CategoryCrypto}, 1},
		{store.InstrumentFilter{SeenInRound: r2.ID}, 1},
		{store.InstrumentFilter{SeenInRound: r1.ID, Venue: canonical.VenueKalshi}, 0},
		{store.InstrumentFilter{Limit: 1}, 1},
	} {
		got, err := s.ListInstruments(ctx, tc.f)
		if err != nil || len(got) != tc.want {
			t.Errorf("ListInstruments(%+v) = %d %v, want %d", tc.f, len(got), err, tc.want)
		}
	}

	bad := instrument("kalshi:X")
	bad.Title = ""
	if err := s.UpsertInstrument(ctx, r1.ID, bad); err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Errorf("invalid instrument = %v", err)
	}
	if err := s.UpsertInstrument(ctx, 9999, instrument("kalshi:Y")); err == nil {
		t.Error("instrument for a missing round accepted")
	}
	if _, err := s.GetInstrument(ctx, "kalshi:NOPE"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetInstrument missing = %v", err)
	}
}

func TestQuotes(t *testing.T) {
	s := newStore(t)
	r := mustRound(t, s)
	in := instrument("kalshi:KXBTCD-1")
	if err := s.UpsertInstrument(ctx, r.ID, in); err != nil {
		t.Fatal(err)
	}
	venueTS := t0.Add(-2 * time.Second)
	q := canonical.Quote{
		InstrumentID: in.ID,
		YesBids:      []canonical.Level{{Price: d("0.45"), Size: d("10")}, {Price: d("0.44"), Size: d("2.5")}},
		YesAsks:      []canonical.Level{{Price: d("0.47"), Size: d("3")}},
		FetchedAt:    t0, VenueTS: &venueTS,
	}
	if err := s.InsertQuote(ctx, r.ID, q); err != nil {
		t.Fatalf("InsertQuote: %v", err)
	}
	got, err := s.GetQuote(ctx, r.ID, in.ID)
	if err != nil {
		t.Fatalf("GetQuote: %v", err)
	}
	if !got.BestBid.Valid || !got.BestBid.Decimal.Equal(d("0.45")) || !got.BestAsk.Decimal.Equal(d("0.47")) || !got.Mid.Decimal.Equal(d("0.46")) {
		t.Errorf("touch = %v %v %v", got.BestBid, got.BestAsk, got.Mid)
	}
	if len(got.YesBids) != 2 || !got.YesBids[1].Size.Equal(d("2.5")) || got.VenueTS == nil || !got.VenueTS.Equal(venueTS) ||
		got.FetchedAt.Location() != time.UTC || got.Quality != nil {
		t.Errorf("quote = %+v", got)
	}
	if err := s.InsertQuote(ctx, r.ID, q); err == nil {
		t.Error("duplicate quote accepted")
	}

	empty := instrument("polymarket:9")
	if err := s.UpsertInstrument(ctx, r.ID, empty); err != nil {
		t.Fatal(err)
	}
	noBook := canonical.Quote{InstrumentID: empty.ID, FetchedAt: t0}
	noBook.Quality.Add(canonical.QualityNoBook, "", "404 from /book")
	if err := s.InsertQuote(ctx, r.ID, noBook); err != nil {
		t.Fatalf("InsertQuote no book: %v", err)
	}
	gotNB, err := s.GetQuote(ctx, r.ID, empty.ID)
	if err != nil || gotNB.BestBid.Valid || gotNB.Mid.Valid || !gotNB.Quality.Has(canonical.QualityNoBook) ||
		len(gotNB.YesBids) != 0 || gotNB.VenueTS != nil {
		t.Errorf("no-book quote = %+v %v", gotNB, err)
	}
	list, err := s.ListQuotes(ctx, r.ID)
	if err != nil || len(list) != 2 || list[0].InstrumentID != in.ID {
		t.Errorf("ListQuotes = %v %v", list, err)
	}
	bad := q
	bad.YesAsks = []canonical.Level{{Price: d("1.5"), Size: d("1")}}
	if err := s.InsertQuote(ctx, r.ID, bad); err == nil {
		t.Error("price > 1 accepted")
	}
	if err := s.InsertQuote(ctx, r.ID, canonical.Quote{InstrumentID: "kalshi:UNKNOWN", FetchedAt: t0}); err == nil {
		t.Error("quote for an unknown instrument accepted")
	}
	if _, err := s.GetQuote(ctx, r.ID, "kalshi:NOPE"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetQuote missing = %v", err)
	}
}

func seedPair(t *testing.T, s *store.Store) (store.Round, canonical.Instrument, canonical.Instrument) {
	t.Helper()
	r := mustRound(t, s)
	a, b := instrument("kalshi:KXBTCD-A"), instrument("polymarket:777")
	for _, in := range []canonical.Instrument{a, b} {
		if err := s.UpsertInstrument(ctx, r.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	return r, a, b
}

func TestRelationships(t *testing.T) {
	s := newStore(t)
	r, a, b := seedPair(t, s)
	rel := canonical.Relationship{
		RoundID: r.ID, AID: a.ID, BID: b.ID, Type: canonical.RelEquivalent, Confidence: d("0.97"),
		Method:      canonical.MethodRules,
		Diff:        []canonical.FieldDiff{{Field: "subject", A: "BTC", B: "BTC", Status: canonical.DiffMatch}},
		FactsUsed:   []canonical.FactUse{{Key: "fed_upper_bound", Value: "4.25", Source: "federalreserve.gov", AsOf: t0}},
		BasisRisk:   &canonical.BasisRisk{Score: d("0.05"), Components: []canonical.BasisComponent{{Name: "cutoff_gap", Score: d("0.05"), Detail: "1h"}}},
		Explanation: "Same subject, comparator, threshold and deadline.",
	}
	id, err := s.UpsertRelationship(ctx, rel)
	if err != nil {
		t.Fatalf("UpsertRelationship: %v", err)
	}
	got, err := s.GetRelationship(ctx, id)
	if err != nil {
		t.Fatalf("GetRelationship: %v", err)
	}
	rel.ID = id
	rel.Vetoes = []canonical.Veto{} // stored as [] when nil
	if !reflect.DeepEqual(got, rel) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, rel)
	}
	// Re-running match replaces the row for the same (round, a, b).
	rel.Type, rel.BasisRisk, rel.Vetoes = canonical.RelRelated, nil, []canonical.Veto{{Code: "deadline_gap", Field: "deadline", Detail: "36h apart"}}
	id2, err := s.UpsertRelationship(ctx, rel)
	if err != nil || id2 != id {
		t.Fatalf("re-upsert = %d %v, want id %d", id2, err, id)
	}
	got, _ = s.GetRelationship(ctx, id)
	if got.Type != canonical.RelRelated || got.BasisRisk != nil || len(got.Vetoes) != 1 {
		t.Errorf("after re-upsert = %+v", got)
	}
	for _, tc := range []struct {
		f    store.RelationshipFilter
		want int
	}{
		{store.RelationshipFilter{RoundID: r.ID}, 1},
		{store.RelationshipFilter{RoundID: r.ID, Type: canonical.RelRelated}, 1},
		{store.RelationshipFilter{RoundID: r.ID, Type: canonical.RelEquivalent}, 0},
		{store.RelationshipFilter{RoundID: r.ID, InstrumentID: b.ID}, 1},
		{store.RelationshipFilter{RoundID: r.ID, InstrumentID: "kalshi:OTHER"}, 0},
		{store.RelationshipFilter{RoundID: r.ID + 1}, 0},
	} {
		got, err := s.ListRelationships(ctx, tc.f)
		if err != nil || len(got) != tc.want {
			t.Errorf("ListRelationships(%+v) = %d %v, want %d", tc.f, len(got), err, tc.want)
		}
	}
	rel.Explanation = ""
	if _, err := s.UpsertRelationship(ctx, rel); err == nil || !strings.Contains(err.Error(), "explanation") {
		t.Errorf("empty explanation = %v", err)
	}
	rel.Explanation, rel.BID = "x", "polymarket:404"
	if _, err := s.UpsertRelationship(ctx, rel); err == nil {
		t.Error("relationship to an unknown instrument accepted")
	}
	if _, err := s.GetRelationship(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetRelationship missing = %v", err)
	}
}

func TestRoutingDecisions(t *testing.T) {
	s := newStore(t)
	r, a, _ := seedPair(t, s)
	qty := d("100")
	dec := canonical.Decision{
		RoundID: r.ID, TargetID: a.ID,
		Order:       canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty, Type: canonical.OrderMarket, TIF: canonical.TIFIOC},
		Chosen:      []canonical.Leg{{InstrumentID: a.ID, Venue: canonical.VenueKalshi, Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: qty, VWAP: d("0.47"), AllInPerContract: d("0.4875"), Route: "direct"}},
		Exclusions:  []canonical.Exclusion{{InstrumentID: "polymarket:777", Venue: canonical.VenuePolymarket, Reason: "stale"}},
		Explanation: "Kalshi fills all 100 at 0.47 plus fees.",
		DecidedAt:   t0,
	}
	id, err := s.InsertRoutingDecision(ctx, dec)
	if err != nil {
		t.Fatalf("InsertRoutingDecision: %v", err)
	}
	got, err := s.GetRoutingDecision(ctx, id)
	if err != nil {
		t.Fatalf("GetRoutingDecision: %v", err)
	}
	if got.RoundID != r.ID || !got.CreatedAt.Equal(t0) || got.Decision.Explanation != dec.Explanation ||
		!got.Order.Quantity.Equal(qty) || len(got.Decision.Chosen) != 1 || !got.Decision.Chosen[0].VWAP.Equal(d("0.47")) ||
		got.Decision.DecidedAt.Location() != time.UTC {
		t.Errorf("routing decision = %+v", got)
	}
	if _, err := s.InsertRoutingDecision(ctx, dec); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListRoutingDecisions(ctx, 10)
	if err != nil || len(list) != 2 || list[0].ID <= list[1].ID {
		t.Errorf("ListRoutingDecisions = %v %v", list, err)
	}
	dec.Explanation = " "
	if _, err := s.InsertRoutingDecision(ctx, dec); err == nil {
		t.Error("decision without explanation accepted")
	}
	if _, err := s.GetRoutingDecision(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetRoutingDecision missing = %v", err)
	}

	// UI-25: verify Inputs and TargetType fields round-trip through store
	order := canonical.Order{Side: canonical.SideBuy, Outcome: canonical.OutcomeYes, Quantity: &qty, Type: canonical.OrderMarket, TIF: canonical.TIFIOC}
	decWithInputs := canonical.Decision{
		RoundID:     r.ID,
		TargetID:    a.ID,
		TargetType:  canonical.RelEquivalent,
		Order:       order,
		Chosen:      []canonical.Leg{},
		DecidedAt:   t0,
		Explanation: "test with inputs",
		Inputs: []canonical.DecisionInput{
			{
				InstrumentID:     a.ID,
				Venue:            canonical.VenueKalshi,
				RelationshipType: "self",
				Confidence:       d("1.0"),
				OutcomeSide:      canonical.OutcomeYes,
				BestAsk:          d("0.52"),
				AskDepth: []canonical.Level{
					{Price: d("0.52"), Size: d("100")},
				},
				FetchedAt: t0,
				RoundID:   r.ID,
			},
			{
				InstrumentID:     "polymarket:456",
				Venue:            canonical.VenuePolymarket,
				RelationshipType: canonical.RelEquivalent,
				Confidence:       d("0.95"),
				OutcomeSide:      canonical.OutcomeYes,
				BestAsk:          d("0.50"),
				AskDepth: []canonical.Level{
					{Price: d("0.50"), Size: d("50")},
					{Price: d("0.53"), Size: d("100")},
				},
				FetchedAt: t0,
				RoundID:   r.ID,
			},
		},
	}
	idWithInputs, err := s.InsertRoutingDecision(ctx, decWithInputs)
	if err != nil {
		t.Fatalf("InsertRoutingDecision with inputs: %v", err)
	}
	gotWithInputs, err := s.GetRoutingDecision(ctx, idWithInputs)
	if err != nil {
		t.Fatalf("GetRoutingDecision with inputs: %v", err)
	}
	if gotWithInputs.Decision.TargetType != canonical.RelEquivalent {
		t.Errorf("TargetType = %q, want equivalent", gotWithInputs.Decision.TargetType)
	}
	if len(gotWithInputs.Decision.Inputs) != 2 {
		t.Errorf("inputs count = %d, want 2", len(gotWithInputs.Decision.Inputs))
	}
	if len(gotWithInputs.Decision.Inputs) > 0 {
		inp := gotWithInputs.Decision.Inputs[0]
		if inp.InstrumentID != a.ID || inp.RelationshipType != "self" || !inp.Confidence.Equal(d("1.0")) || len(inp.AskDepth) != 1 {
			t.Errorf("input[0] = %+v", inp)
		}
	}
	if len(gotWithInputs.Decision.Inputs) > 1 {
		inp := gotWithInputs.Decision.Inputs[1]
		if inp.InstrumentID != "polymarket:456" || inp.RelationshipType != canonical.RelEquivalent || len(inp.AskDepth) != 2 {
			t.Errorf("input[1] = %+v", inp)
		}
	}
	// Verify ListRoutingDecisions also round-trips Inputs
	listWithInputs, err := s.ListRoutingDecisions(ctx, 10)
	if err != nil {
		t.Fatalf("ListRoutingDecisions: %v", err)
	}
	var foundWithInputs bool
	for _, rd := range listWithInputs {
		if rd.ID == idWithInputs {
			foundWithInputs = true
			if len(rd.Decision.Inputs) != 2 {
				t.Errorf("ListRoutingDecisions inputs count = %d, want 2", len(rd.Decision.Inputs))
			}
			if rd.Decision.TargetType != canonical.RelEquivalent {
				t.Errorf("ListRoutingDecisions TargetType = %q, want equivalent", rd.Decision.TargetType)
			}
			break
		}
	}
	if !foundWithInputs {
		t.Error("decision with inputs not found in ListRoutingDecisions")
	}
}

func TestDecisionLog(t *testing.T) {
	s := newStore(t)
	id1, err := s.AppendLog(ctx, store.LogIngest, "round:1", "Round 1 ok: kalshi 10, polymarket 8", map[string]int{"kalshi": 10}, t0)
	if err != nil {
		t.Fatalf("AppendLog: %v", err)
	}
	id2, err := s.AppendLog(ctx, store.LogRelationship, "rel:5", "equivalent: same terms", nil, t0.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.GetLog(ctx, id1)
	if err != nil {
		t.Fatalf("GetLog: %v", err)
	}
	var payload map[string]int
	if err := json.Unmarshal(e.Payload, &payload); err != nil || payload["kalshi"] != 10 || e.Kind != store.LogIngest ||
		e.Ref != "round:1" || !e.CreatedAt.Equal(t0) {
		t.Errorf("entry = %+v (%v)", e, err)
	}
	e2, _ := s.GetLog(ctx, id2)
	if string(e2.Payload) != "{}" {
		t.Errorf("nil payload stored as %s", e2.Payload)
	}
	all, err := s.ListLog(ctx, store.LogFilter{})
	if err != nil || len(all) != 2 || all[0].ID != id2 {
		t.Errorf("ListLog = %v %v", all, err)
	}
	for _, tc := range []struct {
		f    store.LogFilter
		want int
	}{
		{store.LogFilter{Kind: store.LogIngest}, 1},
		{store.LogFilter{Ref: "rel:5"}, 1},
		{store.LogFilter{Kind: store.LogRouting}, 0},
		{store.LogFilter{Limit: 1}, 1},
	} {
		got, err := s.ListLog(ctx, tc.f)
		if err != nil || len(got) != tc.want {
			t.Errorf("ListLog(%+v) = %d %v, want %d", tc.f, len(got), err, tc.want)
		}
	}
	if _, err := s.AppendLog(ctx, "debug", "", "x", nil, t0); err == nil {
		t.Error("invalid kind accepted")
	}
	if _, err := s.AppendLog(ctx, store.LogLLM, "", "  ", nil, t0); err == nil {
		t.Error("blank summary accepted")
	}
	if _, err := s.AppendLog(ctx, store.LogLLM, "", "x", func() {}, t0); err == nil {
		t.Error("unencodable payload accepted")
	}
	if _, err := s.GetLog(ctx, 999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("GetLog missing = %v", err)
	}
}

func TestDailyRoundHistory(t *testing.T) {
	s := newStore(t)

	// Create rounds across multiple days.
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
	yesterday := today.Add(-24 * time.Hour)
	threeDaysAgo := today.Add(-72 * time.Hour)

	// Today: 2 ok, 1 partial
	r1, _ := s.CreateRound(ctx, today)
	_ = s.FinishRound(ctx, r1.ID, store.RoundOK, nil, today.Add(time.Minute))
	r2, _ := s.CreateRound(ctx, today.Add(5*time.Minute))
	_ = s.FinishRound(ctx, r2.ID, store.RoundOK, nil, today.Add(6*time.Minute))
	r3, _ := s.CreateRound(ctx, today.Add(10*time.Minute))
	_ = s.FinishRound(ctx, r3.ID, store.RoundPartial, nil, today.Add(11*time.Minute))

	// Yesterday: 1 failed, 1 ok
	r4, _ := s.CreateRound(ctx, yesterday)
	_ = s.FinishRound(ctx, r4.ID, store.RoundFailed, nil, yesterday.Add(time.Minute))
	r5, _ := s.CreateRound(ctx, yesterday.Add(5*time.Minute))
	_ = s.FinishRound(ctx, r5.ID, store.RoundOK, nil, yesterday.Add(6*time.Minute))

	// Two days ago: no rounds (missing)
	// Three days ago: 1 ok
	r6, _ := s.CreateRound(ctx, threeDaysAgo)
	_ = s.FinishRound(ctx, r6.ID, store.RoundOK, nil, threeDaysAgo.Add(time.Minute))

	// Query 7 days of history.
	history, err := s.DailyRoundHistory(ctx, 7)
	if err != nil {
		t.Fatalf("DailyRoundHistory: %v", err)
	}
	if len(history) != 7 {
		t.Fatalf("len(history) = %d, want 7", len(history))
	}

	// Check today's counts (newest first, so index 0).
	todaySummary := history[0]
	if todaySummary.OK != 2 {
		t.Errorf("today OK = %d, want 2", todaySummary.OK)
	}
	if todaySummary.Partial != 1 {
		t.Errorf("today Partial = %d, want 1", todaySummary.Partial)
	}
	if todaySummary.Failed != 0 {
		t.Errorf("today Failed = %d, want 0", todaySummary.Failed)
	}
	if todaySummary.Missing {
		t.Error("today marked as Missing")
	}

	// Check yesterday's counts.
	yesterdaySummary := history[1]
	if yesterdaySummary.OK != 1 {
		t.Errorf("yesterday OK = %d, want 1", yesterdaySummary.OK)
	}
	if yesterdaySummary.Failed != 1 {
		t.Errorf("yesterday Failed = %d, want 1", yesterdaySummary.Failed)
	}
	if yesterdaySummary.Partial != 0 {
		t.Errorf("yesterday Partial = %d, want 0", yesterdaySummary.Partial)
	}
	if yesterdaySummary.Missing {
		t.Error("yesterday marked as Missing")
	}

	// Check two days ago (no rounds).
	twoDaysAgoSummary := history[2]
	if !twoDaysAgoSummary.Missing {
		t.Error("two days ago not marked as Missing")
	}
	if twoDaysAgoSummary.OK != 0 || twoDaysAgoSummary.Partial != 0 || twoDaysAgoSummary.Failed != 0 {
		t.Errorf("two days ago counts non-zero: %+v", twoDaysAgoSummary)
	}

	// Check three days ago.
	threeDaysAgoSummary := history[3]
	if threeDaysAgoSummary.OK != 1 {
		t.Errorf("three days ago OK = %d, want 1", threeDaysAgoSummary.OK)
	}
	if threeDaysAgoSummary.Missing {
		t.Error("three days ago marked as Missing")
	}

	// Check that the days are in descending order.
	if !history[0].Date.After(history[1].Date) {
		t.Errorf("history not in descending order: %v, %v", history[0].Date, history[1].Date)
	}

	// Test with a larger range.
	history90, err := s.DailyRoundHistory(ctx, 90)
	if err != nil {
		t.Fatalf("DailyRoundHistory(90): %v", err)
	}
	if len(history90) != 90 {
		t.Fatalf("len(history90) = %d, want 90", len(history90))
	}
}
