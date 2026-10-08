// Command equinox is the Equinox CLI. Subcommands live in their own files and
// register themselves in init(), so adding one never requires editing this file.
//
// Usage: equinox [subcommand] [args...]   (default subcommand: serve)
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"syscall"
	_ "time/tzdata" // Embed timezone database for Cloud Run (M3-08 follow-up)
)

// command runs one subcommand. args excludes the subcommand name.
type command func(ctx context.Context, args []string, stdout io.Writer) error

const defaultCommand = "serve"

// commands is the subcommand registry. It is only written by register during init.
var commands = map[string]command{}

// register adds a subcommand. Call it from init() in the subcommand's file.
func register(name string, fn command) {
	if _, dup := commands[name]; dup {
		panic(fmt.Sprintf("equinox: subcommand %q registered twice", name))
	}
	commands[name] = fn
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// errUsage marks errors caused by bad invocation; run exits 2 for them.
var errUsage = errors.New("usage error")

// run dispatches args to a subcommand and returns the process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	name := defaultCommand
	if len(args) > 0 {
		name, args = args[0], args[1:]
	}
	if err := dispatch(ctx, name, args, stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "equinox %s: %v\n", name, err)
		if errors.Is(err, errUsage) {
			usage(stderr)
			return 2
		}
		return 1
	}
	return 0
}

func dispatch(ctx context.Context, name string, args []string, stdout io.Writer) error {
	if fn, ok := commands[name]; ok {
		return fn(ctx, args, stdout)
	}
	if ticket, ok := planned[name]; ok {
		_, err := fmt.Fprintf(stdout, "equinox %s: not implemented yet (ticket %s)\n", name, ticket)
		return err
	}
	switch name {
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	}
	return fmt.Errorf("unknown subcommand %q: %w", name, errUsage)
}

func usage(w io.Writer) {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	_, _ = fmt.Fprintf(w, "usage: equinox [subcommand] [args...]  (default: %s)\nsubcommands:\n", defaultCommand)
	for _, n := range names {
		_, _ = fmt.Fprintf(w, "  %s\n", n)
	}
}
