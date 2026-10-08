package llm

import (
	"context"
	"os"
	"testing"

	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store/testdb"
)

var tdb testdb.DB

func TestMain(m *testing.M) { os.Exit(tdb.Run(m)) }

// TestPGCacheRoundTrip verifies the Postgres-backed cache stores and retrieves verdicts.
func TestPGCacheRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := tdb.Pool(t)
	st := store.New(pool)

	cache := NewPGCache(st, "test-model", "v1")

	a := canonical.Instrument{
		ID:            "kalshi:TEST-A",
		Venue:         canonical.VenueKalshi,
		VenueMarketID: "TEST-A",
		Title:         "Test A",
	}
	b := canonical.Instrument{
		ID:            "polymarket:TEST-B",
		Venue:         canonical.VenuePolymarket,
		VenueMarketID: "TEST-B",
		Title:         "Test B",
	}

	key := CacheKey(a, b, "v1", "test-model")

	// Cache miss
	_, found, err := cache.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("expected cache miss")
	}

	// Store verdict
	verdict := canonical.LLMVerdict{
		Relationship: canonical.RelEquivalent,
		Confidence:   decimal.NewFromFloat(0.9),
	}
	if err := cache.Put(ctx, key, verdict); err != nil {
		t.Fatal(err)
	}

	// Cache hit
	got, found, err := cache.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected cache hit")
	}
	if got.Relationship != verdict.Relationship {
		t.Errorf("relationship: got %s, want %s", got.Relationship, verdict.Relationship)
	}
}
