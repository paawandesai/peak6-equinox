// Package testdb gives each test package its own Postgres schema in
// DATABASE_URL_TEST, migrated to the latest version and dropped afterwards.
//
// Usage, once per test package:
//
//	var tdb testdb.DB
//
//	func TestMain(m *testing.M) { os.Exit(tdb.Run(m)) }
//
//	func TestSomething(t *testing.T) {
//		s := store.New(tdb.Pool(t))
//		tdb.Reset(t) // optional: truncate every table first
//	}
//
// Without DATABASE_URL_TEST, DB tests are skipped locally and fail in CI
// (CI is set), so a missing database can never pass CI silently.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// EnvURL names the variable holding the test database DSN.
const EnvURL = "DATABASE_URL_TEST"

const setupTimeout = 2 * time.Minute

// DB is one test package's schema. The zero value is ready for Run.
type DB struct {
	baseURL string
	schema  string
	dsn     string
	pool    *pgxpool.Pool
	reason  string // why DB tests cannot run, when set
}

// Run creates and migrates the package schema, runs the tests, then drops the
// schema. It returns the exit code for os.Exit.
func (d *DB) Run(m *testing.M) int {
	d.baseURL = os.Getenv(EnvURL)
	if d.baseURL == "" {
		d.reason = EnvURL + " is not set (run make db-up and set it, see docs/environment.md)"
		return m.Run()
	}
	ctx, cancel := context.WithTimeout(context.Background(), setupTimeout)
	defer cancel()
	schema, dsn, err := createSchema(ctx, d.baseURL, packageName())
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "testdb: %v\n", err)
		return 1
	}
	d.schema, d.dsn = schema, dsn
	code := d.setupAndRun(ctx, m)
	dropCtx, dropCancel := context.WithTimeout(context.Background(), setupTimeout)
	defer dropCancel()
	if err := dropSchema(dropCtx, d.baseURL, schema); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "testdb: %v\n", err)
		if code == 0 {
			code = 1
		}
	}
	return code
}

func (d *DB) setupAndRun(ctx context.Context, m *testing.M) int {
	if err := store.Migrate(ctx, d.dsn, store.MigrateUp, io.Discard); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "testdb: migrating schema %s: %v\n", d.schema, err)
		return 1
	}
	pool, err := pgxpool.New(ctx, d.dsn)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "testdb: connecting: %v\n", err)
		return 1
	}
	d.pool = pool
	defer pool.Close()
	return m.Run()
}

// require skips the test (or fails it in CI) when the database is unavailable.
func (d *DB) require(t testing.TB) {
	t.Helper()
	if d.reason == "" && d.pool == nil {
		d.reason = "testdb.DB.Run was not called from TestMain"
	}
	if d.reason == "" {
		return
	}
	if os.Getenv("CI") != "" {
		t.Fatalf("testdb: %s", d.reason)
	}
	t.Skipf("testdb: %s", d.reason)
}

// Pool returns the package's shared pool, whose search_path is the package schema.
func (d *DB) Pool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	d.require(t)
	return d.pool
}

// DSN returns the DSN of the package schema.
func (d *DB) DSN(t testing.TB) string {
	t.Helper()
	d.require(t)
	return d.dsn
}

// Reset truncates every application table and restarts their sequences.
func (d *DB) Reset(t testing.TB) {
	t.Helper()
	d.require(t)
	_, err := d.pool.Exec(context.Background(), `TRUNCATE fetch_rounds, raw_responses, instruments, quotes,
		relationships, llm_verdicts, llm_usage, routing_decisions, decision_log RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("testdb: reset: %v", err)
	}
}

// FreshSchema creates an extra, empty (unmigrated) schema for this test, drops
// it on cleanup, and returns its DSN. Use it for migration tests.
func (d *DB) FreshSchema(t testing.TB) string {
	t.Helper()
	d.require(t)
	ctx := context.Background()
	schema, dsn, err := createSchema(ctx, d.baseURL, packageName()+"_fresh")
	if err != nil {
		t.Fatalf("testdb: %v", err)
	}
	t.Cleanup(func() {
		if err := dropSchema(context.Background(), d.baseURL, schema); err != nil {
			t.Errorf("testdb: %v", err)
		}
	})
	return dsn
}

// packageName derives a short identifier from the test binary name ("store.test").
func packageName() string {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".test")
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
		if b.Len() >= 24 {
			break
		}
	}
	if b.Len() == 0 {
		return "pkg"
	}
	return b.String()
}

// createSchema creates schema test_<label>_<random> and returns it with a DSN
// whose search_path points at it.
func createSchema(ctx context.Context, baseURL, label string) (schema, dsn string, err error) {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", "", fmt.Errorf("random schema suffix: %w", err)
	}
	schema = "test_" + label + "_" + hex.EncodeToString(suffix)
	dsn, err = withSearchPath(baseURL, schema)
	if err != nil {
		return "", "", err
	}
	if err := adminExec(ctx, baseURL, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		return "", "", fmt.Errorf("creating schema %s: %w", schema, err)
	}
	return schema, dsn, nil
}

func dropSchema(ctx context.Context, baseURL, schema string) error {
	if err := adminExec(ctx, baseURL, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
		return fmt.Errorf("dropping schema %s: %w", schema, err)
	}
	return nil
}

// adminExec runs one statement on a short-lived connection to baseURL. The
// statement is built only from a sanitised, generated identifier.
func adminExec(ctx context.Context, baseURL, sql string) error {
	conn, err := pgx.Connect(ctx, baseURL)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", EnvURL, err)
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql)
	return err
}

// withSearchPath adds search_path=<schema> to a URL or keyword/value DSN.
func withSearchPath(baseURL, schema string) (string, error) {
	if !strings.Contains(baseURL, "://") {
		return baseURL + " search_path=" + schema, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parsing %s: invalid URL", EnvURL)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
