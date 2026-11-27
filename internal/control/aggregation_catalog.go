package control

import (
	"context"
	"sort"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

type AggregationLister interface {
	List(ctx context.Context) ([]accounts.Aggregation, error)
}

// aggregationCatalogMember is one configured member resolved to the catalog
// entry that actually serves it. Provider and region are honored so a member
// never borrows another provider's copy of the same model name.
type aggregationCatalogMember struct {
	id    string
	entry map[string]any
}

// aggregationCapabilities is the conservative intersection of every resolved
// member's declared capabilities. A logical model fails over between members,
// so anything advertised here must hold for all of them.
type aggregationCapabilities struct {
	window           int
	hasWindow        bool
	maxOutput        int
	hasMaxOutput     bool
	promptMax        int
	hasPromptMax     bool
	reasoning        []string
	hasReasoning     bool
	defaultReasoning string
	hasDefault       bool
	reasoningType    string
	hasReasoningType bool
	images           bool
	canDisableThink  bool
}

// inheritedAggregationFields are decoration and provider metadata that must not
// survive into a provider-neutral logical entry. The merge starts from the
// first member's (already decorated) catalog row, so these either describe one
// member or duplicate a field this function re-derives.
var inheritedAggregationFields = []string{
	"alias_of", "native_model", "mapped_key", "region", "regions", "credits", "free",
	"catalog_context_length", "catalog_context_length_max",
	"max_output_tokens", "max_output_tokens_max",
	"prompt_max_tokens", "prompt_max_tokens_max",
	"supports_max_mode", "can_disable_thinking", "supports_images",
	"reasoning_options", "reasoning_option_values", "reasoning_default", "reasoning_type",
	"context_length", "default_context_length", "context_custom", "context_editable",
	"settings_key", "max_mode", "reasoning_effort",
}

// MergeAggregationModels folds enabled logical models into the display catalog.
// Each logical entry advertises the conservative intersection of its members'
// capabilities (or the operator-configured overrides), then member models are
// hidden so clients see one stable ID instead of one row per provider. Models
// that are not part of any enabled aggregation are left untouched.
//
// The catalog slices passed in/out are plain value maps, so this never mutates
// the shared catalog cache.
func MergeAggregationModels(ctx context.Context, lister AggregationLister, models []map[string]any) []map[string]any {
	if lister == nil || len(models) == 0 {
		return models
	}
	aggs, err := lister.List(ctx)
	if err != nil || len(aggs) == 0 {
		return models
	}

	index := aggregationCatalogIndex(models)
	hidden := map[string]bool{}
	entries := make([]map[string]any, 0, len(aggs))
	for _, agg := range aggs {
		if !agg.Enabled || len(agg.Members) == 0 {
			continue
		}
		members := resolveAggregationCatalogMembers(agg, index)
		if len(members) == 0 {
			// Every member is filtered out by the caller's provider policy. Do
			// not advertise a logical ID that cannot route to any real model.
			continue
		}
		for _, member := range members {
			hidden[member.id] = true
		}
		entries = append(entries, buildAggregationEntry(agg, members))
	}
	if len(entries) == 0 && len(hidden) == 0 {
		return models
	}

	out := make([]map[string]any, 0, len(models)-len(hidden)+len(entries))
	for _, model := range models {
		id, _ := model["id"].(string)
		if hidden[id] {
			continue
		}
		out = append(out, model)
	}
	out = append(out, entries...)
	sort.SliceStable(out, func(i, j int) bool {
		left, _ := out[i]["id"].(string)
		right, _ := out[j]["id"].(string)
		return left < right
	})
	return out
}

// aggregationCatalogIndex groups catalog rows by (provider family, canonical
// model name). The shared catalog keeps one row per provider, so an index keyed
// by id alone would let whichever provider was appended last win.
func aggregationCatalogIndex(models []map[string]any) map[string][]map[string]any {
	index := make(map[string][]map[string]any, len(models))
	for _, model := range models {
		id, _ := model["id"].(string)
		if strings.TrimSpace(id) == "" {
			continue
		}
		key := aggregationCatalogKey(modelEntryProvider(model), modelAllowKey(id))
		if key == "" {
			continue
		}
		index[key] = append(index[key], model)
	}
	return index
}

func aggregationCatalogKey(provider, modelKey string) string {
	provider = accounts.NormalizeProviderFamily(provider)
	if provider == "" || modelKey == "" {
		return ""
	}
	return provider + "\x00" + modelKey
}

func resolveAggregationCatalogMembers(agg accounts.Aggregation, index map[string][]map[string]any) []aggregationCatalogMember {
	resolved := make([]aggregationCatalogMember, 0, len(agg.Members))
	for _, member := range agg.Members {
		key := aggregationCatalogKey(member.Provider, modelAllowKey(member.Model))
		if key == "" {
			continue
		}
		var picked map[string]any
		for _, candidate := range index[key] {
			if member.Region == "" || accountServesRegion(member.Region, EntryModelRegions(candidate)) {
				picked = candidate
				break
			}
		}
		if picked == nil {
			continue
		}
		id, _ := picked["id"].(string)
		resolved = append(resolved, aggregationCatalogMember{id: id, entry: picked})
	}
	return resolved
}

func summarizeAggregationCapabilities(members []aggregationCatalogMember) aggregationCapabilities {
	caps := aggregationCapabilities{hasReasoning: len(members) > 0, images: true, canDisableThink: true}
	var reasoningSets []map[string]bool
	for index, member := range members {
		options := reasoningValues(member.entry)
		if len(options) == 0 {
			caps.hasReasoning = false
			reasoningSets = append(reasoningSets, nil)
		} else {
			set := make(map[string]bool, len(options))
			for _, option := range providers.UniqueReasoningOptions(options) {
				set[option] = true
			}
			reasoningSets = append(reasoningSets, set)
		}

		level, _ := member.entry["reasoning_default"].(string)
		level = providers.NormalizeReasoningLevel(level)
		if index == 0 {
			caps.defaultReasoning = level
			caps.hasDefault = level != ""
		} else if level != caps.defaultReasoning {
			// Members disagree on the default: advertise none rather than pick one
			// member's choice for the whole pool.
			caps.hasDefault = false
		}

		reasoningType, _ := member.entry["reasoning_type"].(string)
		reasoningType = strings.TrimSpace(reasoningType)
		if index == 0 {
			caps.reasoningType = reasoningType
			caps.hasReasoningType = reasoningType != ""
		} else if reasoningType != caps.reasoningType {
			caps.hasReasoningType = false
		}

		if supported, ok := member.entry["supports_images"].(bool); !ok || !supported {
			caps.images = false
		}
		if enabled, ok := member.entry["can_disable_thinking"].(bool); !ok || !enabled {
			caps.canDisableThink = false
		}
	}
	caps.hasReasoning = caps.hasReasoning && len(reasoningSets) > 0
	caps.window, caps.hasWindow = minDeclaredCatalogInt(members, "catalog_context_length")
	caps.maxOutput, caps.hasMaxOutput = minDeclaredCatalogInt(members, "max_output_tokens")
	caps.promptMax, caps.hasPromptMax = minDeclaredCatalogInt(members, "prompt_max_tokens")
	if caps.hasReasoning {
		caps.reasoning = intersectReasoningOptions(reasoningSets)
		if len(caps.reasoning) == 0 {
			caps.hasReasoning = false
		}
	}
	if caps.hasDefault && !reasoningAccepted(caps.reasoning, caps.defaultReasoning) {
		caps.hasDefault = false
	}
	return caps
}

// minDeclaredCatalogInt returns the smallest value every member declares. A
// member that does not declare the field makes the whole capability unknown:
// the logical model fails over between members, so the conservative answer is
// no claim rather than the maximum of what some members happen to report.
func minDeclaredCatalogInt(members []aggregationCatalogMember, field string) (int, bool) {
	value := 0
	for _, member := range members {
		declared, ok := catalogInt(member.entry[field])
		if !ok || declared <= 0 {
			return 0, false
		}
		if value == 0 || declared < value {
			value = declared
		}
	}
	return value, value > 0
}

func intersectReasoningOptions(sets []map[string]bool) []string {
	if len(sets) == 0 || sets[0] == nil {
		return nil
	}
	out := make([]string, 0, len(sets[0]))
	for level := range sets[0] {
		inAll := true
		for _, set := range sets[1:] {
			if set == nil || !set[level] {
				inAll = false
				break
			}
		}
		if inAll {
			out = append(out, level)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return aggregationReasoningOrder(out[i]) < aggregationReasoningOrder(out[j])
	})
	return out
}

// aggregationReasoningOrder positions a level on the gateway vocabulary so
// the advertised list is deterministic.
func aggregationReasoningOrder(level string) int {
	for index, option := range providers.GatewayReasoningOptions() {
		if option == level {
			return index
		}
	}
	return len(providers.GatewayReasoningOptions())
}

func reasoningAccepted(options []string, level string) bool {
	for _, option := range options {
		if option == level {
			return true
		}
	}
	return false
}

// buildAggregationEntry derives one provider-neutral catalog row. Configured
// overrides win; otherwise every capability is the conservative intersection
// computed from the resolved members.
func buildAggregationEntry(agg accounts.Aggregation, members []aggregationCatalogMember) map[string]any {
	entry := CloneModelEntry(members[0].entry)
	for _, field := range inheritedAggregationFields {
		delete(entry, field)
	}
	caps := summarizeAggregationCapabilities(members)

	entry["id"] = agg.ID
	entry["display_name"] = agg.Name
	entry["provider"] = "aggregation"
	entry["owned_by"] = "aggregation"
	entry["aggregation"] = true
	entry["aggregation_strategy"] = string(agg.Strategy)
	entry["aggregation_members"] = len(agg.Members)
	entry["context_editable"] = false

	window := agg.ContextLength
	if window <= 0 && caps.hasWindow {
		window = caps.window
	}
	if window > 0 {
		entry["catalog_context_length"] = window
		entry["context_length"] = window
		entry["default_context_length"] = window
	}
	if agg.ContextLength > 0 {
		entry["context_custom"] = true
	}

	maxOutput := agg.MaxOutputTokens
	if maxOutput <= 0 && caps.hasMaxOutput {
		maxOutput = caps.maxOutput
	}
	if maxOutput > 0 {
		entry["max_output_tokens"] = maxOutput
	}
	if caps.hasPromptMax {
		entry["prompt_max_tokens"] = caps.promptMax
	}

	options := providers.UniqueReasoningOptions(agg.ReasoningOptions)
	if len(options) == 0 && caps.hasReasoning {
		options = caps.reasoning
	}
	if len(options) > 0 {
		entry["reasoning_options"] = options
	}
	effort := agg.ReasoningEffort
	if effort == "" && caps.hasDefault {
		effort = caps.defaultReasoning
	}
	if effort != "" && reasoningAccepted(options, effort) {
		entry["reasoning_default"] = effort
		entry["reasoning_effort"] = effort
	}
	if caps.hasReasoningType {
		entry["reasoning_type"] = caps.reasoningType
	}
	// Both flags are failover-safe only when every member declares them: a
	// client trusts them for every retry hop, not just the first attempt.
	entry["supports_images"] = caps.images
	if caps.canDisableThink {
		entry["can_disable_thinking"] = true
	}
	return entry
}
