package decisionlog_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/decisionlog"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store/testdb"
)

var (
	tdb testdb.DB
	ctx = context.Background()
	t0  = time.Date(2026, 10, 6, 22, 0, 0, 0, time.UTC)
)

func TestMain(m *testing.M) { os.Exit(tdb.Run(m)) }

func instrument(id string) canonical.Instrument {
	v := canonical.VenueOf(id)
	terms := canonical.UnknownTerms()
	terms.Subject = "BTC"
	return canonical.Instrument{
		ID: id, Venue: v, VenueMarketID: strings.TrimPrefix(id, string(v)+":"), Category: canonical.CategoryCrypto,
		Title: "BTC above $76,600?", Terms: terms, CloseTime: t0, Status: canonical.StatusOpen,
		FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
	}
}

func storedRelationship(t *testing.T, s *store.Store) canonical.Relationship {
	t.Helper()
	round, err := s.CreateRound(ctx, t0)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"kalshi:A", "polymarket:B"} {
		if err := s.UpsertInstrument(ctx, round.ID, instrument(id)); err != nil {
			t.Fatal(err)
		}
	}
	r := canonical.Relationship{
		RoundID: round.ID, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelRelated,
		Confidence: decimal.RequireFromString("0.8"), Method: canonical.MethodRules,
		Vetoes:      []canonical.Veto{{Code: "threshold_mismatch", Field: "threshold", Detail: "76599.99 vs 76600"}},
		Explanation: "related: same subject BTC, but vetoed on threshold (76599.99 vs 76600).",
	}
	r.ID, err = s.UpsertRelationship(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRelationshipEntryRoundTrip(t *testing.T) {
	s := store.New(tdb.Pool(t))
	tdb.Reset(t)
	r := storedRelationship(t, s)
	at := t0.Add(time.Minute)
	log := decisionlog.New(s).WithClock(func() time.Time { return at })

	id, err := log.Relationship(ctx, r)
	if err != nil {
		t.Fatalf("Relationship: %v", err)
	}
	entries, err := log.ForRelationship(ctx, r.ID)
	if err != nil || len(entries) != 1 || entries[0].ID != id {
		t.Fatalf("ForRelationship = %v, %v", entries, err)
	}
	e := entries[0]
	if e.Kind != store.LogRelationship || e.Ref != decisionlog.RelationshipRef(r.ID) || !e.CreatedAt.Equal(at) {
		t.Errorf("entry = %+v", e)
	}
	for _, want := range []string{"related", "0.8", "rules", "kalshi:A", "polymarket:B", "vetoed on threshold"} {
		if !strings.Contains(e.Summary, want) {
			t.Errorf("summary %q lacks %q", e.Summary, want)
		}
	}
	var p decisionlog.RelationshipPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.RelationshipID != r.ID || p.RoundID != r.RoundID || p.Type != r.Type || len(p.Vetoes) != 1 || p.FactsUsed == nil || p.Explanation != r.Explanation {
		t.Errorf("payload = %+v", p)
	}
	all, err := log.Relationships(ctx, 10)
	if err != nil || len(all) != 1 {
		t.Errorf("Relationships = %v, %v", all, err)
	}
}

func TestRelationshipEntryRejectsUnstored(t *testing.T) {
	s := store.New(tdb.Pool(t))
	tdb.Reset(t)
	log := decisionlog.New(s)
	r := storedRelationship(t, s)
	r.ID = 0
	if _, err := log.Relationship(ctx, r); err == nil || !strings.Contains(err.Error(), "not been stored") {
		t.Errorf("err = %v", err)
	}
	r.ID, r.Explanation = 1, ""
	if _, err := log.Relationship(ctx, r); err == nil || !strings.Contains(err.Error(), "explanation is required") {
		t.Errorf("err = %v", err)
	}
}

// failingStore exercises the error path without a database.
type failingStore struct{ err error }

func (f failingStore) AppendLog(context.Context, store.LogKind, string, string, any, time.Time) (int64, error) {
	return 0, f.err
}

func (f failingStore) ListLog(context.Context, store.LogFilter) ([]store.LogEntry, error) {
	return nil, f.err
}

func TestRelationshipEntryWrapsStoreError(t *testing.T) {
	boom := errors.New("boom")
	log := decisionlog.New(failingStore{boom})
	r := canonical.Relationship{
		ID: 5, RoundID: 1, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalent,
		Confidence: decimal.NewFromInt(1), Method: canonical.MethodRules, Explanation: "x",
	}
	if _, err := log.Relationship(ctx, r); !errors.Is(err, boom) || !strings.Contains(err.Error(), "relationship 5") {
		t.Errorf("err = %v", err)
	}
	if _, err := log.Relationships(ctx, 1); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestSummaryIsCapped(t *testing.T) {
	var got string
	rec := recordingStore{onAppend: func(summary string) { got = summary }}
	log := decisionlog.New(rec)
	r := canonical.Relationship{
		ID: 5, RoundID: 1, AID: "kalshi:A", BID: "polymarket:B", Type: canonical.RelEquivalent,
		Confidence: decimal.NewFromInt(1), Method: canonical.MethodRules, Explanation: strings.Repeat("é", 2000),
	}
	if _, err := log.Relationship(ctx, r); err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(got)); n != 500 || !strings.HasSuffix(got, "…") {
		t.Errorf("summary has %d runes: %q", n, got[:40])
	}
}

type recordingStore struct{ onAppend func(summary string) }

func (r recordingStore) AppendLog(_ context.Context, _ store.LogKind, _, summary string, _ any, _ time.Time) (int64, error) {
	r.onAppend(summary)
	return 1, nil
}

func (recordingStore) ListLog(context.Context, store.LogFilter) ([]store.LogEntry, error) {
	return nil, nil
}
