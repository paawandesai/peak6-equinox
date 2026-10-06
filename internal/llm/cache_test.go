package llm

import (
	"context"
	"regexp"
	"testing"
	"time"

	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

var hexSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestCacheKey(t *testing.T) {
	a, b := InjectionCase{Field: "rules", VenueText: "x"}.Instruments()
	base := CacheKey(a, b, PromptVersion, "claude-haiku-4-5-20251001")
	if !hexSHA256.MatchString(base) {
		t.Fatalf("key %q is not hex SHA-256", base)
	}
	if CacheKey(a, b, PromptVersion, "claude-haiku-4-5-20251001") != base {
		t.Error("key must be deterministic")
	}
	variants := map[string]string{
		"model":   CacheKey(a, b, PromptVersion, "other-model"),
		"version": CacheKey(a, b, "v2", "claude-haiku-4-5-20251001"),
		"order":   CacheKey(b, a, PromptVersion, "claude-haiku-4-5-20251001"),
	}
	b2 := b
	b2.RulesText += " extra clause"
	variants["rules text"] = CacheKey(a, b2, PromptVersion, "claude-haiku-4-5-20251001")
	a2 := a
	a2.Title = "Different title"
	variants["title"] = CacheKey(a2, b, PromptVersion, "claude-haiku-4-5-20251001")
	seen := map[string]string{base: "base"}
	for name, k := range variants {
		if prev, dup := seen[k]; dup {
			t.Errorf("%s key collides with %s", name, prev)
		}
		seen[k] = name
	}

	// Only text, prompt version and model matter: IDs, times and terms don't.
	a3 := a
	a3.ID, a3.CloseTime, a3.Quality = "kalshi:OTHER", a.CloseTime.Add(24*time.Hour), canonical.Quality{{Flag: "x"}}
	if CacheKey(a3, b, PromptVersion, "claude-haiku-4-5-20251001") != base {
		t.Error("non-text fields must not change the key")
	}
	// Normalisation: case, whitespace and zero-width characters don't matter.
	a4 := a
	a4.Title = "  BITCOIN   price\tabove $120,000 on Oct 31, 2026 at 12pm\u200B EDT? "
	if CacheKey(a4, b, PromptVersion, "claude-haiku-4-5-20251001") != base {
		t.Error("normalised text must share the key")
	}
	// Moving a boundary between fields must not collide (length prefixes).
	a5, b5 := a, b
	a5.Title, a5.RulesText = a.Title+" "+a.RulesText[:10], a.RulesText[10:]
	if CacheKey(a5, b5, PromptVersion, "claude-haiku-4-5-20251001") == base {
		t.Error("field boundary shift collided")
	}
}

func TestNormalizeText(t *testing.T) {
	cases := map[string]string{
		"  Hello   World ":       "hello world",
		"a\u200Bb\tc\nd":         "ab c d",
		"ＢＴＣ ＞ １２０ｋ":             "btc > 120k",
		"":                       "",
		"\u202E reversed \u202C": "reversed",
	}
	for in, want := range cases {
		if got := NormalizeText(in); got != want {
			t.Errorf("NormalizeText(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestCacheKeyProperty: keys are hex SHA-256; equal inputs give equal keys;
// a change to any text, the version or the model gives a different key.
func TestCacheKeyProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a, b := InjectionCase{Field: "rules", VenueText: "x"}.Instruments()
		a.Title = rapid.String().Draw(t, "at")
		a.RulesText = rapid.String().Draw(t, "ar")
		b.Title = rapid.String().Draw(t, "bt")
		b.RulesText = rapid.String().Draw(t, "br")
		ver := rapid.StringMatching(`v[0-9]{1,3}`).Draw(t, "ver")
		model := rapid.StringMatching(`[a-z0-9-]{1,20}`).Draw(t, "model")
		k := CacheKey(a, b, ver, model)
		if !hexSHA256.MatchString(k) || CacheKey(a, b, ver, model) != k {
			t.Fatalf("key %q", k)
		}
		if CacheKey(a, b, ver+"x", model) == k || CacheKey(a, b, ver, model+"x") == k {
			t.Fatal("version or model change kept the key")
		}
		b2 := b
		b2.RulesText += " z"
		if NormalizeText(b2.RulesText) != NormalizeText(b.RulesText) && CacheKey(a, b2, ver, model) == k {
			t.Fatal("text change kept the key")
		}
	})
}

func TestMemoryCache(t *testing.T) {
	ctx := context.Background()
	var c MemoryCache
	if _, ok, err := c.Get(ctx, "k"); ok || err != nil {
		t.Fatal("empty cache must miss")
	}
	v := verdict(canonical.RelRelated, "0.5")
	if err := c.Put(ctx, "k", v); err != nil {
		t.Fatal(err)
	}
	got, ok, err := c.Get(ctx, "k")
	if !ok || err != nil || got.Relationship != v.Relationship {
		t.Fatalf("Get = %v %v %v", got, ok, err)
	}
	_ = c.Put(ctx, "k", verdict(canonical.RelUnrelated, "0.1"))
	if got, _, _ := c.Get(ctx, "k"); got.Relationship != canonical.RelUnrelated {
		t.Error("Put must replace")
	}
}
