package store_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// dataModelTables lists every table in architecture.md § Data model.
var dataModelTables = []string{
	"fetch_rounds", "raw_responses", "instruments", "quotes", "relationships",
	"llm_verdicts", "llm_usage", "llm_verify_last_run", "routing_decisions", "decision_log",
	"match_runs",
}

func tablesIn(t *testing.T, dsn string) map[string]bool {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	rows, err := conn.Query(context.Background(),
		`SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema()`)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, n := range names {
		out[n] = true
	}
	return out
}

func TestMigrateUpDownCleanly(t *testing.T) {
	dsn := tdb.FreshSchema(t)
	var out bytes.Buffer
	if err := store.Migrate(ctx, dsn, store.MigrateUp, &out); err != nil {
		t.Fatalf("up: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "0001_init.sql") {
		t.Errorf("up output = %q", out.String())
	}
	tables := tablesIn(t, dsn)
	for _, name := range dataModelTables {
		if !tables[name] {
			t.Errorf("table %s missing after up", name)
		}
	}
	out.Reset()
	if err := store.Migrate(ctx, dsn, store.MigrateUp, &out); err != nil || !strings.Contains(out.String(), "nothing to do") {
		t.Errorf("second up = %v %q", err, out.String())
	}
	out.Reset()
	if err := store.Migrate(ctx, dsn, store.MigrateStatus, &out); err != nil || !strings.Contains(out.String(), "applied") {
		t.Errorf("status = %v %q", err, out.String())
	}
	// Reset rolls back all migrations; Down rolls back only one (issue #40).
	if err := store.Migrate(ctx, dsn, store.MigrateReset, &out); err != nil {
		t.Fatalf("reset: %v", err)
	}
	tables = tablesIn(t, dsn)
	for _, name := range dataModelTables {
		if tables[name] {
			t.Errorf("table %s still present after reset", name)
		}
	}
	// Up again after down, then reset.
	if err := store.Migrate(ctx, dsn, store.MigrateUp, &out); err != nil {
		t.Fatalf("up after down: %v", err)
	}
	if err := store.Migrate(ctx, dsn, store.MigrateReset, &out); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if tables := tablesIn(t, dsn); tables["instruments"] {
		t.Error("instruments present after reset")
	}
}

func TestMigrateErrors(t *testing.T) {
	dsn := tdb.FreshSchema(t)
	if err := store.Migrate(ctx, dsn, "sideways", &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "unknown migrate action") {
		t.Errorf("bad action = %v", err)
	}
	if err := store.Migrate(ctx, "::not a dsn::", store.MigrateUp, &bytes.Buffer{}); err == nil {
		t.Error("bad dsn accepted")
	}
	if err := store.Migrate(ctx, dsn, store.MigrateDown, &bytes.Buffer{}); err == nil {
		t.Error("down on an empty schema succeeded")
	}
}

func TestSchemaConstraints(t *testing.T) {
	s := newStore(t)
	pool := tdb.Pool(t)
	r := mustRound(t, s)
	r2 := mustRound(t, s)
	for _, id := range []int64{r.ID, r2.ID} {
		if err := s.FinishRound(ctx, id, store.RoundOK, nil, t0); err != nil {
			t.Fatal(err)
		}
	}
	// At most one pinned round, enforced by the database itself.
	if _, err := pool.Exec(ctx, `UPDATE fetch_rounds SET pinned = true`); err == nil {
		t.Error("two pinned rounds allowed")
	}
	// Prices outside [0,1] are rejected by the database too.
	if err := s.UpsertInstrument(ctx, r.ID, instrument("kalshi:C1")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quotes (round_id, instrument_id, yes_bids, yes_asks, best_bid, fetched_at)
		VALUES ($1, 'kalshi:C1', '[]', '[]', 1.5, now())`, r.ID); err == nil {
		t.Error("best_bid 1.5 accepted")
	}
	if _, err := pool.Exec(ctx, `INSERT INTO llm_verdicts (cache_key, model, prompt_version, verdict) VALUES ('short', 'm', 'v1', '{}')`); err == nil {
		t.Error("non-sha256 cache key accepted")
	}
}

func TestOpen(t *testing.T) {
	dsn := tdb.DSN(t)
	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Ping(ctx); err != nil {
		t.Errorf("Ping: %v", err)
	}
	s.Close()
	if _, err := store.Open(ctx, "postgres://nobody@127.0.0.1:1/none?connect_timeout=1"); err == nil {
		t.Error("Open on a closed port succeeded")
	}
	if _, err := store.Open(ctx, "::not a dsn::"); err == nil {
		t.Error("Open with a bad dsn succeeded")
	}
}
