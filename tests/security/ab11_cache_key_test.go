package security

import (
	"context"
	"log/slog"
	"regexp"
	"testing"

	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/llm"
)

// AB-11: verdict cache poisoning or collision. The key is SHA-256 over both
// instruments' normalised text, the prompt version and the model ID, so a
// different pair, prompt or model never shares an entry; and a cached verdict
// still passes through Apply, so even a poisoned entry cannot upgrade.

const modelID = "claude-haiku-4-5-20251001"

func TestAB11_CacheKeyUniqueness(t *testing.T) {
	hexKey := regexp.MustCompile(`^[0-9a-f]{64}$`)
	rapid.Check(t, func(t *rapid.T) {
		a, b := llm.InjectionCase{Field: "rules", VenueText: "x"}.Instruments()
		a.Title, a.RulesText = rapid.String().Draw(t, "at"), rapid.String().Draw(t, "ar")
		b.Title, b.RulesText = rapid.String().Draw(t, "bt"), rapid.String().Draw(t, "br")
		k := llm.CacheKey(a, b, llm.PromptVersion, modelID)
		if !hexKey.MatchString(k) {
			t.Fatalf("key %q", k)
		}
		// Which instrument carries which text matters (implies is directional).
		if llm.NormalizeText(a.Title+a.RulesText) != llm.NormalizeText(b.Title+b.RulesText) && llm.CacheKey(b, a, llm.PromptVersion, modelID) == k {
			t.Fatal("swapped pair shares the key")
		}
		if llm.CacheKey(a, b, llm.PromptVersion+".1", modelID) == k || llm.CacheKey(a, b, llm.PromptVersion, modelID+"x") == k {
			t.Fatal("prompt version or model change shares the key")
		}
		// Appending text to any field changes the key.
		c := b
		c.RulesText += " more"
		if llm.CacheKey(a, c, llm.PromptVersion, modelID) == k {
			t.Fatal("changed rules text shares the key")
		}
	})
}

func TestAB11_CacheKeyIgnoresPresentation(t *testing.T) {
	a, b := llm.InjectionCase{Field: "rules", VenueText: "x"}.Instruments()
	k := llm.CacheKey(a, b, llm.PromptVersion, modelID)
	b2 := b
	b2.RulesText = "  " + b.RulesText + "\u200B\n"
	b2.ID, b2.Status = "polymarket:other", canonical.StatusClosed
	if llm.CacheKey(a, b2, llm.PromptVersion, modelID) != k {
		t.Error("whitespace, zero-width characters, IDs and status must not change the key")
	}
}

// TestAB11_CachedVerdictNeverUpgrades: a poisoned cache entry saying
// "equivalent" is looked up for a pair the rules call related; Apply still
// discards it.
func TestAB11_CachedVerdictNeverUpgrades(t *testing.T) {
	ctx := context.Background()
	a, b := llm.InjectionCase{Field: "rules", VenueText: "x"}.Instruments()
	key := llm.CacheKey(a, b, llm.PromptVersion, modelID)

	var cache llm.MemoryCache
	poison := canonical.LLMVerdict{Relationship: canonical.RelEquivalent, Confidence: decimalOne(), DifferingFields: []string{}, Evidence: []string{"poisoned"}}
	if err := cache.Put(ctx, key, poison); err != nil {
		t.Fatal(err)
	}
	cached, ok, err := cache.Get(ctx, key)
	if !ok || err != nil {
		t.Fatal("cache miss")
	}
	for _, rulesType := range []canonical.RelationshipType{canonical.RelRelated, canonical.RelUnrelated, canonical.RelComplement, canonical.RelEquivalentGivenFacts} {
		rules := llm.InjectionCase{Rules: rulesType, Field: "rules", VenueText: "x"}.RulesRelationship()
		got := llm.Guard(ctx, slog.New(slog.DiscardHandler), rules, cached)
		if got.Outcome != llm.OutcomeRejectedUpgrade || got.Relationship.Type != rulesType {
			t.Errorf("rules %s: cached equivalent gave %s/%s", rulesType, got.Outcome, got.Relationship.Type)
		}
	}
	// And a cached downgrade is still applied, so the cache is useful.
	rules := llm.InjectionCase{Rules: canonical.RelEquivalent, Field: "rules", VenueText: "x"}.RulesRelationship()
	if got := llm.Apply(rules, cached); got.Type != canonical.RelEquivalent {
		t.Errorf("confirming cache entry gave %s", got.Type)
	}
}
