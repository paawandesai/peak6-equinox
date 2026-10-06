package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/decisionlog"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/match"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/normalize"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func init() { register("match", runMatch) }

// runMatch classifies every cross-venue pair in a round (FR-26 `match`):
//
//	equinox match [--round N] [--store-all]
//
// Without --round it uses the current round (pinned, else the latest finished
// one). Every pair in the same category is compared; equivalent-tier results
// and near-miss related results are stored and logged (D-059). --store-all
// stores and logs every non-unrelated result instead.
func runMatch(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("match", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	roundID := fs.Int64("round", 0, "round to match (default: the current round)")
	storeAll := fs.Bool("store-all", false, "store and log every non-unrelated result, not only near misses")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("match takes no positional arguments: %w", errUsage)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel, ReplaceAttr: utcTime}))
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	sum, err := matchRound(ctx, st, *roundID, *storeAll, logger)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(stdout, sum.String())
	return err
}

// nearMissRelative is how close two known thresholds must be (relative to the
// larger) for a related pair to count as a near miss worth storing (D-059).
var nearMissRelative = decimal.RequireFromString("0.001")

// matchSummary is what one match run did.
type matchSummary struct {
	RoundID     int64
	Instruments int
	Pairs       int
	ByType      map[canonical.RelationshipType]int
	Stored      int
	Logged      int
	Skipped     int // related results not stored because they are not near misses
	Duration    time.Duration
}

func (s matchSummary) String() string {
	return fmt.Sprintf("round %d: %d instruments, %d cross-venue pairs compared in %s\n"+
		"  equivalent %d, related %d (near misses stored %d, other %d not stored), unrelated %d\n"+
		"  relationships stored %d, decision-log entries %d\n",
		s.RoundID, s.Instruments, s.Pairs, s.Duration.Round(time.Millisecond),
		s.ByType[canonical.RelEquivalent], s.ByType[canonical.RelRelated], s.ByType[canonical.RelRelated]-s.Skipped, s.Skipped,
		s.ByType[canonical.RelUnrelated], s.Stored, s.Logged)
}

// matchRound runs the matcher over one round and persists the results.
func matchRound(ctx context.Context, st *store.Store, roundID int64, storeAll bool, logger *slog.Logger) (matchSummary, error) {
	start := time.Now()
	round, err := resolveRound(ctx, st, roundID)
	if err != nil {
		return matchSummary{}, err
	}
	instruments, err := roundInstruments(ctx, st, round.ID)
	if err != nil {
		return matchSummary{}, err
	}
	sum := matchSummary{RoundID: round.ID, Instruments: len(instruments), ByType: map[canonical.RelationshipType]int{}}
	dlog := decisionlog.New(st)
	for _, p := range crossVenuePairs(instruments) {
		if err := ctx.Err(); err != nil {
			return sum, fmt.Errorf("matching round %d: %w", round.ID, err)
		}
		rel := match.Compare(p.a, p.b, nil)
		rel.RoundID = round.ID
		sum.Pairs++
		sum.ByType[rel.Type]++
		if rel.Type == canonical.RelUnrelated {
			continue
		}
		if rel.Type == canonical.RelRelated && !storeAll && !nearMiss(rel) {
			sum.Skipped++
			continue
		}
		rel.ID, err = st.UpsertRelationship(ctx, rel)
		if err != nil {
			return sum, fmt.Errorf("matching round %d: %w", round.ID, err)
		}
		sum.Stored++
		if _, err := dlog.Relationship(ctx, rel); err != nil {
			return sum, fmt.Errorf("matching round %d: %w", round.ID, err)
		}
		sum.Logged++
		logger.Info("relationship", "round", round.ID, "a", rel.AID, "b", rel.BID, "type", rel.Type,
			"confidence", rel.Confidence.String(), "vetoes", len(rel.Vetoes))
	}
	sum.Duration = time.Since(start)
	logger.Info("match finished", "round", round.ID, "instruments", sum.Instruments, "pairs", sum.Pairs,
		"equivalent", sum.ByType[canonical.RelEquivalent], "related", sum.ByType[canonical.RelRelated],
		"unrelated", sum.ByType[canonical.RelUnrelated], "stored", sum.Stored, "duration_ms", sum.Duration.Milliseconds())
	return sum, nil
}

// resolveRound returns the requested round, or the current one, and refuses a
// round that is still running.
func resolveRound(ctx context.Context, st *store.Store, roundID int64) (store.Round, error) {
	var (
		round store.Round
		err   error
	)
	if roundID > 0 {
		round, err = st.GetRound(ctx, roundID)
	} else {
		round, err = st.CurrentRound(ctx)
		if errors.Is(err, store.ErrNotFound) {
			return store.Round{}, errors.New("no finished round to match; run `equinox ingest` first or pass --round")
		}
	}
	if err != nil {
		return store.Round{}, err
	}
	if round.Status == store.RoundRunning {
		return store.Round{}, fmt.Errorf("round %d is still running", round.ID)
	}
	return round, nil
}

// roundInstruments returns the instruments of a round, normalised: those last
// seen in it plus any that have a quote in it (an older round whose
// instruments were seen again later). Terms are re-extracted so the matcher
// does not depend on what the collector stored.
func roundInstruments(ctx context.Context, st *store.Store, roundID int64) ([]canonical.Instrument, error) {
	seen, err := st.ListInstruments(ctx, store.InstrumentFilter{SeenInRound: roundID, Limit: 10000})
	if err != nil {
		return nil, err
	}
	byID := map[string]canonical.Instrument{}
	for _, r := range seen {
		byID[r.ID] = r.Instrument
	}
	quotes, err := st.ListQuotes(ctx, roundID)
	if err != nil {
		return nil, err
	}
	for _, q := range quotes {
		if _, ok := byID[q.InstrumentID]; ok {
			continue
		}
		r, err := st.GetInstrument(ctx, q.InstrumentID)
		if err != nil {
			return nil, err
		}
		byID[r.ID] = r.Instrument
	}
	out := make([]canonical.Instrument, 0, len(byID))
	for _, in := range byID {
		out = append(out, normalize.Normalize(in))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

type pair struct{ a, b canonical.Instrument }

// crossVenuePairs lists every pair of instruments in the same category on
// different venues, A on the alphabetically first venue, in a stable order.
func crossVenuePairs(instruments []canonical.Instrument) []pair {
	groups := map[canonical.Category]map[canonical.Venue][]canonical.Instrument{}
	for _, in := range instruments {
		if groups[in.Category] == nil {
			groups[in.Category] = map[canonical.Venue][]canonical.Instrument{}
		}
		groups[in.Category][in.Venue] = append(groups[in.Category][in.Venue], in)
	}
	var out []pair
	for _, cat := range canonical.Categories() {
		venues := canonical.Venues() // stable order: kalshi before polymarket
		for i := 0; i < len(venues); i++ {
			for j := i + 1; j < len(venues); j++ {
				for _, a := range groups[cat][venues[i]] {
					for _, b := range groups[cat][venues[j]] {
						out = append(out, pair{a, b})
					}
				}
			}
		}
	}
	return out
}

// nearMiss reports whether a related pair is close enough to be worth a human
// look: the thresholds are known and equal, or within nearMissRelative of each
// other (D-059). Ladder neighbours one strike apart are not near misses.
func nearMiss(rel canonical.Relationship) bool {
	for _, d := range rel.Diff {
		if d.Field != "threshold" {
			continue
		}
		if d.Status == canonical.DiffMatch {
			return true
		}
		if d.Status != canonical.DiffDiffer {
			return false
		}
		a, errA := decimal.NewFromString(d.A)
		b, errB := decimal.NewFromString(d.B)
		if errA != nil || errB != nil {
			return false
		}
		larger := decimal.Max(a.Abs(), b.Abs())
		if larger.IsZero() {
			return true
		}
		return a.Sub(b).Abs().Div(larger).LessThanOrEqual(nearMissRelative)
	}
	return false
}
