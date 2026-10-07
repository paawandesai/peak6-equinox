package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// serveConfig is a config for appEnv against dsn, as serve would load it.
func serveConfig(t *testing.T, appEnv, dsn string) config.Config {
	t.Helper()
	cfg, err := config.Load(func(k string) (string, bool) {
		v, ok := map[string]string{
			"APP_ENV":        appEnv,
			"DATABASE_URL":   dsn,
			"ADMIN_USER":     "admin",
			"ADMIN_PASSWORD": "test-only-password-0123456789",
		}[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	return cfg
}

// migrationStatus returns `equinox migrate status` output for dsn.
func migrationStatus(t *testing.T, dsn string) string {
	t.Helper()
	var out bytes.Buffer
	if err := store.Migrate(context.Background(), dsn, store.MigrateStatus, &out); err != nil {
		t.Fatalf("migrate status: %v", err)
	}
	return out.String()
}

// TestMigrateOnStartupAppliesInStagingAndProd checks that serve migrates a
// fresh database in prod and staging, and that a second start is a no-op.
func TestMigrateOnStartupAppliesInStagingAndProd(t *testing.T) {
	for _, env := range []string{config.EnvStaging, config.EnvProd} {
		t.Run(env, func(t *testing.T) {
			dsn := tdb.FreshSchema(t)
			cfg := serveConfig(t, env, dsn)
			if got := migrationStatus(t, dsn); !strings.Contains(got, "pending  0001_init.sql") {
				t.Fatalf("fresh schema status = %q, want pending", got)
			}
			var logBuf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logBuf, nil))
			if err := migrateOnStartup(context.Background(), cfg, logger); err != nil {
				t.Fatalf("first start: %v", err)
			}
			if got := migrationStatus(t, dsn); !strings.Contains(got, "applied  0001_init.sql") {
				t.Errorf("status after start = %q, want applied", got)
			}
			if !strings.Contains(logBuf.String(), "applying migrations on startup") ||
				!strings.Contains(logBuf.String(), "0001_init.sql") {
				t.Errorf("log = %q, want the start-up line and the migration result", logBuf.String())
			}
			logBuf.Reset()
			if err := migrateOnStartup(context.Background(), cfg, logger); err != nil {
				t.Fatalf("second start: %v", err)
			}
			if !strings.Contains(logBuf.String(), "nothing to do") {
				t.Errorf("second start log = %q, want nothing to do", logBuf.String())
			}
		})
	}
}

// TestMigrateOnStartupSkipsLocalAndCI checks that local and ci runs never
// touch the database on start-up, so tests keep control of their schemas.
func TestMigrateOnStartupSkipsLocalAndCI(t *testing.T) {
	unreachable := "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"
	for _, env := range []string{config.EnvLocal, config.EnvCI} {
		cfg := serveConfig(t, env, unreachable)
		if err := migrateOnStartup(context.Background(), cfg, slog.New(slog.DiscardHandler)); err != nil {
			t.Errorf("%s: migrateOnStartup = %v, want nil (no database access)", env, err)
		}
	}
	for env, want := range map[string]bool{"local": false, "ci": false, "staging": true, "prod": true, "": false} {
		if got := migratesOnStartup(env); got != want {
			t.Errorf("migratesOnStartup(%q) = %v, want %v", env, got, want)
		}
	}
}

// TestServeStagingStopsWhenMigrationFails runs the real subcommand: with an
// unreachable database, a staging start must fail before listening instead
// of serving against an unknown schema.
func TestServeStagingStopsWhenMigrationFails(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	t.Setenv("DATABASE_URL", "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "test-only-password-0123456789")
	t.Setenv("LOG_LEVEL", "error")
	var errOut bytes.Buffer
	if code := run(context.Background(), []string{"serve"}, io.Discard, &errOut); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "migrating on startup") {
		t.Errorf("stderr = %q, want a start-up migration error", errOut.String())
	}
}

func TestMigrationLogWritesOneRecordPerLine(t *testing.T) {
	var buf bytes.Buffer
	w := migrationLog{slog.New(slog.NewTextHandler(&buf, nil))}
	n, err := w.Write([]byte("OK   0001_init.sql (12ms)\nmigrate up: nothing to do\n\n"))
	if err != nil || n != len("OK   0001_init.sql (12ms)\nmigrate up: nothing to do\n\n") {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if got := strings.Count(buf.String(), "msg=migration"); got != 2 {
		t.Errorf("records = %d, want 2 in %q", got, buf.String())
	}
}
