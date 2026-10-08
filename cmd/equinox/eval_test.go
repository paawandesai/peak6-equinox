package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/eval"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

const (
	fixtureLabels     = "../../testdata/golden/testdata/labels_small.yaml"
	fixtureCandidates = "../../testdata/golden/testdata/candidates_small.yaml"
)

func TestEvalOfflineFromCandidates(t *testing.T) {
	out := filepath.Join(t.TempDir(), "www", "eval_summary.json")
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"eval", "--labels", fixtureLabels, "--candidates", fixtureCandidates, "--verbose", "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{"golden set (rules): 6 labels evaluated, 4 correct (accuracy 0.6667), 1 not evaluated",
		"equivalent                      2         2    1    1    1    0.5000    0.5000",
		"BELOW target 0.95 (NFR-06)", "rules+llm: not evaluated (M3)",
		"not evaluated: kalshi:FX-A7 / polymarket:1007", "MISS label=equivalent rules=related kalshi:FX-A2 / polymarket:1002",
		"summary written to " + out} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
	// The artifact round-trips through the page's contract.
	sum, err := eval.LoadSummary(out)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Labels != 7 || sum.RoundID != 7 || sum.Method != "rules" || sum.LabelsFile != fixtureLabels || sum.GeneratedAt.IsZero() ||
		!strings.HasPrefix(sum.Command, "equinox eval --labels ") || len(sum.Types) != 6 || sum.Types[0].TP != 1 || sum.Types[0].Precision == nil {
		t.Errorf("summary = %+v", sum)
	}
	// --out= writes nothing.
	stdout.Reset()
	if code := run(context.Background(), []string{"eval", "--labels", fixtureLabels, "--candidates", fixtureCandidates, "--out="}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "summary written") {
		t.Errorf("--out= still wrote: %s", stdout.String())
	}
	if _, err := os.Stat(defaultSummaryFile); err == nil {
		t.Errorf("default artifact %s written by tests", defaultSummaryFile)
	}
}

func TestEvalEmptyLabels(t *testing.T) {
	// An empty labels file in the shipped schema (the real labels.yaml now
	// holds human-confirmed labels, Q-7).
	emptyLabels := filepath.Join(t.TempDir(), "labels.yaml")
	if err := os.WriteFile(emptyLabels, []byte("version: 1\nround_id: 0\nlabels: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "eval_summary.json")
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"eval", "--labels", emptyLabels, "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "has no confirmed labels yet; nothing to evaluate") || !strings.Contains(stdout.String(), "summary written to "+out) {
		t.Errorf("stdout = %s", stdout.String())
	}
	// The page still gets an honest artifact: zero labels, every ratio null, a note.
	sum, err := eval.LoadSummary(out)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Labels != 0 || len(sum.Types) != 6 || sum.Types[0].Precision != nil || !strings.Contains(strings.Join(sum.Notes, "\n"), "no confirmed labels yet") {
		t.Errorf("summary = %+v", sum)
	}
	// --require-labels fails before anything is written.
	out2 := filepath.Join(t.TempDir(), "eval_summary.json")
	stdout.Reset()
	if code := run(context.Background(), []string{"eval", "--labels", emptyLabels, "--require-labels", "--out", out2}, &stdout, &stderr); code != 1 {
		t.Errorf("exit %d with --require-labels, want 1", code)
	}
	if !strings.Contains(stderr.String(), "has no confirmed labels (--require-labels)") || stdout.Len() != 0 {
		t.Errorf("stderr = %s, stdout = %s", stderr.String(), stdout.String())
	}
	if _, err := os.Stat(out2); err == nil {
		t.Error("artifact written despite --require-labels failure")
	}
}

func TestEvalErrors(t *testing.T) {
	for _, args := range [][]string{{"eval", "extra"}, {"eval", "--bogus"}} {
		if code := run(context.Background(), args, io.Discard, io.Discard); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{"eval", "--labels", filepath.Join(t.TempDir(), "none.yaml")}, io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "loading labels") {
		t.Errorf("missing labels: exit %d, %s", code, stderr.String())
	}
	stderr.Reset()
	if code := run(context.Background(), []string{"eval", "--labels", fixtureLabels, "--candidates", filepath.Join(t.TempDir(), "none.yaml")}, io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "loading candidates") {
		t.Errorf("missing candidates: exit %d, %s", code, stderr.String())
	}
	stderr.Reset()
	bad := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(bad, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(context.Background(), []string{"eval", "--labels", fixtureLabels, "--candidates", fixtureCandidates, "--out", filepath.Join(bad, "x.json")}, io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "writing eval summary") {
		t.Errorf("unwritable out: exit %d, %s", code, stderr.String())
	}
}

func TestEvalFromStoreReclassifiesLabelledPairs(t *testing.T) {
	st := store.New(tdb.Pool(t))
	tdb.Reset(t)
	seedMatchRound(t, st, store.RoundOK)
	setEvalEnv(t)
	labels := filepath.Join(t.TempDir(), "labels.yaml")
	body := `version: 1
round_id: 1
labels:
  - a_id: kalshi:KXBTCD-26OCT0618-T76599.99
    b_id: polymarket:1001
    label: equivalent
    confirmed_by: test
    confirmed_on: 2026-10-07
  # written Polymarket first: the matcher still sees Kalshi as A
  - a_id: polymarket:1002
    b_id: kalshi:KXBTCD-26OCT0618-T76599.99
    label: related
    confirmed_by: test
    confirmed_on: 2026-10-07
  # the human disagrees with the rules here: a precision miss for implies
  - a_id: kalshi:KXBTCD-26OCT0618-T76699.99
    b_id: polymarket:1001
    label: related
    confirmed_by: test
    confirmed_on: 2026-10-07
  # not in the store
  - a_id: kalshi:KXBTCD-26OCT0618-T76599.99
    b_id: polymarket:9999
    label: unrelated
    confirmed_by: test
    confirmed_on: 2026-10-07
`
	if err := os.WriteFile(labels, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	out := filepath.Join(t.TempDir(), "eval_summary.json")
	if code := run(context.Background(), []string{"eval", "--labels", labels, "--verbose", "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{"3 labels evaluated, 2 correct (accuracy 0.6667), 1 not evaluated",
		"equivalent                      1         1    1    0    0    1.0000    1.0000",
		"implies                         0         1    0    1    0    0.0000       n/a",
		"related                         2         1    1    0    1    1.0000    0.5000",
		"equivalent precision 1.0000 on 1 predictions: target >= 0.95 met (NFR-06)",
		"not evaluated: kalshi:KXBTCD-26OCT0618-T76599.99 / polymarket:9999",
		"ok   label=related rules=related polymarket:1002 / kalshi:KXBTCD-26OCT0618-T76599.99",
		"MISS label=related rules=implies kalshi:KXBTCD-26OCT0618-T76699.99 / polymarket:1001"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
	sum, err := eval.LoadSummary(out)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Labels != 4 || sum.RoundID != 1 || sum.Types[0].Type != "equivalent" || sum.Types[0].Support != 1 || sum.Types[0].TP != 1 ||
		!strings.Contains(strings.Join(sum.Notes, "\n"), "meets the 0.95 target") || !strings.Contains(strings.Join(sum.Notes, "\n"), "polymarket:9999") {
		t.Errorf("summary = %+v", sum)
	}
}
