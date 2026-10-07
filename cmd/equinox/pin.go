package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func init() { register("pin", runPin) }

// runPin pins the round the public pages read from (D-063, Q-5a):
//
//	equinox pin <round-id>   pin one ok or partial round
//	equinox pin --clear      unpin, so pages follow the latest finished round
//
// The collector does not run the matcher, so without a pin every Scheduler
// tick moves `/` onto a fresh, unmatched round. Run `equinox match` on a
// round, then pin it (M1-12, D-065).
func runPin(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("pin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	unpinAll := fs.Bool("clear", false, "unpin every round")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%w: %w", err, errUsage)
	}
	var id int64
	switch {
	case *unpinAll && fs.NArg() == 0:
	case !*unpinAll && fs.NArg() == 1:
		n, err := strconv.ParseInt(fs.Arg(0), 10, 64)
		if err != nil || n <= 0 {
			return fmt.Errorf("round id must be a positive integer: %w", errUsage)
		}
		id = n
	default:
		return fmt.Errorf("usage: pin <round-id> | pin --clear: %w", errUsage)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if *unpinAll {
		if err := st.UnpinRounds(ctx); err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, "unpinned: pages follow the latest finished round")
		return err
	}
	if err := st.PinRound(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("round %d not found or not finished ok/partial", id)
		}
		return err
	}
	_, err = fmt.Fprintf(stdout, "pinned round %d\n", id)
	return err
}
