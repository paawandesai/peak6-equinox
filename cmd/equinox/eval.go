package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"

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

// defaultSummaryFile is where `eval` writes the read-only artifact the web
// eval page renders (EVAL_SUMMARY_FILE default, M2-07). Generated, not
// committed.
const defaultSummaryFile = "config/eval_summary.json"

// runEval reports precision and recall per relationship type on the golden
// set (FR-18, NFR-06, US-06):
//
//	equinox eval [--labels PATH] [--candidates PATH] [--out PATH] [--method METHOD] [--require-labels] [--verbose]
//
// Each labelled pair is re-classified from the instruments in the database
// with the facts in FACTS_FILE, so the figures are for the matcher as built,
// not for what an earlier `match` stored. With --candidates the rules
// verdicts recorded in a candidates file are used instead and no database
// is needed (an offline dry run). An empty labels file exits 0 with a note,
// or 1 with --require-labels.
//
// --method selects which relationships to evaluate: "rules" (default) uses
// only rules-based relationships, "rules+llm" uses relationships verified by
// the LLM (M3-06). The eval reports the delta in precision/recall when the
// LLM is involved.
//
// The same figures are written as JSON to --out (default
// config/eval_summary.json; --out= disables) in the contract the web eval
// page reads (internal/eval/summary.go), so the page never runs the matcher.
func runEval(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	labelsPath := fs.String("labels", defaultLabelsFile, "human-confirmed labels file")
	candidatesPath := fs.String("candidates", "", "use the rules verdicts in this candidates file instead of the database")
	outPath := fs.String("out", defaultSummaryFile, "write the JSON summary artifact here (empty: do not write)")
	method := fs.String("method", "rules", "relationship method to evaluate: rules or rules+llm")
	require := fs.Bool("require-labels", false, "exit non-zero when the labels file has no labels")
	verbose := fs.Bool("verbose", false, "list every labelled pair with its verdict")
	command := "equinox eval"
	if len(args) > 0 {
		command += " " + strings.Join(args, " ")
	}
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("eval takes no positional arguments: %w", errUsage)
	}
	// Validate method flag
	var evalMethod canonical.Method
	switch *method {
	case "rules":
		evalMethod = canonical.MethodRules
	case "rules+llm":
		evalMethod = canonical.MethodRulesLLM
		// AO review: use method-specific default to avoid overwriting rules summary
		if *outPath == defaultSummaryFile {
			*outPath = "config/eval_summary_llm.json"
		}
	default:
		return fmt.Errorf("--method must be 'rules' or 'rules+llm'; got %q: %w", *method, errUsage)
	}
	labels, err := eval.LoadLabels(*labelsPath)
	if err != nil {
		return err
	}
	// AO review: reject --method rules+llm with --candidates (candidates carry rules verdicts only)
	if evalMethod == canonical.MethodRulesLLM && *candidatesPath != "" {
		return fmt.Errorf("--method rules+llm requires --candidates to be empty (candidates carry rules verdicts only): %w", errUsage)
	}
	if len(labels.Labels) == 0 {
		if *require {
			return fmt.Errorf("%s has no confirmed labels (--require-labels): run `equinox golden draft` and confirm labels first", *labelsPath)
		}
		if _, err := fmt.Fprintf(stdout, "%s has no confirmed labels yet; nothing to evaluate.\n"+
			"  run `equinox golden draft`, confirm pairs in the labels file (Tier 3), then re-run `equinox eval`\n", *labelsPath); err != nil {
			return err
		}
		empty := eval.Evaluate(labels, nil, evalMethod)
		// UI-23: hash labels file and rotate previous summary
		labelsHash, _ := eval.HashLabelsFile(*labelsPath)
		prevPath := eval.RotatePreviousSummary(*outPath)
		return writeSummary(stdout, *outPath, empty.Summary(time.Now(), command, labels.RoundID, *labelsPath, 0, labelsHash, prevPath))
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
		predict, err = storePredictor(ctx, st, labels, factSet, evalMethod)
		if err != nil {
			return err
		}
	}
	report := eval.Evaluate(labels, predict, evalMethod)
	if _, err := fmt.Fprint(stdout, report.String()); err != nil {
		return err
	}
	if *verbose {
		if _, err := fmt.Fprint(stdout, report.Detail()); err != nil {
			return err
		}
	}
	// UI-23: hash labels file and rotate previous summary
	labelsHash, _ := eval.HashLabelsFile(*labelsPath)
	prevPath := eval.RotatePreviousSummary(*outPath)
	return writeSummary(stdout, *outPath, report.Summary(time.Now(), command, labels.RoundID, *labelsPath, len(labels.Labels), labelsHash, prevPath))
}

// writeSummary writes the artifact unless path is empty, and says where.
func writeSummary(stdout io.Writer, path string, s eval.Summary) error {
	if path == "" {
		return nil
	}
	if err := eval.WriteSummary(path, s); err != nil {
		return err
	}
	_, err := fmt.Fprintf(stdout, "summary written to %s\n", path)
	return err
}

// candidatesPredictor answers from the verdicts a draft recorded.
func candidatesPredictor(c eval.Candidates) eval.Predictor {
	suggested := c.Suggestions()
	return func(aID, bID string) (canonical.RelationshipType, decimal.Decimal, bool) {
		t, ok := suggested[eval.PairKey(aID, bID)]
		// Candidates don't store confidence; use 1.0 as default
		return t, decimal.NewFromInt(1), ok
	}
}

// storePredictor loads every labelled instrument once, normalises it and
// classifies pairs on demand. When method is MethodRules, it classifies with
// the rules on demand like `equinox match` does. When method is MethodRulesLLM,
// it reads stored relationships from the database that were LLM-verified.
func storePredictor(ctx context.Context, st *store.Store, labels eval.Labels, fs facts.Set, method canonical.Method) (eval.Predictor, error) {
	if method == canonical.MethodRulesLLM {
		// For rules+llm, we read relationships from the database that have
		// method='rules+llm', and fall back to rules-only for pairs without
		// LLM processing (downgrade-only: the LLM can only downgrade, so pairs
		// without LLM verdicts keep their rules verdict).
		roundID := labels.RoundID
		if roundID == 0 {
			rd, err := st.CurrentRound(ctx)
			if err != nil {
				return nil, fmt.Errorf("getting current round for rules+llm eval: %w", err)
			}
			roundID = rd.ID
		}
		rels, err := st.ListRelationships(ctx, store.RelationshipFilter{RoundID: roundID, Limit: 10000})
		if err != nil {
			return nil, fmt.Errorf("loading relationships for rules+llm eval: %w", err)
		}
		// AO review: warn if we hit the limit (larger rounds would silently fall back to rules)
		if len(rels) == 10000 {
			return nil, fmt.Errorf("round %d has 10000+ relationships; increase ListRelationships limit in eval.go", roundID)
		}
		// Build a map of (aID, bID) -> type for rules+llm relationships
		llmMap := make(map[string]canonical.RelationshipType)
		for _, r := range rels {
			if r.Method == canonical.MethodRulesLLM {
				key := pairKey(r.AID, r.BID)
				llmMap[key] = r.Type
			}
		}
		// Load labelled instruments for rules fallback
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
			return nil, fmt.Errorf("loading labelled instruments for rules fallback: %w", err)
		}
		instruments := make(map[string]canonical.Instrument, len(records))
		for id, r := range records {
			instruments[id] = normalize.Normalize(r.Instrument)
		}
		return func(aID, bID string) (canonical.RelationshipType, decimal.Decimal, bool) {
			key := pairKey(aID, bID)
			// Check rules+llm first
			if t, ok := llmMap[key]; ok {
				// LLM-verified relationships don't store confidence separately; use 1.0
				return t, decimal.NewFromInt(1), true
			}
			// Fall back to rules
			a, okA := instruments[aID]
			b, okB := instruments[bID]
			if !okA || !okB {
				return "", decimal.Zero, false
			}
			if venueRank(a.Venue) > venueRank(b.Venue) || (a.Venue == b.Venue && a.ID > b.ID) {
				a, b = b, a
			}
			rel := match.Compare(a, b, fs)
			return rel.Type, rel.Confidence, true
		}, nil
	}

	// For rules-only, classify on demand
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
	return func(aID, bID string) (canonical.RelationshipType, decimal.Decimal, bool) {
		a, okA := instruments[aID]
		b, okB := instruments[bID]
		if !okA || !okB {
			return "", decimal.Zero, false
		}
		if venueRank(a.Venue) > venueRank(b.Venue) || (a.Venue == b.Venue && a.ID > b.ID) {
			a, b = b, a
		}
		rel := match.Compare(a, b, fs)
		return rel.Type, rel.Confidence, true
	}, nil
}

// pairKey returns a canonical key for an (a, b) pair, with a and b in
// lexicographic order so pairKey(x, y) == pairKey(y, x).
func pairKey(aID, bID string) string {
	if aID > bID {
		aID, bID = bID, aID
	}
	return aID + "|" + bID
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
