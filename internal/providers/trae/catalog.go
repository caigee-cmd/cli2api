package trae

import (
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

var hiddenConfigNames = map[string]struct{}{
	"browser_use_subagent": {},
	"file_search_agent":    {},
	"explore_sub_agent_v2": {},
	"summary":              {},
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

func parseCatalogModels(payload []byte) ([]providers.ModelInfo, error) {
	var env struct {
		ConfigInfoList []catalogConfig `json:"config_info_list"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, err
	}
	var out []providers.ModelInfo
	for _, item := range env.ConfigInfoList {
		info, ok := catalogModel(item)
		if !ok {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

func catalogModel(item catalogConfig) (providers.ModelInfo, bool) {
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
	window := item.ContextWindowTokens.Dev
	windowMax := item.ContextWindowTokens.Max
	maxMode := item.DisplayConfig.MaxMode || item.DisplayConfig.IsDollarMax || (windowMax > 0 && windowMax != window)
	options, defaultLevel := parseReasoningOptions(item.ReasoningEffortConfig)
	if len(options) == 0 {
		options, defaultLevel = parseReasoningOptionList(item.ReasoningEffortOptions, item.DefaultReasoningEffort)
	}
	thinkingType := ""
	promptMax, maxOut := 0, 0
	for _, detail := range item.ModelDetailList {
		if promptMax == 0 {
			promptMax = detail.PromptMaxTokens
		}
		if maxOut == 0 {
			maxOut = detail.MaxTokens
		}
		if thinkingType == "" {
			thinkingType = thinkingTypeFromExtra(detail.ModelExtraConfig)
		}
		if !maxMode && v2MaxModeEnabled(detail.ModelExtraConfig) {
			maxMode = true
		}
	}
	reasoning := len(options) > 0 || thinkingType != "" && thinkingType != "disabled" || item.DisplayConfig.Capability == "reasoning_model" || contactReasoningEnabled(item.DisplayContactConfig)
	if len(options) == 0 && reasoning {
		options, defaultLevel = []string{"low", "high", "xhigh"}, "high"
	}
	return providers.ModelInfo{
		NativeModel: id,
		PublicModel: id,
		DisplayName: display,
		Capabilities: providers.ModelCapabilities{
			ContextWindow:    window,
			ContextWindowMax: windowMax,
			MaxOutput:        maxOut,
			PromptMaxTokens:  promptMax,
			MaxMode:          maxMode,
			Tools:            true,
			Reasoning:        reasoning,
			ReasoningOptions: options,
			ReasoningDefault: defaultLevel,
			ReasoningType:    thinkingType,
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
