package providers

import "sync"

// Registry holds runtime adapters keyed by provider family. Descriptors stay
// static; adapters own upstream protocol behavior.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
}

func NewRegistry() *Registry {
	return &Registry{adapters: map[string]Adapter{}}
}

func (r *Registry) Register(adapter Adapter) {
	if r == nil || adapter.ID == "" {
		return
	}
	r.mu.Lock()
	r.adapters[adapter.ID] = adapter
	r.mu.Unlock()
}

func (r *Registry) Get(providerID string) (Adapter, bool) {
	if r == nil {
		return Adapter{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[providerID]
	return adapter, ok
}
