package accounts

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// AggregationStrategy is the routing strategy for one logical aggregation
// model. It reuses the existing routing strategy vocabulary plus an explicit
// "priority" mode that walks members in their configured order.
type AggregationStrategy string

const (
	AggregationPriority        AggregationStrategy = "priority"
	AggregationRoundRobin      AggregationStrategy = "round-robin"
	AggregationFillFirst       AggregationStrategy = "fill-first"
	AggregationFEFOStrategy    AggregationStrategy = "fefo"
	AggregationLeastQuotaFirst AggregationStrategy = RoutingStrategyLeastQuotaFirst
	AggregationMostQuotaFirst  AggregationStrategy = RoutingStrategyMostQuotaFirst
)

const (
	MaxAggregationMembers = 128
	MaxAggregationNameLen = 128
)

// AggregationMember binds one real model behind a logical model ID.
// Provider/region/model together identify a concrete upstream model. When
// AccountID is set, the member is a deployment pinned to that account; empty
// AccountID keeps the legacy provider-wide pool behavior.
type AggregationMember struct {
	ID        int64  `json:"id,omitempty"`
	Provider  string `json:"provider"`
	Region    string `json:"region,omitempty"`
	Model     string `json:"model"`
	AccountID string `json:"account_id,omitempty"`
	Priority  int    `json:"priority"`
	Weight    int    `json:"weight"`
}

type Aggregation struct {
	ID             string              `json:"id"`
	Name           string              `json:"name"`
	Enabled        bool                `json:"enabled"`
	Strategy       AggregationStrategy `json:"strategy"`
	AllowFallbacks bool                `json:"allow_fallbacks"`
	MaxAttempts    int                 `json:"max_attempts"`
	Members        []AggregationMember `json:"members"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

type CreateAggregation struct {
	ID             string
	Name           string
	Enabled        bool
	Strategy       AggregationStrategy
	AllowFallbacks bool
	MaxAttempts    int
	Members        []AggregationMember
}

type UpdateAggregation struct {
	Name           *string
	Enabled        *bool
	Strategy       *AggregationStrategy
	AllowFallbacks *bool
	MaxAttempts    *int
	Members        *[]AggregationMember
}

// NormalizeAggregationStrategy keeps the per-model strategy inside the four
// supported values. Unknown values fall back to round-robin, the default that
// spreads traffic evenly across the members of a logical model.
func NormalizeAggregationStrategy(value string) AggregationStrategy {
	switch AggregationStrategy(strings.ToLower(strings.TrimSpace(value))) {
	case AggregationRoundRobin:
		return AggregationRoundRobin
	case AggregationFillFirst:
		return AggregationFillFirst
	case AggregationFEFOStrategy:
		return AggregationFEFOStrategy
	case AggregationPriority:
		return AggregationPriority
	case AggregationLeastQuotaFirst:
		return AggregationLeastQuotaFirst
	case AggregationMostQuotaFirst:
		return AggregationMostQuotaFirst
	default:
		return AggregationRoundRobin
	}
}

func normalizeAggregationMembers(members []AggregationMember) ([]AggregationMember, error) {
	if len(members) == 0 {
		return nil, fmt.Errorf("aggregation requires at least one member")
	}
	if len(members) > MaxAggregationMembers {
		return nil, fmt.Errorf("aggregation has too many members (max %d)", MaxAggregationMembers)
	}
	out := make([]AggregationMember, 0, len(members))
	seen := map[string]struct{}{}
	for _, member := range members {
		provider := strings.ToLower(strings.TrimSpace(member.Provider))
		region := strings.ToLower(strings.TrimSpace(member.Region))
		model := strings.TrimSpace(member.Model)
		accountID := strings.TrimSpace(member.AccountID)
		if provider == "" || model == "" {
			return nil, fmt.Errorf("aggregation member requires provider and model")
		}
		if len(accountID) > 256 {
			return nil, fmt.Errorf("aggregation member account id is too long")
		}
		weight := member.Weight
		if weight <= 0 {
			weight = 100
		}
		if weight > 1_000_000 {
			weight = 1_000_000
		}
		priority := member.Priority
		if priority < 0 {
			priority = 0
		}
		member.Provider = provider
		member.Region = region
		member.Model = model
		member.AccountID = accountID
		member.Weight = weight
		member.Priority = priority
		key := provider + "|" + region + "|" + CanonicalModelID(model) + "|" + accountID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, member)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("aggregation requires at least one member")
	}
	// Stable deterministic order; index tie-break keeps the saved order stable.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].Model < out[j].Model
	})
	return out, nil
}

func normalizeAggregationMaxAttempts(value int) int {
	if value <= 0 {
		return 3
	}
	if value > 64 {
		return 64
	}
	return value
}

func PrepareCreateAggregation(input CreateAggregation) (CreateAggregation, error) {
	id := strings.ToLower(strings.TrimSpace(input.ID))
	if id == "" {
		return CreateAggregation{}, fmt.Errorf("aggregation id required")
	}
	if !validAggregationID(id) {
		return CreateAggregation{}, fmt.Errorf("aggregation id may only contain lowercase letters, digits, '.', '-' and '/', got %q", id)
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = id
	}
	if len(name) > MaxAggregationNameLen {
		return CreateAggregation{}, fmt.Errorf("aggregation name too long")
	}
	members, err := normalizeAggregationMembers(input.Members)
	if err != nil {
		return CreateAggregation{}, err
	}
	return CreateAggregation{
		ID:             id,
		Name:           name,
		Enabled:        input.Enabled,
		Strategy:       NormalizeAggregationStrategy(string(input.Strategy)),
		AllowFallbacks: input.AllowFallbacks,
		MaxAttempts:    normalizeAggregationMaxAttempts(input.MaxAttempts),
		Members:        members,
	}, nil
}

func ApplyAggregationUpdate(current Aggregation, input UpdateAggregation) (Aggregation, error) {
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len(name) > MaxAggregationNameLen {
			return Aggregation{}, fmt.Errorf("invalid aggregation name")
		}
		current.Name = name
	}
	if input.Enabled != nil {
		current.Enabled = *input.Enabled
	}
	if input.Strategy != nil {
		current.Strategy = NormalizeAggregationStrategy(string(*input.Strategy))
	}
	if input.AllowFallbacks != nil {
		current.AllowFallbacks = *input.AllowFallbacks
	}
	if input.MaxAttempts != nil {
		current.MaxAttempts = normalizeAggregationMaxAttempts(*input.MaxAttempts)
	}
	if input.Members != nil {
		members, err := normalizeAggregationMembers(*input.Members)
		if err != nil {
			return Aggregation{}, err
		}
		current.Members = members
	}
	return current, nil
}

func validAggregationID(id string) bool {
	if id == "" || len(id) > MaxAggregationNameLen {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '/' || r == '.' {
			continue
		}
		return false
	}
	return true
}
