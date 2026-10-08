package llm

import (
	"context"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// StoreBackend is the subset of store.Store needed for the Postgres verdict
// cache. This interface avoids a circular import (llm cannot import store
// because store uses canonical, which is fine, but we don't want llm to know
// about the full store).
type StoreBackend interface {
	GetLLMVerdict(ctx context.Context, cacheKey string) (canonical.LLMVerdict, bool, error)
	PutLLMVerdict(ctx context.Context, cacheKey, model, promptVersion string, v canonical.LLMVerdict) error
}

// PGCache is a VerdictCache backed by the llm_verdicts table.
type PGCache struct {
	store         StoreBackend
	model         string
	promptVersion string
}

// NewPGCache creates a Postgres-backed verdict cache.
func NewPGCache(store StoreBackend, model, promptVersion string) *PGCache {
	return &PGCache{
		store:         store,
		model:         model,
		promptVersion: promptVersion,
	}
}

// Get retrieves a cached verdict by key.
func (c *PGCache) Get(ctx context.Context, key string) (canonical.LLMVerdict, bool, error) {
	return c.store.GetLLMVerdict(ctx, key)
}

// Put stores a verdict under the given key.
func (c *PGCache) Put(ctx context.Context, key string, v canonical.LLMVerdict) error {
	return c.store.PutLLMVerdict(ctx, key, c.model, c.promptVersion, v)
}
