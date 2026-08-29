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
	ReasoningEffortConfig json.RawMessage `json:"reasoning_effort_config"`
	ModelDetailList       []struct {
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
	}
	reasoning := len(options) > 0 || thinkingType != "" && thinkingType != "disabled" || item.DisplayConfig.Capability == "reasoning_model"
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
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.HasPrefix(raw, "{") {
		return ""
	}
	var extra struct {
		Thinking struct {
			Type string `json:"Type"`
		} `json:"Thinking"`
	}
	if json.Unmarshal([]byte(raw), &extra) != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(extra.Thinking.Type))
}
