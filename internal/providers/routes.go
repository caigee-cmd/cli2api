package providers

import "sync"

// RouteTarget is one executable candidate for a public model ID.
type RouteTarget struct {
	PublicModel  string
	Provider     string
	NativeModel  string
	AccountID    string
	Capabilities ModelCapabilities
}

// ModelRouteRegistry is the in-memory public-model routing table. It is
// rebuilt from per-account catalogs; SQLite gains no route table.
type ModelRouteRegistry struct {
	mu      sync.RWMutex
	targets map[string][]RouteTarget
}

func NewModelRouteRegistry() *ModelRouteRegistry {
	return &ModelRouteRegistry{targets: map[string][]RouteTarget{}}
}

// ReplaceAll atomically swaps the routing table for one provider snapshot.
func (r *ModelRouteRegistry) ReplaceAll(targets []RouteTarget) {
	if r == nil {
		return
	}
	next := make(map[string][]RouteTarget, len(targets))
	for _, target := range targets {
		key := PublicModelID(target.Provider, target.PublicModel)
		next[key] = append(next[key], target)
	}
	r.mu.Lock()
	r.targets = next
	r.mu.Unlock()
}

// Lookup resolves a public model request. providerFilter is set for
// prefixed IDs such as qoder/glm-5.2.
func (r *ModelRouteRegistry) Lookup(publicModel, providerFilter string) []RouteTarget {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []RouteTarget
	for _, target := range r.targets[publicModel] {
		if providerFilter != "" && target.Provider != providerFilter {
			continue
		}
		out = append(out, target)
	}
	return out
}

// Providers returns the provider families serving one public model.
func (r *ModelRouteRegistry) Providers(publicModel string) []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]struct{}{}
	var out []string
	for _, target := range r.targets[publicModel] {
		if _, ok := seen[target.Provider]; ok {
			continue
		}
		seen[target.Provider] = struct{}{}
		out = append(out, target.Provider)
	}
	return out
}

// PublicModelID keeps model IDs exact. Bare IDs stay bare; provider-prefixed
// IDs keep their prefix so qoder/glm-5.2 never collapses into glm-5.2.
func PublicModelID(provider, model string) string {
	return model
}
