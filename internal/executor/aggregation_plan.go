package executor

import (
	"context"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
)

// AggregatePlan is an immutable per-request snapshot of one logical model.
// It is resolved once in Prepare (before any Pool lock) and stored in the
// request context. Everything downstream treats it as read-only, so parallel
// requests never share mutable state and configuration updates cannot change
// the candidate set mid-flight.
type AggregatePlan struct {
	ID             string
	Strategy       accounts.AggregationStrategy
	AllowFallbacks bool
	MaxAttempts    int
	// Members already filtered by the caller's provider grants and sorted by
	// priority. Immutable for the lifetime of the request.
	Members []accounts.AggregationMember
	// byProvider is a prebuilt index so resolver lookup is O(1) per item
	// instead of scanning all members for every account (O(N*M)).
	byProvider map[string][]accounts.AggregationMember
}

// NewAggregatePlan builds a plan and its lookup index. The caller must pass a
// snapshot whose Members slice will not be mutated afterwards.
func NewAggregatePlan(agg accounts.Aggregation) *AggregatePlan {
	plan := &AggregatePlan{
		ID:             agg.ID,
		Strategy:       agg.Strategy,
		AllowFallbacks: agg.AllowFallbacks,
		MaxAttempts:    agg.MaxAttempts,
		Members:        agg.Members,
		byProvider:     make(map[string][]accounts.AggregationMember, len(agg.Members)),
	}
	for _, member := range agg.Members {
		key := strings.ToLower(strings.TrimSpace(member.Provider))
		plan.byProvider[key] = append(plan.byProvider[key], member)
	}
	return plan
}

type aggregatePlanContextKey struct{}

// WithAggregatePlan attaches a resolved plan to the context.
func WithAggregatePlan(ctx context.Context, plan *AggregatePlan) context.Context {
	if ctx == nil || plan == nil {
		return ctx
	}
	return context.WithValue(ctx, aggregatePlanContextKey{}, plan)
}

func aggregatePlanFrom(ctx context.Context) *AggregatePlan {
	if ctx == nil {
		return nil
	}
	plan, _ := ctx.Value(aggregatePlanContextKey{}).(*AggregatePlan)
	return plan
}

// resolver returns a pure function that maps an Item to the concrete real
// model it should serve for this plan. The function:
//   - reads only the immutable plan snapshot and the passed-in Item;
//   - never calls Pool methods, the facade, DB, network, or channels;
//   - is O(candidates for provider), typically O(1), with no allocation.
//
// It is safe to invoke while Pool.mu is held. If a member's region filter is
// set, only items in that region are considered; an empty region matches any.
// matchMember returns the member this item resolves to. Shared by resolver,
// priorityResolver and weightResolver so all three agree on the same member.
func (p *AggregatePlan) matchMember(item Item) (accounts.AggregationMember, bool) {
	if p == nil {
		return accounts.AggregationMember{}, false
	}
	candidates := p.byProvider[strings.ToLower(strings.TrimSpace(itemProvider(item)))]
	region := NormalizeRegion(itemRegion(item))
	for _, member := range candidates {
		if member.Region != "" && NormalizeRegion(member.Region) != region {
			continue
		}
		if itemHasModel(item, member.Model) {
			return member, true
		}
	}
	return accounts.AggregationMember{}, false
}

func (p *AggregatePlan) resolver() func(Item) string {
	if p == nil || len(p.Members) == 0 {
		return nil
	}
	return func(item Item) string {
		member, ok := p.matchMember(item)
		if !ok {
			return ""
		}
		return member.Model
	}
}

// priorityResolver exposes the matched member's configured priority so
// PickRoute can order candidates for a "priority" strategy. Lower is higher
// priority; unmatched items get a large value so they sort last.
func (p *AggregatePlan) priorityResolver() func(Item) int {
	if p == nil || len(p.Members) == 0 {
		return nil
	}
	return func(item Item) int {
		member, ok := p.matchMember(item)
		if !ok {
			return 1 << 30
		}
		return member.Priority
	}
}

// weightResolver exposes the matched member's weight for fill-first and
// weighted-round-robin strategies. Falls back to 100 for unmatched items.
func (p *AggregatePlan) weightResolver() func(Item) int {
	if p == nil || len(p.Members) == 0 {
		return nil
	}
	return func(item Item) int {
		member, ok := p.matchMember(item)
		if !ok {
			return 100
		}
		return member.Weight
	}
}
