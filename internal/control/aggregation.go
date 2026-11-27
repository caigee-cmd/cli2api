package control

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// ErrAggregationNotFound is re-exported for the console layer's convenience;
// it is the same sentinel used by accounts/store.
var ErrAggregationNotFound = accounts.ErrAggregationNotFound

// AggregationStore is the narrow persistence surface the aggregation facade
// needs. *store.Store satisfies it via the AccountStore interface; keeping it
// narrow lets tests use a minimal stub.
type AggregationStore interface {
	InsertAggregation(ctx context.Context, agg accounts.Aggregation) (accounts.Aggregation, error)
	SaveAggregation(ctx context.Context, agg accounts.Aggregation) (accounts.Aggregation, error)
	ListAggregations(ctx context.Context) ([]accounts.Aggregation, error)
	GetAggregation(ctx context.Context, id string) (accounts.Aggregation, error)
	DeleteAggregation(ctx context.Context, id string) error
}

// AggregationMemberValidator checks that every configured member is a model
// the selected provider policy currently allows. The provider policy is the
// single source of truth, so this validation must run on writes instead of
// trusting the console's cached model list.
type AggregationMemberValidator interface {
	ValidateAggregationMembers(ctx context.Context, members []accounts.AggregationMember) error
}

type Aggregations struct {
	store     AggregationStore
	validator AggregationMemberValidator
	// mu serializes console write operations. This gateway runs as a single
	// process, so a facade mutex is enough to keep Update's read-modify-write
	// from losing a concurrent PATCH without a schema/version migration.
	mu sync.Mutex
	// resolveCache is a copy-on-write snapshot of all aggregations so the hot
	// Resolve path does no SQL. Writes invalidate it under mu. The atomic value
	// stores *aggregationSnapshot; a nil pointer means "not loaded".
	resolveCache atomic.Value
}

type aggregationSnapshot map[string]accounts.Aggregation

func NewAggregations(store AggregationStore, validators ...AggregationMemberValidator) *Aggregations {
	if store == nil {
		return nil
	}
	a := &Aggregations{store: store}
	if len(validators) > 0 {
		a.validator = validators[0]
	}
	return a
}

func (a *Aggregations) List(ctx context.Context) ([]accounts.Aggregation, error) {
	list, err := a.store.ListAggregations(ctx)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []accounts.Aggregation{}
	}
	return list, nil
}

func (a *Aggregations) Get(ctx context.Context, id string) (accounts.Aggregation, error) {
	return a.store.GetAggregation(ctx, id)
}

func (a *Aggregations) Create(ctx context.Context, input accounts.CreateAggregation) (accounts.Aggregation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	prepared, err := accounts.PrepareCreateAggregation(input)
	if err != nil {
		return accounts.Aggregation{}, operationError("invalid_request", err.Error())
	}
	if err := a.validateMembers(ctx, prepared.Members); err != nil {
		return accounts.Aggregation{}, err
	}
	now := time.Now().UTC()
	created, err := a.store.InsertAggregation(ctx, accounts.Aggregation{
		ID:               prepared.ID,
		Name:             prepared.Name,
		Enabled:          prepared.Enabled,
		Strategy:         prepared.Strategy,
		AllowFallbacks:   prepared.AllowFallbacks,
		MaxAttempts:      prepared.MaxAttempts,
		ContextLength:    prepared.ContextLength,
		MaxOutputTokens:  prepared.MaxOutputTokens,
		ReasoningOptions: prepared.ReasoningOptions,
		ReasoningEffort:  prepared.ReasoningEffort,
		Members:          prepared.Members,
		CreatedAt:        now,
		UpdatedAt:        now,
	})
	if err != nil {
		return accounts.Aggregation{}, err
	}
	a.invalidateResolveCache()
	return created, nil
}

func (a *Aggregations) Update(ctx context.Context, id string, input accounts.UpdateAggregation) (accounts.Aggregation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	current, err := a.store.GetAggregation(ctx, id)
	if err != nil {
		if errors.Is(err, accounts.ErrAggregationNotFound) {
			return accounts.Aggregation{}, ErrAggregationNotFound
		}
		return accounts.Aggregation{}, err
	}
	merged, err := accounts.ApplyAggregationUpdate(current, input)
	if err != nil {
		return accounts.Aggregation{}, operationError("invalid_request", err.Error())
	}
	if input.Members != nil {
		if err := a.validateMembers(ctx, merged.Members); err != nil {
			return accounts.Aggregation{}, err
		}
	}
	merged.UpdatedAt = time.Now().UTC()
	updated, err := a.store.SaveAggregation(ctx, merged)
	if err != nil {
		if errors.Is(err, accounts.ErrAggregationNotFound) {
			return accounts.Aggregation{}, ErrAggregationNotFound
		}
		return accounts.Aggregation{}, err
	}
	a.invalidateResolveCache()
	return updated, nil
}

func (a *Aggregations) Delete(ctx context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	err := a.store.DeleteAggregation(ctx, id)
	if errors.Is(err, accounts.ErrAggregationNotFound) {
		return ErrAggregationNotFound
	}
	if err == nil {
		a.invalidateResolveCache()
	}
	return err
}

// validateMembers treats a provider-policy rejection as an invalid request so
// console writes surface a 400 instead of silently persisting a dead member.
func (a *Aggregations) validateMembers(ctx context.Context, members []accounts.AggregationMember) error {
	if a.validator == nil {
		return nil
	}
	if err := a.validator.ValidateAggregationMembers(ctx, members); err != nil {
		return operationError("invalid_request", err.Error())
	}
	return nil
}

// invalidateResolveCache must be called with a.mu held by writers.
func (a *Aggregations) invalidateResolveCache() {
	a.resolveCache.Store((*aggregationSnapshot)(nil))
}

// resolveSnapshot returns the copy-on-write aggregation snapshot used by the
// hot path. It loads from the store once (or after a write invalidates it) and
// never mutates the stored maps/slices afterwards.
func (a *Aggregations) resolveSnapshot(ctx context.Context) (aggregationSnapshot, error) {
	if a == nil || a.store == nil {
		return nil, ErrAggregationNotFound
	}
	if cached, ok := a.resolveCache.Load().(*aggregationSnapshot); ok && cached != nil {
		return *cached, nil
	}
	// Cold path: serialize the load so a burst of first requests cannot stampede
	// the database. Writes use the same mutex, so a load cannot race with a
	// write/invalidation and store a snapshot built from a half-updated state.
	a.mu.Lock()
	defer a.mu.Unlock()
	if cached, ok := a.resolveCache.Load().(*aggregationSnapshot); ok && cached != nil {
		return *cached, nil
	}
	list, err := a.store.ListAggregations(ctx)
	if err != nil {
		return nil, err
	}
	snapshot := make(aggregationSnapshot, len(list))
	for _, agg := range list {
		snapshot[agg.ID] = agg
	}
	stored := aggregationSnapshot(snapshot)
	a.resolveCache.Store(&stored)
	return snapshot, nil
}

// Resolve returns an immutable snapshot of one aggregation's members, already
// filtered by the caller's allowed providers. The executor calls this once per
// request before taking the Pool lock, and the result is read-only. The hot
// path is served from an in-memory snapshot, so normal requests add no SQL.
func (a *Aggregations) Resolve(ctx context.Context, id string, allowedProviders map[string]bool) (accounts.Aggregation, error) {
	snapshot, err := a.resolveSnapshot(ctx)
	if err != nil {
		return accounts.Aggregation{}, err
	}
	agg, ok := snapshot[id]
	if !ok || !agg.Enabled {
		return accounts.Aggregation{}, ErrAggregationNotFound
	}
	members := make([]accounts.AggregationMember, 0, len(agg.Members))
	for _, member := range agg.Members {
		if allowedProviders != nil && !allowedProviders[member.Provider] {
			continue
		}
		members = append(members, member)
	}
	if len(members) == 0 {
		return accounts.Aggregation{}, ErrAggregationNotFound
	}
	// Stable priority order; DoS-safe cap even if a stale row has more members.
	if len(members) > accounts.MaxAggregationMembers {
		members = members[:accounts.MaxAggregationMembers]
	}
	sort.SliceStable(members, func(i, j int) bool {
		if members[i].Priority != members[j].Priority {
			return members[i].Priority < members[j].Priority
		}
		return members[i].ID < members[j].ID
	})
	agg.Members = members
	return agg, nil
}
