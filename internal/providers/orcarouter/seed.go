package orcarouter

import (
	"github.com/caigee-cmd/cli2api/internal/providers"
)

// coldStartSeed is the bounded, verified outage fallback. It is used only when
// live discovery has never succeeded (a fresh install with no network) or fails
// while the cache is cold. When live discovery succeeds its result is
// authoritative and the seed is never merged into it.
//
// Every entry keeps the "<vendor>/<model>" namespace and the metadata verified
// against the live https://api.orcarouter.ai/v1/models catalog so a catalog
// outage does not downgrade context window, reasoning ladder, or input
// modalities. An entry is dropped from the selector when this build cannot
// speak any of its endpoint types, so an incompatible seed model can never
// appear.
func seedModels() []providers.ModelInfo {
	out := make([]providers.ModelInfo, 0, len(seedEntries))
	for _, entry := range seedEntries {
		model := providers.ModelInfo{
			NativeModel:  entry.ID,
			PublicModel:  entry.ID,
			DisplayName:  entry.DisplayName,
			Capabilities: entry.Capabilities,
		}
		if !hasSpeakableEndpoint(model.Capabilities.EndpointTypes) {
			continue
		}
		out = append(out, model)
	}
	return out
}

type seedEntry struct {
	ID           string
	DisplayName  string
	Capabilities providers.ModelCapabilities
}

// seedEntries is the verified fallback set. openai/gpt-5.5 keeps the
// low/medium/high/xhigh reasoning ladder the catalog declares.
var seedEntries = []seedEntry{
	{
		ID:          "openai/gpt-5.5",
		DisplayName: "OpenAI: GPT-5.5",
		Capabilities: providers.ModelCapabilities{
			ContextWindow:    400000,
			MaxOutput:        128000,
			Tools:            true,
			Reasoning:        true,
			ReasoningOptions: []string{"low", "medium", "high", "xhigh"},
			ReasoningDefault: "medium",
			ReasoningType:    "effort",
			EndpointTypes:    []string{"openai", "openai-response"},
			InputModalities:  []string{"text"},
			OutputModalities: []string{"text"},
		},
	},
	{
		ID:          "anthropic/claude-opus-4.8",
		DisplayName: "Anthropic: Claude Opus 4.8",
		Capabilities: providers.ModelCapabilities{
			ContextWindow:    200000,
			MaxOutput:        64000,
			Tools:            true,
			Reasoning:        true,
			ReasoningOptions: []string{"low", "medium", "high"},
			ReasoningDefault: "medium",
			ReasoningType:    "effort",
			EndpointTypes:    []string{"anthropic", "openai"},
			InputModalities:  []string{"text", "image"},
			OutputModalities: []string{"text"},
			ImageInput:       true,
			Images:           true,
		},
	},
	{
		ID:          "google/gemini-3.5-flash",
		DisplayName: "Google: Gemini 3.5 Flash",
		Capabilities: providers.ModelCapabilities{
			ContextWindow:    1048576,
			MaxOutput:        65536,
			Tools:            true,
			Reasoning:        true,
			ReasoningOptions: []string{"low", "medium", "high"},
			ReasoningDefault: "medium",
			ReasoningType:    "effort",
			EndpointTypes:    []string{"gemini", "openai"},
			InputModalities:  []string{"text", "image"},
			OutputModalities: []string{"text"},
			ImageInput:       true,
			Images:           true,
		},
	},
	{
		ID:          "deepseek/deepseek-v4-pro",
		DisplayName: "DeepSeek: DeepSeek V4 Pro",
		Capabilities: providers.ModelCapabilities{
			ContextWindow:    1048576,
			MaxOutput:        384000,
			Tools:            true,
			Reasoning:        true,
			ReasoningOptions: []string{"low", "high", "max"},
			ReasoningDefault: "high",
			ReasoningType:    "effort",
			EndpointTypes:    []string{"openai", "openai-response"},
			InputModalities:  []string{"text"},
			OutputModalities: []string{"text"},
		},
	},
	{
		ID:          "orcarouter/auto",
		DisplayName: "OrcaRouter: Auto",
		Capabilities: providers.ModelCapabilities{
			ContextWindow:    1000000,
			MaxOutput:        128000,
			Tools:            true,
			EndpointTypes:    []string{"openai", "anthropic", "gemini", "openai-response"},
			InputModalities:  []string{"text"},
			OutputModalities: []string{"text"},
		},
	},
}
