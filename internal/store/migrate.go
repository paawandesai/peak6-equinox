package store

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"labs.gauntletai.com/paawandesai/peak6-equinox/migrations"
)

// Migration actions accepted by Migrate.
const (
	MigrateUp     = "up"     // apply every pending migration
	MigrateDown   = "down"   // roll back the latest migration
	MigrateReset  = "reset"  // roll back every migration
	MigrateStatus = "status" // print each migration's state
)

// Migrate runs a goose action against dsn using the embedded migrations and
// writes one line per migration to out. A Postgres session lock, keyed by the
// target schema, serialises concurrent runs against the same schema.
func Migrate(ctx context.Context, dsn, action string, out io.Writer) error {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("parsing database url: %w", err)
	}
	db := stdlib.OpenDB(*cfg)
	defer func() { _ = db.Close() }()
	return migrateDB(ctx, db, action, out)
}

func migrateDB(ctx context.Context, db *sql.DB, action string, out io.Writer) error {
	var schema string
	if err := db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		return fmt.Errorf("reading current schema: %w", err)
	}
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(schemaLockID(schema)))
	if err != nil {
		return fmt.Errorf("creating migration lock: %w", err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("loading migrations: %w", err)
	}
	var results []*goose.MigrationResult
	switch action {
	case MigrateUp:
		results, err = p.Up(ctx)
	case MigrateDown:
		var r *goose.MigrationResult
		if r, err = p.Down(ctx); r != nil {
			results = []*goose.MigrationResult{r}
		}
	case MigrateReset:
		results, err = p.DownTo(ctx, 0)
	case MigrateStatus:
		return printStatus(ctx, p, out)
	default:
		return fmt.Errorf("unknown migrate action %q (want up, down, reset or status)", action)
	}
	for _, r := range results {
		_, _ = fmt.Fprintln(out, r.String())
	}
	if err != nil {
		return fmt.Errorf("migrate %s: %w", action, err)
	}
	if len(results) == 0 {
		_, _ = fmt.Fprintf(out, "migrate %s: nothing to do\n", action)
	}
	return nil
}

func printStatus(ctx context.Context, p *goose.Provider, out io.Writer) error {
	statuses, err := p.Status(ctx)
	if err != nil {
		return fmt.Errorf("migrate status: %w", err)
	}
	// goose records applied times as server-local timestamps without a zone,
	// so only the state is printed.
	for _, s := range statuses {
		_, _ = fmt.Fprintf(out, "%-8s %s\n", s.State, s.Source.Path)
	}
	return nil
}

// schemaLockID derives the advisory-lock ID for migrating schema, so runs on
// different schemas of one database (such as parallel test packages) do not
// wait on each other.
func schemaLockID(schema string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("equinox-migrate:" + schema))
	return int64(h.Sum64() & (1<<63 - 1)) //nolint:gosec // masked to 63 bits, cannot overflow
}
