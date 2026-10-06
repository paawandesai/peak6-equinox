package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func init() { register("migrate", runMigrate) }

// runMigrate applies the embedded migrations to DATABASE_URL:
//
//	equinox migrate [up|down|reset|status]   (default: up)
//
// reset (roll back everything) is refused when APP_ENV=prod.
func runMigrate(ctx context.Context, args []string, stdout io.Writer) error {
	action := store.MigrateUp
	switch len(args) {
	case 0:
	case 1:
		action = args[0]
	default:
		return fmt.Errorf("migrate takes at most one action (up, down, reset, status): %w", errUsage)
	}
	switch action {
	case store.MigrateUp, store.MigrateDown, store.MigrateReset, store.MigrateStatus:
	default:
		return fmt.Errorf("unknown migrate action %q (want up, down, reset or status): %w", action, errUsage)
	}
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if action == store.MigrateReset && cfg.AppEnv == config.EnvProd {
		return errors.New("migrate reset is refused when APP_ENV=prod")
	}
	return store.Migrate(ctx, cfg.DatabaseURL, action, stdout)
}
