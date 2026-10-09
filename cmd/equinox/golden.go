package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/eval"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func init() { register("golden", runGolden) }

// defaultCandidatesFile is where `golden draft` writes, relative to the repo root.
const defaultCandidatesFile = "testdata/golden/candidates.yaml"

// runGolden manages the golden set (FR-18, D-023):
//
//	equinox golden draft [--round N] [--n 40] [--out testdata/golden/candidates.yaml]
//
// draft compares every cross-venue pair of a round with the rules (as
// `equinox match` does, without storing anything) and proposes about 40
// pairs for the human to label: every equivalent-tier, complement and
// implies verdict first, then near misses, then a few hard negatives
// (internal/eval/draft.go). It writes candidates.yaml and never writes
// labels.yaml, which the human owns (Tier 3).
func runGolden(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: golden draft [--round N] [--n N] [--out PATH]: %w", errUsage)
	}
	switch args[0] {
	case "draft":
		return runGoldenDraft(ctx, args[1:], stdout)
	}
	return fmt.Errorf("unknown golden subcommand %q (want draft): %w", args[0], errUsage)
}

func runGoldenDraft(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("golden draft", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	roundID := fs.Int64("round", 0, "round to draft from (default: the current round)")
	n := fs.Int("n", eval.DefaultDraftSize, "how many candidates to propose")
	out := fs.String("out", defaultCandidatesFile, "candidates file to write (never labels.yaml)")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("golden draft takes no positional arguments: %w", errUsage)
	}
	if *n <= 0 {
		return fmt.Errorf("--n must be positive: %w", errUsage)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel, ReplaceAttr: utcTime}))
	factSet, err := facts.Load(cfg.FactsFile)
	if err != nil {
		return fmt.Errorf("golden draft needs the facts file: %w", err)
	}
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	cands, pairs, err := draftRound(ctx, st, *roundID, *n, factSet, time.Now())
	if err != nil {
		return err
	}
	if err := eval.WriteCandidates(*out, cands); err != nil {
		return err
	}
	by := cands.CountByType()
	logger.Info("golden draft", "round", cands.RoundID, "pairs", pairs, "candidates", len(cands.Candidates), "out", *out)
	_, err = fmt.Fprintf(stdout, "round %d: %d cross-venue pairs compared, %d candidates written to %s\n"+
		"  equivalent %d, equivalent_given_facts %d, complement %d, complement_given_facts %d, implies %d, related %d, unrelated %d\n"+
		"  next: the human confirms each pair's type in testdata/golden/labels.yaml (Tier 3), then run `equinox eval`\n",
		cands.RoundID, pairs, len(cands.Candidates), *out,
		by[canonical.RelEquivalent], by[canonical.RelEquivalentGivenFacts], by[canonical.RelComplement], by[canonical.RelComplementGivenFacts], by[canonical.RelImplies],
		by[canonical.RelRelated], by[canonical.RelUnrelated])
	return err
}

// draftRound compares every cross-venue pair of a round and drafts n
// candidates. It stores nothing. The second result is the number of pairs
// compared.
func draftRound(ctx context.Context, st *store.Store, roundID int64, n int, fs facts.Set, now time.Time) (eval.Candidates, int, error) {
	round, err := resolveRound(ctx, st, roundID)
	if err != nil {
		return eval.Candidates{}, 0, err
	}
	instruments, err := roundInstruments(ctx, st, round.ID)
	if err != nil {
		return eval.Candidates{}, 0, err
	}
	var pairs []eval.Pair
	for _, p := range crossVenuePairs(instruments) {
		if err := ctx.Err(); err != nil {
			return eval.Candidates{}, 0, fmt.Errorf("drafting round %d: %w", round.ID, err)
		}
		rel := match.Compare(p.a, p.b, fs)
		rel.RoundID = round.ID
		pairs = append(pairs, eval.Pair{A: p.a, B: p.b, Rel: rel})
	}
	return eval.Draft(round.ID, pairs, n, now), len(pairs), nil
}
