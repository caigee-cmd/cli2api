package meta

import "github.com/caigee-cmd/cli2api/internal/providers"

// Static catalog aligned with CPA v8.0.40 GetMetaModels() / models.json meta[].
var staticModels = []providers.ModelInfo{
	modelWonder("muse-wonder-1.0", "Muse Wonder 1.0"),
	modelWonder("muse-wonder-1.0-contributor", "Muse Wonder 1.0 Contributor"),
	modelWonder("muse-wonder-full-1.0", "Muse Wonder Full 1.0"),
	modelWonder("muse-wonder-full-1.0-contributor", "Muse Wonder Full 1.0 Contributor"),
	modelSpark("muse-spark-1.3", "Muse Spark 1.3", []string{"minimal", "low", "medium", "high", "xhigh", "max"}, true),
	modelSpark("muse-spark-1.3-contributor", "Muse Spark 1.3 Contributor", []string{"minimal", "low", "medium", "high", "xhigh", "max"}, true),
	modelSpark("muse-spark-1.2", "Muse Spark 1.2", []string{"minimal", "low", "medium", "high", "xhigh"}, true),
	modelSpark("muse-spark-1.2-contributor", "Muse Spark 1.2 Contributor", []string{"minimal", "low", "medium", "high", "xhigh"}, true),
	modelSpark("muse-spark-1.1", "Muse Spark 1.1", []string{"low", "medium", "high", "xhigh"}, false),
}

func modelWonder(id, display string) providers.ModelInfo {
	return providers.ModelInfo{
		NativeModel: id,
		PublicModel: id,
		DisplayName: display,
		Capabilities: providers.ModelCapabilities{
			ContextWindow:      1007997,
			MaxOutput:          128000,
			Tools:              true,
			Images:             true,
			Reasoning:          true,
			ReasoningOptions:   []string{"minimal", "low", "medium", "high", "xhigh", "max"},
			ReasoningDefault:   "high",
			CanDisableThinking: true,
		},
	}
}

func modelSpark(id, display string, levels []string, images bool) providers.ModelInfo {
	def := "high"
	if len(levels) > 0 {
		for _, l := range levels {
			if l == "high" {
				def = "high"
				break
			}
		}
		if def == "" {
			def = levels[len(levels)/2]
		}
	}
	return providers.ModelInfo{
		NativeModel: id,
		PublicModel: id,
		DisplayName: display,
		Capabilities: providers.ModelCapabilities{
			ContextWindow:      1048576,
			MaxOutput:          65536,
			Tools:              true,
			Images:             images,
			Reasoning:          true,
			ReasoningOptions:   append([]string(nil), levels...),
			ReasoningDefault:   def,
			CanDisableThinking: true,
		},
	}
}

func catalogModelInfos() []providers.ModelInfo {
	out := make([]providers.ModelInfo, len(staticModels))
	copy(out, staticModels)
	return out
}

func capsFor(model string) providers.ModelCapabilities {
	for _, m := range staticModels {
		if m.NativeModel == model || m.PublicModel == model {
			return m.Capabilities
		}
	}
	// Unknown model: allow common Muse reasoning levels so clamp still works.
	return providers.ModelCapabilities{
		Reasoning:        true,
		ReasoningOptions: []string{"minimal", "low", "medium", "high", "xhigh", "max"},
		ReasoningDefault: "high",
		Tools:            true,
		Images:           true,
	}
}
