package trae

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

var hiddenConfigNames = map[string]struct{}{
	"browser_use_subagent": {},
	"file_search_agent":    {},
	"explore_sub_agent_v2": {},
	"summary":              {},
}

// displayNameOverrides corrects catalog display names that upstream reports
// wrongly. The chat_v3 catalog labels the newer deepseek-v4.1-flash config
// "DeepSeek-V4-Flash 正式版" — byte-identical to the older
// DeepSeek-V4-Flash-Official row — so the console renders two indistinguishable
// entries. The model's own name is "DeepSeek-V4.1-Flash". Keys are canonical ids.
var displayNameOverrides = map[string]string{
	"deepseek-v4.1-flash": "DeepSeek-V4.1-Flash",
}

type catalogConfig struct {
	ConfigName          string `json:"config_name"`
	IsInvisibleToUser   bool   `json:"is_invisible_to_user"`
	ContextWindowTokens struct {
		Dev int `json:"dev"`
		Max int `json:"max"`
	} `json:"context_window_tokens"`
	DisplayConfig struct {
		DisplayName string `json:"display_name"`
		MaxMode     bool   `json:"max_mode"`
		IsDollarMax bool   `json:"is_dollar_max"`
		Capability  string `json:"model_capability"`
	} `json:"display_config"`
	DisplayContactConfig   json.RawMessage `json:"display_contact_config"`
	ReasoningEffortConfig  json.RawMessage `json:"reasoning_effort_config"`
	ReasoningEffortOptions []string        `json:"reasoning_effort_options"`
	DefaultReasoningEffort string          `json:"default_reasoning_effort"`
	ModelDetailList        []struct {
		MaxTokens        int    `json:"max_tokens"`
		PromptMaxTokens  int    `json:"prompt_max_tokens"`
		ModelExtraConfig string `json:"model_extra_config"`
	} `json:"model_detail_list"`
}

type reasoningEffortConfig struct {
	SupportThinking bool     `json:"support_thinking"`
	Options         []string `json:"options"`
	DefaultLevel    string   `json:"default_level"`
}

// canonicalCatalogID folds a config_name to the canonical form the console and
// scheduler use (lowercased, separators folded), so override/merge keys match
// regardless of upstream casing.
func canonicalCatalogID(id string) string {
	return accounts.CanonicalModelID(id)
}

func parseCatalogModels(payload []byte, scene string) ([]providers.ModelInfo, error) {
	var env struct {
		ConfigInfoList []catalogConfig `json:"config_info_list"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, err
	}
	var out []providers.ModelInfo
	// The catalog can list the same config_name twice (e.g. a public entry
	// with a consumption rate and a rate-less duplicate). Keep the richer
	// entry so the console still shows the multiplier.
	index := map[string]int{}
	for _, item := range env.ConfigInfoList {
		info, ok := catalogModel(item, scene)
		if !ok {
			continue
		}
		if at, dup := index[info.NativeModel]; dup {
			if out[at].Credits == "" && info.Credits != "" {
				out[at] = info
			}
			continue
		}
		index[info.NativeModel] = len(out)
		out = append(out, info)
	}
	return out, nil
}

// preferredScene picks which scene a model should be served through. A model
// listed by the primary (superset) scene is served there; a model only the
// secondary scene carries keeps that scene. This is derived purely from catalog
// membership, so a new model is routed automatically on the next refresh.
func preferredScene(a, b string) string {
	if a == PrimaryScene || b == PrimaryScene {
		return PrimaryScene
	}
	if a != "" {
		return a
	}
	return b
}

// mergeCatalogModels merges the secondary scene's models into the primary list.
// Both scenes are the same provider/account, so a duplicate is one entry; the
// two entries are combined, favouring the one that declares the larger context
// window (the Max-mode tier) and backfilling credits/display from the other.
func mergeCatalogModels(primary, extra []providers.ModelInfo) []providers.ModelInfo {
	if len(extra) == 0 {
		return primary
	}
	index := make(map[string]int, len(primary))
	for i, m := range primary {
		index[canonicalCatalogID(m.NativeModel)] = i
	}
	for _, m := range extra {
		key := canonicalCatalogID(m.NativeModel)
		if at, dup := index[key]; dup {
			primary[at] = combineCatalogEntry(primary[at], m)
			continue
		}
		index[key] = len(primary)
		primary = append(primary, m)
	}
	return primary
}

// combineCatalogEntry folds two entries for the same model (one per scene). The
// entry declaring a Max-mode tier (larger context window) becomes the base so
// the toggle and its tier ceilings surface; credits, display name, the serving
// scene and any missing tier fields are backfilled from the other entry.
func combineCatalogEntry(a, b providers.ModelInfo) providers.ModelInfo {
	winner, loser := a, b
	if b.Capabilities.ContextWindowMax > a.Capabilities.ContextWindowMax {
		winner, loser = b, a
	}
	winner.Scene = preferredScene(a.Scene, b.Scene)
	if winner.Credits == "" && loser.Credits != "" {
		winner.Credits = loser.Credits
	}
	if winner.DisplayName == "" {
		winner.DisplayName = loser.DisplayName
	}
	if winner.Capabilities.PromptMaxTokens == 0 {
		winner.Capabilities.PromptMaxTokens = loser.Capabilities.PromptMaxTokens
	}
	if winner.Capabilities.MaxOutput == 0 {
		winner.Capabilities.MaxOutput = loser.Capabilities.MaxOutput
	}
	if winner.Capabilities.ContextWindow == 0 {
		winner.Capabilities.ContextWindow = loser.Capabilities.ContextWindow
	}
	if winner.Capabilities.PromptMaxTokensMax == 0 && loser.Capabilities.PromptMaxTokensMax > 0 {
		winner.Capabilities.PromptMaxTokensMax = loser.Capabilities.PromptMaxTokensMax
	}
	if winner.Capabilities.MaxOutputMax == 0 && loser.Capabilities.MaxOutputMax > 0 {
		winner.Capabilities.MaxOutputMax = loser.Capabilities.MaxOutputMax
	}
	return winner
}

func catalogModel(item catalogConfig, scene string) (providers.ModelInfo, bool) {
	id := strings.TrimSpace(item.ConfigName)
	if id == "" || item.IsInvisibleToUser {
		return providers.ModelInfo{}, false
	}
	if _, hidden := hiddenConfigNames[id]; hidden {
		return providers.ModelInfo{}, false
	}
	display := strings.TrimSpace(item.DisplayConfig.DisplayName)
	if display == "" || display == "-" || strings.HasPrefix(id, "custom_model_") {
		return providers.ModelInfo{}, false
	}
	if corrected, ok := displayNameOverrides[canonicalCatalogID(id)]; ok {
		display = corrected
	}
	window := item.ContextWindowTokens.Dev
	windowMax := item.ContextWindowTokens.Max
	maxMode := item.DisplayConfig.MaxMode || item.DisplayConfig.IsDollarMax || (windowMax > 0 && windowMax != window)
	options, defaultLevel := parseReasoningOptions(item.ReasoningEffortConfig)
	if len(options) == 0 {
		options, defaultLevel = parseReasoningOptionList(item.ReasoningEffortOptions, item.DefaultReasoningEffort)
	}
	thinkingType := ""
	promptMax, maxOut := 0, 0
	// Trae expresses context tiers as model_detail_list entries: the first is
	// the default tier, and a second (larger) entry is the Max-mode tier. Track
	// the max-tier ceilings separately so the console can show the value that
	// matches the max-mode toggle.
	promptMaxMax, maxOutMax := 0, 0
	for i, detail := range item.ModelDetailList {
		if i == 0 {
			promptMax = detail.PromptMaxTokens
			maxOut = detail.MaxTokens
		}
		if detail.PromptMaxTokens > promptMaxMax {
			promptMaxMax = detail.PromptMaxTokens
		}
		if detail.MaxTokens > maxOutMax {
			maxOutMax = detail.MaxTokens
		}
		if thinkingType == "" {
			thinkingType = thinkingTypeFromExtra(detail.ModelExtraConfig)
		}
		if !maxMode && v2MaxModeEnabled(detail.ModelExtraConfig) {
			maxMode = true
		}
	}
	// Only keep the max tier when it is distinct from the default tier.
	if promptMaxMax <= promptMax {
		promptMaxMax = 0
	}
	if maxOutMax <= maxOut {
		maxOutMax = 0
	}
	reasoning := len(options) > 0 || thinkingType != "" && thinkingType != "disabled" || item.DisplayConfig.Capability == "reasoning_model" || contactReasoningEnabled(item.DisplayContactConfig)
	if len(options) == 0 && reasoning {
		options, defaultLevel = []string{"low", "high", "xhigh"}, "high"
	}
	return providers.ModelInfo{
		NativeModel: id,
		PublicModel: id,
		DisplayName: display,
		Credits:     creditsText(contactConsumptionRate(item.DisplayContactConfig)),
		Scene:       scene,
		Capabilities: providers.ModelCapabilities{
			ContextWindow:      window,
			ContextWindowMax:   windowMax,
			MaxOutput:          maxOut,
			PromptMaxTokens:    promptMax,
			PromptMaxTokensMax: promptMaxMax,
			MaxOutputMax:       maxOutMax,
			MaxMode:            maxMode,
			Tools:              true,
			Reasoning:          reasoning,
			ReasoningOptions:   options,
			ReasoningDefault:   defaultLevel,
			ReasoningType:      thinkingType,
		},
	}, true
}

func parseReasoningOptions(raw json.RawMessage) ([]string, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, ""
	}
	var cfg reasoningEffortConfig
	if json.Unmarshal(raw, &cfg) != nil || !cfg.SupportThinking {
		return nil, ""
	}
	seen := map[string]struct{}{}
	var options []string
	for _, option := range cfg.Options {
		option = normalizeReasoningLevel(option)
		if option == "" {
			return nil, ""
		}
		if _, dup := seen[option]; dup {
			return nil, ""
		}
		seen[option] = struct{}{}
		options = append(options, option)
	}
	if len(options) == 0 {
		return nil, ""
	}
	defaultLevel := normalizeReasoningLevel(cfg.DefaultLevel)
	if defaultLevel == "" {
		return nil, ""
	}
	if _, ok := seen[defaultLevel]; !ok {
		return nil, ""
	}
	return options, defaultLevel
}

func thinkingTypeFromExtra(raw string) string {
	extra := extraConfigMap(raw)
	thinking, _ := extra["Thinking"].(map[string]any)
	if thinking == nil {
		thinking, _ = extra["thinking"].(map[string]any)
	}
	if thinking == nil {
		return ""
	}
	typ, _ := thinking["Type"].(string)
	if typ == "" {
		typ, _ = thinking["type"].(string)
	}
	return strings.ToLower(strings.TrimSpace(typ))
}

func extraConfigMap(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return nil
	}
	var extra map[string]any
	if json.Unmarshal([]byte(raw), &extra) != nil {
		return nil
	}
	return extra
}

func v2MaxModeEnabled(raw string) bool {
	value, ok := extraConfigMap(raw)["v2_max_mode_enabled"]
	if !ok {
		return false
	}
	enabled, _ := value.(bool)
	return enabled
}

func contactReasoningEnabled(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	payload := raw
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil && strings.TrimSpace(encoded) != "" {
		payload = json.RawMessage(encoded)
	}
	var contact struct {
		Reasoning struct {
			Enable bool `json:"enable"`
		} `json:"reasoning"`
	}
	if json.Unmarshal(payload, &contact) != nil {
		return false
	}
	return contact.Reasoning.Enable
}

// contactConsumptionRate extracts the model's credit multiplier from
// display_contact_config.consumption_rate.data.rate (e.g. 0.78). Trae reports
// this per model; the console shows it next to the model. Returns 0 when absent.
func contactConsumptionRate(raw json.RawMessage) float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	payload := raw
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil && strings.TrimSpace(encoded) != "" {
		payload = json.RawMessage(encoded)
	}
	var contact struct {
		ConsumptionRate struct {
			Enable bool `json:"enable"`
			Data   struct {
				Rate float64 `json:"rate"`
			} `json:"data"`
		} `json:"consumption_rate"`
	}
	if json.Unmarshal(payload, &contact) != nil {
		return 0
	}
	if !contact.ConsumptionRate.Enable {
		return 0
	}
	return contact.ConsumptionRate.Data.Rate
}

// creditsText renders a consumption rate the way the console displays credits
// ("x0.78 credits").
func creditsText(rate float64) string {
	if rate <= 0 {
		return ""
	}
	return fmt.Sprintf("x%s credits", strconv.FormatFloat(rate, 'f', -1, 64))
}

func parseReasoningOptionList(values []string, defaultLevel string) ([]string, string) {
	seen := map[string]struct{}{}
	var options []string
	for _, option := range values {
		option = normalizeReasoningLevel(option)
		if option == "" {
			continue
		}
		if _, dup := seen[option]; dup {
			continue
		}
		seen[option] = struct{}{}
		options = append(options, option)
	}
	if len(options) == 0 {
		return nil, ""
	}
	fallback := normalizeReasoningLevel(defaultLevel)
	if fallback == "" {
		if _, ok := seen["medium"]; ok {
			fallback = "medium"
		} else {
			fallback = options[0]
		}
	} else if _, ok := seen[fallback]; !ok {
		fallback = options[0]
	}
	return options, fallback
}
