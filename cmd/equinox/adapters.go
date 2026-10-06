package main

import (
	"fmt"
	"log/slog"
	"sort"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// adapterBuilder constructs one venue's adapter from the configuration.
type adapterBuilder func(cfg config.Config, logger *slog.Logger) (venues.Adapter, error)

// adapterBuilders is filled during init by cmd/equinox/venue_<venue>.go, one
// file per venue (added by M1-04 and M1-05), so adapter tickets never share a
// file. It is only written by registerAdapter during init.
var adapterBuilders = map[canonical.Venue]adapterBuilder{}

// registerAdapter adds a venue's adapter builder. Call it from init().
func registerAdapter(v canonical.Venue, b adapterBuilder) {
	if _, dup := adapterBuilders[v]; dup {
		panic(fmt.Sprintf("equinox: adapter for venue %q registered twice", v))
	}
	adapterBuilders[v] = b
}

// buildRegistry constructs every registered adapter, in venue order.
func buildRegistry(cfg config.Config, logger *slog.Logger) (*venues.Registry, error) {
	names := make([]canonical.Venue, 0, len(adapterBuilders))
	for v := range adapterBuilders {
		names = append(names, v)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	reg, err := venues.NewRegistry()
	if err != nil {
		return nil, err
	}
	for _, v := range names {
		a, err := adapterBuilders[v](cfg, logger)
		if err != nil {
			return nil, fmt.Errorf("building %s adapter: %w", v, err)
		}
		if err := reg.Register(a); err != nil {
			return nil, err
		}
	}
	return reg, nil
}
