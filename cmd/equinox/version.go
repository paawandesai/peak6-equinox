package main

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
)

// version is the build version, set with -ldflags "-X main.version=<v>".
var version = "dev"

func init() { register("version", runVersion) }

func runVersion(_ context.Context, _ []string, stdout io.Writer) error {
	_, err := fmt.Fprintf(stdout, "equinox %s\n", buildVersion())
	return err
}

// buildVersion returns version, falling back to the VCS revision embedded by
// the Go toolchain when version was not set at link time.
func buildVersion() string {
	if version != "dev" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return version
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		rev += "-dirty"
	}
	return version + "+" + rev
}
