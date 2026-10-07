package eval

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

var draftNow = time.Date(2026, 10, 7, 5, 0, 0, 123, time.UTC)

// mkPair builds a synthetic pair: Kalshi instrument i against Polymarket
// instrument j with the given rules verdict.
func mkPair(i, j int, typ canonical.RelationshipType, conf string, opts ...func(*canonical.Relationship)) Pair {
	a := canonical.Instrument{ID: canonical.InstrumentID(canonical.VenueKalshi, fmt.Sprintf("K%d", i)), Venue: canonical.VenueKalshi, Category: canonical.CategoryCrypto, Title: fmt.Sprintf("K%d", i)}
	b := canonical.Instrument{ID: canonical.InstrumentID(canonical.VenuePolymarket, fmt.Sprintf("%d", j)), Venue: canonical.VenuePolymarket, Category: canonical.CategoryCrypto, Title: fmt.Sprintf("P%d", j)}
	rel := canonical.Relationship{AID: a.ID, BID: b.ID, Type: typ, Confidence: decimal.RequireFromString(conf), Method: canonical.MethodRules,
		Explanation: string(typ) + ": synthetic", Vetoes: []canonical.Veto{}, Diff: []canonical.FieldDiff{}}
	for _, o := range opts {
		o(&rel)
	}
	return Pair{A: a, B: b, Rel: rel}
}

func withVetoes(n int) func(*canonical.Relationship) {
	return func(r *canonical.Relationship) {
		for k := 0; k < n; k++ {
			r.Vetoes = append(r.Vetoes, canonical.Veto{Code: fmt.Sprintf("v%d", k), Field: "f", Detail: "d"})
		}
	}
}

func withThresholdDiff(a, b string) func(*canonical.Relationship) {
	status := canonical.DiffDiffer
	if a == b {
		status = canonical.DiffMatch
	}
	return func(r *canonical.Relationship) {
		r.Diff = append(r.Diff, canonical.FieldDiff{Field: "threshold", A: a, B: b, Status: status})
	}
}

func withMatches(n int) func(*canonical.Relationship) {
	return func(r *canonical.Relationship) {
		for k := 0; k < n; k++ {
			r.Diff = append(r.Diff, canonical.FieldDiff{Field: fmt.Sprintf("m%d", k), A: "x", B: "x", Status: canonical.DiffMatch})
		}
	}
}

func withFact() func(*canonical.Relationship) {
	return func(r *canonical.Relationship) {
		r.FactsUsed = append(r.FactsUsed, canonical.FactUse{Key: "fed_upper_bound", Value: "4.00"})
	}
}

func TestDraftOrdersBucketsAndCapsNegatives(t *testing.T) {
	var pairs []Pair
	// 3 equivalent, 1 given facts, 2 complement, 2 implies, 2 near misses,
	// 12 plain related, 12 unrelated: 34 pairs.
	for i := 0; i < 3; i++ {
		pairs = append(pairs, mkPair(i, i, canonical.RelEquivalent, fmt.Sprintf("0.9%d", i)))
	}
	pairs = append(pairs, mkPair(10, 10, canonical.RelEquivalentGivenFacts, "0.93", withFact()))
	pairs = append(pairs, mkPair(11, 11, canonical.RelComplement, "0.97"), mkPair(12, 12, canonical.RelComplement, "0.95"))
	pairs = append(pairs, mkPair(13, 13, canonical.RelImplies, "0.93"), mkPair(14, 14, canonical.RelImplies, "0.91"))
	pairs = append(pairs, mkPair(20, 20, canonical.RelRelated, "0.5", withThresholdDiff("100", "100")),
		mkPair(21, 21, canonical.RelRelated, "0.5", withFact()))
	for i := 0; i < 12; i++ {
		pairs = append(pairs, mkPair(30+i, 30+i, canonical.RelRelated, "0.5", withVetoes(3-i%3), withThresholdDiff("1", "2")))
		pairs = append(pairs, mkPair(50+i, 50+i, canonical.RelUnrelated, "0.95", withMatches(i%4)))
	}
	c := Draft(2, pairs, 16, draftNow)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.RoundID != 2 || c.Method != canonical.MethodRules || !c.DraftedAt.Equal(draftNow.Truncate(time.Second)) {
		t.Errorf("header = %+v", c)
	}
	if len(c.Candidates) != 16 {
		t.Fatalf("got %d candidates", len(c.Candidates))
	}
	// First turn: 2 equivalent, 1 given facts, 1 complement, 1 implies, 1 near miss, 1 related, 1 unrelated.
	gotTypes := make([]canonical.RelationshipType, 8)
	for i := range gotTypes {
		gotTypes[i] = c.Candidates[i].Suggested
	}
	wantFirst := []canonical.RelationshipType{canonical.RelEquivalent, canonical.RelEquivalent, canonical.RelEquivalentGivenFacts,
		canonical.RelComplement, canonical.RelImplies, canonical.RelRelated, canonical.RelRelated, canonical.RelUnrelated}
	if !reflect.DeepEqual(gotTypes, wantFirst) {
		t.Errorf("first turn = %v, want %v", gotTypes, wantFirst)
	}
	if c.Candidates[0].AID != "kalshi:K2" || c.Candidates[0].Confidence != "0.92" {
		t.Errorf("equivalent pairs not ordered by confidence: %+v", c.Candidates[0])
	}
	if c.Candidates[5].AID != "kalshi:K20" || c.Candidates[5].Why == "" || c.Candidates[5].Why == c.Candidates[6].Why {
		t.Errorf("near miss before plain related: %+v / %+v", c.Candidates[5], c.Candidates[6])
	}
	if c.Candidates[6].Vetoes == nil || len(c.Candidates[6].Vetoes) != 1 {
		t.Errorf("plain related should have the fewest vetoes: %+v", c.Candidates[6])
	}
	if c.Candidates[7].AID != "kalshi:K53" {
		t.Errorf("unrelated should have the most matches: %+v", c.Candidates[7])
	}
	by := c.CountByType()
	// Caps: 16/8 = 2 plain related and 2 unrelated in the round-robin (14
	// picks); the fill pass then takes 2 more plain related. Near misses are
	// uncapped. Unrelated stays at its cap because related fills first.
	if by[canonical.RelUnrelated] != 2 || by[canonical.RelRelated] != 6 || by[canonical.RelEquivalent] != 3 || by[canonical.RelComplement] != 2 || by[canonical.RelImplies] != 2 {
		t.Errorf("by type = %v", by)
	}
	// Given facts carries the fact id and the veto summary format.
	if got := c.Candidates[2]; len(got.Facts) != 1 || got.Facts[0] != "fed_upper_bound" {
		t.Errorf("facts = %+v", got)
	}
	if got := c.Candidates[6].Vetoes[0]; got != "v0 f: d" {
		t.Errorf("veto summary = %q", got)
	}
	// A larger n fills from the capped buckets once the others are empty.
	all := Draft(2, pairs, 40, draftNow)
	if len(all.Candidates) != len(pairs) {
		t.Errorf("n >= pairs should take everything: %d", len(all.Candidates))
	}
	if d := Draft(2, pairs, 0, draftNow); len(d.Candidates) != len(pairs) {
		t.Errorf("n <= 0 means %d: got %d", DefaultDraftSize, len(d.Candidates))
	}
}

func TestDraftPrefersUnseenInstruments(t *testing.T) {
	// One Kalshi strike implies four Polymarket buckets; a second strike implies one.
	pairs := []Pair{
		mkPair(1, 1, canonical.RelImplies, "0.93"), mkPair(1, 2, canonical.RelImplies, "0.93"),
		mkPair(1, 3, canonical.RelImplies, "0.93"), mkPair(1, 4, canonical.RelImplies, "0.93"),
		mkPair(2, 5, canonical.RelImplies, "0.90"),
	}
	c := Draft(1, pairs, 2, draftNow)
	if len(c.Candidates) != 2 || c.Candidates[0].AID != "kalshi:K1" || c.Candidates[1].AID != "kalshi:K2" {
		t.Errorf("draft = %+v", c.Candidates)
	}
	// With room for more, the repeats come after the fresh pair.
	c = Draft(1, pairs, 5, draftNow)
	if len(c.Candidates) != 5 || c.Candidates[1].AID != "kalshi:K2" || c.Candidates[2].BID != "polymarket:2" {
		t.Errorf("draft = %+v", c.Candidates)
	}
}

func TestDraftEmpty(t *testing.T) {
	c := Draft(1, nil, 40, draftNow)
	if len(c.Candidates) != 0 || c.Candidates == nil {
		t.Errorf("empty draft = %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Error(err)
	}
}

// Draft never exceeds n, never duplicates a pair, only returns input pairs,
// takes every pair when n allows, and is deterministic.
func TestDraftProperties(t *testing.T) {
	types := canonical.RelationshipTypes()
	rapid.Check(t, func(rt *rapid.T) {
		count := rapid.IntRange(0, 80).Draw(rt, "count")
		n := rapid.IntRange(1, 50).Draw(rt, "n")
		var pairs []Pair
		for i := 0; i < count; i++ {
			typ := rapid.SampledFrom(types).Draw(rt, "type")
			conf := fmt.Sprintf("0.%02d", rapid.IntRange(0, 99).Draw(rt, "conf"))
			var opts []func(*canonical.Relationship)
			if rapid.Bool().Draw(rt, "fact") {
				opts = append(opts, withFact())
			}
			opts = append(opts, withVetoes(rapid.IntRange(0, 4).Draw(rt, "vetoes")), withMatches(rapid.IntRange(0, 4).Draw(rt, "matches")))
			if rapid.Bool().Draw(rt, "thr") {
				opts = append(opts, withThresholdDiff("100", rapid.SampledFrom([]string{"100", "100.05", "200"}).Draw(rt, "b")))
			}
			pairs = append(pairs, mkPair(rapid.IntRange(0, 9).Draw(rt, "i"), i, typ, conf, opts...))
		}
		c := Draft(5, pairs, n, draftNow)
		if err := c.Validate(); err != nil {
			rt.Fatal(err)
		}
		if len(c.Candidates) > n {
			rt.Fatalf("%d candidates > n %d", len(c.Candidates), n)
		}
		if n >= count && len(c.Candidates) != count {
			rt.Fatalf("n %d >= %d pairs but drafted %d", n, count, len(c.Candidates))
		}
		in := map[string]bool{}
		for _, p := range pairs {
			in[PairKey(p.A.ID, p.B.ID)] = true
		}
		for _, e := range c.Candidates {
			if !in[PairKey(e.AID, e.BID)] {
				rt.Fatalf("candidate %s/%s not in input", e.AID, e.BID)
			}
		}
		if again := Draft(5, pairs, n, draftNow); !reflect.DeepEqual(again, c) {
			rt.Fatal("draft is not deterministic")
		}
	})
}
