package workbuddy

import (
	"strings"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
)

type catalogReasoning struct {
	Effort             string   `json:"effort"`
	DefaultEffort      string   `json:"defaultEffort"`
	SupportedEfforts   []string `json:"supportedEfforts"`
	CanDisableThinking *bool    `json:"canDisableThinking"`
	Summary            string   `json:"summary"`
}

type catalogContextWindow struct {
	DefaultLength    int   `json:"defaultLength"`
	SupportedLengths []int `json:"supportedLengths"`
}

type catalogModelEntry struct {
	ID                string               `json:"id"`
	Name              string               `json:"name"`
	MaxInputTokens    int                  `json:"maxInputTokens"`
	MaxOutputTokens   int                  `json:"maxOutputTokens"`
	Disabled          bool                 `json:"disabled"`
	OnlyReasoning     bool                 `json:"onlyReasoning"`
	SupportsReasoning bool                 `json:"supportsReasoning"`
	SupportsImages    bool                 `json:"supportsImages"`
	SupportsToolCall  bool                 `json:"supportsToolCall"`
	ContextWindow     catalogContextWindow `json:"contextWindow"`
	Reasoning         catalogReasoning     `json:"reasoning"`
}

func catalogModel(model catalogModelEntry) providers.ModelInfo {
	options := providers.UniqueReasoningOptions(model.Reasoning.SupportedEfforts)
	defaultLevel := providers.NormalizeReasoningLevel(model.Reasoning.DefaultEffort)
	if defaultLevel == "" {
		defaultLevel = providers.NormalizeReasoningLevel(model.Reasoning.Effort)
	}
	if len(options) == 0 && defaultLevel != "" {
		options = []string{defaultLevel}
	}
	if defaultLevel == "" && len(options) > 0 {
		defaultLevel = options[0]
	} else if defaultLevel != "" && !containsLevel(options, defaultLevel) {
		options = append([]string{defaultLevel}, options...)
	}
	canDisable := !model.OnlyReasoning
	if model.Reasoning.CanDisableThinking != nil {
		canDisable = *model.Reasoning.CanDisableThinking && !model.OnlyReasoning
	}
	if canDisable && (len(options) > 0 || model.SupportsReasoning) && !containsLevel(options, "none") {
		options = append([]string{"none"}, options...)
	}
	window, windowMax := contextWindows(model)
	return providers.ModelInfo{
		NativeModel: model.ID,
		PublicModel: model.ID,
		DisplayName: model.Name,
		Capabilities: providers.ModelCapabilities{
			ContextWindow:      window,
			ContextWindowMax:   windowMax,
			MaxOutput:          model.MaxOutputTokens,
			Tools:              true,
			Images:             model.SupportsImages,
			Reasoning:          model.SupportsReasoning || len(options) > 0 || defaultLevel != "",
			ReasoningOptions:   options,
			ReasoningDefault:   defaultLevel,
			CanDisableThinking: canDisable,
		},
	}
}

func contextWindows(model catalogModelEntry) (int, int) {
	cap := model.MaxInputTokens
	seen := map[int]struct{}{}
	var lengths []int
	for _, value := range model.ContextWindow.SupportedLengths {
		if value <= 0 || (cap > 0 && value > cap) {
			continue
		}
		if _, dup := seen[value]; dup {
			continue
		}
		seen[value] = struct{}{}
		lengths = append(lengths, value)
	}
	for i := 0; i < len(lengths); i++ {
		for j := i + 1; j < len(lengths); j++ {
			if lengths[j] < lengths[i] {
				lengths[i], lengths[j] = lengths[j], lengths[i]
			}
		}
	}
	dev := model.ContextWindow.DefaultLength
	if dev <= 0 || (cap > 0 && dev > cap) || (len(lengths) > 0 && !containsWindow(lengths, dev)) {
		if len(lengths) > 0 {
			dev = lengths[0]
		} else {
			dev = cap
		}
	}
	max := cap
	if len(lengths) > 0 {
		max = lengths[len(lengths)-1]
	}
	if max == dev {
		max = 0
	}
	return dev, max
}

func containsWindow(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsLevel(options []string, level string) bool {
	for _, option := range options {
		if option == level {
			return true
		}
	}
	return false
}

func (c *Client) rememberCatalog(models []providers.ModelInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.catalog = make(map[string]providers.ModelInfo, len(models))
	for _, model := range models {
		c.catalog[model.NativeModel] = model
		c.catalog[strings.ToLower(model.NativeModel)] = model
	}
}

func (c *Client) capsFor(model string) providers.ModelCapabilities {
	model = strings.TrimSpace(model)
	c.mu.Lock()
	info, ok := c.catalog[model]
	if !ok {
		// Public model IDs may arrive underscored or mixed-case; the console
		// canonical form (lowercase, _ folded to -) is the stable join key.
		info, ok = c.catalog[accounts.CanonicalModelID(model)]
	}
	c.mu.Unlock()
	if ok {
		return info.Capabilities
	}
	return providers.ModelCapabilities{}
}
