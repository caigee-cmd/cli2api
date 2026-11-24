package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

type providerPolicyEnvelope struct {
	Version  int                                `json:"version"`
	Policies map[string]accounts.ProviderPolicy `json:"policies"`
}

// providerPolicyCache holds the exact secret payload, not a decoded envelope,
// so callers cannot mutate shared policy maps. Invalidation happens after a
// successful secret write/delete.
type providerPolicyCache struct {
	mu     sync.Mutex
	loaded bool
	raw    string
	found  bool
}

func (c *providerPolicyCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raw = ""
	c.found = false
	c.loaded = false
}

func (s *Store) providerPolicySecret(ctx context.Context) (string, bool, error) {
	c := &s.providerPolicy
	// Keep the cache lock across the first load so an invalidation from a
	// concurrent SaveProviderPolicy cannot be overwritten by a stale read.
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.loaded {
		return c.raw, c.found, nil
	}
	raw, found, err := s.GetSecret(ctx, accounts.ProviderPolicySecret)
	if err != nil {
		return "", false, err
	}
	c.raw = raw
	c.found = found
	c.loaded = true
	return raw, found, nil
}

func (s *Store) invalidateProviderPolicyCache() {
	s.providerPolicy.invalidate()
}

func (s *Store) readProviderPolicies(ctx context.Context) (providerPolicyEnvelope, error) {
	envelope := providerPolicyEnvelope{Version: 1, Policies: map[string]accounts.ProviderPolicy{}}
	raw, found, err := s.providerPolicySecret(ctx)
	if err != nil {
		return envelope, err
	}
	if !found || raw == "" {
		return envelope, nil
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return envelope, fmt.Errorf("decode provider settings: %w", err)
	}
	if envelope.Policies == nil {
		envelope.Policies = map[string]accounts.ProviderPolicy{}
	}
	return envelope, nil
}

func (s *Store) ListProviderPolicies(ctx context.Context) ([]accounts.ProviderPolicy, error) {
	envelope, err := s.readProviderPolicies(ctx)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(envelope.Policies))
	for key := range envelope.Policies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	list := make([]accounts.ProviderPolicy, 0, len(keys))
	for _, key := range keys {
		list = append(list, envelope.Policies[key])
	}
	return list, nil
}

func (s *Store) GetProviderPolicy(ctx context.Context, provider, region string) (accounts.ProviderPolicy, bool, error) {
	envelope, err := s.readProviderPolicies(ctx)
	if err != nil {
		return accounts.ProviderPolicy{}, false, err
	}
	policy, ok := envelope.Policies[accounts.ProviderPolicyKey(provider, region)]
	return policy, ok, nil
}

func (s *Store) SaveProviderPolicy(ctx context.Context, policy accounts.ProviderPolicy) error {
	normalized, err := accounts.NormalizeProviderPolicy(policy)
	if err != nil {
		return err
	}
	envelope, err := s.readProviderPolicies(ctx)
	if err != nil {
		return err
	}
	envelope.Version = 1
	envelope.Policies[accounts.ProviderPolicyKey(normalized.Provider, normalized.Region)] = normalized
	raw, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode provider settings: %w", err)
	}
	return s.SetSecret(ctx, accounts.ProviderPolicySecret, string(raw))
}
