package codex

import (
	_ "embed"
	"encoding/json"
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

//go:embed models.json
var catalogJSON []byte

// catalogEntry mirrors the slim fields extracted from the official codex client
// model list (CLIProxyAPI internal/registry/models/codex_client_models.json).
type catalogEntry struct {
	Slug             string   `json:"slug"`
	DisplayName      string   `json:"display_name"`
	ContextWindow    int      `json:"context_window"`
	MaxContextWindow int      `json:"max_context_window"`
	DefaultReasoning string   `json:"default_reasoning_level"`
	ReasoningLevels  []string `json:"supported_reasoning_levels"`
	InputModalities  []string `json:"input_modalities"`
	PreferWebsockets bool     `json:"prefer_websockets"`
	UseResponsesLite bool     `json:"use_responses_lite"`
	SupportedInAPI   bool     `json:"supported_in_api"`
}

var catalog []catalogEntry

func init() {
	var doc struct {
		Models []catalogEntry `json:"models"`
	}
	if err := json.Unmarshal(catalogJSON, &doc); err == nil {
		catalog = doc.Models
	}
}

// lookupModel resolves a request model to its catalog entry. The public model
// id is the upstream slug verbatim.
func lookupModel(model string) (catalogEntry, bool) {
	model = strings.TrimSpace(model)
	for _, entry := range catalog {
		if entry.Slug == model {
			return entry, true
		}
	}
	return catalogEntry{}, false
}

func entryModelInfo(entry catalogEntry) providers.ModelInfo {
	images := false
	for _, modality := range entry.InputModalities {
		if modality == "image" {
			images = true
		}
	}
	options := providers.UniqueReasoningOptions(entry.ReasoningLevels)
	caps := providers.ModelCapabilities{
		ContextWindow:    entry.ContextWindow,
		ContextWindowMax: entry.MaxContextWindow,
		Tools:            true,
		Images:           images,
		Reasoning:        len(options) > 0,
		ReasoningOptions: options,
		ReasoningDefault: providers.NormalizeReasoningLevel(entry.DefaultReasoning),
		ReasoningType:    "effort",
	}
	return providers.ModelInfo{
		NativeModel:  entry.Slug,
		PublicModel:  entry.Slug,
		DisplayName:  entry.DisplayName,
		Capabilities: caps,
	}
}

// catalogModelInfos returns every model the catalog declares API-servable.
func catalogModelInfos() []providers.ModelInfo {
	out := make([]providers.ModelInfo, 0, len(catalog))
	for _, entry := range catalog {
		if !entry.SupportedInAPI {
			continue
		}
		out = append(out, entryModelInfo(entry))
	}
	return out
}

// capsFor returns the catalog capabilities for a model, or zero caps when the
// model is unknown.
func capsFor(model string) providers.ModelCapabilities {
	if entry, ok := lookupModel(model); ok {
		return entryModelInfo(entry).Capabilities
	}
	return providers.ModelCapabilities{}
}

// usesResponsesLite reports whether the model must be sent under the
// responses-lite variant (parallel_tool_calls forced off upstream).
func usesResponsesLite(model string) bool {
	entry, ok := lookupModel(model)
	return ok && entry.UseResponsesLite
}
