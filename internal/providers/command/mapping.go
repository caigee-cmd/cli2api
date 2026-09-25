package command

import (
	"strings"

	"github.com/caigee-cmd/cli2api/internal/providers"
)

// reasoningModels declares the reasoning_effort levels a model accepts. Command
// Code's catalog does not expose reasoning metadata, so this table only lists
// models whose reasoning_effort support has been verified against the gateway
// (DeepSeek V4 Pro/Flash, which accept "high"/"max"; absent means the gateway
// picks its own behavior). It is deliberately small and explicit rather than a
// guessed per-model default, and unlisted models advertise no reasoning so the
// client is never offered a level the model does not declare.
//
// The adapter only forwards reasoning_effort when the request carries an
// explicit level; it never injects one on its own.
var reasoningModels = map[string]struct {
	Options []string
	Default string
}{
	"deepseek/deepseek-v4-pro":   {Options: []string{"high", "max"}, Default: "high"},
	"deepseek/deepseek-v4-flash": {Options: []string{"high", "max"}, Default: "high"},
}

// applyReasoningCaps decorates a catalog entry's capabilities with the verified
// reasoning metadata, if any.
func applyReasoningCaps(id string, caps providers.ModelCapabilities) providers.ModelCapabilities {
	entry, ok := reasoningModels[normalizeModelKey(id)]
	if !ok || len(entry.Options) == 0 {
		return caps
	}
	caps.Reasoning = true
	caps.ReasoningOptions = append([]string(nil), entry.Options...)
	caps.ReasoningDefault = entry.Default
	caps.ReasoningType = "effort"
	return caps
}

func normalizeModelKey(id string) string {
	return strings.ToLower(strings.TrimSpace(id))
}
