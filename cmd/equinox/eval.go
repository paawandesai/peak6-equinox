package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/eval"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/facts"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/normalize"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func init() { register("eval", runEval) }

// defaultLabelsFile is the human-owned golden labels file (architecture.md).
const defaultLabelsFile = "testdata/golden/labels.yaml"

// runEval reports precision and recall per relationship type on the golden
// set, rules only (FR-18, NFR-06, US-06; rules+LLM is M3):
//
//	equinox eval [--labels PATH] [--candidates PATH] [--require-labels] [--verbose]
//
// Each labelled pair is re-classified from the instruments in the database
// with the facts in FACTS_FILE, so the figures are for the matcher as built,
// not for what an earlier `match` stored. With --candidates the rules
// verdicts recorded in a candidates file are used instead and no database
// is needed (an offline dry run). An empty labels file exits 0 with a note,
// or 1 with --require-labels.
func runEval(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	labelsPath := fs.String("labels", defaultLabelsFile, "human-confirmed labels file")
	candidatesPath := fs.String("candidates", "", "use the rules verdicts in this candidates file instead of the database")
	require := fs.Bool("require-labels", false, "exit non-zero when the labels file has no labels")
	verbose := fs.Bool("verbose", false, "list every labelled pair with its verdict")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("eval takes no positional arguments: %w", errUsage)
	}
	labels, err := eval.LoadLabels(*labelsPath)
	if err != nil {
		return err
	}
	if len(labels.Labels) == 0 {
		if *require {
			return fmt.Errorf("%s has no confirmed labels (--require-labels): run `equinox golden draft` and confirm labels first", *labelsPath)
		}
		_, err = fmt.Fprintf(stdout, "%s has no confirmed labels yet; nothing to evaluate.\n"+
			"  run `equinox golden draft`, confirm pairs in the labels file (Tier 3), then re-run `equinox eval`\n", *labelsPath)
		return err
	}
	var predict eval.Predictor
	if *candidatesPath != "" {
		cands, err := eval.LoadCandidates(*candidatesPath)
		if err != nil {
			return err
		}
		predict = candidatesPredictor(cands)
	} else {
		cfg, err := config.FromEnv()
		if err != nil {
			return err
		}
		factSet, err := facts.Load(cfg.FactsFile)
		if err != nil {
			return fmt.Errorf("eval needs the facts file: %w", err)
		}
		st, err := store.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer st.Close()
		predict, err = storePredictor(ctx, st, labels, factSet)
		if err != nil {
			return err
		}
	}
	report := eval.Evaluate(labels, predict)
	if _, err := fmt.Fprint(stdout, report.String()); err != nil {
		return err
	}
	if *verbose {
		_, err = fmt.Fprint(stdout, report.Detail())
	}
	return err
}

// candidatesPredictor answers from the verdicts a draft recorded.
func candidatesPredictor(c eval.Candidates) eval.Predictor {
	suggested := c.Suggestions()
	return func(aID, bID string) (canonical.RelationshipType, bool) {
		t, ok := suggested[eval.PairKey(aID, bID)]
		return t, ok
	}
}

// storePredictor loads every labelled instrument once, normalises it and
// classifies pairs on demand with the rules, A on the first venue in
// canonical order as `equinox match` does.
func storePredictor(ctx context.Context, st *store.Store, labels eval.Labels, fs facts.Set) (eval.Predictor, error) {
	idSet := map[string]struct{}{}
	for _, l := range labels.Labels {
		idSet[l.AID] = struct{}{}
		idSet[l.BID] = struct{}{}
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	records, err := st.GetInstruments(ctx, ids)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("loading labelled instruments: %w", err)
	}
	instruments := make(map[string]canonical.Instrument, len(records))
	for id, r := range records {
		instruments[id] = normalize.Normalize(r.Instrument)
	}
	return func(aID, bID string) (canonical.RelationshipType, bool) {
		a, okA := instruments[aID]
		b, okB := instruments[bID]
		if !okA || !okB {
			return "", false
		}
		if venueRank(a.Venue) > venueRank(b.Venue) || (a.Venue == b.Venue && a.ID > b.ID) {
			a, b = b, a
		}
		return match.Compare(a, b, fs).Type, true
	}, nil
}

// venueRank is a venue's position in canonical.Venues().
func venueRank(v canonical.Venue) int {
	for i, w := range canonical.Venues() {
		if w == v {
			return i
		}
	}
	return len(canonical.Venues())
}
