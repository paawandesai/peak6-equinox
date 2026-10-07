package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/eval"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// setEvalEnv points the CLI at the test database and the repo's facts file
// (tests run with cwd cmd/equinox).
func setEvalEnv(t *testing.T) {
	t.Helper()
	setAppEnv(t, tdb.DSN(t))
	t.Setenv("FACTS_FILE", "../../config/facts.yaml")
	t.Setenv("UNIVERSE_DIR", "../../config/universe")
	t.Setenv("LOG_LEVEL", "error")
}

func TestDraftRoundSelectsEveryVerdictWorthALook(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	ctx := context.Background()
	round := seedMatchRound(t, st, store.RoundOK)
	now := time.Date(2026, 10, 7, 6, 0, 0, 0, time.UTC)

	cands, pairs, err := draftRound(ctx, st, round.ID, 40, testFacts(t), now)
	if err != nil {
		t.Fatalf("draftRound: %v", err)
	}
	// 2 Kalshi × 4 Polymarket crypto pairs, all of them fit in 40.
	if pairs != 8 || len(cands.Candidates) != 8 || cands.RoundID != round.ID || !cands.DraftedAt.Equal(now) {
		t.Fatalf("pairs %d, candidates %d, header %+v", pairs, len(cands.Candidates), cands)
	}
	by := cands.CountByType()
	if by[canonical.RelEquivalent] != 1 || by[canonical.RelImplies] != 1 || by[canonical.RelRelated] != 4 || by[canonical.RelUnrelated] != 2 {
		t.Errorf("by type = %v", by)
	}
	first := cands.Candidates[0]
	if first.Suggested != canonical.RelEquivalent || first.AID != "kalshi:KXBTCD-26OCT0618-T76599.99" || first.BID != "polymarket:1001" ||
		first.ATitle == "" || first.BTitle == "" || first.Explanation == "" || first.Category != canonical.CategoryCrypto {
		t.Errorf("first candidate = %+v", first)
	}
	// With room for three: the equivalent pair, the implication, then the one-cent near miss.
	three, _, err := draftRound(ctx, st, round.ID, 3, testFacts(t), now)
	if err != nil {
		t.Fatal(err)
	}
	var types []canonical.RelationshipType
	for _, c := range three.Candidates {
		types = append(types, c.Suggested)
	}
	if len(types) != 3 || types[0] != canonical.RelEquivalent || types[1] != canonical.RelImplies || types[2] != canonical.RelRelated ||
		three.Candidates[2].BID != "polymarket:1002" || !strings.Contains(three.Candidates[2].Why, "near miss") {
		t.Errorf("three = %v / %+v", types, three.Candidates)
	}
	// Nothing was stored.
	rels, err := st.ListRelationships(ctx, store.RelationshipFilter{RoundID: round.ID})
	if err != nil || len(rels) != 0 {
		t.Errorf("draft stored %d relationships, %v", len(rels), err)
	}
	// A running round is refused, as for match.
	running, err := st.CreateRound(ctx, matchT0.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := draftRound(ctx, st, running.ID, 40, testFacts(t), now); err == nil || !strings.Contains(err.Error(), "still running") {
		t.Errorf("running round: %v", err)
	}
}

func TestGoldenDraftCommandWritesCandidatesOnly(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	round := seedMatchRound(t, st, store.RoundOK)
	setEvalEnv(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "candidates.yaml")

	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"golden", "draft", "--round", itoa64(round.ID), "--n", "5", "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{"8 cross-venue pairs compared, 5 candidates written to " + out, "equivalent 1,", "implies 1,", "labels.yaml (Tier 3)"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
	cands, err := eval.LoadCandidates(out)
	if err != nil || len(cands.Candidates) != 5 || cands.RoundID != round.ID {
		t.Fatalf("written file: %v %+v", err, cands)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("draft wrote other files: %v", entries)
	}
	// Re-running overwrites candidates.yaml in place.
	stdout.Reset()
	if code := run(context.Background(), []string{"golden", "draft", "--round", itoa64(round.ID), "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if cands, err = eval.LoadCandidates(out); err != nil || len(cands.Candidates) != 8 {
		t.Errorf("rewritten file: %v, %d candidates", err, len(cands.Candidates))
	}
	// It never writes labels.yaml, whatever the directory.
	labels := filepath.Join(dir, "labels.yaml")
	stderr.Reset()
	if code := run(context.Background(), []string{"golden", "draft", "--round", itoa64(round.ID), "--out", labels}, &stdout, &stderr); code != 1 {
		t.Errorf("exit %d writing labels.yaml, stderr %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "refusing to write labels.yaml") {
		t.Errorf("stderr = %s", stderr.String())
	}
	if _, err := os.Stat(labels); !errors.Is(err, os.ErrNotExist) {
		t.Error("labels.yaml was written")
	}
}

func TestGoldenUsageErrors(t *testing.T) {
	cases := [][]string{
		{"golden"},
		{"golden", "label"},
		{"golden", "draft", "extra"},
		{"golden", "draft", "--n", "0"},
		{"golden", "draft", "--bogus"},
	}
	for _, args := range cases {
		var stderr bytes.Buffer
		if code := run(context.Background(), args, io.Discard, &stderr); code != 2 {
			t.Errorf("%v: exit %d, want 2 (%s)", args, code, stderr.String())
		}
	}
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
