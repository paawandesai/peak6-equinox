package venues

import (
	"fmt"
	"sort"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Registry holds at most one Adapter per venue. The zero value is not usable;
// create one with NewRegistry. It is not safe for concurrent Register calls;
// build it once at startup and then only read it.
type Registry struct {
	adapters map[canonical.Venue]Adapter
}

// NewRegistry returns a registry holding the given adapters.
func NewRegistry(adapters ...Adapter) (*Registry, error) {
	r := &Registry{adapters: make(map[canonical.Venue]Adapter, len(adapters))}
	for _, a := range adapters {
		if err := r.Register(a); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// Register adds a, rejecting a nil adapter, an unsupported venue, or a second
// adapter for the same venue.
func (r *Registry) Register(a Adapter) error {
	if a == nil {
		return fmt.Errorf("registering adapter: nil adapter")
	}
	v := a.Venue()
	if !v.Valid() {
		return fmt.Errorf("registering adapter: unsupported venue %q", v)
	}
	if _, dup := r.adapters[v]; dup {
		return fmt.Errorf("registering adapter: venue %q registered twice", v)
	}
	r.adapters[v] = a
	return nil
}

// Get returns the adapter for v, if registered.
func (r *Registry) Get(v canonical.Venue) (Adapter, bool) {
	a, ok := r.adapters[v]
	return a, ok
}

// Adapters returns every registered adapter, ordered by venue name.
func (r *Registry) Adapters() []Adapter {
	out := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Venue() < out[j].Venue() })
	return out
}

// Len returns the number of registered adapters.
func (r *Registry) Len() int { return len(r.adapters) }
