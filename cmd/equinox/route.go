package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/route"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func init() { register("route", runRoute) }

// maxRoutePartners caps how many relationships one route call loads.
const maxRoutePartners = 1000

// routeArgs are the parsed `equinox route` flags.
type routeArgs struct {
	target string
	round  int64
	json   bool
}

// routeUsage documents the flags (FR-26, FR-27).
const routeUsage = `route --target <instrument-id> --side buy|sell --outcome yes|no (--qty <contracts> | --notional <usd>)
      [--type market|limit] [--limit <price>] [--tif IOC|FOK] [--round <id>] [--json]`

// routeUsageErr marks err as a usage error and appends the route usage line.
func routeUsageErr(err error) error {
	return fmt.Errorf("%w\n  usage: equinox %s: %w", err, routeUsage, errUsage)
}

// parseRouteArgs turns the flags into an Order. --limit implies --type limit.
func parseRouteArgs(args []string) (routeArgs, canonical.Order, error) {
	fs := flag.NewFlagSet("route", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var (
		ra      routeArgs
		side    = fs.String("side", "buy", "buy or sell")
		outcome = fs.String("outcome", "yes", "yes or no")
		qty     = fs.String("qty", "", "quantity in contracts")
		notnl   = fs.String("notional", "", "size in USD, converted at the touch")
		typ     = fs.String("type", "", "market or limit (default market, or limit when --limit is set)")
		lim     = fs.String("limit", "", "limit price in [0,1]")
		tif     = fs.String("tif", string(canonical.TIFIOC), "IOC or FOK")
	)
	fs.StringVar(&ra.target, "target", "", "target instrument id")
	fs.Int64Var(&ra.round, "round", 0, "fetch round id (default: the pinned or latest round)")
	fs.BoolVar(&ra.json, "json", false, "print the decision as JSON")
	if err := fs.Parse(args); err != nil {
		return ra, canonical.Order{}, routeUsageErr(err)
	}
	if fs.NArg() > 0 {
		return ra, canonical.Order{}, routeUsageErr(fmt.Errorf("unexpected argument %q", fs.Arg(0)))
	}
	if err := canonical.ValidateInstrumentID(ra.target); err != nil {
		return ra, canonical.Order{}, routeUsageErr(fmt.Errorf("--target: %w", err))
	}
	o := canonical.Order{
		Side:    canonical.Side(strings.ToLower(*side)),
		Outcome: canonical.Outcome(strings.ToLower(*outcome)),
		Type:    canonical.OrderType(strings.ToLower(*typ)),
		TIF:     canonical.TimeInForce(strings.ToUpper(*tif)),
	}
	var err error
	if o.Quantity, err = optionalDecimal("--qty", *qty); err != nil {
		return ra, canonical.Order{}, err
	}
	if o.Notional, err = optionalDecimal("--notional", *notnl); err != nil {
		return ra, canonical.Order{}, err
	}
	if o.LimitPrice, err = optionalDecimal("--limit", *lim); err != nil {
		return ra, canonical.Order{}, err
	}
	if o.Type == "" {
		o.Type = canonical.OrderMarket
		if o.LimitPrice != nil {
			o.Type = canonical.OrderLimit
		}
	}
	if err := o.Validate(); err != nil {
		return ra, canonical.Order{}, routeUsageErr(fmt.Errorf("order: %w", err))
	}
	return ra, o, nil
}

// optionalDecimal parses a flag value, returning nil when it is empty.
func optionalDecimal(name, v string) (*decimal.Decimal, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	d, err := decimal.NewFromString(strings.TrimSpace(v))
	if err != nil {
		return nil, routeUsageErr(fmt.Errorf("%s: %q is not a decimal", name, v))
	}
	return &d, nil
}

// runRoute simulates one order against the target instrument and the
// instruments related to it in the round, stores the decision, logs it, and
// prints it (FR-12, FR-26). It never places an order anywhere.
func runRoute(ctx context.Context, args []string, stdout io.Writer) error {
	ra, o, err := parseRouteArgs(args)
	if err != nil {
		return err
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

	in, err := loadRouteInputs(ctx, st, logger, ra.target, ra.round)
	if err != nil {
		return err
	}
	rcfg := route.Config{
		RoundID:                in.round.ID,
		MinRouteConfidence:     cfg.MinRouteConfidence,
		BasisPenaltyMultiplier: cfg.BasisPenaltyMult,
		Eligibility: route.EligibilityConfig{
			Snapshot:     in.round.SnapshotAt(),
			StaleAfter:   cfg.StaleAfter,
			SkewWarn:     cfg.SkewWarn,
			SkewMax:      cfg.SkewMax,
			MinBookDepth: cfg.MinBookDepth,
		},
	}
	dec, err := route.Route(o, in.target, in.partners, rcfg, in.targetType, time.Now().UTC())
	if err != nil {
		return err
	}
	id, err := st.InsertRoutingDecision(ctx, dec)
	if err != nil {
		return err
	}
	if _, err := st.AppendLog(ctx, store.LogRouting, strconv.FormatInt(id, 10), logSummary(dec), dec, dec.DecidedAt); err != nil {
		return err
	}
	logger.Info("routing decision stored", "decision_id", id, "round_id", dec.RoundID, "target", dec.TargetID,
		"chosen", len(dec.Chosen), "alternatives", len(dec.Alternatives), "exclusions", len(dec.Exclusions))
	if ra.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(dec)
	}
	// Warn if round data is stale (reviewer fix).
	snapshot := in.round.SnapshotAt()
	if !snapshot.IsZero() && cfg.StaleRoundWarnMinutes > 0 {
		age := time.Now().UTC().Sub(snapshot)
		ageMin := int(age.Minutes())
		if ageMin >= cfg.StaleRoundWarnMinutes {
			fmt.Fprintf(stdout, "WARNING: Data is %d minutes old.\n\n", ageMin)
		}
	}
	return printDecision(stdout, id, dec, snapshot)
}

// routeInputs is everything Route needs from the database.
type routeInputs struct {
	round      store.Round
	target     canonical.Candidate
	partners   []canonical.Candidate
	targetType canonical.RelationshipType // strongest relationship type for UI-25
}

// loadRouteInputs reads the round (pinned or latest unless roundID is given),
// the target with its quote, and every instrument related to the target in
// that round with its quote. A missing quote becomes an empty book flagged
// no_book; a missing partner instrument is logged and skipped.
func loadRouteInputs(ctx context.Context, st *store.Store, logger *slog.Logger, targetID string, roundID int64) (routeInputs, error) {
	var (
		in  routeInputs
		err error
	)
	if roundID > 0 {
		in.round, err = st.GetRound(ctx, roundID)
	} else {
		in.round, err = st.CurrentRound(ctx)
	}
	if err != nil {
		return in, fmt.Errorf("loading round: %w", err)
	}
	in.target, err = loadCandidate(ctx, st, in.round.ID, targetID)
	if err != nil {
		return in, fmt.Errorf("loading target: %w", err)
	}
	rels, err := st.ListRelationships(ctx, store.RelationshipFilter{RoundID: in.round.ID, InstrumentID: targetID, Limit: maxRoutePartners})
	if err != nil {
		return in, err
	}
	for i := range rels {
		r := rels[i]
		otherID := r.BID
		if otherID == targetID {
			otherID = r.AID
		}
		c, err := loadCandidate(ctx, st, in.round.ID, otherID)
		if err != nil {
			logger.Warn("skipping partner", "instrument", otherID, "relationship_id", r.ID, "err", err.Error())
			continue
		}
		c.Relationship = &r
		in.partners = append(in.partners, c)
		// Use the first (strongest per domain.md § Relationships) relationship type for targetType (UI-25).
		if in.targetType == "" {
			in.targetType = r.Type
		}
	}
	// If no partners, default to "equivalent" for targetType.
	// Web uses the pair's rel.Type; CLI has no pair context, so defaults to equivalent.
	if in.targetType == "" {
		in.targetType = canonical.RelEquivalent
	}
	return in, nil
}

// loadCandidate reads an instrument and its quote for the round.
func loadCandidate(ctx context.Context, st *store.Store, roundID int64, id string) (canonical.Candidate, error) {
	rec, err := st.GetInstrument(ctx, id)
	if err != nil {
		return canonical.Candidate{}, err
	}
	c := canonical.Candidate{Instrument: rec.Instrument}
	q, err := st.GetQuote(ctx, roundID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		c.Quote = canonical.Quote{InstrumentID: id, FetchedAt: time.Now().UTC()}
		c.Quote.Quality.Add(canonical.QualityNoBook, "", fmt.Sprintf("no quote in round %d", roundID))
	case err != nil:
		return canonical.Candidate{}, err
	default:
		c.Quote = q.Quote
	}
	return c, nil
}

// logSummary is the one-line decision-log entry for a routing decision.
func logSummary(dec canonical.Decision) string {
	size := "?"
	switch {
	case dec.Order.Quantity != nil:
		size = dec.Order.Quantity.String() + " contracts"
	case dec.Order.Notional != nil:
		size = "$" + dec.Order.Notional.String()
	}
	if len(dec.Chosen) == 0 {
		return fmt.Sprintf("route %s %s %s on %s: no venue can fill (%d excluded)", dec.Order.Side, size, dec.Order.Outcome, dec.TargetID, len(dec.Exclusions))
	}
	l := dec.Chosen[0]
	return fmt.Sprintf("route %s %s %s on %s: %s %s at %s per contract", dec.Order.Side, size, dec.Order.Outcome, dec.TargetID,
		l.Venue, l.InstrumentID, l.AllInPerContract.StringFixed(4))
}

// printDecision renders a decision for a terminal.
func printDecision(w io.Writer, id int64, dec canonical.Decision, snapshot time.Time) error {
	o := dec.Order
	size := ""
	switch {
	case o.Quantity != nil:
		size = o.Quantity.String() + " contracts"
	case o.Notional != nil:
		size = "$" + o.Notional.String() + " notional"
	}
	orderLine := fmt.Sprintf("%s %s %s %s %s", o.Side, size, strings.ToUpper(string(o.Outcome)), o.Type, o.TIF)
	if o.LimitPrice != nil {
		orderLine += " limit " + o.LimitPrice.StringFixed(4)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Routing decision #%d (round %d) for %s at %s\n", id, dec.RoundID, dec.TargetID, dec.DecidedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "As of:    %s\n", snapshot.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Order:    %s\n", orderLine)
	if len(dec.Chosen) == 0 {
		b.WriteString("Chosen:   none\n")
	} else {
		l := dec.Chosen[0]
		fmt.Fprintf(&b, "Chosen:   %s %s (%s): %s contracts, VWAP %s, fee %s, basis %s, all-in %s\n",
			l.Venue, l.InstrumentID, l.Route, l.Quantity, l.VWAP.StringFixed(4), l.FeePerContract.StringFixed(4),
			l.BasisPenaltyPerContract.StringFixed(4), l.AllInPerContract.StringFixed(4))
	}
	if len(dec.Alternatives) > 0 {
		b.WriteString("Ranked:\n")
		for _, alt := range dec.Alternatives {
			l := alt.Legs[0]
			fmt.Fprintf(&b, "  #%d %s %s: all-in %s (VWAP %s + fee %s + basis %s), %s contracts",
				alt.Rank, l.Venue, l.InstrumentID, alt.AllInPerContract.StringFixed(4), l.VWAP.StringFixed(4),
				l.FeePerContract.StringFixed(4), l.BasisPenaltyPerContract.StringFixed(4), l.Quantity)
			if alt.Note != "" {
				fmt.Fprintf(&b, " [%s]", alt.Note)
			}
			b.WriteString("\n")
		}
	}
	if len(dec.Exclusions) > 0 {
		b.WriteString("Excluded:\n")
		for _, ex := range dec.Exclusions {
			fmt.Fprintf(&b, "  %s %s: %s (%s)\n", ex.Venue, ex.InstrumentID, ex.Reason, ex.Detail)
		}
	}
	if len(dec.Warnings) > 0 {
		b.WriteString("Warnings:\n")
		for _, w := range dec.Warnings {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}
	fmt.Fprintf(&b, "Explanation:\n  %s\n", dec.Explanation)
	_, err := io.WriteString(w, b.String())
	return err
}
