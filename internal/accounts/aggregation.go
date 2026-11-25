package accounts

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/caigee-cmd/cli2api/internal/providers"
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
	// ContextLength overrides the context window advertised for the logical
	// model. Zero keeps the conservative value derived from the members.
	ContextLength int `json:"context_length,omitempty"`
	// MaxOutputTokens overrides the largest output advertised for the logical
	// model. Zero keeps the members' conservative value.
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
	// ReasoningOptions pins the reasoning levels the logical model advertises.
	// Empty keeps the intersection of the members' declared levels.
	ReasoningOptions []string `json:"reasoning_options,omitempty"`
	// ReasoningEffort is the default reasoning level advertised for the logical
	// model. It is a default only: a client may still request any other level.
	ReasoningEffort string              `json:"reasoning_effort,omitempty"`
	Members         []AggregationMember `json:"members"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

type CreateAggregation struct {
	ID               string
	Name             string
	Enabled          bool
	Strategy         AggregationStrategy
	AllowFallbacks   bool
	MaxAttempts      int
	ContextLength    int
	MaxOutputTokens  int
	ReasoningOptions []string
	ReasoningEffort  string
	Members          []AggregationMember
}

type UpdateAggregation struct {
	Name             *string
	Enabled          *bool
	Strategy         *AggregationStrategy
	AllowFallbacks   *bool
	MaxAttempts      *int
	ContextLength    *int
	MaxOutputTokens  *int
	ReasoningOptions *[]string
	ReasoningEffort  *string
	Members          *[]AggregationMember
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

// reasoningOrderIndex positions a normalized level on the gateway vocabulary so
// the advertised list is deterministic. Unknown levels sort last.
func reasoningOrderIndex(level string) int {
	for index, option := range providers.GatewayReasoningOptions() {
		if option == level {
			return index
		}
	}
	return len(providers.GatewayReasoningOptions())
}

func normalizeAggregationReasoningOptions(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, value := range raw {
		level := providers.NormalizeReasoningLevel(value)
		if level == "" {
			return nil, fmt.Errorf("unsupported reasoning level %q", strings.TrimSpace(value))
		}
		if seen[level] {
			continue
		}
		seen[level] = true
		out = append(out, level)
	}
	sort.Slice(out, func(i, j int) bool {
		return reasoningOrderIndex(out[i]) < reasoningOrderIndex(out[j])
	})
	return out, nil
}

func normalizeAggregationReasoningEffort(raw string, options []string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	level := providers.NormalizeReasoningLevel(raw)
	if level == "" {
		return "", fmt.Errorf("unsupported reasoning effort %q", raw)
	}
	if len(options) == 0 {
		return "", fmt.Errorf("reasoning_effort requires reasoning_options")
	}
	for _, option := range options {
		if option == level {
			return level, nil
		}
	}
	return "", fmt.Errorf("reasoning_effort %q must be one of the configured reasoning options", level)
}

// normalizeAggregationCapabilities validates the logical model's advertised
// capability overrides. Zero/empty values mean "derive from the members", so an
// unspecified aggregation behaves exactly like before this feature.
func normalizeAggregationCapabilities(contextLength, maxOutput int, options []string, effort string) (int, int, []string, string, error) {
	if contextLength < 0 || contextLength > 4_000_000 || (contextLength > 0 && contextLength < 1024) {
		return 0, 0, nil, "", fmt.Errorf("aggregation context_length must be 0 or between 1024 and 4000000")
	}
	if maxOutput < 0 || maxOutput > 4_000_000 || (maxOutput > 0 && maxOutput < 1024) {
		return 0, 0, nil, "", fmt.Errorf("aggregation max_output_tokens must be 0 or between 1024 and 4000000")
	}
	if contextLength > 0 && maxOutput > contextLength {
		return 0, 0, nil, "", fmt.Errorf("aggregation max_output_tokens must not exceed context_length")
	}
	normalizedOptions, err := normalizeAggregationReasoningOptions(options)
	if err != nil {
		return 0, 0, nil, "", err
	}
	normalizedEffort, err := normalizeAggregationReasoningEffort(effort, normalizedOptions)
	if err != nil {
		return 0, 0, nil, "", err
	}
	return contextLength, maxOutput, normalizedOptions, normalizedEffort, nil
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
	contextLength, maxOutput, options, effort, err := normalizeAggregationCapabilities(
		input.ContextLength, input.MaxOutputTokens, input.ReasoningOptions, input.ReasoningEffort,
	)
	if err != nil {
		return CreateAggregation{}, err
	}
	return CreateAggregation{
		ID:               id,
		Name:             name,
		Enabled:          input.Enabled,
		Strategy:         NormalizeAggregationStrategy(string(input.Strategy)),
		AllowFallbacks:   input.AllowFallbacks,
		MaxAttempts:      normalizeAggregationMaxAttempts(input.MaxAttempts),
		ContextLength:    contextLength,
		MaxOutputTokens:  maxOutput,
		ReasoningOptions: options,
		ReasoningEffort:  effort,
		Members:          members,
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
	if input.ContextLength != nil {
		current.ContextLength = *input.ContextLength
	}
	if input.MaxOutputTokens != nil {
		current.MaxOutputTokens = *input.MaxOutputTokens
	}
	if input.ReasoningOptions != nil {
		current.ReasoningOptions = *input.ReasoningOptions
	}
	if input.ReasoningEffort != nil {
		current.ReasoningEffort = *input.ReasoningEffort
	}
	if input.Members != nil {
		members, err := normalizeAggregationMembers(*input.Members)
		if err != nil {
			return Aggregation{}, err
		}
		current.Members = members
	}
	contextLength, maxOutput, options, effort, err := normalizeAggregationCapabilities(
		current.ContextLength, current.MaxOutputTokens, current.ReasoningOptions, current.ReasoningEffort,
	)
	if err != nil {
		return Aggregation{}, err
	}
	current.ContextLength = contextLength
	current.MaxOutputTokens = maxOutput
	current.ReasoningOptions = options
	current.ReasoningEffort = effort
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
