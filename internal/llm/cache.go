package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// CacheKey is the llm_verdicts primary key for a verdict on (a, b): the hex
// SHA-256 of the normalised title and rules of both instruments, the prompt
// version and the model ID (AB-11). The key is order-sensitive because
// "implies" is directional. Each component is length-prefixed, so no two
// different inputs can serialise to the same bytes.
func CacheKey(a, b canonical.Instrument, promptVersion, modelID string) string {
	h := sha256.New()
	for _, part := range []string{
		NormalizeText(a.Title), NormalizeText(a.RulesText),
		NormalizeText(b.Title), NormalizeText(b.RulesText),
		promptVersion, modelID,
	} {
		h.Write([]byte(strconv.Itoa(len(part))))
		h.Write([]byte{':'})
		h.Write([]byte(part))
		h.Write([]byte{';'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// NormalizeText is the text form hashed into the cache key: NFKC, the same
// character filter as Sanitize, lower-cased, with every whitespace run
// collapsed to one space and the ends trimmed. It is not truncated, so two
// texts that differ only after the prompt cap still get different keys.
func NormalizeText(s string) string {
	fields := strings.FieldsFunc(strings.ToLower(stripRunes(norm.NFKC.String(s))), unicode.IsSpace)
	return strings.Join(fields, " ")
}

// VerdictCache stores schema-valid verdicts by CacheKey. A cached verdict is
// still only a verdict: it goes through Apply like a fresh one, so a cache
// entry can never upgrade a relationship (AB-11). The Postgres implementation
// over llm_verdicts arrives with the client in M3.
type VerdictCache interface {
	Get(ctx context.Context, key string) (canonical.LLMVerdict, bool, error)
	Put(ctx context.Context, key string, v canonical.LLMVerdict) error
}

// MemoryCache is an in-memory VerdictCache for tests and offline runs.
type MemoryCache struct {
	mu       sync.Mutex
	verdicts map[string]canonical.LLMVerdict
}

// Get returns the verdict stored under key, if any.
func (c *MemoryCache) Get(_ context.Context, key string) (canonical.LLMVerdict, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.verdicts[key]
	return v, ok, nil
}

// Put stores v under key, replacing any earlier verdict.
func (c *MemoryCache) Put(_ context.Context, key string, v canonical.LLMVerdict) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.verdicts == nil {
		c.verdicts = map[string]canonical.LLMVerdict{}
	}
	c.verdicts[key] = v
	return nil
}
